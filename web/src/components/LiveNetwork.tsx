"use client";
import { Bot, Box, CircuitBoard, Cpu, Database, HardDrive, Laptop, type LucideIcon, Plug, Server, Smartphone, Cctv, User } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import {
  KINDS,
  type Kind,
  type NetDevice,
  type Activity,
  type NetEvent,
  type Network,
  allDevices,
  diff,
  fetchActivity,
  fetchNetwork,
  kindCounts,
  kindLabel,
} from "@/lib/network";

// meshy-sync's /v1/map reads ENS on every call and takes several seconds, so poll
// back-to-back with a short gap rather than on a fixed interval that could cut it off.
const GAP_MS = 2_000;
// How long a blocked attempt keeps flashing on the map.
const BLOCK_FLASH_MS = 120_000;

const ICON: Record<Kind, LucideIcon> = { server: Server, gpu: Cpu, pc: Laptop, phone: Smartphone, camera: Cctv, pi: CircuitBoard, nas: HardDrive, device: Box };
const serviceIcon = (s: { kind: string; proto: string; label: string }) => (s.kind === "camera" ? Cctv : /postgres|sql|db/.test(`${s.proto} ${s.label}`) ? Database : Plug);

// Columns of the map, in viewBox units.
const W = 1200;
const X = { agent: 96, check: 330, prov: 590, net: 760, dev: 990, svc: 1120 };
const ROW = 100;
const TOP = 74;

type P = [number, number];
type Curve = [P, P, P, P];

const bez = (c: Curve, t: number): P => {
  const u = 1 - t;
  return [0, 1].map((i) => u * u * u * c[0][i] + 3 * u * u * t * c[1][i] + 3 * u * t * t * c[2][i] + t * t * t * c[3][i]) as P;
};
const d = (c: Curve) => `M${c[0]} C${c[1]} ${c[2]} ${c[3]}`;
const flow = (a: P, b: P, k = 0.45): Curve => [a, [a[0] + (b[0] - a[0]) * k, a[1]], [b[0] - (b[0] - a[0]) * k, b[1]], b];

/** Split a cubic at the point where it crosses x (x is monotonic along these curves). */
function splitAtX(c: Curve, x: number): { head: Curve; at: P } {
  let lo = 0;
  let hi = 1;
  for (let i = 0; i < 30; i++) {
    const m = (lo + hi) / 2;
    if (bez(c, m)[0] < x) lo = m;
    else hi = m;
  }
  const t = (lo + hi) / 2;
  const lerp = (a: P, b: P): P => [a[0] + (b[0] - a[0]) * t, a[1] + (b[1] - a[1]) * t];
  const ab = lerp(c[0], c[1]);
  const bc = lerp(c[1], c[2]);
  const abc = lerp(ab, bc);
  const at = bez(c, t);
  return { head: [c[0], ab, abc, at], at };
}

type AgentRoute = { agent: string; net: string; curve: Curve; state: "live" | "revoked" | "none"; target?: string; head: Curve; at: P };

function layout(n: Network) {
  const rows: { net: string; device?: NetDevice }[] = [];
  for (const net of n.networks) {
    if (!net.devices.length) rows.push({ net: net.name });
    for (const dv of net.devices) rows.push({ net: net.name, device: dv });
  }
  const nRows = Math.max(rows.length, 3);
  const H = TOP + nRows * ROW + 24;
  const off = TOP + ((nRows - rows.length) * ROW) / 2 + ROW / 2;
  const devY = new Map<string, number>();
  const netYs = new Map<string, number[]>();
  rows.forEach((r, i) => {
    const y = off + i * ROW;
    if (r.device) devY.set(r.device.name, y);
    netYs.set(r.net, [...(netYs.get(r.net) ?? []), y]);
  });
  const netY = new Map([...netYs].map(([k, ys]) => [k, ys.reduce((a, b) => a + b, 0) / ys.length]));

  const span = Math.min(nRows * ROW - ROW, Math.max(0, n.agents.length - 1) * ROW * 1.1);
  const mid = TOP + (nRows * ROW) / 2;
  const agentY = new Map(n.agents.map((a, i) => [a.name, n.agents.length === 1 ? mid : mid - span / 2 + (i * span) / (n.agents.length - 1)]));

  const deviceNet = new Map(allDevices(n).map((dv) => [dv.name, dv.network]));
  const routes: AgentRoute[] = [];
  for (const a of n.agents) {
    const ya = agentY.get(a.name)!;
    const mine = n.grants.filter((g) => g.agent === a.name);
    const live = mine.filter((g) => g.status === "live");
    const picks = live.length ? live : mine.slice(0, 1);
    const targets = picks.length ? picks : [undefined];
    for (const g of targets) {
      const net = (g && deviceNet.get(g.device)) ?? n.networks[0]?.name ?? "";
      const yn = netY.get(net) ?? mid;
      const curve = flow([X.agent + 26, ya], [X.prov, yn], 0.5);
      const { head, at } = splitAtX(curve, X.check);
      routes.push({ agent: a.name, net, curve, head, at, target: g?.target, state: g?.status === "live" ? "live" : g ? "revoked" : "none" });
    }
  }
  return { H, devY, netY, agentY, routes };
}

/** A dash that runs along a path. pathLength=100 keeps the speed independent of length. */
function Comet({ path, dur, delay, cls, width = 2 }: { path: string; dur: number; delay: number; cls: string; width?: number }) {
  return (
    <path
      d={path}
      pathLength={100}
      className={`comet ${cls}`}
      strokeWidth={width}
      style={{ animationDuration: `${dur}s`, animationDelay: `-${delay}s` }}
    />
  );
}

function NodeIcon({ Icon, x, y, size = 22 }: { Icon: LucideIcon; x: number; y: number; size?: number }) {
  return <Icon x={x - size / 2} y={y - size / 2} width={size} height={size} strokeWidth={1.6} />;
}

const plural = (n: number, one: string, many = `${one}s`) => `${n} ${n === 1 ? one : many}`;

const ago = (ms: number) => {
  const s = Math.max(0, Math.round(ms / 1000));
  return s < 60 ? `${s}s ago` : `${Math.round(s / 60)}m ago`;
};

export function LiveNetwork({ org }: { org: string }) {
  const [net, setNet] = useState<Network>();
  const [offline, setOffline] = useState(false);
  const [events, setEvents] = useState<NetEvent[]>([]);
  const [fresh, setFresh] = useState<Set<string>>(new Set());
  const [hover, setHover] = useState<string>();
  // Clock starts in the effect: Date.now() during render breaks the prerender.
  const [now, setNow] = useState(0);
  const [synced, setSynced] = useState(0);
  const [activity, setActivity] = useState<Activity>();
  const [arkivDown, setArkivDown] = useState(false);
  const last = useRef<Network | undefined>(undefined);
  const lastAct = useRef<Activity | undefined>(undefined);

  useEffect(() => {
    let alive = true;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const ctrl = new AbortController();
    const push = (ev: NetEvent[]) => ev.length && setEvents((e) => [...ev, ...e].slice(0, 5));

    const readActivity = async (n: Network) => {
      try {
        const act = await fetchActivity(n);
        if (!alive) return;
        const prev = lastAct.current;
        if (prev) {
          const at = Date.now();
          const key = (x: { agent: string; resource: string }) => `${x.agent}@${x.resource}`;
          const was = new Set(prev.sessions.map(key));
          const is = new Set(act.sessions.map(key));
          const seen = new Set(prev.blocked.map((x) => `${key(x)}#${x.at}`));
          const label = (res: string) => res.replace(`.${n.org}`, "");
          push([
            ...act.sessions.filter((x) => !was.has(key(x))).map((x) => ({ id: `s+${key(x)}${at}`, at, text: `${x.agent} opened a session on ${label(x.resource)}`, tone: "ok" as const })),
            ...prev.sessions.filter((x) => !is.has(key(x))).map((x) => ({ id: `s-${key(x)}${at}`, at, text: `${x.agent}'s session on ${label(x.resource)} ended`, tone: "info" as const })),
            ...act.blocked.filter((x) => !seen.has(`${key(x)}#${x.at}`)).map((x) => ({ id: `b${key(x)}${x.at}`, at, text: `${x.agent} blocked on ${label(x.resource)}${x.reason ? `: ${x.reason}` : ""}`, tone: "bad" as const })),
          ]);
        }
        lastAct.current = act;
        setActivity(act);
        setArkivDown(false);
      } catch {
        if (alive) setArkivDown(true);
      }
    };

    const tick = async () => {
      if (!document.hidden) {
        try {
          const n = await fetchNetwork(org, ctrl.signal);
          if (!alive) return;
          const prev = last.current;
          // meshy-sync occasionally answers with an empty tree mid-rebuild; keep the last good one.
          if (!(prev && allDevices(prev).length && !n.networks.length)) {
            const t = Date.now();
            setSynced(t);
            setNow(t);
            if (prev) {
              push(diff(prev, n));
              const old = new Set(allDevices(prev).map((x) => x.name));
              const added = allDevices(n).filter((x) => !old.has(x.name)).map((x) => x.name);
              if (added.length) {
                setFresh(new Set(added));
                setTimeout(() => alive && setFresh(new Set()), 6000);
              }
            }
            last.current = n;
            setNet(n);
            setOffline(false);
            readActivity(n);
          }
        } catch (e) {
          if (!alive || (e as Error).name === "AbortError") return;
          setOffline(true);
        }
      }
      if (alive) timer = setTimeout(tick, GAP_MS);
    };
    tick();
    const clock = setInterval(() => setNow(Date.now()), 1000);
    return () => {
      alive = false;
      ctrl.abort();
      clearTimeout(timer);
      clearInterval(clock);
    };
  }, [org]);

  const L = useMemo(() => (net ? layout(net) : undefined), [net]);
  const counts = useMemo(() => (net ? kindCounts(net) : undefined), [net]);

  if (!net || !L || !counts) return <MapSkeleton offline={offline} />;

  const devices = allDevices(net);
  const liveGrants = net.grants.filter((g) => g.status === "live");
  // What the hovered node touches: itself, its network, and any agent routed to it.
  const hoverDev = devices.find((x) => x.name === hover);
  const lit = (keys: string[]) => !hover || keys.includes(hover) || (hoverDev && keys.includes(hoverDev.network));
  const isTarget = (dv: NetDevice, t?: string) => !!t && (t === dv.name || dv.services.some((s) => s.name === t));
  // A session counts only while its grant is live: a start with no end after a revoke is stale.
  const agentLabel = (full: string) => full.split(".")[0];
  const inSession = (agent: string, t?: string) =>
    !!t && !!activity?.sessions.some((x) => x.agent === agentLabel(agent) && (x.resource === t || t.endsWith(`.${x.resource}`)));
  const blockedRecently = (agent: string) => !!activity?.blocked.some((x) => x.agent === agentLabel(agent) && now - x.at < BLOCK_FLASH_MS);
  const deviceBusy = (dv: NetDevice) => liveGrants.some((g) => isTarget(dv, g.target) && inSession(g.agent, g.target));

  return (
    <div className="relative">
      <div className="flex flex-wrap items-center gap-x-4 gap-y-2 px-1 text-[13px] text-[#93a0c8]">
        <span className="inline-flex items-center gap-2 font-medium text-[#e8edff]">
          <span className={`h-2 w-2 rounded-full ${offline ? "bg-[#facc15]" : "live-dot bg-[#4ade80]"}`} />
          {offline ? "Reconnecting" : "Live"}
        </span>
        <span className="mono text-[#e8edff]">{net.org}</span>
        {net.block > 0 && <span className="mono tnum">Sepolia #{net.block.toLocaleString("en-US")}</span>}
        {synced > 0 && <span className="tnum">synced {ago(now - synced)}</span>}
        {arkivDown && <span className="text-[#5b6787]">Arkiv sessions unavailable</span>}
        <span className="ml-auto mono">
          {devices.length} device{devices.length === 1 ? "" : "s"} · {plural(net.agents.filter((x) => !x.person).length, "agent")}{net.agents.some((x) => x.person) && ` · ${plural(net.agents.filter((x) => x.person).length, "person", "people")}`} · {liveGrants.length} live grant{liveGrants.length === 1 ? "" : "s"}
          {activity && ` · ${activity.sessions.length} session${activity.sessions.length === 1 ? "" : "s"}`}
        </span>
      </div>

      {/* Map: md and up */}
      <div className="relative mt-4 hidden md:block">
        <svg viewBox={`0 0 ${W} ${L.H}`} className="block h-auto w-full" role="img" aria-label={`Live map of ${net.org}: ${devices.map((x) => `${x.label} (${kindLabel(x.kind)})`).join(", ")}`}>
          <defs>
            <radialGradient id="ln-glow">
              <stop offset="0%" stopColor="#38bdf8" stopOpacity="0.8" />
              <stop offset="100%" stopColor="#38bdf8" stopOpacity="0" />
            </radialGradient>
            <radialGradient id="ln-glow-bad">
              <stop offset="0%" stopColor="#fb7185" stopOpacity="0.9" />
              <stop offset="100%" stopColor="#fb7185" stopOpacity="0" />
            </radialGradient>
            <radialGradient id="ln-glow-ok">
              <stop offset="0%" stopColor="#bef264" stopOpacity="0.75" />
              <stop offset="100%" stopColor="#bef264" stopOpacity="0" />
            </radialGradient>
            <filter id="ln-blur" x="-50%" y="-50%" width="200%" height="200%">
              <feGaussianBlur stdDeviation="2.2" />
            </filter>
          </defs>

          {/* column headings */}
          {(
            [
              [X.agent, "Agents & people"],
              [X.check, "ENS grant check"],
              [X.prov, "Meshy provider"],
              [X.net, "Networks"],
              [X.dev, "Devices"],
              [X.svc, "Services"],
            ] as const
          ).map(([x, t]) => (
            <text key={t} x={x} y={28} textAnchor="middle" className="ln-head">
              {t}
            </text>
          ))}

          {/* the two vertical lines every packet must cross */}
          <line x1={X.check} x2={X.check} y1={48} y2={L.H - 12} className="ln-check" />
          <line x1={X.prov} x2={X.prov} y1={48} y2={L.H - 12} className="ln-prov" />
          <text x={X.prov} y={L.H - 2} textAnchor="middle" className="ln-sub">WireGuard mesh</text>
          <text x={X.check} y={L.H - 2} textAnchor="middle" className="ln-sub">Sepolia ENSv2</text>

          {/* provider → network → device → service. Packets only where an agent really has access. */}
          {net.networks.map((nw, ni) => {
            const yn = L.netY.get(nw.name)!;
            const seg = `M${X.prov},${yn} L${X.net - 24},${yn}`;
            return (
              <g key={nw.name} opacity={lit([nw.name]) ? 1 : 0.2} className="ln-fade">
                <path d={seg} className={nw.devices.some((dv) => liveGrants.some((g) => isTarget(dv, g.target))) ? "ln-wire-hot" : "ln-wire"} />
                {nw.devices.some((dv) => liveGrants.some((g) => isTarget(dv, g.target))) && <Comet path={seg} dur={0.5} delay={ni * 0.2} cls="ln-comet-hot" width={2.6} />}
                {nw.devices.map((dv, di) => {
                  const yd = L.devY.get(dv.name)!;
                  const c = d(flow([X.net + 24, yn], [X.dev - 30, yd]));
                  const hot = liveGrants.some((g) => isTarget(dv, g.target));
                  return (
                    <g key={dv.name} opacity={lit([dv.name, nw.name]) ? 1 : 0.2} className="ln-fade">
                      <path d={c} className={hot ? "ln-wire-hot" : "ln-wire"} />
                      {hot && <Comet path={c} dur={deviceBusy(dv) ? 0.6 : 0.9} delay={di * 0.3} cls="ln-comet-hot" width={2.6} />}
                      {hot && <Comet path={c} dur={deviceBusy(dv) ? 0.6 : 0.9} delay={di * 0.3 + 0.45} cls="ln-comet-hot" width={2.6} />}
                      {dv.services.map((s, si) => {
                        const sx = X.svc - 16;
                        const sy = yd + (si - (dv.services.length - 1) / 2) * 34;
                        const sp = d(flow([X.dev + 30, yd], [sx, sy], 0.5));
                        const shot = liveGrants.some((g) => g.target === s.name);
                        return (
                          <g key={s.name}>
                            <path d={sp} className={shot ? "ln-wire-hot" : "ln-wire"} />
                            {shot && <Comet path={sp} dur={0.6} delay={0.2} cls="ln-comet-hot" width={2.4} />}
                          </g>
                        );
                      })}
                    </g>
                  );
                })}
              </g>
            );
          })}

          {/* agents → grant check → provider */}
          {L.routes.map((r, i) => {
            const keys = [r.agent, r.net, ...devices.filter((dv) => isTarget(dv, r.target)).map((dv) => dv.name)];
            const on = lit(keys);
            return (
              <g key={`${r.agent}-${i}`} opacity={on ? 1 : 0.15} className="ln-fade">
                {r.state === "live" ? (
                  <>
                    <path d={d(r.curve)} className="ln-wire-hot" />
                    {[0, 1, 2].map((k) => (
                      <Comet key={k} path={d(r.curve)} dur={inSession(r.agent, r.target) ? 0.8 : 1.2} delay={i * 0.3 + (k * (inSession(r.agent, r.target) ? 0.8 : 1.2)) / 3} cls="ln-comet-hot" width={2.6} />
                    ))}
                    <circle cx={r.at[0]} cy={r.at[1]} r={26} fill="url(#ln-glow-ok)" />
                    <circle cx={r.at[0]} cy={r.at[1]} r={3} className="fill-[#bef264]" />
                    <text x={r.at[0] + 12} y={r.at[1] - 10} className="ln-tag">
                      {inSession(r.agent, r.target) ? "session live" : "granted"}
                    </text>
                  </>
                ) : (
                  <>
                    <path d={d(r.head)} className="ln-wire" />
                    <path d={d(r.curve)} className="ln-wire-ghost" />
                    {(r.state === "revoked" || blockedRecently(r.agent)) && (
                      <>
                        <Comet path={d(r.head)} dur={1.4} delay={i * 0.3} cls="ln-comet-bad" width={2.4} />
                        <circle
                          cx={r.at[0]}
                          cy={r.at[1]}
                          r={24}
                          fill="url(#ln-glow-bad)"
                          className="ln-burst"
                          style={{ animationDuration: "1.4s", animationDelay: `-${i * 0.3}s` }}
                        />
                      </>
                    )}
                    <g transform={`translate(${r.at[0]} ${r.at[1]})`} className={r.state === "revoked" ? "stroke-[#fb7185]" : "stroke-[#93a0c8]"} strokeWidth={2} strokeLinecap="round">
                      <path d="M-4,-4 L4,4 M4,-4 L-4,4" />
                    </g>
                    <text x={r.at[0] + 12} y={r.at[1] - 10} className={r.state === "revoked" ? "ln-tag-bad" : "ln-tag"}>
                      {r.state === "revoked" ? "revoked" : "no grant"}
                    </text>
                  </>
                )}
              </g>
            );
          })}

          {/* agent nodes */}
          {net.agents.map((a) => {
            const y = L.agentY.get(a.name)!;
            return (
              <g key={a.name} className="cursor-default ln-fade" opacity={lit([a.name]) ? 1 : 0.3} onMouseEnter={() => setHover(a.name)} onMouseLeave={() => setHover(undefined)}>
                <circle cx={X.agent} cy={y} r={30} fill="url(#ln-glow)" />
                <rect x={X.agent - 22} y={y - 22} width={44} height={44} rx={12} className="ln-node" />
                <g className="text-[#a5d8ff]">
                  <NodeIcon Icon={a.person ? User : Bot} x={X.agent} y={y} />
                </g>
                <text x={X.agent} y={y + 40} textAnchor="middle" className="ln-label">{a.label}</text>
                <text x={X.agent} y={y + 56} textAnchor="middle" className="ln-sub">{a.person ? "member" : a.verified ? "human-backed" : "agent"}</text>
              </g>
            );
          })}
          {net.agents.length === 0 && (
            <text x={X.agent} y={TOP + 40} textAnchor="middle" className="ln-sub">No agents or people yet</text>
          )}

          {/* network hubs */}
          {net.networks.map((nw) => {
            const y = L.netY.get(nw.name)!;
            const hex = Array.from({ length: 6 }, (_, k) => {
              const t = (Math.PI / 3) * k - Math.PI / 2;
              return `${X.net + 22 * Math.cos(t)},${y + 22 * Math.sin(t)}`;
            }).join(" ");
            return (
              <g key={nw.name} className="ln-fade" opacity={lit([nw.name]) ? 1 : 0.3} onMouseEnter={() => setHover(nw.name)} onMouseLeave={() => setHover(undefined)}>
                <circle cx={X.net} cy={y} r={34} fill="url(#ln-glow)" />
                <polygon points={hex} className="ln-hex" />
                <circle cx={X.net} cy={y} r={4} className="fill-[#38bdf8]" />
                <text x={X.net} y={y + 40} textAnchor="middle" className="ln-label">{nw.label}</text>
              </g>
            );
          })}

          {/* devices */}
          {devices.map((dv) => {
            const y = L.devY.get(dv.name)!;
            const hot = liveGrants.some((g) => isTarget(dv, g.target));
            return (
              <g
                key={dv.name}
                className={`ln-fade ${fresh.has(dv.name) ? "ln-join" : ""}`}
                opacity={lit([dv.name, dv.network]) ? 1 : 0.3}
                onMouseEnter={() => setHover(dv.name)}
                onMouseLeave={() => setHover(undefined)}
              >
                {fresh.has(dv.name) && <circle cx={X.dev} cy={y} r={30} className="ln-ring" />}
                <circle cx={X.dev} cy={y} r={36} fill={hot ? "url(#ln-glow-ok)" : "url(#ln-glow)"} />
                <rect x={X.dev - 26} y={y - 26} width={52} height={52} rx={14} className={hot ? "ln-node-hot" : "ln-node"} />
                <g className="text-[#e8edff]">
                  <NodeIcon Icon={ICON[dv.kind]} x={X.dev} y={y} size={26} />
                </g>
                <circle cx={X.dev + 24} cy={y - 24} r={5} className="fill-[#4ade80] stroke-[#050b24]" strokeWidth={2} />
                <text x={X.dev} y={y + 44} textAnchor="middle" className="ln-label">{dv.label}</text>
                <text x={X.dev} y={y + 59} textAnchor="middle" className="ln-kind">{kindLabel(dv.kind)}</text>
                {dv.services.map((s, si) => {
                  const sy = y + (si - (dv.services.length - 1) / 2) * 34;
                  const Icon = serviceIcon(s);
                  return (
                    <g key={s.name}>
                      <rect x={X.svc - 15} y={sy - 15} width={30} height={30} rx={9} className={liveGrants.some((g) => g.target === s.name) ? "ln-node-hot" : "ln-node"} />
                      <g className="text-[#a5d8ff]">
                        <NodeIcon Icon={Icon} x={X.svc} y={sy} size={16} />
                      </g>
                      <text x={X.svc + 22} y={sy + 4} className="ln-label">{s.label}</text>
                    </g>
                  );
                })}
              </g>
            );
          })}
        </svg>

        {hoverDev && (
          <div
            className="pointer-events-none absolute z-10 w-64 -translate-x-full -translate-y-1/2 rounded-xl border border-[#26346a] bg-[#0b1435]/95 p-3 text-[13px] shadow-xl backdrop-blur"
            style={{ left: `${((X.dev - 46) / W) * 100}%`, top: `${(L.devY.get(hoverDev.name)! / L.H) * 100}%` }}
          >
            <p className="mono break-all font-medium text-[#e8edff]">{hoverDev.name}</p>
            <p className="mt-1 text-[#93a0c8]">
              {kindLabel(hoverDev.kind)}
              {hoverDev.rawKind && hoverDev.rawKind !== hoverDev.kind ? ` · ${hoverDev.rawKind}` : ""} · on the mesh
            </p>
            {(hoverDev.meta.model || hoverDev.meta.location) && (
              <p className="mt-1 text-[#c4cbff]">{[hoverDev.meta.model, hoverDev.meta.location].filter(Boolean).join(" · ")}</p>
            )}
            {hoverDev.services.length > 0 && <p className="mt-1 text-[#93a0c8]">Services: {hoverDev.services.map((s) => `${s.label} (${s.proto})`).join(", ")}</p>}
            <p className="mt-1 text-[#93a0c8]">
              {liveGrants.filter((g) => isTarget(hoverDev, g.target)).length || "No"} live grant{liveGrants.filter((g) => isTarget(hoverDev, g.target)).length === 1 ? "" : "s"}
            </p>
          </div>
        )}
      </div>

      {/* Phones: a list instead of the map */}
      <ul className="mt-4 grid grid-cols-1 gap-2 sm:grid-cols-2 md:hidden">
        {devices.map((dv) => {
          const Icon = ICON[dv.kind];
          return (
            <li key={dv.name} className={`flex items-center gap-3 rounded-xl border border-[#1a2652] bg-[#0b1435] p-3 ${fresh.has(dv.name) ? "fade-up" : ""}`}>
              <span className="relative grid h-10 w-10 shrink-0 place-items-center rounded-lg border border-[#26346a] text-[#e8edff]">
                <Icon size={20} strokeWidth={1.6} />
                <span className="absolute -right-1 -top-1 h-2.5 w-2.5 rounded-full border-2 border-[#0b1435] bg-[#4ade80]" />
              </span>
              <span className="min-w-0">
                <span className="mono block truncate text-[14px] text-[#e8edff]">{dv.label}</span>
                <span className="block text-[12px] text-[#93a0c8]">
                  {kindLabel(dv.kind)}
                  {dv.services.length > 0 && ` · ${dv.services.map((s) => s.label).join(", ")}`}
                </span>
              </span>
            </li>
          );
        })}
      </ul>

      {/* What kinds of device are in the network */}
      <ul className="mt-5 flex flex-wrap gap-2" aria-label="Device kinds on the network">
        {KINDS.map(({ kind, label }) => {
          const Icon = ICON[kind];
          const c = counts[kind];
          return (
            <li
              key={kind}
              className={`inline-flex items-center gap-2 rounded-full border px-3 py-1.5 text-[13px] ${c ? "border-[#38bdf8]/50 bg-[#38bdf8]/10 text-[#e8edff]" : "border-[#1a2652] text-[#5b6787]"}`}
            >
              <Icon size={15} strokeWidth={1.8} />
              {label}
              <span className={`mono tnum ${c ? "text-[#38bdf8]" : ""}`}>{c}</span>
            </li>
          );
        })}
      </ul>

      <div className="mt-4 min-h-[24px] text-[13px]" aria-live="polite">
        {events.length ? (
          <ul className="space-y-1">
            {events.map((e) => (
              <li key={e.id} className="fade-up flex items-center gap-2">
                <span className={`h-1.5 w-1.5 rounded-full ${e.tone === "ok" ? "bg-[#4ade80]" : e.tone === "bad" ? "bg-[#fb7185]" : "bg-[#38bdf8]"}`} />
                <span className="text-[#e8edff]">{e.text}</span>
                <span className="text-[#5b6787]">{ago(now - e.at)}</span>
              </li>
            ))}
          </ul>
        ) : (
          <p className="text-[#5b6787]">
            {`Watching ${net.org}. Add a device, grant or revoke access, and it shows up here as soon as it's on-chain.`}
          </p>
        )}
      </div>
    </div>
  );
}

function MapSkeleton({ offline }: { offline: boolean }) {
  return (
    <div className="flex h-[420px] items-center justify-center rounded-2xl border border-dashed border-[#1a2652] px-6 text-center text-[14px] text-[#93a0c8]">
      <span className={`mr-3 h-2 w-2 shrink-0 rounded-full ${offline ? "bg-[#facc15]" : "live-dot bg-[#38bdf8]"}`} />
      {offline ? "Can't reach the Meshy provider. Retrying…" : "Reading the network from ENS…"}
    </div>
  );
}

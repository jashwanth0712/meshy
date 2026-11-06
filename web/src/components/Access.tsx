"use client";
import { KeyRound, Plus, Trash2, X, Zap } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { type Address, zeroAddress } from "viem";
import { type Step, Steps } from "@/components/Steps";
import { Badge, Button, Card, Countdown, Empty, Field, Name, Ring, cx, inputCls, useNow } from "@/components/ui";
import { revokesFor } from "@/lib/arkiv";
import { type PathRule, sealPolicy } from "@/lib/envelope";
import { type GrantNode, type ResourceNode, type ServiceNode, getState, labelId, recordCalls, registryAbi, resolverAbi, send } from "@/lib/ens";
import { useMeshy } from "@/lib/store";

const DURATIONS = [
  { label: "15 min", s: 900 },
  { label: "1 hour", s: 3600 },
  { label: "4 hours", s: 14400 },
];

export function GrantDrawer({ onClose, preset }: { onClose: () => void; preset?: { resource?: string; agent?: string } }) {
  const { tree, wallet, client, account, refresh, toast } = useMeshy();
  const resources = useMemo(() => tree?.networks.flatMap((n) => n.resources) ?? [], [tree]);
  const services = useMemo(() => resources.flatMap((r) => r.services), [resources]);
  const [resName, setResName] = useState(preset?.resource ?? resources[0]?.name ?? "");
  const [agent, setAgent] = useState(preset?.agent ?? tree?.agents[0]?.label ?? "");
  const res = resources.find((r) => r.name === resName);
  const svc = services.find((x) => x.name === resName);
  const [paths, setPaths] = useState<PathRule[]>(() => (res?.roots[0] ? [{ p: `${res.roots[0]}/reports`, m: "ro" }] : []));
  const [dur, setDur] = useState(3600);
  const [steps, setSteps] = useState<Step[]>();
  const busy = steps?.some((s) => s.state === "doing");
  const grantName = agent && (svc || res) ? `${agent}.${(svc ?? res)!.name}` : "";
  const exp = new Date(Date.now() + dur * 1000);
  const ag = tree?.agents.find((a) => a.label === agent);
  const isOwner = !!account; // roles are enforced on-chain (owner or delegated admin)

  const valid = svc ? !!ag : !!res && !!ag && paths.length > 0 && paths.every((p) => p.p.startsWith("/") && p.p !== "/") && !!res.encKey && !!tree?.orgKey;

  async function submit() {
    if (!wallet || !tree || !ag) return;
    const up = (i: number, p: Partial<Step>) => setSteps((s) => s!.map((x, j) => (j === i ? { ...x, ...p } : x)));
    if (svc) {
      setSteps([{ label: `Register ENS subname ${grantName} (port-level access to ${svc.proto}://${svc.target})`, state: "doing", detail: `owner ${ag.addr} · expires ${exp.toLocaleTimeString()}` }]);
      try {
        const r = await send(wallet, client, { address: svc.registry, abi: registryAbi, functionName: "register", args: [agent, ag.addr, zeroAddress, tree.resolver, 0n, BigInt(Math.floor(Date.now() / 1000) + dur)] }, (h) => up(0, { tx: h }));
        up(0, { state: "done", detail: `block ${r.blockNumber}` });
        toast(`Granted ${grantName}`, "ok");
        refresh();
      } catch (e) {
        const msg = e instanceof Error ? e.message.split("\n")[0] : String(e);
        setSteps((s) => s?.map((x) => (x.state === "doing" ? { ...x, state: "fail", detail: msg } : x)));
        toast(msg, "err");
      }
      return;
    }
    if (!res) return;
    setSteps([
      { label: `Register ENS subname ${grantName}`, state: "doing", detail: `owner ${ag.addr} · expires ${exp.toLocaleTimeString()}` },
      { label: "Read the grant's token id (binds the policy)", state: "todo" },
      { label: "Seal policy to the resource daemon + org key (in your browser)", state: "todo" },
      { label: "Write meshy.policy on the org resolver", state: "todo" },
    ]);
    try {
      const r1 = await send(wallet, client, {
        address: res.registry,
        abi: registryAbi,
        functionName: "register",
        args: [agent, ag.addr, zeroAddress, tree.resolver, 0n, BigInt(Math.floor(Date.now() / 1000) + dur)],
      }, (h) => up(0, { tx: h }));
      up(0, { state: "done", detail: `block ${r1.blockNumber}` });
      up(1, { state: "doing" });
      const st = await getState(client, res.registry, agent);
      up(1, { state: "done", detail: `tokenId ${st.tokenId.toString().slice(0, 18)}…` });
      up(2, { state: "doing" });
      const sealed = sealPolicy({ v: 1, grant: grantName, grantTokenId: st.tokenId.toString(), paths }, [res.encKey, tree.orgKey]);
      up(2, { state: "done", detail: `${sealed.length} bytes, readable only by ${res.label} and the org` });
      up(3, { state: "doing" });
      const r3 = await send(wallet, client, {
        address: tree.resolver,
        abi: resolverAbi,
        functionName: "multicall",
        args: [recordCalls(grantName, { "meshy.policy": sealed })],
      }, (h) => up(3, { tx: h }));
      up(3, { state: "done", detail: `block ${r3.blockNumber}` });
      toast(`Granted ${grantName}`, "ok");
      refresh();
    } catch (e) {
      const msg = e instanceof Error ? e.message.split("\n")[0] : String(e);
      setSteps((s) => s?.map((x) => (x.state === "doing" ? { ...x, state: "fail", detail: msg } : x)));
      toast(msg, "err");
    }
  }

  return (
    <div className="fixed inset-0 z-40 flex justify-end bg-black/50" onClick={onClose}>
      <div className="h-full w-full max-w-lg overflow-y-auto border-l border-line bg-panel p-6" onClick={(e) => e.stopPropagation()}>
        <div className="mb-5 flex items-center justify-between">
          <h2 className="flex items-center gap-2 text-lg font-semibold">
            <KeyRound size={18} className="text-accent" /> Grant access
          </h2>
          <Button kind="ghost" onClick={onClose}>
            <X size={16} />
          </Button>
        </div>
        <div className="space-y-5">
          <Field label="Agent">
            <select className={inputCls} value={agent} onChange={(e) => setAgent(e.target.value)}>
              {tree?.agents.map((a) => (
                <option key={a.name} value={a.label}>
                  {a.name} {a.worldid ? "· human-backed" : "· NO World ID"}
                </option>
              ))}
            </select>
          </Field>
          <Field label="Target" hint="A device (folder-sandboxed shell) or one of its services (a single port).">
            <select
              className={inputCls}
              value={resName}
              onChange={(e) => {
                setResName(e.target.value);
                const r = resources.find((x) => x.name === e.target.value);
                setPaths(r?.roots[0] ? [{ p: r.roots[0], m: "ro" }] : []);
              }}
            >
              <optgroup label="Devices — folders">
                {resources.map((r) => (
                  <option key={r.name} value={r.name}>{r.name}</option>
                ))}
              </optgroup>
              {services.length > 0 && (
                <optgroup label="Services — one port">
                  {services.map((x) => (
                    <option key={x.name} value={x.name}>{x.name} ({x.proto} → {x.target})</option>
                  ))}
                </optgroup>
              )}
            </select>
          </Field>
          {svc ? (
            <div className="rounded-lg border border-accent-2/30 bg-accent-2/5 p-3 text-sm">
              Port-level grant: the agent can reach <span className="mono">{svc.proto}://{svc.target}</span> on <span className="mono">{svc.device}</span> through{" "}
              <span className="mono">meshy connect {svc.name}</span> — nothing else on the device.
            </div>
          ) : (
          <Field label="Folders" hint={res?.roots.length ? <>Advertised roots: <span className="mono">{res.roots.join(", ")}</span></> : "This resource advertises no roots."}>
            <div className="space-y-2">
              {paths.map((p, i) => (
                <div key={i} className="flex gap-2">
                  <input className={inputCls} value={p.p} onChange={(e) => setPaths(paths.map((x, j) => (j === i ? { ...x, p: e.target.value } : x)))} />
                  <div className="flex overflow-hidden rounded-lg border border-line text-xs">
                    {(["ro", "rw"] as const).map((m) => (
                      <button key={m} onClick={() => setPaths(paths.map((x, j) => (j === i ? { ...x, m } : x)))} className={cx("px-2.5", p.m === m ? (m === "rw" ? "bg-warn/20 text-warn" : "bg-accent/15 text-accent") : "text-muted")}>
                        {m}
                      </button>
                    ))}
                  </div>
                  <Button kind="ghost" onClick={() => setPaths(paths.filter((_, j) => j !== i))}>
                    <Trash2 size={14} />
                  </Button>
                </div>
              ))}
              <Button kind="ghost" onClick={() => setPaths([...paths, { p: res?.roots[0] ?? "/srv", m: "ro" }])}>
                <Plus size={14} /> Add folder
              </Button>
            </div>
          </Field>
          )}
          <Field label="Duration" hint="Agent grants are capped at 4 hours. Expiry is native ENSv2 — no one has to remember to revoke.">
            <div className="flex gap-2">
              {DURATIONS.map((d) => (
                <button key={d.s} onClick={() => setDur(d.s)} className={cx("rounded-lg border px-3 py-1.5 text-sm", dur === d.s ? "border-accent/60 bg-accent/15 text-accent" : "border-line text-muted")}>
                  {d.label}
                </button>
              ))}
            </div>
          </Field>
          <div className="rounded-lg border border-line bg-bg p-3 text-sm">
            <div className="text-xs uppercase tracking-wider text-muted">Preview</div>
            <div className="mt-1">
              Creates <Name name={grantName || "—"} /> owned by the agent, expiring <b>{exp.toLocaleTimeString()}</b>.{" "}
              {svc ? (
                <>The service&apos;s daemon forwards only to the target in its ENS record while this name is live.</>
              ) : (
                <>
                  Policy ({paths.map((p) => `${p.p}:${p.m}`).join(", ") || "no folders"}) is encrypted to <span className="mono">{res?.label}</span> + the org key — folder paths never
                  appear on-chain in clear.
                </>
              )}
            </div>
          </div>
          {!tree?.orgKey && <div className="text-sm text-bad">Org has no meshy.orgkey record.</div>}
          {!svc && res && !res.encKey && <div className="text-sm text-bad">Resource has no meshy.enckey record.</div>}
          {ag && !ag.worldid && <div className="text-sm text-warn">This agent has no World ID attestation: meshyd will refuse it (MESHY_NOT_HUMAN_BACKED).</div>}
          {steps ? (
            <Steps steps={steps} />
          ) : !account ? (
            <div className="text-sm text-muted">Connect the org owner's wallet to grant.</div>
          ) : (
            <Button kind="primary" disabled={!valid || busy || !isOwner} onClick={submit}>
              <KeyRound size={14} /> {isOwner ? (svc ? "Grant (1 transaction)" : "Grant (2 transactions)") : "Connect a wallet to grant"}
            </Button>
          )}
        </div>
      </div>
    </div>
  );
}

export function RevokeTimeline({ grant, res, onDone }: { grant: GrantNode; res: { label: string; name: string }; onDone: () => void }) {
  const { wallet, client, org, refresh, toast } = useMeshy();
  const [steps, setSteps] = useState<Step[]>([
    { label: "Send unregister() for the grant", state: "doing" },
    { label: "Revocation mined on Sepolia", state: "todo" },
    { label: `${res.label} daemon kills live sessions (watching every block)`, state: "todo" },
    { label: "Revocation recorded on Arkiv", state: "todo" },
  ]);
  const [result, setResult] = useState<string>();
  const up = (i: number, p: Partial<Step>) => setSteps((s) => s.map((x, j) => (j === i ? { ...x, ...p } : x)));
  const fired = useRef(false);
  useEffect(() => {
    if (fired.current) return; // exactly one unregister tx, even under StrictMode
    fired.current = true;
    (async () => {
      if (!wallet) return;
      const t0 = Date.now();
      try {
        const r = await send(wallet, client, { address: grant.registry, abi: registryAbi, functionName: "unregister", args: [labelId(grant.agent)] }, (h) => {
          up(0, { state: "done", tx: h, detail: "sent" });
          up(1, { state: "doing" });
        });
        const mined = Date.now();
        up(1, { state: "done", detail: `block ${r.blockNumber} · ${((mined - t0) / 1000).toFixed(1)}s after send` });
        up(2, { state: "doing", detail: "waiting for the daemon's report on Arkiv…" });
        refresh();
        for (let i = 0; i < 20; i++) {
          await new Promise((x) => setTimeout(x, 3000));
          const rows = await revokesFor(org, grant.name).catch(() => []);
          const hit = rows.find((x) => x.block === Number(r.blockNumber) || x.ts >= t0 - 5000);
          if (hit) {
            up(2, { state: "done", detail: `session killed ${hit.latencyMs ?? "?"}ms after the daemon saw block ${hit.block}` });
            up(3, { state: "done", detail: `entity ${hit.key.slice(0, 12)}…` });
            const total = (hit.ts - t0) / 1000;
            setResult(`Access revoked — live session terminated ${total.toFixed(1)}s after you clicked.`);
            return;
          }
        }
        up(2, { state: "done", detail: "no live session to terminate (or daemon offline)" });
        up(3, { state: "done", detail: "nothing to record" });
        setResult(`Access revoked in block ${r.blockNumber}. New connections are refused.`);
      } catch (e) {
        const msg = e instanceof Error ? e.message.split("\n")[0] : String(e);
        setSteps((s) => s.map((x) => (x.state === "doing" ? { ...x, state: "fail", detail: msg } : x)));
        toast(msg, "err");
      }
    })();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  return (
    <div className="fixed inset-0 z-40 grid place-items-center bg-black/60" onClick={onDone}>
      <div className="w-full max-w-lg rounded-xl border border-bad/40 bg-panel p-6" onClick={(e) => e.stopPropagation()}>
        <h2 className="mb-1 flex items-center gap-2 text-lg font-semibold text-bad">
          <Zap size={18} /> Revoking
        </h2>
        <div className="mb-4">
          <Name name={grant.name} />
        </div>
        <Steps steps={steps} />
        {result && <div className="mt-5 rounded-lg border border-accent/40 bg-accent/10 p-3 text-sm text-accent">{result}</div>}
        <div className="mt-5 flex justify-end">
          <Button onClick={onDone}>Close</Button>
        </div>
      </div>
    </div>
  );
}

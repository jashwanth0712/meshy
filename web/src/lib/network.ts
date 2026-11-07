// Live network snapshot for the landing hero. One call to meshy-sync's /v1/map
// (the same read the menu-bar app uses), normalised into what the map draws.
import { liveSessions, orgFeed } from "./arkiv";
import { PROVIDER_API } from "./config";

export type Kind = "server" | "gpu" | "pc" | "phone" | "camera" | "pi" | "nas" | "device";

export const KINDS: { kind: Kind; label: string }[] = [
  { kind: "server", label: "Server" },
  { kind: "gpu", label: "GPU" },
  { kind: "pc", label: "PC / laptop" },
  { kind: "phone", label: "Phone" },
  { kind: "camera", label: "Camera" },
  { kind: "pi", label: "Raspberry Pi" },
  { kind: "nas", label: "NAS" },
];

export const kindLabel = (k: Kind) => KINDS.find((x) => x.kind === k)?.label ?? "Device";

export type NetService = { name: string; label: string; proto: string; desc: string; kind: Kind | "service" };
export type NetDevice = { name: string; label: string; network: string; addr: string; kind: Kind; rawKind: string; meta: Record<string, string>; services: NetService[] };
export type NetNetwork = { name: string; label: string; devices: NetDevice[] };
/** Who can hold a grant: an agent (agents.<org>) or a person (people.<org>). */
export type NetAgent = { name: string; label: string; verified: boolean; person: boolean };
export type NetGrant = { name: string; agent: string; target: string; device: string; status: "live" | "expired" | "revoked"; expiry: number };
export type Network = { org: string; block: number; at: number; networks: NetNetwork[]; agents: NetAgent[]; grants: NetGrant[] };

/** Classify a device from its meshy.kind record, falling back on its name and meta. */
export function classify(kind: string, label = "", meta: Record<string, string> = {}): Kind {
  const s = `${kind} ${label} ${meta.model ?? ""}`.toLowerCase();
  if (/gpu|rtx|a100|h100|cuda/.test(s)) return "gpu";
  if (/camera|cctv|\bcam\b|webcam/.test(s)) return "camera";
  if (/phone|iphone|android|pixel/.test(s)) return "phone";
  if (/raspberry|\bpi\b|^pi/.test(s)) return "pi";
  if (/\bnas\b|storage|synology/.test(s)) return "nas";
  if (/laptop|desktop|\bpc\b|macbook|\bmac\b|imac|workstation|ipad|surface/.test(s)) return "pc";
  if (/server|vps|\bdb|rig|node/.test(s) || kind) return "server";
  return "device";
}

const json = <T,>(s: unknown, d: T): T => {
  if (typeof s !== "string") return (s as T) ?? d;
  try {
    return JSON.parse(s) as T;
  } catch {
    return d;
  }
};

type Raw = Record<string, unknown>;
const list = (v: unknown) => (Array.isArray(v) ? (v as Raw[]) : []);
const str = (v: unknown) => (typeof v === "string" ? v : "");

/** Strip the org suffix: db1.prod.meshydemo.eth → db1.prod. */
const short = (full: string, org: string) => full.replace(new RegExp(`\\.${org.replace(/\./g, "\\.")}$`), "");

/** Agents, members, and anyone else holding a grant (so every live grant has someone to draw from). */
function holders(m: Raw, org: string): NetAgent[] {
  const out = new Map<string, NetAgent>();
  for (const a of list(m.agents)) {
    const name = str(a.fullName) || `${str(a.name)}.agents.${org}`;
    out.set(name, { name, label: str(a.name) || name.split(".")[0], verified: a.worldIdValid === true, person: false });
  }
  const members = Array.isArray(m.members) ? (m.members as unknown[]) : [];
  for (const v of members) {
    const name = typeof v === "string" ? v : str((v as Raw).fullName) || str((v as Raw).name);
    if (name && !out.has(name)) out.set(name, { name, label: name.split(".")[0], verified: false, person: true });
  }
  for (const g of list(m.grants)) {
    const name = str(g.agent);
    if (name && !out.has(name)) out.set(name, { name, label: name.split(".")[0], verified: false, person: name.includes(".people.") });
  }
  return [...out.values()];
}

export function normalize(m: Raw): Network {
  const org = str(m.org);
  // meshy-sync doesn't promise an order; sort so the map doesn't reshuffle between polls.
  const byName = (a: { name: string }, b: { name: string }) => a.name.localeCompare(b.name);
  const networks: NetNetwork[] = list(m.networks).map((n) => {
    const name = str(n.fullName) || `${str(n.name)}.${org}`;
    return {
      name,
      label: str(n.name),
      devices: list(n.resources).map((r) => {
        const full = str(r.fullName) || `${str(r.name)}.${name}`;
        const meta = json<Record<string, string>>(r.meta, {});
        const rawKind = str(r.kind);
        return {
          name: full,
          label: short(full, org),
          network: name,
          addr: str(r.addr),
          kind: classify(rawKind, str(r.name), meta),
          rawKind,
          meta,
          services: list(r.services).map((sv) => {
            const spec = json<{ proto?: string; desc?: string }>(sv.spec, {});
            const k = classify("", `${str(sv.name)} ${spec.desc ?? ""}`);
            return {
              name: str(sv.fullName),
              label: str(sv.name),
              proto: spec.proto ?? "",
              desc: spec.desc ?? "",
              kind: k === "camera" ? "camera" : "service",
            } as NetService;
          }),
        };
      }).sort(byName),
    };
  }).sort(byName);
  return {
    org,
    block: Number(m.block ?? 0),
    at: Date.parse(str(m.generatedAt)) || Date.now(),
    networks,
    agents: holders(m, org).sort(byName),
    grants: list(m.grants).map((g) => ({
      name: str(g.fullName),
      agent: str(g.agent),
      target: str(g.target),
      device: str(g.device) || str(g.target),
      status: (["live", "expired", "revoked"].includes(str(g.status)) ? str(g.status) : "expired") as NetGrant["status"],
      expiry: Number(g.expiry ?? 0),
    })),
  };
}

export async function fetchNetwork(org: string, signal?: AbortSignal): Promise<Network> {
  const res = await fetch(`${PROVIDER_API}/v1/map?org=${encodeURIComponent(org)}`, { signal, cache: "no-store" });
  if (!res.ok) throw new Error(`meshy-sync ${res.status}`);
  return normalize((await res.json()) as Raw);
}

export const allDevices = (n: Network) => n.networks.flatMap((x) => x.devices);

/** Device and service counts per kind. Camera services (camera.pi…) count as cameras. */
export function kindCounts(n: Network): Record<Kind, number> {
  const c = { server: 0, gpu: 0, pc: 0, phone: 0, camera: 0, pi: 0, nas: 0, device: 0 } as Record<Kind, number>;
  for (const d of allDevices(n)) {
    c[d.kind]++;
    for (const s of d.services) if (s.kind === "camera") c.camera++;
  }
  return c;
}

export type NetEvent = { id: string; at: number; text: string; tone: "ok" | "bad" | "info" };

/** What changed between two snapshots, in words. */
export function diff(prev: Network, next: Network): NetEvent[] {
  const out: NetEvent[] = [];
  const at = Date.now();
  const add = (text: string, tone: NetEvent["tone"]) => out.push({ id: `${at}-${out.length}-${text}`, at, text, tone });
  const pd = new Map(allDevices(prev).map((d) => [d.name, d]));
  const nd = new Map(allDevices(next).map((d) => [d.name, d]));
  for (const [k, d] of nd) if (!pd.has(k)) add(`${d.label} joined the mesh as a ${kindLabel(d.kind).toLowerCase()}`, "ok");
  for (const [k, d] of pd) if (!nd.has(k)) add(`${d.label} left the mesh`, "bad");
  for (const [k, d] of nd) {
    const old = new Set(pd.get(k)?.services.map((s) => s.name) ?? []);
    if (pd.has(k)) for (const s of d.services) if (!old.has(s.name)) add(`${s.label}.${d.label} published (${s.proto || "service"})`, "ok");
  }
  const pa = new Set(prev.agents.map((a) => a.name));
  for (const a of next.agents) if (!pa.has(a.name)) add(`${a.person ? "Member" : "Agent"} ${a.label} joined`, "info");
  const pg = new Map(prev.grants.map((g) => [g.name, g.status]));
  for (const g of next.grants) {
    const was = pg.get(g.name);
    if (was === g.status) continue;
    const who = `${g.agent.split(".")[0]} → ${short(g.target, next.org)}`;
    if (g.status === "live") add(`${who}: access granted`, "ok");
    else if (g.status === "revoked") add(`${who}: revoked on-chain`, "bad");
    else if (was) add(`${who}: grant expired`, "info");
  }
  return out;
}

/** Agent sessions and blocks on this org's devices, from Arkiv (written by each device's meshyd). */
export type Activity = { sessions: { agent: string; resource: string; since: number }[]; blocked: { agent: string; resource: string; at: number; reason: string }[] };

export async function fetchActivity(n: Network): Promise<Activity> {
  // Only trust entities signed by the devices' own keys (their ENS addr records).
  const rows = await orgFeed(n.org, undefined, 40, allDevices(n).map((d) => d.addr));
  return {
    sessions: liveSessions(rows).map((r) => ({ agent: r.agent.split(".")[0], resource: r.resource, since: r.ts })),
    blocked: rows.filter((r) => r.schema === "meshy.blocked").map((r) => ({ agent: r.agent.split(".")[0], resource: r.resource, at: r.ts, reason: r.reason })),
  };
}

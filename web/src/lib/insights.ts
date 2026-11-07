// Insights: ENS grant history joined with Arkiv sessions on one timeline.
// See docs/insights/PLAN.md §3. Pure functions; the React hook lives in components/insights.
import { type Address, type Hex, type PublicClient, decodeEventLog, zeroAddress } from "viem";
import type { Row } from "./arkiv";
import { FROM_BLOCK } from "./config";
import { type OrgTree, registryAbi } from "./ens";

export type EndCause = "live" | "revoked" | "expired" | "reissued";

/** One grant instance: a (grant name, tokenId) registration, from ENS logs alone. */
export type GrantInterval = {
  key: string;
  grant: string; // claude-1.db1.prod.meshydemo.eth
  tokenId: string;
  kind: "device" | "service";
  agent: string; // claude-1
  target: string; // db1.prod.meshydemo.eth | camera.pi.home.meshydemo.eth
  resource: string; // the device whose daemon enforces it (and writes its audit)
  network: string; // prod
  granter: Address; // LabelRegistered.sender
  from: number; // ms (block timestamp)
  until: number; // ms (expiry, moved by ExpiryUpdated)
  ended?: number; // ms
  endCause: EndCause;
  revoker?: Address; // LabelUnregistered.sender
  blocks: { registered: number; unregistered?: number; renewed: number[] };
  txs: { registered: Hex; unregistered?: Hex };
  sessions: SessionSpan[];
};

/** One meshyd session (shell, exec or port forward), from Arkiv meshy.session rows. */
export type SessionSpan = {
  session: string;
  grant: string;
  agent: string;
  resource: string;
  start: number;
  end?: number;
  live: boolean;
  exit?: string; // "exit 0" (sealed payload; only with the org key)
  argv?: string[];
  kill?: Kill;
  keys: string[]; // Arkiv entity keys (proof links)
};

/** A session.revoke row: the daemon killed a session because its grant stopped being valid. */
export type Kill = {
  key: string;
  ts: number;
  block?: number;
  latencyMs?: number;
  reason: string;
  failClosed: boolean;
  grant: string;
  agent: string;
  resource: string;
  session: string;
};

type Target = { registry: Address; parent: string; kind: "device" | "service"; resource: string; network: string; skip: Set<string> };

function targets(tree: OrgTree): Target[] {
  const out: Target[] = [];
  for (const net of tree.networks)
    for (const r of net.resources) {
      if (r.registry !== zeroAddress)
        out.push({ registry: r.registry, parent: r.name, kind: "device", resource: r.name, network: net.label, skip: new Set(r.services.map((s) => s.label)) });
      for (const s of r.services)
        if (s.registry !== zeroAddress) out.push({ registry: s.registry, parent: s.name, kind: "service", resource: r.name, network: net.label, skip: new Set() });
    }
  return out;
}

const grantEvents = registryAbi.filter(
  (x) => x.type === "event" && (x.name === "LabelRegistered" || x.name === "LabelUnregistered" || x.name === "ExpiryUpdated"),
);

type RawLog = { address: Address; data: Hex; topics: [Hex, ...Hex[]] | []; blockNumber: bigint | null; logIndex: number | null; transactionHash: Hex | null };

async function logsFor(c: PublicClient, addresses: Address[]): Promise<RawLog[]> {
  try {
    return (await c.getLogs({ address: addresses, events: grantEvents as never, fromBlock: FROM_BLOCK, toBlock: "latest" })) as never[];
  } catch {
    const latest = await c.getBlockNumber();
    const out: RawLog[] = [];
    for (let from = FROM_BLOCK; from <= latest; from += 5000n) {
      const to = from + 4999n > latest ? latest : from + 4999n;
      out.push(...((await c.getLogs({ address: addresses, events: grantEvents as never, fromBlock: from, toBlock: to })) as never[]));
    }
    return out;
  }
}

const TS_CACHE = "meshy.blockts";

/** Block timestamps (ms). Blocks never change, so they are cached forever in localStorage. */
async function blockTimes(c: PublicClient, blocks: bigint[]): Promise<Map<string, number>> {
  let cache: Record<string, number> = {};
  try {
    cache = JSON.parse(localStorage.getItem(TS_CACHE) ?? "{}");
  } catch {}
  const missing = [...new Set(blocks.map(String))].filter((b) => !(b in cache));
  // Batched into one JSON-RPC request by the client's http transport.
  const got = await Promise.all(missing.map((b) => c.getBlock({ blockNumber: BigInt(b) }).then((x) => [b, Number(x.timestamp) * 1000] as const)));
  for (const [b, t] of got) cache[b] = t;
  try {
    localStorage.setItem(TS_CACHE, JSON.stringify(cache));
  } catch {}
  return new Map(Object.entries(cache));
}

/** Every grant instance ever issued in the org's device and service registries. */
export async function grantHistory(c: PublicClient, tree: OrgTree, now = Date.now()): Promise<GrantInterval[]> {
  const ts = targets(tree);
  if (ts.length === 0) return [];
  const byReg = new Map(ts.map((t) => [t.registry.toLowerCase(), t]));
  const logs = (await logsFor(c, ts.map((t) => t.registry)))
    .filter((l) => l.blockNumber !== null)
    .sort((a, b) => Number(a.blockNumber! - b.blockNumber!) || (a.logIndex ?? 0) - (b.logIndex ?? 0));
  const times = await blockTimes(c, logs.map((l) => l.blockNumber!));

  const byToken = new Map<string, GrantInterval>(); // registry:tokenId
  const openByLabel = new Map<string, GrantInterval>(); // registry:label
  const out: GrantInterval[] = [];
  for (const l of logs) {
    const t = byReg.get(l.address.toLowerCase());
    if (!t) continue;
    const ev = decodeEventLog({ abi: registryAbi, data: l.data, topics: l.topics as never }) as {
      eventName: "LabelRegistered" | "LabelUnregistered" | "ExpiryUpdated";
      args: { tokenId: bigint; label?: string; owner?: Address; expiry?: bigint; newExpiry?: bigint; sender: Address };
    };
    const at = times.get(String(l.blockNumber)) ?? 0;
    const blk = Number(l.blockNumber);
    const tk = `${t.registry}:${ev.args.tokenId}`;
    if (ev.eventName === "LabelRegistered") {
      const label = ev.args.label ?? "";
      if (!label || t.skip.has(label)) continue; // services live in device registries too
      const lk = `${t.registry}:${label}`;
      const prev = openByLabel.get(lk);
      if (prev && prev.ended === undefined) {
        prev.ended = Math.min(at, prev.until);
        prev.endCause = at < prev.until ? "reissued" : "expired";
      }
      const iv: GrantInterval = {
        key: `${label}.${t.parent}#${ev.args.tokenId}`,
        grant: `${label}.${t.parent}`,
        tokenId: ev.args.tokenId.toString(),
        kind: t.kind,
        agent: label,
        target: t.parent,
        resource: t.resource,
        network: t.network,
        granter: ev.args.sender,
        from: at,
        until: Number(ev.args.expiry ?? 0n) * 1000,
        endCause: "live",
        blocks: { registered: blk, renewed: [] },
        txs: { registered: l.transactionHash ?? "0x" },
        sessions: [],
      };
      byToken.set(tk, iv);
      openByLabel.set(lk, iv);
      out.push(iv);
    } else {
      const iv = byToken.get(tk);
      if (!iv) continue;
      if (ev.eventName === "LabelUnregistered" && iv.ended === undefined) {
        iv.ended = at;
        iv.endCause = "revoked";
        iv.revoker = ev.args.sender;
        iv.blocks.unregistered = blk;
        iv.txs.unregistered = l.transactionHash ?? undefined;
      } else if (ev.eventName === "ExpiryUpdated") {
        iv.until = Number(ev.args.newExpiry ?? 0n) * 1000;
        iv.blocks.renewed.push(blk);
      }
    }
  }
  for (const iv of out)
    if (iv.ended === undefined && iv.until <= now) {
      iv.ended = iv.until;
      iv.endCause = "expired";
    }
  return out;
}

export function kills(rows: Row[]): Kill[] {
  return rows
    .filter((r) => r.kind === "session.revoke")
    .map((r) => ({
      key: r.key,
      ts: r.ts,
      block: r.block,
      latencyMs: r.latencyMs,
      reason: r.reason,
      failClosed: /fail closed/i.test(r.reason),
      grant: r.grant,
      agent: r.agent,
      resource: r.resource,
      session: r.session,
    }));
}

/** Group meshy.session rows by session id. Sessions whose start has already expired from Arkiv are dropped. */
export function sessionSpans(rows: Row[]): SessionSpan[] {
  const by = new Map<string, Row[]>();
  for (const r of rows) if (r.schema === "meshy.session" && r.session) by.set(r.session, [...(by.get(r.session) ?? []), r]);
  const ks = new Map(kills(rows).map((k) => [k.session, k]));
  const out: SessionSpan[] = [];
  for (const [id, rs] of by) {
    const start = rs.find((r) => r.kind === "session.start");
    if (!start) continue;
    const end = rs.find((r) => r.kind === "session.end");
    const kill = ks.get(id);
    const stop = end?.ts ?? kill?.ts;
    out.push({
      session: id,
      grant: start.grant,
      agent: start.agent,
      resource: start.resource,
      start: start.ts,
      end: stop,
      live: stop === undefined,
      exit: end?.event?.reason,
      argv: start.event?.argv,
      kill,
      keys: rs.map((r) => r.key),
    });
  }
  return out.sort((a, b) => a.start - b.start);
}

const SLACK = 5000; // Arkiv batch lag + daemon clock vs block time

/** Attach each session to the grant instance it ran under (same name, inside its window). Returns the unmatched ones. */
export function joinSessions(grants: GrantInterval[], spans: SessionSpan[], now = Date.now()): SessionSpan[] {
  for (const g of grants) g.sessions = [];
  const byName = new Map<string, GrantInterval[]>();
  for (const g of grants) byName.set(g.grant, [...(byName.get(g.grant) ?? []), g]);
  const orphans: SessionSpan[] = [];
  for (const s of spans) {
    const cands = (byName.get(s.grant) ?? []).filter((g) => g.from - SLACK <= s.start && s.start <= (g.ended ?? Math.max(g.until, now)) + SLACK);
    const g = cands.sort((a, b) => b.from - a.from)[0];
    if (g) g.sessions.push(s);
    else orphans.push(s);
  }
  return orphans;
}

export const grantEnd = (g: GrantInterval, now = Date.now()) => g.ended ?? Math.min(g.until, now);

/** One row of the unified org timeline: ENS grant changes and Arkiv events, newest first. PLAN §3.2. */
export type InsightEvent = {
  t: number;
  src: "ens" | "arkiv";
  kind: "grant.created" | "grant.renewed" | "grant.revoked" | "grant.expired" | "session.start" | "session.end" | "session.revoke" | "blocked" | "denied";
  network: string;
  resource: string;
  agent?: string;
  grant?: string;
  session?: string;
  actor?: Address; // granter / revoker for ENS rows
  op?: string;
  path?: string; // sealed on Arkiv; present only after decryption in this browser
  outcome: "ok" | "blocked" | "denied" | "revoked" | "expired";
  reason?: string;
  block?: number;
  latencyMs?: number;
  argv?: string[];
  proof: { arkivKey?: string; tx?: Hex };
  sealed: boolean; // a payload exists but wasn't decrypted (no org key)
};

const netOf = (resource: string) => resource.split(".")[1] ?? "";

export function insightEvents(grants: GrantInterval[], rows: Row[]): InsightEvent[] {
  const out: InsightEvent[] = [];
  for (const g of grants) {
    const base = { src: "ens" as const, network: g.network, resource: g.resource, agent: g.agent, grant: g.grant, sealed: false };
    out.push({ ...base, t: g.from, kind: "grant.created", actor: g.granter, outcome: "ok", block: g.blocks.registered, proof: { tx: g.txs.registered } });
    for (const b of g.blocks.renewed) out.push({ ...base, t: g.from, kind: "grant.renewed", outcome: "ok", block: b, proof: {} });
    if (g.endCause === "revoked" && g.ended !== undefined)
      out.push({ ...base, t: g.ended, kind: "grant.revoked", actor: g.revoker, outcome: "revoked", block: g.blocks.unregistered, proof: { tx: g.txs.unregistered } });
    if (g.endCause === "expired" && g.ended !== undefined) out.push({ ...base, t: g.ended, kind: "grant.expired", outcome: "expired", proof: {} });
  }
  for (const r of rows) {
    if (r.schema !== "meshy.session" && r.schema !== "meshy.blocked") continue;
    const kind = r.kind as InsightEvent["kind"];
    // denied rows carry the grant they wanted only inside `reason` (PLAN gap G9)
    const grant = r.grant || /grant=(\S+)/.exec(r.reason)?.[1];
    out.push({
      t: r.ts,
      src: "arkiv",
      kind,
      network: netOf(r.resource),
      resource: r.resource,
      agent: r.agent || undefined,
      grant: grant || undefined,
      session: r.session || undefined,
      op: r.op || undefined,
      path: r.event?.path ?? r.path,
      outcome: kind === "blocked" ? "blocked" : kind === "denied" ? "denied" : kind === "session.revoke" ? "revoked" : "ok",
      reason: r.reason || r.event?.reason,
      block: r.block,
      latencyMs: r.latencyMs,
      argv: r.event?.argv,
      proof: { arkivKey: r.key },
      sealed: !!r.payload && !r.event,
    });
  }
  return out.sort((a, b) => b.t - a.t);
}

/** ms of the grant window during which at least one session was running. */
export function usedMs(g: GrantInterval, now = Date.now()): number {
  const lo = g.from, hi = grantEnd(g, now);
  const iv = g.sessions
    .map((s) => [Math.max(lo, s.start), Math.min(hi, s.end ?? now)] as const)
    .filter(([a, b]) => b > a)
    .sort((a, b) => a[0] - b[0]);
  let total = 0, cur: [number, number] | undefined;
  for (const [a, b] of iv) {
    if (!cur || a > cur[1]) {
      if (cur) total += cur[1] - cur[0];
      cur = [a, b];
    } else cur[1] = Math.max(cur[1], b);
  }
  if (cur) total += cur[1] - cur[0];
  return total;
}

export function median(xs: number[]): number | undefined {
  if (xs.length === 0) return undefined;
  const s = [...xs].sort((a, b) => a - b), m = Math.floor(s.length / 2);
  return s.length % 2 ? s[m] : (s[m - 1] + s[m]) / 2;
}

// ---------- time axis with idle gaps collapsed ----------

export type AxisPart = { from: number; to: number; x0: number; w: number };
export type Axis = { x: (t: number) => number; segments: AxisPart[]; gaps: AxisPart[]; ticks: { x: number; label: string }[]; gapLabels: { x: number; label: string }[] };

const STEPS = [1, 5, 10, 30, 60, 120, 300, 600, 900, 1800, 3600, 7200, 21600, 43200, 86400].map((s) => s * 1000);

export function fmtDur(ms: number): string {
  const s = Math.round(ms / 1000);
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return s % 60 && m < 10 ? `${m}m ${s % 60}s` : `${m}m`;
  const h = Math.floor(m / 60);
  if (h < 48) return m % 60 ? `${h}h ${m % 60}m` : `${h}h`;
  return `${Math.floor(h / 24)}d`;
}

/** An x-axis (0–100 %) over the given spans. With collapse, quiet stretches longer than gapMs shrink to a
 *  fixed-width hatched gap and each busy segment gets width ∝ log(duration), so a 10 s burst and a 1 h grant
 *  are both readable on one row. */
export function buildAxis(spans: [number, number][], collapse = true, gapMs = 10 * 60_000): Axis {
  const ok = spans.filter(([a, b]) => Number.isFinite(a) && Number.isFinite(b) && b >= a).sort((a, b) => a[0] - b[0]);
  if (ok.length === 0) return { x: () => 0, segments: [], gaps: [], ticks: [], gapLabels: [] };
  let merged: [number, number][] = [];
  for (const [a, b] of ok) {
    const last = merged[merged.length - 1];
    if (last && a - last[1] < gapMs) last[1] = Math.max(last[1], b);
    else merged.push([a, b]);
  }
  if (!collapse) merged = [[merged[0][0], merged[merged.length - 1][1]]];
  const padded = merged.map(([a, b]) => {
    const p = Math.min(gapMs / 4, Math.max(5000, (b - a) * 0.04)); // never pads across a gap
    return [a - p, b + p] as [number, number];
  });
  const GAP = padded.length > 1 ? Math.min(4, 24 / (padded.length - 1)) : 0;
  const avail = 100 - GAP * (padded.length - 1);
  // log weighting: a 4-minute burst still gets about half the width of an hour-long window
  const weights = padded.map(([a, b]) => Math.log(1 + Math.max(b - a, 30_000) / 10_000));
  const sum = weights.reduce((x, y) => x + y, 0);
  const segments: AxisPart[] = [];
  const gaps: AxisPart[] = [];
  let x0 = 0;
  padded.forEach(([a, b], i) => {
    const w = (avail * weights[i]) / sum;
    segments.push({ from: a, to: b, x0, w });
    x0 += w;
    if (i < padded.length - 1) {
      gaps.push({ from: b, to: padded[i + 1][0], x0, w: GAP });
      x0 += GAP;
    }
  });
  const x = (t: number) => {
    if (t <= segments[0].from) return 0;
    for (const s of segments) if (t <= s.to) return t >= s.from ? s.x0 + ((t - s.from) / (s.to - s.from)) * s.w : s.x0;
    return 100;
  };
  const ticks: Axis["ticks"] = [];
  for (const s of segments) {
    const want = Math.max(1, Math.floor(s.w / 11));
    const step = STEPS.find((st) => (s.to - s.from) / st <= want) ?? STEPS[STEPS.length - 1];
    const withSec = step < 60_000;
    const fmt = (v: number) => new Date(v).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", ...(withSec ? { second: "2-digit" } : {}) });
    const before = ticks.length;
    for (let t = Math.ceil(s.from / step) * step; t <= s.to; t += step) ticks.push({ x: x(t), label: fmt(t) });
    if (ticks.length === before) ticks.push({ x: s.x0 + s.w / 2, label: fmt((s.from + s.to) / 2) }); // every segment gets a label
  }
  const gapLabels = gaps.map((g) => ({ x: g.x0 + g.w / 2, label: fmtDur(g.to - g.from) }));
  return { x, segments, gaps, ticks, gapLabels };
}

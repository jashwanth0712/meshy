// Arkiv (Rabdi) reads for Meshy's audit log. Schema: arkiv/schema.md.
import { gunzipSync } from "fflate";
import { openBytes, openJSON } from "./envelope";

export const ARKIV_RPC = "/api/arkiv";
export const ARKIV_PUBLIC_RPC = "https://rpc.rabdi.db-chain.testnet.arkiv.network";

export type Attr = { name: string; type: string; value: string };
export type Entity = { key: string; attrs: Record<string, string | number>; raw: Attr[] };

export type AuditEvent = {
  kind: string;
  ts: string;
  resource: string;
  agent?: string;
  grant?: string;
  session?: string;
  op?: string;
  path?: string;
  bytes?: number;
  reason?: string;
  block?: number;
  latencyMs?: number;
  argv?: string[];
};

export type Row = {
  key: string;
  schema: string;
  kind: string;
  ts: number;
  agent: string;
  grant: string;
  resource: string;
  session: string;
  op: string;
  reason: string;
  block?: number;
  latencyMs?: number;
  nOps?: number;
  /** session: shell | exec | files (Finder volume) | connect (a port) */
  channel?: string;
  /** session.end: bytes both ways and how long it ran */
  bytes?: number;
  durationMs?: number;
  /** audit chunk: file telemetry that can be read without the org key */
  nRead?: number;
  nWrite?: number;
  nList?: number;
  nDelete?: number;
  bytesRead?: number;
  bytesWritten?: number;
  payload?: string; // sealed envelope (hex-decoded string) or plaintext JSON
  event?: AuditEvent; // decrypted
  ops?: AuditEvent[]; // decrypted audit chunk
  path?: string;
};

function decodeValue(a: Attr): string | number {
  if (a.type === "u64" || a.type === "u256" || a.type === "i32") {
    const v = typeof a.value === "string" && a.value.startsWith("0x") ? parseInt(a.value, 16) : Number(a.value);
    return Number.isFinite(v) ? v : a.value;
  }
  return a.value;
}

export async function query(q: string, limit = 50): Promise<Entity[]> {
  const res = await fetch(ARKIV_RPC, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({
      jsonrpc: "2.0",
      id: 1,
      method: "arkiv_query",
      params: [q, { select: { key: true, attributes: true }, limit: "0x" + limit.toString(16) }],
    }),
  });
  const j = await res.json();
  if (j.error) throw new Error(j.error.message ?? JSON.stringify(j.error));
  const data = Array.isArray(j.result) ? j.result : j.result?.data ?? [];
  return data.map((it: { key: string; attributes: Attr[] }) => {
    const attrs: Record<string, string | number> = {};
    for (const a of it.attributes ?? []) attrs[a.name] = decodeValue(a);
    return { key: it.key, attrs, raw: it.attributes ?? [] };
  });
}

function hexToBytes(h: string): Uint8Array {
  const s = h.startsWith("0x") ? h.slice(2) : h;
  const out = new Uint8Array(s.length / 2);
  for (let i = 0; i < out.length; i++) out[i] = parseInt(s.slice(i * 2, i * 2 + 2), 16);
  return out;
}

const num = (v: string | number | undefined) => (v === undefined || v === "" ? undefined : Number(v));

export function toRow(e: Entity): Row {
  const a = e.attrs;
  return {
    key: e.key,
    schema: String(a["arkiv.schema"] ?? ""),
    kind: String(a.kind ?? (a["arkiv.schema"] === "meshy.audit-chunk" ? "ops" : "")),
    ts: Number(a.ts ?? 0),
    agent: String(a.agent ?? ""),
    grant: String(a.grant ?? ""),
    resource: String(a.resource ?? ""),
    session: String(a.session ?? ""),
    op: String(a.op ?? ""),
    reason: String(a.reason ?? ""),
    block: a.block !== undefined ? Number(a.block) : undefined,
    latencyMs: a.latencyMs !== undefined ? Number(a.latencyMs) : undefined,
    nOps: a.nOps !== undefined ? Number(a.nOps) : undefined,
    channel: typeof a.channel === "string" ? a.channel : undefined,
    bytes: num(a.bytes),
    durationMs: num(a.durationMs),
    nRead: num(a.nRead),
    nWrite: num(a.nWrite),
    nList: num(a.nList),
    nDelete: num(a.nDelete),
    bytesRead: num(a.bytesRead),
    bytesWritten: num(a.bytesWritten),
    payload: typeof a.payload === "string" ? a.payload : undefined,
    path: typeof a.path === "string" ? a.path : undefined,
  };
}

/** Decrypt a row's payload with the org's X25519 private key (base64). */
export function decryptRow(r: Row, orgPriv?: string): Row {
  if (!r.payload) return r;
  try {
    const bytes = hexToBytes(r.payload);
    const text = new TextDecoder().decode(bytes);
    if (r.schema === "meshy.audit-chunk") {
      const gz = text.trim().startsWith("{\"v\"") && orgPriv ? openBytes(text, r.schema, orgPriv) : bytes;
      const lines = new TextDecoder().decode(gunzipSync(gz)).trim().split("\n");
      return { ...r, ops: lines.filter(Boolean).map((l) => JSON.parse(l) as AuditEvent) };
    }
    if (text.trim().startsWith("{\"v\"")) {
      if (!orgPriv) return r;
      return { ...r, event: openJSON<AuditEvent>(text, r.schema, orgPriv) };
    }
    return { ...r, event: JSON.parse(text) as AuditEvent };
  } catch {
    return r;
  }
}

const esc = (s: string) => s.replace(/'/g, "");

/** Build an owner-filter clause from verified writer addresses.
 *  Arkiv entities are public: anyone can write org = 'meshydemo.eth'.
 *  By filtering on $owner we only trust entities written by the resource
 *  daemon addresses published in ENS (the resource's `addr` record).
 *  If no writers are provided the filter is omitted (dev / unknown orgs). */
function ownerClause(writers: string[]): string {
  const addrs = writers.filter((w) => /^0x[0-9a-fA-F]{40}$/.test(w));
  if (addrs.length === 0) return "";
  const parts = addrs.map((a) => `$owner = addr(${a})`);
  return ` AND (${parts.join(" OR ")})`;
}

export async function orgFeed(org: string, orgPriv?: string, limit = 60, writers: string[] = []): Promise<Row[]> {
  const ownerFilter = ownerClause(writers);
  const [sessions, blocked, chunks] = await Promise.all([
    query(`arkiv.schema = str('meshy.session') AND org = str('${esc(org)}')${ownerFilter} ORDER BY ts:u64 DESC`, limit),
    query(`arkiv.schema = str('meshy.blocked') AND org = str('${esc(org)}')${ownerFilter} ORDER BY ts:u64 DESC`, limit),
    query(`arkiv.schema = str('meshy.audit-chunk') AND org = str('${esc(org)}')${ownerFilter} ORDER BY ts:u64 DESC`, limit),
  ]);
  return [...sessions, ...blocked, ...chunks]
    .map(toRow)
    .map((r) => decryptRow(r, orgPriv))
    .sort((a, b) => b.ts - a.ts);
}

/** Rabdi caps a page at 100 rows, and a cursor only works at the head block it was issued at,
 *  so page backwards by timestamp instead (keyset pagination). Numeric literals must be
 *  typed u64(): u256() against a u64 attribute silently matches nothing. */
export async function queryAll(where: string, max = 600): Promise<Entity[]> {
  const PAGE = 100;
  const seen = new Set<string>();
  const out: Entity[] = [];
  let before: number | undefined;
  while (out.length < max) {
    const page = await query(`${where}${before !== undefined ? ` AND ts <= u64(${before})` : ""} ORDER BY ts:u64 DESC`, PAGE);
    let fresh = 0;
    for (const e of page) {
      if (seen.has(e.key)) continue;
      seen.add(e.key);
      out.push(e);
      fresh++;
    }
    if (page.length < PAGE || fresh === 0) break;
    const last = Number(page[page.length - 1].attrs.ts);
    if (!Number.isFinite(last)) break;
    before = last; // `<=` plus dedupe, so rows sharing the boundary timestamp aren't skipped
  }
  return out;
}

/** Every session and refusal entity for an org (newest first), from verified writers only. */
export async function orgHistory(org: string, writers: string[] = [], max = 600): Promise<Row[]> {
  const of = ownerClause(writers);
  const [s, b] = await Promise.all([
    queryAll(`arkiv.schema = str('meshy.session') AND org = str('${esc(org)}')${of}`, max),
    queryAll(`arkiv.schema = str('meshy.blocked') AND org = str('${esc(org)}')${of}`, max),
  ]);
  return [...s, ...b].map(toRow).sort((a, b) => b.ts - a.ts);
}

export async function revokesFor(org: string, grant: string): Promise<Row[]> {
  const rows = await query(
    `arkiv.schema = str('meshy.session') AND kind = str('session.revoke') AND org = str('${esc(org)}') AND grant = str('${esc(grant)}') ORDER BY ts:u64 DESC`,
    10,
  );
  return rows.map(toRow);
}

/** Sessions with a start and no end/revoke, i.e. live right now (Arkiv lags ~5 s). */
export function liveSessions(rows: Row[]): Row[] {
  const ended = new Set(rows.filter((r) => r.kind === "session.end" || r.kind === "session.revoke").map((r) => r.session));
  return rows.filter((r) => r.kind === "session.start" && !ended.has(r.session));
}

export function runnableQuery(org: string): string {
  return `curl -s ${ARKIV_PUBLIC_RPC} -H 'content-type: application/json' -d '{"jsonrpc":"2.0","id":1,"method":"arkiv_query","params":["arkiv.schema = str(\\u0027meshy.blocked\\u0027) AND org = str(\\u0027${org}\\u0027) ORDER BY ts:u64 DESC",{"select":{"key":true,"attributes":true},"limit":"0x14"}]}'`;
}

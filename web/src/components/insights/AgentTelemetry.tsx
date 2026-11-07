"use client";
// Per-agent telemetry from Arkiv: what each agent or person did on which device, at the level of
// sessions (shell / commands / Finder volume / port), commands, files and network bytes, blocks.
// Counts and bytes are plain Arkiv attributes; commands and paths need the org key.
import type { Row } from "@/lib/arkiv";

export type AgentStats = {
  agent: string;
  devices: Set<string>;
  sessions: Record<string, number>; // channel -> count
  commands: string[];
  filesRead: number;
  filesWritten: number;
  bytesRead: number;
  bytesWritten: number;
  listings: number;
  deletes: number;
  netBytes: number;
  blocked: number;
  revoked: number;
  ms: number;
  last: number;
  paths: Map<string, { op: string; bytes: number }>;
};

export function agentStats(rows: Row[], org: string): AgentStats[] {
  const by = new Map<string, AgentStats>();
  const get = (a: string) => {
    let s = by.get(a);
    if (!s) {
      s = { agent: a, devices: new Set(), sessions: {}, commands: [], filesRead: 0, filesWritten: 0, bytesRead: 0, bytesWritten: 0, listings: 0, deletes: 0, netBytes: 0, blocked: 0, revoked: 0, ms: 0, last: 0, paths: new Map() };
      by.set(a, s);
    }
    return s;
  };
  for (const r of rows) {
    if (!r.agent) continue;
    const s = get(r.agent);
    s.devices.add(r.resource.replace(`.${org}`, ""));
    s.last = Math.max(s.last, r.ts);
    const ch = r.channel ?? r.event?.op ?? "shell";
    if (r.kind === "session.start") {
      s.sessions[ch] = (s.sessions[ch] ?? 0) + 1;
      const argv = r.event?.argv;
      if (argv && ch !== "files") s.commands.push(ch === "connect" ? argv.slice(1).join(" ") : argv.slice(argv[0] === "/bin/sh" ? 2 : 0).join(" "));
    } else if (r.kind === "session.end") {
      s.ms += r.durationMs ?? 0;
      if (ch === "connect") s.netBytes += r.bytes ?? 0;
    } else if (r.kind === "session.revoke") {
      s.revoked++;
    } else if (r.kind === "blocked" || r.kind === "denied") {
      s.blocked++;
    } else if (r.schema === "meshy.audit-chunk") {
      s.filesRead += r.nRead ?? 0;
      s.filesWritten += r.nWrite ?? 0;
      s.bytesRead += r.bytesRead ?? 0;
      s.bytesWritten += r.bytesWritten ?? 0;
      s.listings += r.nList ?? 0;
      s.deletes += r.nDelete ?? 0;
      for (const o of r.ops ?? []) if (o.path && o.op !== "readdir") s.paths.set(o.path, { op: o.op ?? "", bytes: (s.paths.get(o.path)?.bytes ?? 0) + (o.bytes ?? 0) });
    }
  }
  return [...by.values()].sort((a, b) => b.last - a.last);
}

export const fmtBytes = (n: number) => (n < 1024 ? `${n} B` : n < 1 << 20 ? `${(n / 1024).toFixed(1)} KB` : `${(n / (1 << 20)).toFixed(1)} MB`);
const fmtDur = (ms: number) => (ms < 60000 ? `${Math.round(ms / 1000)}s` : ms < 3600000 ? `${Math.round(ms / 60000)}m` : `${(ms / 3600000).toFixed(1)}h`);
const CH: Record<string, string> = { shell: "shell", exec: "commands", files: "Finder volume", connect: "port" };

function Stat({ k, v, sub, bad }: { k: string; v: string | number; sub?: string; bad?: boolean }) {
  return (
    <div>
      <div className="text-[11px] uppercase tracking-[0.06em] text-muted">{k}</div>
      <div className={`mono mt-0.5 text-[18px] font-semibold ${bad ? "text-bad" : ""}`}>{v}</div>
      {sub && <div className="text-[12px] text-muted">{sub}</div>}
    </div>
  );
}

export function AgentTelemetry({ rows, org, hasKey }: { rows: Row[]; org: string; hasKey: boolean }) {
  const stats = agentStats(rows, org);
  if (stats.length === 0) return null;
  return (
    <div className="mb-8 grid gap-3 md:grid-cols-2">
      {stats.map((s) => {
        const total = Object.values(s.sessions).reduce((a, b) => a + b, 0);
        const files = [...s.paths.entries()].slice(0, 5);
        return (
          <div key={s.agent} className="rounded-2xl border border-line bg-panel p-5">
            <div className="flex items-baseline justify-between gap-3">
              <div className="mono text-[15px] font-semibold">{s.agent}</div>
              <div className="truncate text-[12px] text-muted">on {[...s.devices].join(", ")}</div>
            </div>
            <div className="mt-1 text-[12px] text-muted">
              {total} session{total === 1 ? "" : "s"}
              {total > 0 && ` (${Object.entries(s.sessions).map(([c, n]) => `${n} ${CH[c] ?? c}`).join(", ")})`}
              {s.ms > 0 && ` · ${fmtDur(s.ms)} connected`}
            </div>
            <div className="mt-4 grid grid-cols-3 gap-3">
              <Stat k="Read" v={s.filesRead} sub={fmtBytes(s.bytesRead)} />
              <Stat k="Written" v={s.filesWritten} sub={fmtBytes(s.bytesWritten)} />
              <Stat k="Blocked" v={s.blocked} sub={s.revoked ? `${s.revoked} revoked` : undefined} bad={s.blocked > 0} />
              <Stat k="Listed" v={s.listings} />
              <Stat k="Deleted" v={s.deletes} />
              <Stat k="Network" v={fmtBytes(s.netBytes)} sub="through ports" />
            </div>
            {(s.commands.length > 0 || files.length > 0) && (
              <div className="mt-4 space-y-1 border-t border-line pt-3 text-[12px]">
                {s.commands.slice(0, 4).map((c, i) => (
                  <div key={i} className="mono truncate text-muted" title={c}>
                    <span className="text-fg">$</span> {c || "(interactive shell)"}
                  </div>
                ))}
                {files.map(([p, f]) => (
                  <div key={p} className="mono truncate text-muted" title={p}>
                    <span className="text-fg">{f.op}</span> {p.split("/").slice(-2).join("/")} {f.bytes ? `· ${fmtBytes(f.bytes)}` : ""}
                  </div>
                ))}
              </div>
            )}
            {!hasKey && total > 0 && s.commands.length === 0 && <div className="mt-3 text-[12px] text-muted">Commands and file names are encrypted: import the org key to read them.</div>}
          </div>
        );
      })}
    </div>
  );
}

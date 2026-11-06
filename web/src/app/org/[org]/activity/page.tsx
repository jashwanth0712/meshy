"use client";
import { Ban, Copy, FileText, Lock, Play, Square, Zap } from "lucide-react";
import Link from "next/link";
import { useCallback, useEffect, useState } from "react";
import { Badge, Button, Card, Empty, Ext, ago, cx, links, useNow } from "@/components/ui";
import { type Row, liveSessions, orgFeed, runnableQuery } from "@/lib/arkiv";
import { useMeshy } from "@/lib/store";

const FILTERS = ["all", "blocked", "sessions", "files"] as const;

function icon(r: Row) {
  if (r.kind === "blocked" || r.kind === "denied") return <Ban size={14} className="text-bad" />;
  if (r.kind === "session.revoke") return <Zap size={14} className="text-warn" />;
  if (r.kind === "session.start") return <Play size={14} className="text-accent" />;
  if (r.kind === "session.end") return <Square size={14} className="text-muted" />;
  return <FileText size={14} className="text-accent-2" />;
}

function describe(r: Row, hasKey: boolean) {
  const e = r.event;
  const path = e?.path ?? r.path;
  switch (r.kind) {
    case "blocked":
      return (
        <>
          <b className="text-bad">blocked</b> {r.op} {path ? <span className="mono">{path}</span> : hasKey ? null : <Lock size={11} className="inline" />} — {r.reason}
        </>
      );
    case "denied":
      return (
        <>
          <b className="text-bad">login denied</b> — <span className="mono text-xs">{r.reason.split(" ")[0]}</span>
        </>
      );
    case "session.start":
      return (
        <>
          session started{e?.argv ? <> · <span className="mono">{e.argv.slice(2).join(" ") || e.argv.join(" ")}</span></> : hasKey ? null : <> <Lock size={11} className="inline" /></>}
        </>
      );
    case "session.end":
      return <>session ended ({e?.reason ?? "exit"})</>;
    case "session.revoke":
      return (
        <>
          <b className="text-warn">session killed by revocation</b> at block {r.block} · {r.latencyMs}ms after the daemon saw it
        </>
      );
    default:
      return (
        <>
          {r.nOps} file op{r.nOps === 1 ? "" : "s"}
          {r.ops ? (
            <span className="mono text-xs text-muted">
              {" "}
              · {r.ops.slice(0, 4).map((o) => `${o.op} ${o.path}${o.bytes ? ` (${o.bytes}B)` : ""}`).join(" · ")}
              {r.ops.length > 4 ? " …" : ""}
            </span>
          ) : hasKey ? null : (
            <> <Lock size={11} className="inline" /></>
          )}
        </>
      );
  }
}

export default function ActivityPage() {
  const { org, orgKey } = useMeshy();
  const now = useNow(5000);
  const [rows, setRows] = useState<Row[]>();
  const [err, setErr] = useState<string>();
  const [f, setF] = useState<(typeof FILTERS)[number]>("all");
  const load = useCallback(async () => {
    try {
      setRows(await orgFeed(org, orgKey, 80));
      setErr(undefined);
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    }
  }, [org, orgKey]);
  useEffect(() => {
    load();
    const t = setInterval(load, 5000);
    return () => clearInterval(t);
  }, [load]);

  const shown = (rows ?? []).filter((r) =>
    f === "all" ? true : f === "blocked" ? r.kind === "blocked" || r.kind === "denied" : f === "sessions" ? r.kind.startsWith("session") : r.schema === "meshy.audit-chunk",
  );
  const live = liveSessions(rows ?? []);
  const blocks = (rows ?? []).filter((r) => r.kind === "blocked" || r.kind === "denied").length;
  const revokes = (rows ?? []).filter((r) => r.kind === "session.revoke");

  return (
    <div className="space-y-5">
      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        <Stat t="Live sessions" v={live.length} accent />
        <Stat t="Blocked attempts" v={blocks} bad />
        <Stat t="Revocations" v={revokes.length} />
        <Stat t="Avg kill latency (daemon)" v={revokes.length ? `${Math.round(revokes.reduce((a, r) => a + (r.latencyMs ?? 0), 0) / revokes.length)}ms` : "—"} />
      </div>
      {live.length > 0 && (
        <Card title="Live now">
          <div className="space-y-2">
            {live.map((s) => (
              <div key={s.key} className="flex items-center gap-3 text-sm">
                <span className="h-2 w-2 rounded-full bg-accent live-dot" />
                <span className="mono">{s.agent}</span> on <span className="mono">{s.resource}</span>
                <span className="text-muted">{s.event?.argv ? s.event.argv.slice(2).join(" ") : ""}</span>
                <span className="ml-auto text-xs text-muted">started {ago(s.ts, now)}</span>
              </div>
            ))}
          </div>
        </Card>
      )}
      <Card
        title="Audit log · Arkiv"
        actions={
          <>
            {FILTERS.map((x) => (
              <button key={x} onClick={() => setF(x)} className={cx("rounded-md px-2 py-1 text-xs", f === x ? "bg-panel-2 text-fg" : "text-muted")}>
                {x}
              </button>
            ))}
          </>
        }
      >
        {!orgKey && (
          <div className="mb-4 rounded-lg border border-warn/30 bg-warn/5 p-3 text-sm text-warn">
            Paths and commands are encrypted to your org key (Arkiv is public). <Link href={`/org/${org}/settings`} className="underline">Import the org key</Link> to decrypt them here.
          </div>
        )}
        {err && <div className="mb-3 text-sm text-bad">{err}</div>}
        {!rows ? (
          <div className="text-sm text-muted">Querying Arkiv…</div>
        ) : shown.length === 0 ? (
          <Empty>No audit events yet.</Empty>
        ) : (
          <div className="divide-y divide-line">
            {shown.map((r) => (
              <div key={r.key} className="flex items-start gap-3 py-2.5 text-sm">
                <span className="mt-0.5">{icon(r)}</span>
                <div className="min-w-0 flex-1">
                  <div>
                    <span className="mono text-accent-2">{r.agent || "—"}</span> <span className="text-muted">on</span> <span className="mono">{r.resource.split(".")[0]}</span> · {describe(r, !!orgKey)}
                  </div>
                </div>
                <span className="shrink-0 text-xs text-muted" title={new Date(r.ts).toLocaleString()}>
                  {ago(r.ts, now)}
                </span>
                <Ext href={links.arkivEntity(r.key)}>
                  <span className="text-xs">entity</span>
                </Ext>
              </div>
            ))}
          </div>
        )}
      </Card>
      <Card title="Verify it yourself" actions={<Badge tone="info">public Arkiv RPC, no key</Badge>}>
        <div className="flex items-start gap-2">
          <pre className="mono flex-1 overflow-x-auto whitespace-pre-wrap break-all rounded-lg border border-line bg-bg p-3 text-[11px] text-muted">{runnableQuery(org)}</pre>
          <Button kind="ghost" onClick={() => navigator.clipboard.writeText(runnableQuery(org))}>
            <Copy size={14} />
          </Button>
        </div>
      </Card>
    </div>
  );
}

function Stat({ t, v, accent, bad }: { t: string; v: number | string; accent?: boolean; bad?: boolean }) {
  return (
    <div className="rounded-xl border border-line bg-panel p-4">
      <div className="text-xs text-muted">{t}</div>
      <div className={cx("mt-2 text-2xl font-semibold tabular-nums", accent && "text-accent", bad && "text-bad")}>{v}</div>
    </div>
  );
}

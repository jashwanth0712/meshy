"use client";
import Link from "next/link";
import { useCallback, useEffect, useState } from "react";
import { Badge, Ext, PageHeader, Tabs, cx, links, useNow } from "@/components/ui";
import { KillLatency } from "@/components/insights/KillLatency";
import { ViewSwitch } from "@/components/insights/ViewSwitch";
import { type Row, liveSessions, orgFeed, runnableQuery } from "@/lib/arkiv";
import { kills } from "@/lib/insights";
import { useMeshy } from "@/lib/store";

type F = "all" | "refused" | "sessions" | "revocations";

function event(r: Row, hasKey: boolean): { label: React.ReactNode; detail: string } {
  const e = r.event;
  const path = e?.path ?? r.path;
  switch (r.kind) {
    case "blocked":
      return { label: <Badge tone="bad">Refused</Badge>, detail: r.reason.startsWith("MESHY_") ? r.reason.split(" ")[0] : `${r.op} ${path ?? (hasKey ? "" : "(encrypted)")}`.trim() };
    case "denied":
      return { label: <Badge tone="bad">Refused</Badge>, detail: r.reason.split(" ")[0] };
    case "session.start":
      return { label: "Session started", detail: e?.argv ? e.argv.slice(2).join(" ") || e.argv.join(" ") : hasKey ? "" : "(encrypted)" };
    case "session.end":
      return { label: "Session ended", detail: e?.reason ?? "" };
    case "session.revoke":
      return { label: <Badge tone="warn">Revoked</Badge>, detail: `ended ${r.latencyMs} ms after block ${r.block}` };
    default:
      return { label: "Files", detail: r.ops ? r.ops.slice(0, 2).map((o) => `${o.op} ${o.path}`).join(", ") + (r.ops.length > 2 ? " …" : "") : `${r.nOps} file op${r.nOps === 1 ? "" : "s"}${hasKey ? "" : " (encrypted)"}` };
  }
}

export default function ActivityPage() {
  const { org, orgKey, tree } = useMeshy();
  useNow(30000);
  const [rows, setRows] = useState<Row[]>();
  const [err, setErr] = useState<string>();
  const [f, setF] = useState<F>("all");
  const [showQuery, setShowQuery] = useState(false);
  // Only trust entities written by the device daemons' ENS addresses (spoof-proof reads).
  const writers = tree?.networks.flatMap((n) => n.resources).map((r) => r.addr as string) ?? [];
  const load = useCallback(async () => {
    try {
      setRows(await orgFeed(org, orgKey, 80, writers));
      setErr(undefined);
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [org, orgKey, writers.join(",")]);
  useEffect(() => {
    load();
    const t = setInterval(load, 5000);
    return () => clearInterval(t);
  }, [load]);

  const all = rows ?? [];
  const refused = all.filter((r) => r.kind === "blocked" || r.kind === "denied");
  const sessions = all.filter((r) => r.kind.startsWith("session"));
  const revokes = all.filter((r) => r.kind === "session.revoke");
  const shown = f === "all" ? all : f === "refused" ? refused : f === "sessions" ? sessions : revokes;
  const live = liveSessions(all).length;

  return (
    <>
      <PageHeader
        title="Activity"
        sub={rows ? `${live} live session${live === 1 ? "" : "s"} · ${refused.length} refused · ${revokes.length} revocation${revokes.length === 1 ? "" : "s"}` : "Reading Arkiv…"}
        action={<ViewSwitch org={org} current="feed" />}
      />
      {rows && revokes.length > 0 && (
        <div className="mb-8">
          <KillLatency kills={kills(all)} />
        </div>
      )}
      <Tabs
        value={f}
        onChange={setF}
        tabs={[
          { v: "all", label: "All" },
          { v: "refused", label: "Refused" },
          { v: "sessions", label: "Sessions" },
          { v: "revocations", label: "Revocations" },
        ]}
      />
      {err && <p className="mb-3 text-[14px] text-bad">{err}</p>}
      <div className="overflow-x-auto rounded-2xl border border-line bg-panel">
        <table className="w-full text-[14px]">
          <thead>
            <tr className="border-b border-line bg-panel-2 text-left text-[11px] uppercase tracking-[0.06em] text-muted">
              <th className="w-20 px-5 py-2.5 font-medium">Time</th>
              <th className="px-5 py-2.5 font-medium">Event</th>
              <th className="px-5 py-2.5 font-medium">Agent</th>
              <th className="px-5 py-2.5 font-medium">Device</th>
              <th className="hidden px-5 py-2.5 font-medium lg:table-cell">Detail</th>
            </tr>
          </thead>
          <tbody>
            {!rows ? (
              <tr><td colSpan={5} className="px-5 py-8 text-center text-muted">Reading Arkiv…</td></tr>
            ) : shown.length === 0 ? (
              <tr><td colSpan={5} className="px-5 py-8 text-center text-muted">Nothing yet.</td></tr>
            ) : (
              shown.map((r) => {
                const ev = event(r, !!orgKey);
                return (
                  <tr key={r.key} className="border-t border-line first:border-t-0 hover:bg-panel-2">
                    <td className="mono px-5 py-3 text-[13px] text-muted" title={new Date(r.ts).toLocaleString()}>
                      <a href={links.arkivEntity(r.key)} target="_blank" rel="noreferrer" className="hover:text-accent">
                        {new Date(r.ts).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}
                      </a>
                    </td>
                    <td className="px-5 py-3">{ev.label}</td>
                    <td className="mono px-5 py-3">{r.agent || "—"}</td>
                    <td className="mono px-5 py-3">{r.resource.replace(`.${org}`, "")}</td>
                    <td className={cx("mono hidden max-w-[260px] truncate px-5 py-3 text-[13px] text-muted lg:table-cell")} title={ev.detail}>{ev.detail}</td>
                  </tr>
                );
              })
            )}
          </tbody>
        </table>
      </div>
      <p className="mt-4 text-[13px] text-muted">
        {!orgKey && (
          <>
            Commands and paths are encrypted. <Link href={`/org/${org}/settings`} className="font-medium text-accent hover:underline">Import the org key</Link> to read them.{" "}
          </>
        )}
        Every row is a public Arkiv record (click its time).{" "}
        <button onClick={() => setShowQuery(!showQuery)} className="font-medium text-accent hover:underline">
          {showQuery ? "Hide query" : "Verify it yourself"}
        </button>
      </p>
      {showQuery && <pre className="mono mt-3 overflow-x-auto whitespace-pre-wrap break-all rounded-xl bg-panel-2 p-4 text-[12px] text-muted">{runnableQuery(org)}</pre>}
      {writers.length > 0 && showQuery && (
        <p className="mt-2 text-[12px] text-muted">
          Only records written by your devices are shown:{" "}
          {writers.map((w) => (
            <span key={w} className="mr-2"><Ext href={links.arkivAddr(w)}>{w.slice(0, 8)}…</Ext></span>
          ))}
        </p>
      )}
    </>
  );
}

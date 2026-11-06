"use client";
import { ArrowRight, Ban, ChevronDown, ChevronRight, FileText, Play, Plus, Zap } from "lucide-react";
import Link from "next/link";
import { useCallback, useEffect, useMemo, useState } from "react";
import { GrantDrawer, RevokeTimeline } from "@/components/Access";
import { Button, ago, cx, fmtLeft, useNow } from "@/components/ui";
import { type Row, orgFeed } from "@/lib/arkiv";
import type { GrantNode, ResourceNode, ServiceNode } from "@/lib/ens";
import { useMeshy } from "@/lib/store";

type Target = ResourceNode | ServiceNode;
type Line = { g: GrantNode; t: Target; kind: "port" | "folders" };

const short = (name: string, org: string) => name.replace(`.${org}`, "");

function scope(l: Line) {
  if (l.kind === "port") {
    const s = l.t as ServiceNode;
    return `${s.proto} · port ${s.target.split(":").pop()}`;
  }
  return l.g.policySet ? "folders · sealed policy" : "folders";
}

function AccessLine({ l, org, onRevoke, onRegrant, canWrite }: { l: Line; org: string; onRevoke: () => void; onRegrant: () => void; canWrite: boolean }) {
  const now = useNow();
  const left = l.g.expiry - now / 1000;
  const live = l.g.status === "live" && left > 0;
  const total = 4 * 3600;
  return (
    <div className={cx("group grid grid-cols-[minmax(0,1fr)_auto] items-center gap-4 py-3.5", !live && "opacity-50")}>
      <div className="min-w-0">
        <div className="flex flex-wrap items-baseline gap-x-2 text-[15px]">
          <span className="mono text-warn">{l.g.agent}</span>
          <ArrowRight size={13} className="translate-y-0.5 text-muted" />
          <span className="mono">{short(l.t.name, org)}</span>
          <span className="text-xs text-muted">{scope(l)}</span>
        </div>
        {live && (
          <div className="mt-2 h-1 w-56 overflow-hidden rounded-full bg-panel-2">
            <div className={cx("h-full", left < 300 ? "bg-warn" : "bg-accent")} style={{ width: `${Math.min(100, (left / total) * 100)}%` }} />
          </div>
        )}
      </div>
      <div className="flex items-center gap-3">
        <span className={cx("mono w-24 text-right text-sm tabular-nums", live ? (left < 300 ? "text-warn" : "text-fg") : "text-muted")}>{live ? fmtLeft(left) : l.g.status}</span>
        {live ? (
          <Button kind="danger" disabled={!canWrite} onClick={onRevoke}>
            <Zap size={13} /> Revoke
          </Button>
        ) : (
          <Button kind="ghost" disabled={!canWrite} onClick={onRegrant}>
            Re-grant
          </Button>
        )}
      </div>
    </div>
  );
}

function sentence(r: Row, org: string): { icon: React.ReactNode; text: React.ReactNode } {
  const who = <span className="mono text-warn">{r.agent || "someone"}</span>;
  const where = <span className="mono">{short(r.resource, org)}</span>;
  const path = r.event?.path ?? r.path;
  switch (r.kind) {
    case "blocked":
      if (r.reason.startsWith("MESHY_")) {
        const code = r.reason.split(" ")[0];
        const g = r.reason.match(/grant=(\S+)/)?.[1];
        return { icon: <Ban size={14} className="text-bad" />, text: <>{who} was refused {g ? <>a connection to <span className="mono">{short(g.split(".").slice(1).join("."), org)}</span></> : <>on {where}</>} <span className="text-muted">({code === "MESHY_NO_GRANT" ? "no live grant" : code})</span></> };
      }
      return { icon: <Ban size={14} className="text-bad" />, text: <>{who} was blocked on {where}{path ? <> — <span className="mono">{path.split("/").slice(-2).join("/")}</span></> : null} <span className="text-muted">({r.reason})</span></> };
    case "denied":
      return { icon: <Ban size={14} className="text-bad" />, text: <>{who} was refused on {where} <span className="text-muted">— no live grant</span></> };
    case "session.revoke":
      return { icon: <Zap size={14} className="text-warn" />, text: <>{who}&apos;s session on {where} was killed by a revocation <span className="text-muted">· block {r.block} · {r.latencyMs}ms</span></> };
    case "session.start":
      return { icon: <Play size={14} className="text-accent" />, text: <>{who} connected to {where}</> };
    case "session.end":
      return { icon: <Play size={14} className="text-muted" />, text: <>{who} disconnected from {where}</> };
    default:
      return { icon: <FileText size={14} className="text-accent-2" />, text: <>{who} touched {r.nOps} file{r.nOps === 1 ? "" : "s"} on {where}</> };
  }
}

export default function AccessPage() {
  const { tree, org, orgKey, account, error, loading } = useMeshy();
  const now = useNow(5000);
  const [drawer, setDrawer] = useState<{ resource?: string; agent?: string } | null>(null);
  const [revoking, setRevoking] = useState<Line>();
  const [showEnded, setShowEnded] = useState(false);
  const [feed, setFeed] = useState<Row[]>();

  const lines = useMemo<Line[]>(() => {
    if (!tree) return [];
    const devs = tree.networks.flatMap((n) => n.resources);
    return [
      ...devs.flatMap((d) => d.grants.map((g) => ({ g, t: d as Target, kind: "folders" as const }))),
      ...devs.flatMap((d) => d.services).flatMap((s) => s.grants.map((g) => ({ g, t: s as Target, kind: "port" as const }))),
    ].sort((a, b) => b.g.expiry - a.g.expiry);
  }, [tree]);
  const live = lines.filter((l) => l.g.status === "live" && l.g.expiry > now / 1000);
  const ended = lines.filter((l) => !live.includes(l));
  const writers = useMemo(() => tree?.networks.flatMap((n) => n.resources).map((r) => r.addr) ?? [], [tree]);

  const load = useCallback(async () => {
    try {
      setFeed((await orgFeed(org, orgKey, 30, writers)).filter((r) => r.kind !== "session.end").slice(0, 8));
    } catch {}
  }, [org, orgKey, writers]);
  useEffect(() => {
    if (!writers.length) return;
    load();
    const t = setInterval(load, 6000);
    return () => clearInterval(t);
  }, [load, writers.length]);

  if (error) return <div className="mx-auto max-w-3xl text-bad">{error}</div>;
  if (!tree) return <div className="mx-auto max-w-3xl text-muted">{loading ? "Reading the org from ENSv2 on Sepolia…" : ""}</div>;
  const canWrite = !!account;
  const devices = tree.networks.flatMap((n) => n.resources);
  const services = devices.flatMap((d) => d.services);

  return (
    <div className="mx-auto max-w-3xl space-y-12 py-4">
      <section>
        <div className="mb-1 flex items-end justify-between">
          <div>
            <h1 className="text-2xl font-semibold">Access right now</h1>
            <p className="mt-1 text-sm text-muted">
              {live.length === 0 ? "No agent can reach anything." : `${live.length} agent grant${live.length > 1 ? "s" : ""} live.`} Every line is an ENS name that expires on its own; revoking
              cuts live sessions in the next block.
            </p>
          </div>
          <Button kind="primary" onClick={() => setDrawer({})} disabled={!tree.agents.length || !canWrite}>
            <Plus size={14} /> Give access
          </Button>
        </div>
        {!canWrite && <p className="text-xs text-muted">Read-only: connect a wallet that is the org owner or a delegated admin to give or revoke access.</p>}
        <div className="mt-4 divide-y divide-line border-y border-line">
          {live.length === 0 ? (
            <div className="py-8 text-center text-sm text-muted">Nothing live. Give an agent access to a device&apos;s folders or to a single service port.</div>
          ) : (
            live.map((l) => (
              <AccessLine key={l.g.name} l={l} org={org} canWrite={canWrite} onRevoke={() => setRevoking(l)} onRegrant={() => setDrawer({ resource: l.t.name, agent: l.g.agent })} />
            ))
          )}
        </div>
        {ended.length > 0 && (
          <div className="mt-3">
            <button onClick={() => setShowEnded(!showEnded)} className="flex items-center gap-1 text-xs text-muted hover:text-fg">
              {showEnded ? <ChevronDown size={13} /> : <ChevronRight size={13} />} {ended.length} ended (expired or revoked)
            </button>
            {showEnded && (
              <div className="divide-y divide-line">
                {ended.map((l) => (
                  <AccessLine key={l.g.name} l={l} org={org} canWrite={canWrite} onRevoke={() => {}} onRegrant={() => setDrawer({ resource: l.t.name, agent: l.g.agent })} />
                ))}
              </div>
            )}
          </div>
        )}
      </section>

      <section>
        <div className="mb-3 flex items-end justify-between">
          <h2 className="text-lg font-semibold">Happening now</h2>
          <Link href={`/org/${org}/activity`} className="text-xs text-accent-2 hover:underline">
            Full audit log on Arkiv →
          </Link>
        </div>
        {!feed ? (
          <div className="text-sm text-muted">Reading Arkiv…</div>
        ) : feed.length === 0 ? (
          <div className="text-sm text-muted">Quiet.</div>
        ) : (
          <ul className="space-y-2.5">
            {feed.map((r) => {
              const s = sentence(r, org);
              return (
                <li key={r.key} className="flex items-start gap-3 text-sm">
                  <span className="mt-0.5">{s.icon}</span>
                  <span className="min-w-0 flex-1">{s.text}</span>
                  <span className="shrink-0 text-xs text-muted">{ago(r.ts, now)}</span>
                </li>
              );
            })}
          </ul>
        )}
      </section>

      <section className="text-sm text-muted">
        <Link href={`/org/${org}/directory`} className="hover:text-fg">
          {tree.networks.length} networks · {devices.length} devices · {services.length} services · {tree.agents.length} agents — open the directory →
        </Link>
      </section>

      {drawer && <GrantDrawer preset={drawer} onClose={() => setDrawer(null)} />}
      {revoking && <RevokeTimeline grant={revoking.g} res={revoking.t} onDone={() => setRevoking(undefined)} />}
    </div>
  );
}

"use client";
import Link from "next/link";
import { useCallback, useEffect, useMemo, useState } from "react";
import { GrantDrawer, RevokeTimeline } from "@/components/Access";
import { Button, Card, Empty, Modal, PageHeader, Ring, Row, Tabs, ago, fmtLeft, useNow } from "@/components/ui";
import { type Row as Ev, orgFeed } from "@/lib/arkiv";
import type { GrantNode, ResourceNode, ServiceNode } from "@/lib/ens";
import { useMeshy } from "@/lib/store";

type Target = ResourceNode | ServiceNode;
type Line = { g: GrantNode; t: Target; kind: "port" | "folders" };

const short = (name: string, org: string) => name.replace(`.${org}`, "");
const plural = (n: number, w: string) => `${n === 1 ? "one" : n} ${w}${n === 1 ? "" : "s"}`;

function GrantLine({ l, org, canWrite, onRevoke, onRegrant }: { l: Line; org: string; canWrite: boolean; onRevoke: () => void; onRegrant: () => void }) {
  const now = useNow();
  const left = l.g.expiry - now / 1000;
  const live = l.g.status === "live" && left > 0;
  const scope = l.kind === "port" ? `${(l.t as ServiceNode).proto} port` : "Folders";
  return (
    <Row>
      {live ? <Ring until={l.g.expiry} /> : <span className="h-[34px] w-[34px] shrink-0 rounded-full border border-dashed border-line-strong" />}
      <div className="min-w-0 flex-1">
        <div className="mono truncate text-[15px] font-semibold">
          {l.g.agent} <span className="font-normal text-muted">→</span> {short(l.t.name, org)}
        </div>
        <div className="mt-0.5 text-[13px] text-muted">{live ? scope : `${scope} · ${l.g.status}`}</div>
      </div>
      {live ? (
        <>
          <div className="text-right">
            <div className="mono tnum font-semibold">{fmtLeft(left)}</div>
            <div className="text-[12px] text-muted">until {new Date(l.g.expiry * 1000).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}</div>
          </div>
          <Button kind="danger" className="h-8 px-3.5" disabled={!canWrite} title={canWrite ? undefined : "Connect the owner's or an admin's wallet"} onClick={onRevoke}>
            Revoke
          </Button>
        </>
      ) : (
        <Button className="h-8 px-3.5" disabled={!canWrite} onClick={onRegrant}>
          Grant again
        </Button>
      )}
    </Row>
  );
}

/** One plain sentence per audit event, plus what to grant if it was a refusal. */
function sentence(r: Ev, org: string): { text: React.ReactNode; grant?: { agent: string; resource: string } } {
  const who = <b className="mono font-semibold">{r.agent || "someone"}</b>;
  const where = <span className="mono">{short(r.resource, org)}</span>;
  if (r.kind === "blocked" || r.kind === "denied") {
    const g = r.reason.match(/grant=(\S+)/)?.[1];
    const target = g ? g.split(".").slice(1).join(".") : r.resource;
    return { text: <>{who} was refused on <span className="mono">{short(target, org)}</span></>, grant: r.agent ? { agent: r.agent, resource: target } : undefined };
  }
  if (r.kind === "session.revoke") return { text: <>{who}&apos;s session on {where} ended when access was revoked</> };
  if (r.kind === "session.start") return { text: <>{who} opened a session on {where}</> };
  if (r.kind === "session.end") return { text: <>{who} closed a session on {where}</> };
  return { text: <>{who} touched {r.nOps} file{r.nOps === 1 ? "" : "s"} on {where}</> };
}

export default function AccessPage() {
  const { tree, org, orgKey, account, error, loading } = useMeshy();
  const now = useNow(5000);
  const [tab, setTab] = useState<"live" | "ended">("live");
  const [drawer, setDrawer] = useState<{ resource?: string; agent?: string } | null>(null);
  const [confirming, setConfirming] = useState<Line>();
  const [revoking, setRevoking] = useState<Line>();
  const [feed, setFeed] = useState<Ev[]>();

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
      setFeed((await orgFeed(org, orgKey, 30, writers)).filter((r) => r.kind !== "session.end" && r.schema !== "meshy.audit-chunk").slice(0, 5));
    } catch {}
  }, [org, orgKey, writers]);
  useEffect(() => {
    if (!writers.length) return;
    load();
    const t = setInterval(load, 6000);
    return () => clearInterval(t);
  }, [load, writers.length]);

  // ?give=<agent> (from Approve and the agent page) opens the sheet
  useEffect(() => {
    const a = new URLSearchParams(window.location.search).get("give");
    if (a && tree) setDrawer({ agent: a });
  }, [tree]);

  const canWrite = !!account;
  if (error) return <p className="text-[14px] text-bad">{error}</p>;
  if (!tree) return <p className="pt-14 text-[14px] text-muted">{loading ? "Reading the org from ENS…" : ""}</p>;

  const agents = new Set(live.map((l) => l.g.agent)).size;
  const sub = live.length === 0 ? "No agent can reach anything right now." : `${agents === 1 ? "One agent" : `${agents} agents`} can reach ${plural(live.length, "thing")} right now.`;

  return (
    <>
      <PageHeader
        title="Access"
        sub={sub}
        action={
          <Button kind="primary" onClick={() => setDrawer({})} disabled={!tree.agents.length || !canWrite} title={canWrite ? undefined : "Connect a wallet first"}>
            Give access
          </Button>
        }
      />

      <Tabs value={tab} onChange={setTab} tabs={[{ v: "live", label: "Live", count: live.length }, { v: "ended", label: "Ended", count: ended.length }]} />
      <Card>
        {(tab === "live" ? live : ended).length === 0 ? (
          <Empty>{tab === "live" ? "Nothing live." : "Nothing has ended yet."}</Empty>
        ) : (
          (tab === "live" ? live : ended).map((l) => (
            <GrantLine key={l.g.name} l={l} org={org} canWrite={canWrite} onRevoke={() => setConfirming(l)} onRegrant={() => setDrawer({ resource: l.t.name, agent: l.g.agent })} />
          ))
        )}
      </Card>
      {!canWrite && <p className="mt-3 text-[13px] text-muted">Read-only. Connect the owner&apos;s or a delegated admin&apos;s wallet to give or revoke access.</p>}

      <Card title="Recent activity" actions={<Link href={`/org/${org}/activity`} className="text-[13px] font-medium text-accent hover:underline">View all</Link>} className="mt-10">
        {!feed ? (
          <Empty>Reading Arkiv…</Empty>
        ) : feed.length === 0 ? (
          <Empty>Quiet.</Empty>
        ) : (
          feed.map((r) => {
            const s = sentence(r, org);
            return (
              <Row key={r.key}>
                <span className="min-w-0 flex-1">{s.text}</span>
                {s.grant && canWrite && (
                  <button onClick={() => setDrawer(s.grant!)} className="text-[13px] font-medium text-accent hover:underline">
                    Grant
                  </button>
                )}
                <span className="shrink-0 text-[12px] text-muted">{ago(r.ts, now).replace(" ago", "")}</span>
              </Row>
            );
          })
        )}
      </Card>

      {drawer && <GrantDrawer preset={drawer} onClose={() => setDrawer(null)} />}
      {confirming && (
        <Modal onClose={() => setConfirming(undefined)}>
          <h2 className="text-[20px] font-semibold tracking-[-0.015em]">
            Revoke {confirming.g.agent} on {short(confirming.t.name, org)}?
          </h2>
          <p className="mt-2 text-[14px] leading-relaxed text-muted">Any open session ends in the next block. This takes one transaction.</p>
          <div className="mt-6 flex justify-end gap-2">
            <Button onClick={() => setConfirming(undefined)}>Cancel</Button>
            <Button
              kind="primary"
              className="bg-bad text-white"
              onClick={() => {
                setRevoking(confirming);
                setConfirming(undefined);
              }}
            >
              Revoke
            </Button>
          </div>
        </Modal>
      )}
      {revoking && <RevokeTimeline grant={revoking.g} res={revoking.t} onDone={() => setRevoking(undefined)} />}
    </>
  );
}

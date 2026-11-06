"use client";
import { useState } from "react";
import { GrantDrawer } from "@/components/Access";
import { InviteSheet } from "@/components/Agents";
import { AddSheet } from "@/components/Devices";
import { Badge, Button, Card, Name, OnChain, PageHeader, Row, Short, cx, useNow } from "@/components/ui";
import type { AgentNode, GrantNode, ResourceNode, ServiceNode } from "@/lib/ens";
import { useMeshy } from "@/lib/store";

type Sel = { kind: "device"; d: ResourceNode } | { kind: "service"; s: ServiceNode } | { kind: "agent"; a: AgentNode };

const tilde = (p: string) => p.replace(/^\/Users\/[^/]+/, "~").replace(/^\/home\/[^/]+/, "~");

export default function DirectoryPage() {
  const { tree, org, account } = useMeshy();
  const now = useNow(10000) / 1000;
  // ?n=<full ENS name> (from the Mac menu bar) selects that item
  const [sel, setSel] = useState<string | undefined>(() => (typeof window === "undefined" ? undefined : new URLSearchParams(window.location.search).get("n") ?? undefined));
  // ?add=<meshy-join code> (from "Share this Mac…" in the Mac app) opens Add device, prefilled
  const [addCode] = useState(() => (typeof window === "undefined" ? "" : (new URLSearchParams(window.location.search).get("add") ?? "").trim()));
  const [adding, setAdding] = useState(!!addCode);
  const [inviting, setInviting] = useState(false);
  const [giving, setGiving] = useState<{ resource?: string; agent?: string } | null>(null);
  if (!tree) return <p className="pt-14 text-[14px] text-muted">Reading the org from ENS…</p>;

  const devices = tree.networks.flatMap((n) => n.resources);
  const services = devices.flatMap((d) => d.services);
  const short = (n: string) => n.replace(`.${org}`, "");
  const liveOn = (gs: GrantNode[]) => gs.filter((g) => g.status === "live" && g.expiry > now);

  const pick = (): Sel | undefined => {
    const key = sel ?? devices[0]?.name;
    const d = devices.find((x) => x.name === key);
    if (d) return { kind: "device", d };
    const s = services.find((x) => x.name === key);
    if (s) return { kind: "service", s };
    const a = tree.agents.find((x) => x.name === key);
    if (a) return { kind: "agent", a };
  };
  const cur = pick();

  const item = (name: string, label: string, indent = false) => (
    <button
      key={name}
      onClick={() => setSel(name)}
      className={cx("mono block w-full rounded-lg px-3 py-1.5 text-left text-[14px]", indent && "pl-6 text-muted", (sel ?? devices[0]?.name) === name ? "bg-soft font-medium text-accent" : "hover:bg-panel-2")}
    >
      {label}
    </button>
  );

  return (
    <>
      <PageHeader
        title="Directory"
        sub="Your devices, services and agents."
        action={
          <div className="flex gap-2">
            <Button onClick={() => setInviting(true)}>Invite agent</Button>
            <Button kind="primary" onClick={() => setAdding(true)} disabled={!account} title={account ? undefined : "Connect a wallet first"}>
              Add device
            </Button>
          </div>
        }
      />

      <div className="grid gap-8 md:grid-cols-[200px_minmax(0,1fr)]">
        <nav aria-label="Directory">
          {tree.networks.map((n) => (
            <div key={n.name} className="mb-5">
              <div className="px-3 pb-1 text-[11px] font-medium uppercase tracking-[0.08em] text-muted">{n.label}</div>
              {n.resources.map((d) => [item(d.name, d.label), ...d.services.map((s) => item(s.name, s.label, true))])}
              {n.resources.length === 0 && <div className="px-3 py-1.5 text-[13px] text-muted">No devices</div>}
            </div>
          ))}
          <div className="px-3 pb-1 text-[11px] font-medium uppercase tracking-[0.08em] text-muted">agents</div>
          {tree.agents.map((a) => item(a.name, a.label))}
          {tree.agents.length === 0 && <div className="px-3 py-1.5 text-[13px] text-muted">None yet</div>}
        </nav>

        <section className="min-w-0 space-y-6">
          {!cur && <p className="text-[14px] text-muted">Nothing here yet. Add a device to get started.</p>}

          {cur?.kind === "device" && (
            <>
              <Head
                name={cur.d.name}
                sub={[cur.d.kind, cur.d.meta.model, cur.d.meta.location].filter(Boolean).join(" · ") || "Device"}
                right={account && <Button className="h-8 px-3.5" onClick={() => setGiving({ resource: cur.d.name })}>Give access</Button>}
              />
              <Command text={`meshy ssh ${cur.d.name}`} />
              <Card>
                <Fact k="Shared folder">{cur.d.roots.map(tilde).join(", ") || "—"}</Fact>
                <Fact k="Who can reach it">
                  {liveOn(cur.d.grants).length ? liveOn(cur.d.grants).map((g) => <span key={g.name} className="mono mr-2">{g.agent}</span>) : <span className="font-sans text-muted">Nobody right now</span>}
                </Fact>
                {cur.d.services.map((s) => (
                  <Fact key={s.name} k="Service">
                    {s.label} <span className="font-sans text-muted">· {s.proto} {s.target}</span>
                  </Fact>
                ))}
                <Fact k="Device key"><Short v={cur.d.addr} /></Fact>
              </Card>
              <OnChain name={cur.d.name} addr={cur.d.addr} />
            </>
          )}

          {cur?.kind === "service" && (
            <>
              <Head name={cur.s.name} sub={`${cur.s.proto} → ${cur.s.target}${cur.s.desc ? ` · ${cur.s.desc}` : ""}`} right={account && <Button className="h-8 px-3.5" onClick={() => setGiving({ resource: cur.s.name })}>Give access</Button>} />
              <Command text={`meshy connect ${cur.s.name}`} />
              <Card>
                <Fact k="On device">{short(cur.s.device)}</Fact>
                <Fact k="Who can reach it">
                  {liveOn(cur.s.grants).length ? liveOn(cur.s.grants).map((g) => <span key={g.name} className="mono mr-2">{g.agent}</span>) : <span className="font-sans text-muted">Nobody right now</span>}
                </Fact>
              </Card>
              <p className="text-[13px] text-muted">A grant on a service opens this one port. It never gives a shell.</p>
            </>
          )}

          {cur?.kind === "agent" && (
            <>
              <Head name={cur.a.name} sub={cur.a.worldid ? "Human-backed with World ID" : "No World ID attestation, so devices will refuse it"} badge={cur.a.worldid ? undefined : <Badge tone="bad">No World ID</Badge>} right={account && <Button className="h-8 px-3.5" onClick={() => setGiving({ agent: cur.a.label })}>Give access</Button>} />
              <Card title="Can reach now">
                {(() => {
                  const gs = [...devices, ...services].flatMap((r) => liveOn(r.grants).filter((g) => g.agent === cur.a.label).map((g) => ({ g, r })));
                  return gs.length ? (
                    gs.map(({ g, r }) => (
                      <Row key={g.name}>
                        <span className="mono flex-1">{short(r.name)}</span>
                        <span className="mono tnum text-muted">{Math.max(0, Math.round((g.expiry - now) / 60))} min left</span>
                      </Row>
                    ))
                  ) : (
                    <Row><span className="text-muted">Nothing right now</span></Row>
                  );
                })()}
              </Card>
              <Card title="Details">
                <Fact k="Address"><Short v={cur.a.addr} /></Fact>
                <Fact k="Approved by"><Short v={cur.a.owner} /></Fact>
              </Card>
              <OnChain name={cur.a.name} addr={cur.a.addr} />
            </>
          )}
        </section>
      </div>

      {adding && <AddSheet initialCode={addCode} onClose={() => setAdding(false)} />}
      {inviting && <InviteSheet onClose={() => setInviting(false)} />}
      {giving && <GrantDrawer preset={giving} onClose={() => setGiving(null)} />}
    </>
  );
}

function Head({ name, sub, badge, right }: { name: string; sub: string; badge?: React.ReactNode; right?: React.ReactNode }) {
  const [first, ...rest] = name.split(".");
  return (
    <div className="flex flex-wrap items-start justify-between gap-3">
      <div>
      <h2 className="mono flex flex-wrap items-center gap-2 text-[20px] font-semibold">
        <span>
          {first}
          <span className="font-normal text-muted">.{rest.join(".")}</span>
        </span>
        {badge}
      </h2>
      <p className="mt-1 text-[14px] text-muted">{sub}</p>
      </div>
      {right}
    </div>
  );
}

function Command({ text }: { text: string }) {
  return (
    <div className="flex items-center rounded-xl bg-panel-2 px-4 py-3">
      <Name name={text} className="flex-1 justify-between" />
    </div>
  );
}

function Fact({ k, children }: { k: string; children: React.ReactNode }) {
  return (
    <Row>
      <span className="w-36 shrink-0 text-muted">{k}</span>
      <span className="mono min-w-0 flex-1 break-all">{children}</span>
    </Row>
  );
}

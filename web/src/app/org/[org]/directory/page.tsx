"use client";
import { useState } from "react";
import { GrantDrawer } from "@/components/Access";
import { InviteSheet } from "@/components/Agents";
import { AddSheet } from "@/components/Devices";
import { Badge, Button, Card, Name, OnChain, PageHeader, Row, Short, cx, useNow } from "@/components/ui";
import { type AgentNode, type GrantNode, type ResourceNode, type ServiceNode, labelId, recordCalls, registryAbi, resolverAbi, send } from "@/lib/ens";
import { useMeshy } from "@/lib/store";

type Sel = { kind: "device"; d: ResourceNode } | { kind: "service"; s: ServiceNode } | { kind: "agent"; a: AgentNode };

const tilde = (p: string) => p.replace(/^\/Users\/[^/]+/, "~").replace(/^\/home\/[^/]+/, "~");

export default function DirectoryPage() {
  const { tree, org, wallet, client, refresh, toast, canManage } = useMeshy();
  const [removingDev, setRemovingDev] = useState(false);
  async function removeDevice(d: ResourceNode) {
    if (!wallet) return;
    if (!window.confirm(`Remove ${d.name}? Every grant on it stops working and its seat is freed. This takes one transaction.`)) return;
    setRemovingDev(true);
    try {
      await send(wallet, client, { address: d.parentRegistry, abi: registryAbi, functionName: "unregister", args: [labelId(d.label)] });
      toast(`Removed ${d.label}`, "ok");
      refresh();
    } catch (e) {
      toast(e instanceof Error ? e.message.split("\n")[0] : String(e), "err");
    } finally {
      setRemovingDev(false);
    }
  }
  const now = useNow(10000) / 1000;
  // ?n=<full ENS name> (from the Mac menu bar) selects that item
  const [sel, setSel] = useState<string | undefined>(() => (typeof window === "undefined" ? undefined : new URLSearchParams(window.location.search).get("n") ?? undefined));
  // ?add=<meshy-join code> (from "Share this Mac…" in the Mac app) opens Add device, prefilled
  const [addCode] = useState(() => (typeof window === "undefined" ? "" : (new URLSearchParams(window.location.search).get("add") ?? "").trim()));
  const [adding, setAdding] = useState(!!addCode);
  const [inviting, setInviting] = useState(false);
  const [giving, setGiving] = useState<{ resource?: string; agent?: string } | null>(null);
  // ?roots=<json> (from the Mac app: the folders that device shares now) opens the folder editor, prefilled
  const [rootsLink] = useState<string[] | undefined>(() => {
    try {
      const r = typeof window === "undefined" ? null : new URLSearchParams(window.location.search).get("roots");
      return r ? (JSON.parse(r) as string[]).filter((x) => typeof x === "string" && x.startsWith("/")) : undefined;
    } catch {
      return undefined;
    }
  });
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
            {canManage && <Button onClick={() => setInviting(true)}>Invite agent</Button>}
            {canManage && (
              <Button kind="primary" onClick={() => setAdding(true)}>
                Add device
              </Button>
            )}
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
                right={
                  canManage && (
                    <div className="flex gap-2">
                      <Button className="h-8 px-3.5" onClick={() => setGiving({ resource: cur.d.name })}>Give access</Button>
                      <Button kind="danger" className="h-8 px-3.5" disabled={removingDev} onClick={() => removeDevice(cur.d)}>
                        {removingDev ? "Removing…" : "Remove"}
                      </Button>
                    </div>
                  )
                }
              />
              <Command text={`meshy ssh ${cur.d.name}`} />
              <Card>
                <Fact k="Shared folders">
                  <FolderEditor key={cur.d.name} d={cur.d} canManage={canManage} proposed={sel === cur.d.name ? rootsLink : undefined} />
                </Fact>
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
              <Head name={cur.s.name} sub={`${cur.s.proto} → ${cur.s.target}${cur.s.desc ? ` · ${cur.s.desc}` : ""}`} right={canManage && <Button className="h-8 px-3.5" onClick={() => setGiving({ resource: cur.s.name })}>Give access</Button>} />
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
              <Head name={cur.a.name} sub={cur.a.worldid ? "Human-backed with World ID" : "No World ID attestation, so devices will refuse it"} badge={cur.a.worldid ? undefined : <Badge tone="bad">No World ID</Badge>} right={canManage && <Button className="h-8 px-3.5" onClick={() => setGiving({ agent: cur.a.label })}>Give access</Button>} />
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

/** A device's shared folders (meshy.roots). Admins publish changes with one signature; the Mac sharing
 *  the device is the source (This Mac › Shared Folders), and its "Publish" link prefills this. */
function FolderEditor({ d, canManage, proposed }: { d: ResourceNode; canManage: boolean; proposed?: string[] }) {
  const { wallet, client, tree, refresh, toast, connect, account } = useMeshy();
  const [editing, setEditing] = useState(!!proposed);
  const [list, setList] = useState<string[]>(proposed ?? d.roots);
  const [add, setAdd] = useState("");
  const [busy, setBusy] = useState(false);
  const changed = JSON.stringify(list) !== JSON.stringify(d.roots);
  const name = (p: string) => p.split("/").filter(Boolean).pop() ?? p;
  async function publish() {
    if (!wallet) return connect();
    setBusy(true);
    try {
      await send(wallet, client, { address: tree!.resolver, abi: resolverAbi, functionName: "multicall", args: [recordCalls(d.name, { "meshy.roots": JSON.stringify(list) })] });
      toast(`${d.label} shares ${list.length} folder${list.length === 1 ? "" : "s"}`, "ok");
      setEditing(false);
      refresh();
    } catch (e) {
      toast(e instanceof Error ? e.message.split("\n")[0] : String(e), "err");
    } finally {
      setBusy(false);
    }
  }
  if (!editing)
    return (
      <span className="font-sans">
        {d.roots.length === 0 ? <span className="text-muted">None yet</span> : d.roots.map((r) => (
          <span key={r} className="mr-3 inline-flex items-baseline gap-1" title={tilde(r)}>
            <b className="font-medium">{name(r)}</b> <span className="mono text-[12px] text-muted">{tilde(r)}</span>
          </span>
        ))}
        {canManage && (
          <button className="text-[13px] text-accent hover:underline" onClick={() => setEditing(true)}>
            Edit
          </button>
        )}
      </span>
    );
  return (
    <div className="w-full space-y-2 font-sans">
      {proposed && <p className="text-[12px] text-muted">{d.label} changed the folders it shares. Publish so everyone sees the new list.</p>}
      {list.map((r) => (
        <div key={r} className="flex items-center gap-2 text-[13px]">
          <b className="font-medium">{name(r)}</b>
          <span className="mono flex-1 truncate text-[12px] text-muted">{tilde(r)}</span>
          {!d.roots.includes(r) && <span className="text-[11px] text-ok">new</span>}
          <button className="text-[12px] text-muted hover:text-bad" onClick={() => setList(list.filter((x) => x !== r))}>
            Remove
          </button>
        </div>
      ))}
      {d.roots.filter((r) => !list.includes(r)).map((r) => (
        <div key={r} className="flex items-center gap-2 text-[13px] text-muted line-through">
          {name(r)} <span className="mono text-[12px]">{tilde(r)}</span>
        </div>
      ))}
      <div className="flex gap-2">
        <input className={cx("h-8 flex-1 rounded-lg border border-line bg-panel px-2 text-[13px]")} placeholder="/Users/you/Projects (a folder on that device)" value={add} onChange={(e) => setAdd(e.target.value.trim())} />
        <Button className="h-8 px-3" disabled={!add.startsWith("/") || add === "/" || list.includes(add)} onClick={() => (setList([...list, add.replace(/\/+$/, "")]), setAdd(""))}>
          Add
        </Button>
      </div>
      <div className="flex gap-2 pt-1">
        <Button kind="primary" className="h-8 px-3.5" disabled={!changed || list.length === 0 || busy || (!!account && !canManage)} onClick={publish}>
          {busy ? "Publishing…" : "Publish · 1 signature"}
        </Button>
        <Button kind="ghost" className="h-8 px-3" onClick={() => (setList(d.roots), setEditing(false))}>
          Cancel
        </Button>
      </div>
    </div>
  );
}

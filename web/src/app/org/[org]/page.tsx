"use client";
import { Background, Controls, type Edge, Handle, type Node, type NodeProps, Position, ReactFlow } from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import { Bot, Building2, KeyRound, Network, Server } from "lucide-react";
import Link from "next/link";
import { useMemo, useState } from "react";
import { Badge, Card, Countdown, Empty, Name, OnChain, Short, cx, fmtLeft, useNow } from "@/components/ui";
import type { AgentNode, GrantNode, OrgTree, ResourceNode } from "@/lib/ens";
import { useMeshy } from "@/lib/store";

type NData = { kind: "org" | "network" | "resource" | "agent"; title: string; sub?: string; live?: number; obj?: unknown };

function MeshNode({ data, selected }: NodeProps<Node<NData>>) {
  const I = { org: Building2, network: Network, resource: Server, agent: Bot }[data.kind];
  const tone = { org: "text-accent", network: "text-accent-2", resource: "text-fg", agent: "text-warn" }[data.kind];
  return (
    <div className={cx("min-w-48 rounded-xl border bg-panel px-3 py-2.5 shadow-lg", selected ? "border-accent" : "border-line")}>
      <Handle type="target" position={Position.Left} className="!bg-line !border-0" />
      <div className="flex items-center gap-2">
        <I size={15} className={tone} />
        <span className="mono text-[13px] text-fg">{data.title}</span>
        {data.live ? <span className="ml-auto h-2 w-2 rounded-full bg-accent live-dot" title={`${data.live} live grant(s)`} /> : null}
      </div>
      {data.sub && <div className="mt-1 text-[11px] text-muted">{data.sub}</div>}
      <Handle type="source" position={Position.Right} className="!bg-line !border-0" />
    </div>
  );
}

const nodeTypes = { mesh: MeshNode };

function buildGraph(t: OrgTree, now: number) {
  const nodes: Node<NData>[] = [];
  const edges: Edge[] = [];
  const resources = t.networks.flatMap((n) => n.resources);
  const rows = Math.max(resources.length, t.agents.length, t.networks.length, 1);
  const H = 96;
  nodes.push({ id: t.org, type: "mesh", position: { x: 0, y: ((rows - 1) * H) / 2 }, data: { kind: "org", title: t.org, sub: `${t.networks.length} networks · ${t.agents.length} agents` } });
  let ri = 0;
  t.networks.forEach((n, ni) => {
    const y = n.resources.length ? (ri + (n.resources.length - 1) / 2) * H : ni * H;
    nodes.push({ id: n.name, type: "mesh", position: { x: 300, y }, data: { kind: "network", title: n.name, sub: `${n.resources.length} resources` } });
    edges.push({ id: `${t.org}>${n.name}`, source: t.org, target: n.name, style: { stroke: "var(--color-line)" } });
    n.resources.forEach((r) => {
      const live = r.grants.filter((g) => g.status === "live" && g.expiry > now / 1000).length;
      nodes.push({ id: r.name, type: "mesh", position: { x: 620, y: ri * H }, data: { kind: "resource", title: r.name, sub: `${r.roots.join(", ") || "no roots"} · ${live} live`, live, obj: r } });
      edges.push({ id: `${n.name}>${r.name}`, source: n.name, target: r.name, style: { stroke: "var(--color-line)" } });
      ri++;
    });
  });
  t.agents.forEach((a, ai) => {
    nodes.push({ id: a.name, type: "mesh", position: { x: 1000, y: ai * H }, data: { kind: "agent", title: a.name, sub: a.worldid ? "human-backed (World ID)" : "no World ID attestation", obj: a } });
  });
  for (const r of resources) {
    for (const g of r.grants) {
      const agent = t.agents.find((a) => a.label === g.agent);
      if (!agent) continue;
      const live = g.status === "live" && g.expiry > now / 1000;
      edges.push({
        id: g.name,
        source: r.name,
        target: agent.name,
        animated: live,
        label: live ? `grant · ${fmtLeft(g.expiry - now / 1000)}` : g.status,
        labelStyle: { fill: live ? "var(--color-accent)" : "var(--color-muted)", fontSize: 11, fontFamily: "var(--font-mono)" },
        labelBgStyle: { fill: "var(--color-panel)" },
        style: { stroke: live ? "var(--color-accent)" : "var(--color-line)", strokeDasharray: live ? undefined : "4 4", strokeWidth: live ? 2 : 1 },
      });
    }
  }
  return { nodes, edges };
}

function Details({ sel, tree }: { sel?: NData; tree: OrgTree }) {
  if (!sel) return <Empty>Select a node to see its ENS records.</Empty>;
  if (sel.kind === "resource") {
    const r = sel.obj as ResourceNode;
    return (
      <div className="space-y-3 text-sm">
        <Name name={r.name} />
        <OnChain name={r.name} addr={r.addr} />
        <Rec k="addr (daemon)" v={<Short v={r.addr} />} />
        <Rec k="meshy.roots" v={r.roots.join(", ")} />
        <Rec k="meshy.endpoint" v={r.endpoint || "mesh DNS"} />
        <Rec k="meshy.enckey" v={<Short v={r.encKey} n={8} />} />
        <Rec k="meshy.ssh-host" v={<Short v={r.sshHost} n={10} />} />
        <div className="pt-2 text-xs font-semibold uppercase tracking-wider text-muted">Grants</div>
        {r.grants.length ? r.grants.map((g) => <GrantRow key={g.name} g={g} />) : <div className="text-muted">none</div>}
      </div>
    );
  }
  if (sel.kind === "agent") {
    const a = sel.obj as AgentNode;
    return (
      <div className="space-y-3 text-sm">
        <Name name={a.name} />
        <OnChain name={a.name} addr={a.addr} />
        <Rec k="addr" v={<Short v={a.addr} />} />
        <Rec k="meshy.ssh" v={<Short v={a.ssh} n={10} />} />
        <Rec k="approved by" v={<Short v={a.owner} />} />
        <Rec k="World ID" v={a.worldid ? <Badge tone="ok">attested</Badge> : <Badge tone="bad">missing</Badge>} />
        <Rec k="agent-context" v={<span className="text-muted">{a.agentContext}</span>} />
      </div>
    );
  }
  if (sel.kind === "org")
    return (
      <div className="space-y-3 text-sm">
        <Name name={tree.org} />
        <OnChain name={tree.org} addr={tree.owner} />
        <Rec k="owner" v={<Short v={tree.owner} />} />
        <Rec k="meshy.orgkey" v={<Short v={tree.orgKey} n={8} />} />
        <Rec k="meshy.attestor" v={tree.attestors.map((a) => <Short key={a} v={a} />)} />
        <Rec k="resolver" v={<Short v={tree.resolver} />} />
      </div>
    );
  return (
    <div className="space-y-3 text-sm">
      <Name name={sel.title} />
      <OnChain name={sel.title} />
      <div className="text-muted">{sel.sub}</div>
    </div>
  );
}

function Rec({ k, v }: { k: string; v: React.ReactNode }) {
  return (
    <div className="grid grid-cols-[110px_1fr] gap-2">
      <span className="mono text-[11px] text-muted">{k}</span>
      <span className="min-w-0 break-all text-[13px]">{v}</span>
    </div>
  );
}

export function GrantRow({ g }: { g: GrantNode }) {
  const now = useNow();
  const live = g.status === "live" && g.expiry > now / 1000;
  return (
    <div className="flex items-center justify-between gap-2 rounded-lg border border-line px-2.5 py-1.5">
      <span className="mono truncate text-xs">{g.agent}</span>
      {live ? <Countdown until={g.expiry} /> : <Badge tone={g.status === "revoked" ? "bad" : "muted"}>{g.status}</Badge>}
    </div>
  );
}

export default function MapPage() {
  const { tree, error, loading, org } = useMeshy();
  const now = useNow(5000);
  const [sel, setSel] = useState<NData>();
  const graph = useMemo(() => (tree ? buildGraph(tree, now) : { nodes: [], edges: [] }), [tree, now]);
  if (error)
    return (
      <Card title="Could not load org">
        <p className="text-bad">{error}</p>
        <p className="mt-2 text-sm text-muted">
          Is <span className="mono">{org}</span> a Meshy org on Sepolia ENSv2? Create one with <span className="mono">meshy-admin org init</span>.
        </p>
      </Card>
    );
  if (!tree) return <div className="text-muted">{loading ? "Reading the org from ENSv2 on Sepolia…" : ""}</div>;
  const resources = tree.networks.flatMap((n) => n.resources);
  const grants = resources.flatMap((r) => r.grants);
  const live = grants.filter((g) => g.status === "live" && g.expiry > now / 1000);
  return (
    <div className="space-y-5">
      <div className="grid grid-cols-2 gap-3 lg:grid-cols-5">
        {[
          { t: "Networks", v: tree.networks.length, i: Network },
          { t: "Resources", v: resources.length, i: Server },
          { t: "Agents", v: tree.agents.length, i: Bot },
          { t: "Live grants", v: live.length, i: KeyRound, accent: true },
          { t: "Revoked / expired", v: grants.length - live.length, i: KeyRound },
        ].map(({ t, v, i: I, accent }) => (
          <div key={t} className="rounded-xl border border-line bg-panel p-4">
            <div className="flex items-center justify-between text-xs text-muted">
              {t} <I size={14} />
            </div>
            <div className={cx("mt-2 text-2xl font-semibold tabular-nums", accent && "text-accent")}>{v}</div>
          </div>
        ))}
      </div>
      <div className="grid gap-5 xl:grid-cols-[1fr_340px]">
        <div className="h-[560px] overflow-hidden rounded-xl border border-line bg-panel">
          <ReactFlow
            nodes={graph.nodes}
            edges={graph.edges}
            nodeTypes={nodeTypes}
            fitView
            colorMode="dark"
            nodesDraggable
            onNodeClick={(_, n) => setSel(n.data as NData)}
            proOptions={{ hideAttribution: true }}
          >
            <Background color="#1d2128" gap={22} />
            <Controls showInteractive={false} />
          </ReactFlow>
        </div>
        <Card title="Details" actions={<Link className="text-xs text-accent-2 hover:underline" href={`/org/${org}/grants`}>Manage grants →</Link>}>
          <Details sel={sel} tree={tree} />
        </Card>
      </div>
    </div>
  );
}

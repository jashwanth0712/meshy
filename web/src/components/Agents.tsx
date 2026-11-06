"use client";
import { Bot, Copy, KeyRound, UserPlus } from "lucide-react";
import Link from "next/link";
import { useState } from "react";
import { Badge, Button, Card, Empty, Field, Name, OnChain, Short, inputCls } from "@/components/ui";
import { useMeshy } from "@/lib/store";

export function Agents() {
  const { tree, org } = useMeshy();
  const [label, setLabel] = useState("claude-2");
  const [copied, setCopied] = useState(false);
  if (!tree) return null;
  const invite = typeof window !== "undefined" ? `${window.location.origin}/approve?org=${org}&agent=${label}` : "";
  const grantsByAgent = (a: string) => {
    const devs = tree.networks.flatMap((n) => n.resources);
    const targets = [...devs, ...devs.flatMap((d) => d.services)];
    return targets.flatMap((r) => r.grants.filter((g) => g.agent === a && g.status === "live" && g.expiry > Date.now() / 1000).map((g) => ({ g, r })));
  };
  return (
    <div className="grid gap-5 xl:grid-cols-[1fr_380px]">
      <Card title="Agents">
        {tree.agents.length === 0 ? (
          <Empty>No agents yet — invite one.</Empty>
        ) : (
          <div className="space-y-3">
            {tree.agents.map((a) => {
              const gs = grantsByAgent(a.label);
              return (
                <div key={a.name} className="rounded-xl border border-line bg-bg p-4">
                  <div className="flex items-center justify-between gap-3">
                    <div className="flex items-center gap-2">
                      <Bot size={16} className="text-warn" />
                      <Name name={a.name} />
                    </div>
                    {a.worldid ? <Badge tone="ok">human-backed · World ID</Badge> : <Badge tone="bad">no World ID</Badge>}
                  </div>
                  <div className="mt-3 grid grid-cols-[110px_1fr] gap-y-1.5 text-xs">
                    <span className="text-muted">address</span>
                    <Short v={a.addr} />
                    <span className="text-muted">ssh key</span>
                    <Short v={a.ssh.split(" ")[1] ?? ""} n={10} />
                    <span className="text-muted">approved by</span>
                    <Short v={a.owner} />
                    <span className="text-muted">agent-context</span>
                    <span className="text-muted">{a.agentContext || "—"}</span>
                    <span className="text-muted">live grants</span>
                    <span>{gs.length ? gs.map(({ r }) => <span key={r.name} className="mono mr-2">{r.label}</span>) : <span className="text-muted">none</span>}</span>
                  </div>
                  <div className="mt-3 flex items-center justify-between">
                    <OnChain name={a.name} addr={a.addr} />
                    <Link href={`/org/${org}/grants`} className="inline-flex items-center gap-1 text-xs text-accent-2 hover:underline">
                      <KeyRound size={12} /> grant access
                    </Link>
                  </div>
                </div>
              );
            })}
          </div>
        )}
      </Card>
      <Card title={<span className="flex items-center gap-2"><UserPlus size={15} /> Invite an agent</span>}>
        <div className="space-y-4 text-sm">
          <Field label="Agent label" hint={<>Becomes <span className="mono">{label}.agents.{org}</span></>}>
            <input className={inputCls} value={label} onChange={(e) => setLabel(e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, ""))} />
          </Field>
          <div>
            <div className="mb-1.5 text-xs font-medium uppercase tracking-wider text-muted">Invite link</div>
            <div className="flex gap-2">
              <input readOnly className={inputCls} value={invite} />
              <Button
                onClick={() => {
                  navigator.clipboard.writeText(invite);
                  setCopied(true);
                  setTimeout(() => setCopied(false), 1500);
                }}
              >
                <Copy size={14} /> {copied ? "Copied" : "Copy"}
              </Button>
            </div>
          </div>
          <ol className="list-decimal space-y-1.5 pl-5 text-muted">
            <li>
              Give the link to the agent. In Claude Code: <span className="mono text-fg">/meshy:join &lt;link&gt;</span>
            </li>
            <li>The agent generates its keys and sends you back an approval link.</li>
            <li>Open it, verify with World ID, and approve — this writes the agent&apos;s ENS name.</li>
            <li>Grant it folders on a resource.</li>
          </ol>
        </div>
      </Card>
    </div>
  );
}

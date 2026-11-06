"use client";
import { Camera, Network, Plus, Server, Terminal } from "lucide-react";
import { useState } from "react";
import { type Address, getAddress } from "viem";
import { type Step, Steps } from "@/components/Steps";
import { Badge, Button, Card, Empty, Field, Name, OnChain, Short, inputCls } from "@/components/ui";
import { deployRegistry, recordCalls, registryAbi, resolverAbi, send, yearFromNow } from "@/lib/ens";
import { useMeshy } from "@/lib/store";

type Join = { name: string; addr: string; "meshy.enckey": string; "meshy.ssh-host": string; "meshy.roots": string[]; "meshy.endpoint"?: string };

function parseJoin(code: string): Join {
  const b = code.trim().replace(/^meshy-join:/, "").replace(/-/g, "+").replace(/_/g, "/");
  return JSON.parse(atob(b + "=".repeat((4 - (b.length % 4)) % 4))) as Join;
}

export function Devices() {
  const { tree, wallet, client, account, refresh, toast, org } = useMeshy();
  const [mode, setMode] = useState<"none" | "network" | "resource" | "service">("none");
  const [svcForm, setSvcForm] = useState({ device: "", name: "camera", target: "127.0.0.1:8001", proto: "http", desc: "" });
  const [netLabel, setNetLabel] = useState("");
  const [code, setCode] = useState("");
  const [devKind, setDevKind] = useState("server");
  const [devMeta, setDevMeta] = useState({ model: "", location: "", gpus: "" });
  const [steps, setSteps] = useState<Step[]>();
  if (!tree) return null;
  const isOwner = !!account; // roles are enforced on-chain (owner or delegated admin)
  const up = (i: number, p: Partial<Step>) => setSteps((s) => s!.map((x, j) => (j === i ? { ...x, ...p } : x)));
  const fail = (e: unknown) => {
    const msg = e instanceof Error ? e.message.split("\n")[0] : String(e);
    setSteps((s) => s?.map((x) => (x.state === "doing" ? { ...x, state: "fail", detail: msg } : x)));
    toast(msg, "err");
  };

  async function addNetwork() {
    if (!wallet || !account) return;
    setSteps([
      { label: "Deploy the network's registry (VerifiableFactory proxy)", state: "doing" },
      { label: `Register ${netLabel}.${org}`, state: "todo" },
    ]);
    try {
      const reg = await deployRegistry(wallet, client, account);
      up(0, { state: "done", detail: reg });
      up(1, { state: "doing" });
      await send(wallet, client, { address: tree!.orgRegistry, abi: registryAbi, functionName: "register", args: [netLabel, account, reg, tree!.resolver, 0n, yearFromNow()] });
      up(1, { state: "done" });
      toast(`Network ${netLabel}.${org} created`, "ok");
      refresh();
    } catch (e) {
      fail(e);
    }
  }

  async function addService() {
    if (!wallet || !account) return;
    const dev = tree!.networks.flatMap((n) => n.resources).find((r) => r.name === svcForm.device);
    if (!dev) return toast("Pick a device", "err");
    const name = `${svcForm.name}.${dev.name}`;
    setSteps([
      { label: `Deploy ${svcForm.name}'s grants registry`, state: "doing" },
      { label: `Register ${name}`, state: "todo" },
      { label: `Publish meshy.service → ${svcForm.proto}://${svcForm.target}`, state: "todo" },
    ]);
    try {
      const reg = await deployRegistry(wallet, client, account);
      up(0, { state: "done", detail: reg });
      up(1, { state: "doing" });
      await send(wallet, client, { address: dev.registry, abi: registryAbi, functionName: "register", args: [svcForm.name, account, reg, tree!.resolver, 0n, yearFromNow()] });
      up(1, { state: "done" });
      up(2, { state: "doing" });
      const texts: Record<string, string> = { "meshy.service": JSON.stringify({ proto: svcForm.proto, target: svcForm.target, desc: svcForm.desc }) };
      await send(wallet, client, { address: tree!.resolver, abi: resolverAbi, functionName: "multicall", args: [recordCalls(name, texts)] });
      up(2, { state: "done" });
      toast(`Service ${name} published`, "ok");
      refresh();
    } catch (e) {
      fail(e);
    }
  }

  async function addResource() {
    if (!wallet || !account) return;
    let j: Join;
    try {
      j = parseJoin(code);
    } catch {
      toast("Invalid join code", "err");
      return;
    }
    const parent = j.name.split(".").slice(1).join(".");
    const net = tree!.networks.find((n) => n.name === parent);
    if (!net) {
      toast(`Join code is for ${j.name}, but network ${parent} does not exist`, "err");
      return;
    }
    const label = j.name.split(".")[0];
    setSteps([
      { label: `Deploy ${label}'s grants registry`, state: "doing" },
      { label: `Register ${j.name}`, state: "todo" },
      { label: "Publish daemon keys, roots and endpoint (addr, meshy.enckey, meshy.ssh-host, meshy.roots)", state: "todo" },
    ]);
    try {
      const reg = await deployRegistry(wallet, client, account);
      up(0, { state: "done", detail: reg });
      up(1, { state: "doing" });
      await send(wallet, client, { address: net.registry, abi: registryAbi, functionName: "register", args: [label, account, reg, tree!.resolver, 0n, yearFromNow()] });
      up(1, { state: "done" });
      up(2, { state: "doing" });
      const texts: Record<string, string> = { "meshy.enckey": j["meshy.enckey"], "meshy.ssh-host": j["meshy.ssh-host"], "meshy.roots": JSON.stringify(j["meshy.roots"]) };
      if (j["meshy.endpoint"]) texts["meshy.endpoint"] = j["meshy.endpoint"];
      if (devKind) texts["meshy.kind"] = devKind;
      const meta = Object.fromEntries(Object.entries(devMeta).filter(([, v]) => v.trim()));
      if (Object.keys(meta).length) texts["meshy.meta"] = JSON.stringify(meta);
      await send(wallet, client, { address: tree!.resolver, abi: resolverAbi, functionName: "multicall", args: [recordCalls(j.name, texts, getAddress(j.addr) as Address)] });
      up(2, { state: "done" });
      toast(`Resource ${j.name} registered`, "ok");
      refresh();
    } catch (e) {
      fail(e);
    }
  }

  return (
    <div>
      <Card
        title="Devices & services"
        actions={
          <>
            <Button onClick={() => (setMode("network"), setSteps(undefined))} disabled={!isOwner}>
              <Network size={14} /> Add network
            </Button>
            <Button onClick={() => (setMode("service"), setSteps(undefined), setSvcForm((f) => ({ ...f, device: tree.networks[0]?.resources[0]?.name ?? "" })))} disabled={!isOwner}>
              <Camera size={14} /> Add service
            </Button>
            <Button kind="primary" onClick={() => (setMode("resource"), setSteps(undefined))} disabled={!isOwner}>
              <Plus size={14} /> Add device
            </Button>
          </>
        }
      >
        {mode === "network" && (
          <div className="mb-5 space-y-3 rounded-lg border border-line bg-bg p-4">
            <Field label="Network label" hint={<>Creates <span className="mono">{netLabel || "staging"}.{org}</span> with its own registry.</>}>
              <input className={inputCls} value={netLabel} onChange={(e) => setNetLabel(e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, ""))} placeholder="staging" />
            </Field>
            {steps ? <Steps steps={steps} /> : <Button kind="primary" disabled={!netLabel} onClick={addNetwork}>Create network (2 tx)</Button>}
          </div>
        )}
        {mode === "service" && (
          <div className="mb-5 space-y-3 rounded-lg border border-line bg-bg p-4">
            <div className="text-sm text-muted">
              Publish a port on a device under a clean ENS name — e.g. a camera on a Raspberry Pi, an inference API on a GPU box. Agents reach it with{" "}
              <span className="mono">meshy connect &lt;name&gt;</span>, only while they hold a grant on that service.
            </div>
            <div className="grid gap-3 md:grid-cols-2">
              <Field label="Device">
                <select className={inputCls} value={svcForm.device} onChange={(e) => setSvcForm({ ...svcForm, device: e.target.value })}>
                  {tree.networks.flatMap((n) => n.resources).map((r) => (
                    <option key={r.name}>{r.name}</option>
                  ))}
                </select>
              </Field>
              <Field label="Service label" hint={<span className="mono">{svcForm.name || "camera"}.{svcForm.device}</span>}>
                <input className={inputCls} value={svcForm.name} onChange={(e) => setSvcForm({ ...svcForm, name: e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, "") })} />
              </Field>
              <Field label="Target on the device (host:port)">
                <input className={inputCls} value={svcForm.target} onChange={(e) => setSvcForm({ ...svcForm, target: e.target.value })} />
              </Field>
              <Field label="Protocol">
                <select className={inputCls} value={svcForm.proto} onChange={(e) => setSvcForm({ ...svcForm, proto: e.target.value })}>
                  {["http", "tcp", "rtsp", "grpc", "postgres"].map((p) => (
                    <option key={p}>{p}</option>
                  ))}
                </select>
              </Field>
            </div>
            <Field label="Description">
              <input className={inputCls} value={svcForm.desc} onChange={(e) => setSvcForm({ ...svcForm, desc: e.target.value })} placeholder="front door camera" />
            </Field>
            {steps ? <Steps steps={steps} /> : <Button kind="primary" disabled={!svcForm.device || !svcForm.name || !svcForm.target} onClick={addService}>Publish service (3 tx)</Button>}
          </div>
        )}
        {mode === "resource" && (
          <div className="mb-5 space-y-3 rounded-lg border border-line bg-bg p-4">
            <div className="text-sm text-muted">On the server, run meshyd init and paste the join code it prints:</div>
            <pre className="mono rounded-lg border border-line bg-panel p-3 text-xs">
              <Terminal size={12} className="mr-2 inline" />
              meshyd init --name db2.{tree.networks[0]?.label ?? "prod"}.{org} --roots /srv
            </pre>
            <Field label="Join code">
              <textarea className={inputCls + " h-24"} value={code} onChange={(e) => setCode(e.target.value)} placeholder="meshy-join:eyJuYW1lIjoi…" />
            </Field>
            <div className="grid gap-3 md:grid-cols-4">
              <Field label="Kind">
                <select className={inputCls} value={devKind} onChange={(e) => setDevKind(e.target.value)}>
                  {["server", "gpu-server", "raspberry-pi", "laptop", "nas", "camera-hub"].map((k) => (
                    <option key={k}>{k}</option>
                  ))}
                </select>
              </Field>
              <Field label="Model">
                <input className={inputCls} value={devMeta.model} onChange={(e) => setDevMeta({ ...devMeta, model: e.target.value })} placeholder="Raspberry Pi 5" />
              </Field>
              <Field label="GPUs">
                <input className={inputCls} value={devMeta.gpus} onChange={(e) => setDevMeta({ ...devMeta, gpus: e.target.value })} placeholder="2× RTX 4090" />
              </Field>
              <Field label="Location">
                <input className={inputCls} value={devMeta.location} onChange={(e) => setDevMeta({ ...devMeta, location: e.target.value })} placeholder="lab rack 2" />
              </Field>
            </div>
            {steps ? <Steps steps={steps} /> : <Button kind="primary" disabled={!code} onClick={addResource}>Register resource (3 tx)</Button>}
          </div>
        )}
        {tree.networks.length === 0 ? (
          <Empty>No networks yet.</Empty>
        ) : (
          <div className="space-y-7">
            {tree.networks.map((n) => (
              <div key={n.name}>
                <div className="mb-1 flex items-center gap-2 text-xs uppercase tracking-wider text-muted">
                  <Network size={12} /> {n.name} <OnChain name={n.name} />
                </div>
                {n.resources.length === 0 ? (
                  <div className="py-3 text-sm text-muted">No devices.</div>
                ) : (
                  <div className="divide-y divide-line border-y border-line">
                    {n.resources.map((r) => {
                      const live = r.grants.filter((g) => g.status === "live" && g.expiry > Date.now() / 1000).length;
                      const facts = [r.kind, r.meta.model, r.meta.location, r.meta.gpus].filter(Boolean);
                      return (
                        <div key={r.name} className="py-3">
                          <div className="flex items-baseline justify-between gap-3">
                            <div className="flex min-w-0 items-baseline gap-2">
                              <Server size={14} className="translate-y-0.5 text-muted" />
                              <Name name={r.name} />
                              <span className="truncate text-xs text-muted">{facts.join(" · ")}</span>
                            </div>
                            <span className="shrink-0 text-xs text-muted">
                              {live ? <span className="text-accent">{live} live folder grant{live > 1 ? "s" : ""}</span> : "no folder grants"}
                            </span>
                          </div>
                          <div className="mt-1 pl-6 text-xs text-muted">
                            folders {r.roots.join(", ") || "—"} · daemon <Short v={r.addr} n={4} /> · <span className="mono">meshy ssh {r.name}</span>
                          </div>
                          {r.services.map((sv) => {
                            const sl = sv.grants.filter((g) => g.status === "live" && g.expiry > Date.now() / 1000).length;
                            return (
                              <div key={sv.name} className="mt-2 flex items-baseline justify-between gap-3 pl-6 text-sm">
                                <span className="flex min-w-0 items-baseline gap-2">
                                  <Camera size={13} className="translate-y-0.5 text-accent" />
                                  <span className="mono">{sv.label}</span>
                                  <span className="truncate text-xs text-muted">
                                    {sv.proto} → {sv.target}
                                    {sv.desc ? ` · ${sv.desc}` : ""}
                                  </span>
                                </span>
                                <span className="shrink-0 text-xs text-muted">{sl ? <span className="text-accent">{sl} live</span> : "no grants"}</span>
                              </div>
                            );
                          })}
                        </div>
                      );
                    })}
                  </div>
                )}
              </div>
            ))}
          </div>
        )}
      </Card>
    </div>
  );
}

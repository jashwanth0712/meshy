"use client";
import { useState } from "react";
import { type Address, getAddress } from "viem";
import { type Step, Steps } from "@/components/Steps";
import { Button, Field, Seg, Sheet, cx, inputCls } from "@/components/ui";
import { type Call, recordCalls, registryAbi, registryDeployCall, resolverAbi, sendAll, yearFromNow } from "@/lib/ens";
import { useMeshy } from "@/lib/store";

type Join = { name: string; addr: string; "meshy.enckey": string; "meshy.ssh-host": string; "meshy.roots": string[]; "meshy.endpoint"?: string };

function parseJoin(code: string): Join {
  const b = code.trim().replace(/^meshy-join:/, "").replace(/-/g, "+").replace(/_/g, "/");
  return JSON.parse(atob(b + "=".repeat((4 - (b.length % 4)) % 4))) as Join;
}

function joinName(code: string): string | undefined {
  try {
    return parseJoin(code).name;
  } catch {
    return undefined;
  }
}

export type AddMode = "resource" | "service" | "network";

/** Add a device (join code), a service on a device, or a network: one wallet confirmation each. */
export function AddSheet({ onClose, initial = "resource", initialCode = "" }: { onClose: () => void; initial?: AddMode; initialCode?: string }) {
  const { tree, wallet, client, account, refresh, toast, org, connect, canManage } = useMeshy();
  const [mode, setMode] = useState<AddMode>(initial);
  const [svcForm, setSvcForm] = useState({ device: tree?.networks[0]?.resources[0]?.name ?? "", name: "", target: "", proto: "http", desc: "" });
  const [netLabel, setNetLabel] = useState("");
  const [code, setCode] = useState(initialCode);
  const [devKind, setDevKind] = useState("server");
  const [devMeta, setDevMeta] = useState({ model: "", location: "" });
  const [steps, setSteps] = useState<Step[]>();
  const busy = steps?.some((s) => s.state === "doing");
  if (!tree) return null;
  const up = (i: number, p: Partial<Step>) => setSteps((s) => s!.map((x, j) => (j === i ? { ...x, ...p } : x)));
  /** One action = one wallet confirmation where the wallet can batch (else one per step). */
  async function run(labels: string[], build: (reg: Address) => Call[], done: string) {
    if (!wallet || !account) return;
    setSteps(labels.map((label, i) => ({ label, state: i === 0 ? "doing" : "todo" })));
    try {
      const { call, address: reg } = await registryDeployCall(client, account);
      let i = 0;
      const { batched } = await sendAll(wallet, client, [call, ...build(reg)], (h) => {
        up(i, { state: "done", tx: h });
        i++;
        if (i < labels.length) up(i, { state: "doing" });
      });
      const code = await client.getCode({ address: reg });
      if (!code || code === "0x") throw new Error(`the registry isn't at ${reg}: try again`);
      setSteps(labels.map((label, j) => ({ label, state: "done", detail: j === 0 && batched ? "one signature for all steps" : undefined })));
      toast(done, "ok");
      refresh();
    } catch (e) {
      fail(e);
    }
  }

  const fail = (e: unknown) => {
    const msg = e instanceof Error ? e.message.split("\n")[0] : String(e);
    setSteps((s) => s?.map((x) => (x.state === "doing" ? { ...x, state: "fail", detail: msg } : x)));
    toast(msg, "err");
  };

  async function addNetwork() {
    await run(
      ["Deploy the network's registry", `Register ${netLabel}.${org}`],
      (reg) => [{ address: tree!.orgRegistry, abi: registryAbi, functionName: "register", args: [netLabel, account, reg, tree!.resolver, 0n, yearFromNow()] }],
      `Network ${netLabel} created`,
    );
  }

  async function addService() {
    const dev = tree!.networks.flatMap((n) => n.resources).find((r) => r.name === svcForm.device);
    if (!dev) return toast("Pick a device", "err");
    const name = `${svcForm.name}.${dev.name}`;
    const texts: Record<string, string> = { "meshy.service": JSON.stringify({ proto: svcForm.proto, target: svcForm.target, desc: svcForm.desc }) };
    await run(
      [`Deploy ${svcForm.name}'s grants registry`, `Register ${name.replace(`.${org}`, "")}`, `Publish ${svcForm.proto}://${svcForm.target}`],
      (reg) => [
        { address: dev.registry, abi: registryAbi, functionName: "register", args: [svcForm.name, account, reg, tree!.resolver, 0n, yearFromNow()] },
        { address: tree!.resolver, abi: resolverAbi, functionName: "multicall", args: [recordCalls(name, texts)] },
      ],
      `Service ${svcForm.name} published`,
    );
  }

  async function addResource() {
    if (!wallet || !account) return;
    let j: Join;
    try {
      j = parseJoin(code);
    } catch {
      toast("That join code isn't valid", "err");
      return;
    }
    const parent = j.name.split(".").slice(1).join(".");
    const net = tree!.networks.find((n) => n.name === parent);
    if (!net) {
      toast(`The code is for ${j.name}, but network ${parent} doesn't exist`, "err");
      return;
    }
    const label = j.name.split(".")[0];
    const texts: Record<string, string> = { "meshy.enckey": j["meshy.enckey"], "meshy.ssh-host": j["meshy.ssh-host"], "meshy.roots": JSON.stringify(j["meshy.roots"]) };
    if (j["meshy.endpoint"]) texts["meshy.endpoint"] = j["meshy.endpoint"];
    if (devKind) texts["meshy.kind"] = devKind;
    const meta = Object.fromEntries(Object.entries(devMeta).filter(([, v]) => v.trim()));
    if (Object.keys(meta).length) texts["meshy.meta"] = JSON.stringify(meta);
    await run(
      [`Deploy ${label}'s grants registry`, `Register ${j.name.replace(`.${org}`, "")}`, "Publish the device's keys and folders"],
      (reg) => [
        { address: net.registry, abi: registryAbi, functionName: "register", args: [label, account, reg, tree!.resolver, 0n, yearFromNow()] },
        { address: tree!.resolver, abi: resolverAbi, functionName: "multicall", args: [recordCalls(j.name, texts, getAddress(j.addr) as Address)] },
      ],
      `${label} added`,
    );
  }

  const go = mode === "resource" ? addResource : mode === "service" ? addService : addNetwork;
  const ready = mode === "resource" ? !!code.trim() : mode === "service" ? !!svcForm.device && !!svcForm.name && !!svcForm.target : !!netLabel;
  const txs = mode === "network" ? 2 : 3;

  return (
    <Sheet
      title={mode === "resource" ? "Add a device" : mode === "service" ? "Add a service" : "Add a network"}
      onClose={onClose}
      locked={busy}
      footer={
        steps && !busy ? (
          <>
            <span />
            <Button kind="primary" onClick={onClose}>Done</Button>
          </>
        ) : (
          <>
            <span className="text-[13px] text-muted">1 signature ({txs} if your wallet can&apos;t batch)</span>
            <Button kind="primary" disabled={account ? !ready || busy || !canManage : false} onClick={account ? go : connect}>
              {!account ? "Connect a wallet" : canManage ? "Add" : "Owners and admins only"}
            </Button>
          </>
        )
      }
    >
      {steps ? (
        <Steps steps={steps} />
      ) : (
        <>
          <Seg value={mode} onChange={setMode} options={[{ v: "resource", label: "Device" }, { v: "service", label: "Service" }, { v: "network", label: "Network" }]} />
          {mode === "resource" && (
            <>
              {initialCode ? (
                <p className="text-[14px] text-muted">
                  Meshy for Mac filled in the join code for <b className="mono text-fg">{joinName(code) ?? "this device"}</b>. Check it, then add it.
                </p>
              ) : (
                <div>
                  <p className="text-[14px] text-muted">In Meshy for Mac choose This Mac › Share this Mac…, or on the device run this and paste the code it prints:</p>
                  <pre className="mono mt-2 overflow-x-auto rounded-xl bg-panel-2 px-4 py-3 text-[13px]">meshyd init --name &lt;device&gt;.{tree.networks[0]?.label ?? "<network>"}.{org} --roots ~/shared</pre>
                </div>
              )}
              <Field label="Join code">
                <textarea className={cx(inputCls, "h-24")} value={code} onChange={(e) => setCode(e.target.value)} placeholder="meshy-join:…" />
              </Field>
              <div className="grid grid-cols-2 gap-3">
                <Field label="Kind">
                  <select className={inputCls} value={devKind} onChange={(e) => setDevKind(e.target.value)}>
                    {["server", "gpu-server", "raspberry-pi", "laptop", "pc", "phone", "nas", "camera"].map((k) => (
                      <option key={k}>{k}</option>
                    ))}
                  </select>
                </Field>
                <Field label="Location (optional)">
                  <input className={inputCls} value={devMeta.location} onChange={(e) => setDevMeta({ ...devMeta, location: e.target.value })} placeholder="lab rack 2" />
                </Field>
              </div>
            </>
          )}
          {mode === "service" && (
            <>
              <p className="text-[14px] text-muted">Publish one port on a device. An agent with a grant on it reaches that port and nothing else.</p>
              <Field label="Device">
                <select className={inputCls} value={svcForm.device} onChange={(e) => setSvcForm({ ...svcForm, device: e.target.value })}>
                  {tree.networks.flatMap((n) => n.resources).map((r) => (
                    <option key={r.name} value={r.name}>{r.name.replace(`.${org}`, "")}</option>
                  ))}
                </select>
              </Field>
              <Field label="Name">
                <input className={inputCls} value={svcForm.name} placeholder="e.g. camera" onChange={(e) => setSvcForm({ ...svcForm, name: e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, "") })} />
              </Field>
              <div className="grid grid-cols-[1fr_120px] gap-3">
                <Field label="Target (host:port)">
                  <input className={inputCls} value={svcForm.target} placeholder="127.0.0.1:8080 on that device" onChange={(e) => setSvcForm({ ...svcForm, target: e.target.value })} />
                </Field>
                <Field label="Protocol">
                  <select className={inputCls} value={svcForm.proto} onChange={(e) => setSvcForm({ ...svcForm, proto: e.target.value })}>
                    {["http", "tcp", "rtsp", "grpc", "postgres"].map((p) => (
                      <option key={p}>{p}</option>
                    ))}
                  </select>
                </Field>
              </div>
            </>
          )}
          {mode === "network" && (
            <Field label="Name" hint={<span className="mono">{netLabel || "staging"}.{org}</span>}>
              <input className={inputCls} value={netLabel} onChange={(e) => setNetLabel(e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, ""))} placeholder="staging" />
            </Field>
          )}
        </>
      )}
    </Sheet>
  );
}

"use client";
import { useState } from "react";
import { type Address, getAddress } from "viem";
import { type Step, Steps } from "@/components/Steps";
import { Button, Field, Seg, Sheet, cx, inputCls } from "@/components/ui";
import { deployRegistry, recordCalls, registryAbi, resolverAbi, send, yearFromNow } from "@/lib/ens";
import { useMeshy } from "@/lib/store";

type Join = { name: string; addr: string; "meshy.enckey": string; "meshy.ssh-host": string; "meshy.roots": string[]; "meshy.endpoint"?: string };

function parseJoin(code: string): Join {
  const b = code.trim().replace(/^meshy-join:/, "").replace(/-/g, "+").replace(/_/g, "/");
  return JSON.parse(atob(b + "=".repeat((4 - (b.length % 4)) % 4))) as Join;
}

export type AddMode = "resource" | "service" | "network";

/** Add a device (join code), a service on a device, or a network. Same transactions as before. */
export function AddSheet({ onClose, initial = "resource" }: { onClose: () => void; initial?: AddMode }) {
  const { tree, wallet, client, account, refresh, toast, org } = useMeshy();
  const [mode, setMode] = useState<AddMode>(initial);
  const [svcForm, setSvcForm] = useState({ device: tree?.networks[0]?.resources[0]?.name ?? "", name: "camera", target: "127.0.0.1:8001", proto: "http", desc: "" });
  const [netLabel, setNetLabel] = useState("");
  const [code, setCode] = useState("");
  const [devKind, setDevKind] = useState("server");
  const [devMeta, setDevMeta] = useState({ model: "", location: "" });
  const [steps, setSteps] = useState<Step[]>();
  const busy = steps?.some((s) => s.state === "doing");
  if (!tree) return null;
  const up = (i: number, p: Partial<Step>) => setSteps((s) => s!.map((x, j) => (j === i ? { ...x, ...p } : x)));
  const fail = (e: unknown) => {
    const msg = e instanceof Error ? e.message.split("\n")[0] : String(e);
    setSteps((s) => s?.map((x) => (x.state === "doing" ? { ...x, state: "fail", detail: msg } : x)));
    toast(msg, "err");
  };

  async function addNetwork() {
    if (!wallet || !account) return;
    setSteps([
      { label: "Deploy the network's registry", state: "doing" },
      { label: `Register ${netLabel}.${org}`, state: "todo" },
    ]);
    try {
      const reg = await deployRegistry(wallet, client, account);
      up(0, { state: "done", detail: reg });
      up(1, { state: "doing" });
      await send(wallet, client, { address: tree!.orgRegistry, abi: registryAbi, functionName: "register", args: [netLabel, account, reg, tree!.resolver, 0n, yearFromNow()] });
      up(1, { state: "done" });
      toast(`Network ${netLabel} created`, "ok");
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
      { label: `Register ${name.replace(`.${org}`, "")}`, state: "todo" },
      { label: `Publish ${svcForm.proto}://${svcForm.target}`, state: "todo" },
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
      toast(`Service ${svcForm.name} published`, "ok");
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
    setSteps([
      { label: `Deploy ${label}'s grants registry`, state: "doing" },
      { label: `Register ${j.name.replace(`.${org}`, "")}`, state: "todo" },
      { label: "Publish the device's keys and folders", state: "todo" },
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
      toast(`${label} added`, "ok");
      refresh();
    } catch (e) {
      fail(e);
    }
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
            <span className="text-[13px] text-muted">{txs} transactions</span>
            <Button kind="primary" disabled={!ready || busy || !account} onClick={go}>
              {account ? "Add" : "Connect a wallet"}
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
              <div>
                <p className="text-[14px] text-muted">On the device, run this and paste the code it prints:</p>
                <pre className="mono mt-2 overflow-x-auto rounded-xl bg-panel-2 px-4 py-3 text-[13px]">meshyd init --name db2.{tree.networks[0]?.label ?? "prod"}.{org} --roots /srv</pre>
              </div>
              <Field label="Join code">
                <textarea className={cx(inputCls, "h-24")} value={code} onChange={(e) => setCode(e.target.value)} placeholder="meshy-join:…" />
              </Field>
              <div className="grid grid-cols-2 gap-3">
                <Field label="Kind">
                  <select className={inputCls} value={devKind} onChange={(e) => setDevKind(e.target.value)}>
                    {["server", "gpu-server", "raspberry-pi", "laptop", "nas", "camera-hub"].map((k) => (
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
                <input className={inputCls} value={svcForm.name} onChange={(e) => setSvcForm({ ...svcForm, name: e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, "") })} />
              </Field>
              <div className="grid grid-cols-[1fr_120px] gap-3">
                <Field label="Target (host:port)">
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

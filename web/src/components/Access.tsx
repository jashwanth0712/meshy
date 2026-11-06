"use client";
import { Trash2 } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { zeroAddress } from "viem";
import { type Step, Steps } from "@/components/Steps";
import { Button, Modal, Seg, Sheet, cx, inputCls } from "@/components/ui";
import { revokesFor } from "@/lib/arkiv";
import { type PathRule, sealPolicy } from "@/lib/envelope";
import { type GrantNode, getState, labelId, recordCalls, registryAbi, resolverAbi, send } from "@/lib/ens";
import { useMeshy } from "@/lib/store";

const DURATIONS = [
  { v: 900, label: "15 min" },
  { v: 3600, label: "1 hour" },
  { v: 14400, label: "4 hours" },
  { v: 86400, label: "1 day" },
];

/** What a deep link (Mac menu bar "Request access…", Approve, activity) can prefill. */
export type GrantPreset = { resource?: string; agent?: string; path?: string; mode?: "ro" | "rw"; dur?: number };

export function GrantDrawer({ onClose, preset }: { onClose: () => void; preset?: GrantPreset }) {
  const { tree, wallet, client, account, refresh, toast, org } = useMeshy();
  const resources = useMemo(() => tree?.networks.flatMap((n) => n.resources) ?? [], [tree]);
  const services = useMemo(() => resources.flatMap((r) => r.services), [resources]);
  const [resName, setResName] = useState(preset?.resource ?? resources[0]?.name ?? "");
  const [agent, setAgent] = useState(preset?.agent ?? tree?.agents[0]?.label ?? "");
  const res = resources.find((r) => r.name === resName);
  const svc = services.find((x) => x.name === resName);
  const [paths, setPaths] = useState<PathRule[]>(() =>
    preset?.path?.startsWith("/") ? [{ p: preset.path, m: preset.mode ?? "ro" }] : res?.roots[0] ? [{ p: res.roots[0], m: preset?.mode ?? "ro" }] : [],
  );
  const [dur, setDur] = useState(DURATIONS.some((d) => d.v === preset?.dur) ? preset!.dur! : 3600);
  const [steps, setSteps] = useState<Step[]>();
  const busy = steps?.some((s) => s.state === "doing");
  const finished = steps && !busy;
  const grantName = agent && (svc || res) ? `${agent}.${(svc ?? res)!.name}` : "";
  const exp = new Date(Date.now() + dur * 1000);
  const ag = tree?.agents.find((a) => a.label === agent);
  const short = (n: string) => n.replace(`.${org}`, "");

  const valid = svc ? !!ag : !!res && !!ag && paths.length > 0 && paths.every((p) => p.p.startsWith("/") && p.p !== "/") && !!res.encKey && !!tree?.orgKey;

  async function submit() {
    if (!wallet || !tree || !ag) return;
    const up = (i: number, p: Partial<Step>) => setSteps((s) => s!.map((x, j) => (j === i ? { ...x, ...p } : x)));
    if (svc) {
      setSteps([{ label: `Register ${grantName}`, state: "doing", detail: `expires ${exp.toLocaleTimeString()}` }]);
      try {
        const r = await send(wallet, client, { address: svc.registry, abi: registryAbi, functionName: "register", args: [agent, ag.addr, zeroAddress, tree.resolver, 0n, BigInt(Math.floor(Date.now() / 1000) + dur)] }, (h) => up(0, { tx: h }));
        up(0, { state: "done", detail: `block ${r.blockNumber}` });
        toast(`Granted ${short(grantName)}`, "ok");
        refresh();
      } catch (e) {
        const msg = e instanceof Error ? e.message.split("\n")[0] : String(e);
        setSteps((s) => s?.map((x) => (x.state === "doing" ? { ...x, state: "fail", detail: msg } : x)));
        toast(msg, "err");
      }
      return;
    }
    if (!res) return;
    setSteps([
      { label: `Register ${short(grantName)}`, state: "doing", detail: `expires ${exp.toLocaleTimeString()}` },
      { label: "Read the grant's token id", state: "todo" },
      { label: "Seal the folder policy in your browser", state: "todo" },
      { label: "Write the policy on ENS", state: "todo" },
    ]);
    try {
      const r1 = await send(wallet, client, {
        address: res.registry,
        abi: registryAbi,
        functionName: "register",
        args: [agent, ag.addr, zeroAddress, tree.resolver, 0n, BigInt(Math.floor(Date.now() / 1000) + dur)],
      }, (h) => up(0, { tx: h }));
      up(0, { state: "done", detail: `block ${r1.blockNumber}` });
      up(1, { state: "doing" });
      const st = await getState(client, res.registry, agent);
      up(1, { state: "done" });
      up(2, { state: "doing" });
      const sealed = sealPolicy({ v: 1, grant: grantName, grantTokenId: st.tokenId.toString(), paths }, [res.encKey, tree.orgKey]);
      up(2, { state: "done", detail: `readable only by ${res.label} and the org` });
      up(3, { state: "doing" });
      const r3 = await send(wallet, client, {
        address: tree.resolver,
        abi: resolverAbi,
        functionName: "multicall",
        args: [recordCalls(grantName, { "meshy.policy": sealed })],
      }, (h) => up(3, { tx: h }));
      up(3, { state: "done", detail: `block ${r3.blockNumber}` });
      toast(`Granted ${short(grantName)}`, "ok");
      refresh();
    } catch (e) {
      const msg = e instanceof Error ? e.message.split("\n")[0] : String(e);
      setSteps((s) => s?.map((x) => (x.state === "doing" ? { ...x, state: "fail", detail: msg } : x)));
      toast(msg, "err");
    }
  }

  const pathNames = paths.map((p) => p.p.split("/").filter(Boolean).pop() ?? p.p).join(", ");
  const writes = paths.some((p) => p.m === "rw");
  const target = svc ?? res;

  return (
    <Sheet
      title="Give access"
      onClose={onClose}
      locked={busy}
      footer={
        finished ? (
          <>
            <span />
            <Button kind="primary" onClick={onClose}>Done</Button>
          </>
        ) : (
          <>
            <span className="text-[13px] text-muted">{svc ? "1 transaction" : "2 transactions"}</span>
            <Button kind="primary" disabled={!valid || busy || !account} onClick={submit}>
              {account ? "Give access" : "Connect a wallet"}
            </Button>
          </>
        )
      }
    >
      {steps ? (
        <Steps steps={steps} />
      ) : (
        <>
          <label className="block">
            <span className="text-[13px] font-semibold">Agent</span>
            <select className={cx(inputCls, "mt-2")} value={agent} onChange={(e) => setAgent(e.target.value)}>
              {tree?.agents.map((a) => (
                <option key={a.name} value={a.label}>
                  {a.label}
                  {a.worldid ? "" : " (no World ID)"}
                </option>
              ))}
            </select>
          </label>

          <label className="block">
            <span className="text-[13px] font-semibold">Device or service</span>
            <select
              className={cx(inputCls, "mt-2")}
              value={resName}
              onChange={(e) => {
                setResName(e.target.value);
                const r = resources.find((x) => x.name === e.target.value);
                setPaths(r?.roots[0] ? [{ p: r.roots[0], m: "ro" }] : []);
              }}
            >
              <optgroup label="Devices">
                {resources.map((r) => (
                  <option key={r.name} value={r.name}>{short(r.name)}</option>
                ))}
              </optgroup>
              {services.length > 0 && (
                <optgroup label="Services (one port)">
                  {services.map((x) => (
                    <option key={x.name} value={x.name}>{short(x.name)}</option>
                  ))}
                </optgroup>
              )}
            </select>
          </label>

          {!svc && (
            <div>
              <span className="text-[13px] font-semibold">Folders</span>
              <div className="mt-2 space-y-2">
                {paths.map((p, i) => (
                  <div key={i} className="space-y-2">
                    <div className="flex gap-2">
                      <input className={inputCls} value={p.p} onChange={(e) => setPaths(paths.map((x, j) => (j === i ? { ...x, p: e.target.value } : x)))} />
                      {paths.length > 1 && (
                        <button onClick={() => setPaths(paths.filter((_, j) => j !== i))} className="px-2 text-muted hover:text-fg" aria-label="Remove folder">
                          <Trash2 size={15} />
                        </button>
                      )}
                    </div>
                    <Seg value={p.m} onChange={(m) => setPaths(paths.map((x, j) => (j === i ? { ...x, m } : x)))} options={[{ v: "ro", label: "Read" }, { v: "rw", label: "Read and write" }]} />
                  </div>
                ))}
                <button onClick={() => setPaths([...paths, { p: res?.roots[0] ?? "", m: "ro" }])} className="text-[13px] font-medium text-accent hover:underline">
                  Add another folder
                </button>
              </div>
            </div>
          )}

          <div>
            <span className="text-[13px] font-semibold">For</span>
            <div className="mt-2">
              <Seg value={dur} onChange={setDur} options={DURATIONS} />
            </div>
          </div>

          {target && ag && (
            <p className="rounded-xl bg-panel-2 p-4 text-[14px] leading-relaxed">
              <b className="mono">{agent}</b>{" "}
              {svc ? (
                <>
                  can reach <span className="mono">{svc.proto}://{svc.target}</span> on <b className="mono">{short(svc.device)}</b>
                </>
              ) : (
                <>
                  can {writes ? "read and write" : "read"} <span className="mono">{pathNames || "—"}</span> on <b className="mono">{res?.label}</b>
                </>
              )}{" "}
              until {exp.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}. Nothing else.
            </p>
          )}
          {!svc && !tree?.orgKey && <p className="text-[13px] text-bad">This org has no meshy.orgkey record, so folder grants can&apos;t be sealed.</p>}
          {!svc && res && !res.encKey && <p className="text-[13px] text-bad">{res.label} has no meshy.enckey record.</p>}
          {ag && !ag.worldid && <p className="text-[13px] text-warn">{agent} has no World ID attestation. Devices will refuse it.</p>}
        </>
      )}
    </Sheet>
  );
}

export function RevokeTimeline({ grant, res, onDone }: { grant: GrantNode; res: { label: string; name: string }; onDone: () => void }) {
  const { wallet, client, org, refresh, toast } = useMeshy();
  const [steps, setSteps] = useState<Step[]>([
    { label: "Send the revoke transaction", state: "doing" },
    { label: "Mined on Sepolia", state: "todo" },
    { label: `Session ended on ${res.label}`, state: "todo" },
    { label: "Recorded on Arkiv", state: "todo" },
  ]);
  const [result, setResult] = useState<string>();
  const [t0] = useState(() => Date.now());
  const up = (i: number, p: Partial<Step>) => setSteps((s) => s.map((x, j) => (j === i ? { ...x, ...p } : x)));
  const fired = useRef(false);
  useEffect(() => {
    if (fired.current) return; // exactly one unregister tx, even under StrictMode
    fired.current = true;
    (async () => {
      if (!wallet) return;
      try {
        const r = await send(wallet, client, { address: grant.registry, abi: registryAbi, functionName: "unregister", args: [labelId(grant.agent)] }, (h) => {
          up(0, { state: "done", tx: h });
          up(1, { state: "doing" });
        });
        const mined = Date.now();
        up(1, { state: "done", detail: `block ${r.blockNumber} · ${((mined - t0) / 1000).toFixed(1)} s` });
        up(2, { state: "doing", detail: "waiting for the device's report…" });
        refresh();
        for (let i = 0; i < 20; i++) {
          await new Promise((x) => setTimeout(x, 3000));
          const rows = await revokesFor(org, grant.name).catch(() => []);
          const hit = rows.find((x) => x.block === Number(r.blockNumber) || x.ts >= t0 - 5000);
          if (hit) {
            up(2, { state: "done", detail: `${hit.latencyMs ?? "?"} ms after the device saw the block` });
            up(3, { state: "done" });
            setResult(`${((hit.ts - t0) / 1000).toFixed(1)} s`);
            return;
          }
        }
        up(2, { state: "done", detail: "no open session" });
        up(3, { state: "done", detail: "nothing to record" });
        setResult("done");
      } catch (e) {
        const msg = e instanceof Error ? e.message.split("\n")[0] : String(e);
        setSteps((s) => s.map((x) => (x.state === "doing" ? { ...x, state: "fail", detail: msg } : x)));
        toast(msg, "err");
        setResult("failed");
      }
    })();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  const running = !result;
  return (
    <Modal onClose={onDone} locked={running}>
      <div className="flex items-baseline justify-between">
        <h2 className="text-[20px] font-semibold tracking-[-0.015em]">{result === "failed" ? "Revoke failed" : running ? "Revoking…" : "Revoked"}</h2>
        {result && result !== "failed" && result !== "done" && <span className="mono tnum text-[13px] text-muted">{result}</span>}
      </div>
      <p className="mono mt-1 text-[13px] text-muted">{grant.name.replace(`.${org}`, "")}</p>
      <div className="mt-5">
        <Steps steps={steps} />
      </div>
      <div className="mt-6 flex justify-end">
        <Button onClick={onDone} disabled={running}>{running ? "Working…" : "Done"}</Button>
      </div>
    </Modal>
  );
}

"use client";
import { Trash2 } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { zeroAddress } from "viem";
import { type Step, Steps } from "@/components/Steps";
import { Button, Modal, Seg, Sheet, cx, inputCls } from "@/components/ui";
import { revokesFor } from "@/lib/arkiv";
import { type PathRule, sealPolicy } from "@/lib/envelope";
import { type GrantNode, labelId, recordCalls, registryAbi, resolverAbi, send, sendAll } from "@/lib/ens";
import { useMeshy } from "@/lib/store";

const DURATIONS = [
  { v: 900, label: "15 min" },
  { v: 3600, label: "1 hour" },
  { v: 14400, label: "4 hours" },
  { v: 86400, label: "1 day" },
];

/** What a deep link (Mac menu bar "Request access…", Approve, activity) can prefill. */
export type GrantPreset = { resource?: string; agent?: string; path?: string; mode?: "ro" | "rw"; dur?: number; sign?: boolean };

export function GrantDrawer({ onClose, preset }: { onClose: () => void; preset?: GrantPreset }) {
  const { tree, wallet, client, account, refresh, toast, org, connect, canManage } = useMeshy();
  const resources = useMemo(() => tree?.networks.flatMap((n) => n.resources) ?? [], [tree]);
  const services = useMemo(() => resources.flatMap((r) => r.services), [resources]);
  const [resName, setResName] = useState(preset?.resource ?? resources[0]?.name ?? "");
  const [agent, setAgent] = useState(preset?.agent ?? tree?.agents[0]?.label ?? "");
  const res = resources.find((r) => r.name === resName);
  const svc = services.find((x) => x.name === resName);
  const [paths, setPaths] = useState<PathRule[]>(() =>
    preset?.path?.startsWith("/")
      ? [{ p: preset.path, m: preset.mode ?? "ro" }]
      : preset?.agent && preset?.resource
        ? [] // a request for "any of its folders": the admin picks
        : res?.roots[0] ? [{ p: res.roots[0], m: preset?.mode ?? "ro" }] : [],
  );
  const [dur, setDur] = useState(DURATIONS.some((d) => d.v === preset?.dur) ? preset!.dur! : 3600);
  const [steps, setSteps] = useState<Step[]>();
  const busy = steps?.some((s) => s.state === "doing");
  const finished = steps && !busy;
  const grantName = agent && (svc || res) ? `${agent}.${(svc ?? res)!.name}` : "";
  const exp = new Date(Date.now() + dur * 1000);
  const ag = tree?.agents.find((a) => a.label === agent);
  const short = (n: string) => n.replace(`.${org}`, "");

  const valid = svc ? !!ag : !!res && !!ag && paths.length > 0 && paths.every((p) => p.p.startsWith("/") && p.p !== "/" && !p.p.endsWith("/")) && !!res.encKey && !!tree?.orgKey;

  /**
   * The calls that make `agent` hold a grant in `registry` until `expiry`. Registering a label
   * that is still live reverts (LabelAlreadyRegistered), so granting again to someone who
   * already has access extends it with renew; renew can't shorten, so a shorter grant
   * replaces the old one (unregister, then register).
   */
  async function grantCalls(registry: `0x${string}`, owner: string, expiry: bigint) {
    const register = { address: registry, abi: registryAbi, functionName: "register", args: [agent, owner, zeroAddress, tree!.resolver, 0n, expiry] } as const;
    const st = await client.readContract({ address: registry, abi: registryAbi, functionName: "getState", args: [labelId(agent)] });
    const now = BigInt(Math.floor(Date.now() / 1000));
    if (st.status !== 2 || st.expiry <= now) return [register];
    if (expiry > st.expiry) return [{ address: registry, abi: registryAbi, functionName: "renew", args: [st.tokenId, expiry] } as const];
    return [{ address: registry, abi: registryAbi, functionName: "unregister", args: [st.tokenId] } as const, register];
  }

  async function submit() {
    if (!wallet || !tree || !ag || !canManage) return;
    // A member's grant belongs to their wallet (the owner of <you>.people.<org>), so it keeps working
    // on every Mac they link; meshyd accepts either the wallet or the linked device key.
    const owner = ag.kind === "person" && ag.owner && ag.owner !== zeroAddress ? ag.owner : ag.addr;
    const up = (i: number, p: Partial<Step>) => setSteps((s) => s!.map((x, j) => (j === i ? { ...x, ...p } : x)));
    if (svc) {
      setSteps([{ label: `Register ${grantName}`, state: "doing", detail: `expires ${exp.toLocaleTimeString()}` }]);
      try {
        const calls = await grantCalls(svc.registry, owner, BigInt(Math.floor(Date.now() / 1000) + dur));
        if (calls.length === 1) {
          const r = await send(wallet, client, calls[0], (h) => up(0, { tx: h }));
          up(0, { state: "done", detail: `block ${r.blockNumber}` });
        } else {
          await sendAll(wallet, client, calls, (h) => up(0, { tx: h }));
          up(0, { state: "done", detail: "replaced the earlier grant" });
        }
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
    // One confirmation: the policy is sealed here first and bound to the grant's exact expiry,
    // so registering the grant and writing its policy can go in a single batch.
    const expiry = BigInt(Math.floor(Date.now() / 1000) + dur);
    setSteps([
      { label: "Seal the folder policy in your browser", state: "doing" },
      { label: `Register ${short(grantName)} and write its policy`, state: "todo", detail: `expires ${exp.toLocaleTimeString()}` },
    ]);
    try {
      const sealed = sealPolicy({ v: 1, grant: grantName, grantTokenId: "", grantExpiry: Number(expiry), paths }, [res.encKey, tree.orgKey]);
      up(0, { state: "done", detail: `readable only by ${res.label} and the org` });
      up(1, { state: "doing" });
      const { batched } = await sendAll(
        wallet,
        client,
        [
          ...(await grantCalls(res.registry, owner, expiry)),
          { address: tree.resolver, abi: resolverAbi, functionName: "multicall", args: [recordCalls(grantName, { "meshy.policy": sealed })] },
        ],
        (h) => up(1, { tx: h }),
      );
      up(1, { state: "done", detail: batched ? "one signature" : "2 signatures (your wallet can't batch)" });
      toast(`Granted ${short(grantName)}`, "ok");
      refresh();
    } catch (e) {
      const msg = e instanceof Error ? e.message.split("\n")[0] : String(e);
      setSteps((s) => s?.map((x) => (x.state === "doing" ? { ...x, state: "fail", detail: msg } : x)));
      toast(msg, "err");
    }
  }

  // Approve from the Mac menu bar (?sign=1): start signing as soon as the sheet is filled in.
  const autoSigned = useRef(false);
  useEffect(() => {
    if (!preset?.sign || autoSigned.current || !valid || !wallet || !canManage || steps) return;
    autoSigned.current = true;
    submit();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [preset?.sign, valid, wallet, canManage, steps]);

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
            <span className="text-[13px] text-muted">1 signature</span>
            <Button kind="primary" disabled={account ? !valid || busy || !canManage : false} onClick={account ? submit : connect} title={account && !canManage ? "Only the owner or an admin can give access" : undefined}>
              {!account ? "Connect a wallet" : canManage ? "Give access" : "Owners and admins only"}
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
            <span className="text-[13px] font-semibold">Who</span>
            <select className={cx(inputCls, "mt-2")} value={agent} onChange={(e) => setAgent(e.target.value)}>
              {tree?.agents.map((a) => (
                <option key={a.name} value={a.label}>
                  {a.label}
                  {a.kind === "person" ? " (member)" : a.worldid ? "" : " (no World ID)"}
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

          {!svc && res && (
            <div>
              <span className="text-[13px] font-semibold">Folders</span>
              <p className="mt-0.5 text-[12px] text-muted">What {res.label} shares. Pick one or more, and how much.</p>
              <div className="mt-2 divide-y divide-line overflow-hidden rounded-xl border border-line">
                {res.roots.length === 0 && <div className="px-3 py-3 text-[13px] text-muted">{res.label} doesn&apos;t share any folders yet.</div>}
                {res.roots.map((root) => {
                  const i = paths.findIndex((x) => x.p === root || x.p.startsWith(root + "/"));
                  const on = i >= 0;
                  const name = root.split("/").filter(Boolean).pop() ?? root;
                  const set = (p: Partial<PathRule>) => setPaths(paths.map((x, j) => (j === i ? { ...x, ...p } : x)));
                  return (
                    <div key={root} className={cx("px-3 py-2.5", on && "bg-panel-2")}>
                      <label className="flex cursor-pointer items-center gap-3">
                        <input type="checkbox" checked={on} onChange={(e) => setPaths(e.target.checked ? [...paths, { p: root, m: preset?.mode ?? "ro" }] : paths.filter((_, j) => j !== i))} />
                        <span className="min-w-0 flex-1">
                          <span className="block text-[14px] font-medium">{name}</span>
                          <span className="mono block truncate text-[11px] text-muted">{root.replace(/^\/(Users|home)\/[^/]+/, "~")}</span>
                        </span>
                        {on && (
                          <span onClick={(e) => e.preventDefault()}>
                            <Seg value={paths[i].m} onChange={(m) => set({ m })} options={[{ v: "ro", label: "Read" }, { v: "rw", label: "Read and write" }]} />
                          </span>
                        )}
                      </label>
                      {on && (
                        <div className="mt-2 pl-7">
                          {paths[i].p === root ? (
                            <button className="text-[12px] text-accent hover:underline" onClick={() => set({ p: root + "/" })}>
                              Only a subfolder…
                            </button>
                          ) : (
                            <div className="flex items-center gap-1 text-[12px]">
                              <span className="mono text-muted">{name}/</span>
                              <input
                                autoFocus
                                className={cx(inputCls, "h-7 py-0 text-[12px]")}
                                placeholder="reports/2026"
                                value={paths[i].p.slice(root.length + 1)}
                                onChange={(e) => set({ p: `${root}/${e.target.value.replace(/^\/+/, "").replace(/\.\.+/g, "")}` })}
                              />
                              <button className="px-1 text-muted hover:text-fg" aria-label="Whole folder" onClick={() => set({ p: root })}>
                                <Trash2 size={13} />
                              </button>
                            </div>
                          )}
                        </div>
                      )}
                    </div>
                  );
                })}
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

"use client";
import { useCallback, useEffect, useState } from "react";
import { namehash } from "viem";
import { type Step, Steps } from "@/components/Steps";
import { Badge, Button, Ext, PageHeader, Seg, cx, dateFmt, inputCls, links } from "@/components/ui";
import { send } from "@/lib/ens";
import { type Provider, SUBSCRIPTION, USDC, erc20Abi, registryAbi, REGISTRY, subscriptionAbi, usdc } from "@/lib/contracts";
import { useMeshy } from "@/lib/store";

export default function BillingPage() {
  const { org, tree, client, wallet, account, toast } = useMeshy();
  const node = namehash(org);
  const [sub, setSub] = useState<{ seats: number; paidUntil: number; providerId: bigint }>();
  const [prov, setProv] = useState<Provider>();
  const [mode, setMode] = useState<"seats" | "renew" | null>(null);
  const [extra, setExtra] = useState(5);
  const [months, setMonths] = useState(1);
  const [steps, setSteps] = useState<Step[]>();
  const load = useCallback(async () => {
    const [seats, paidUntil, providerId] = (await client.readContract({ address: SUBSCRIPTION, abi: subscriptionAbi, functionName: "seatsOf", args: [node] })) as [number, bigint, bigint];
    setSub({ seats: Number(seats), paidUntil: Number(paidUntil), providerId });
    if (providerId > 0n) setProv((await client.readContract({ address: REGISTRY, abi: registryAbi, functionName: "getProvider", args: [providerId] })) as Provider);
  }, [client, node]);
  useEffect(() => {
    load().catch((e) => toast(String(e), "err"));
  }, [load, toast]);

  const devices = tree ? tree.networks.reduce((n, x) => n + x.resources.length, 0) : 0;
  const agents = tree?.agents.length ?? 0;
  const used = devices + agents;
  const active = sub && sub.paidUntil > Date.now() / 1000;
  const isOwner = !!account && !!tree && tree.owner.toLowerCase() === account.toLowerCase();
  const daysLeft = sub ? Math.max(0, Math.round((sub.paidUntil - Date.now() / 1000) / 86400)) : 0;
  const remainingMonths = sub ? Math.max(1, Math.ceil((sub.paidUntil - Date.now() / 1000) / (30 * 86400))) : 1;
  const cost = prov && sub ? (mode === "seats" ? prov.pricePerSeatPerMonth * BigInt(extra) * BigInt(remainingMonths) : prov.pricePerSeatPerMonth * BigInt(sub.seats) * BigInt(months)) : 0n;

  async function pay() {
    if (!wallet || !account || !sub || !prov || !mode) return;
    const up = (i: number, p: Partial<Step>) => setSteps((s) => s!.map((x, j) => (j === i ? { ...x, ...p } : x)));
    setSteps([
      { label: `Mint ${usdc(cost)} test USDC`, state: "doing" },
      { label: `Approve ${usdc(cost)} USDC`, state: "todo" },
      { label: mode === "seats" ? `Add ${extra} seats` : `Renew for ${months} month${months === 1 ? "" : "s"}`, state: "todo" },
    ]);
    try {
      await send(wallet, client, { address: USDC, abi: erc20Abi, functionName: "mint", args: [account, cost] }, (h) => up(0, { tx: h }));
      up(0, { state: "done" });
      up(1, { state: "doing" });
      await send(wallet, client, { address: USDC, abi: erc20Abi, functionName: "approve", args: [SUBSCRIPTION, cost] }, (h) => up(1, { tx: h }));
      up(1, { state: "done" });
      up(2, { state: "doing" });
      await send(
        wallet,
        client,
        mode === "seats"
          ? { address: SUBSCRIPTION, abi: subscriptionAbi, functionName: "addSeats", args: [node, extra] }
          : { address: SUBSCRIPTION, abi: subscriptionAbi, functionName: "renew", args: [node, months] },
        (h) => up(2, { tx: h }),
      );
      up(2, { state: "done" });
      toast("Subscription updated", "ok");
      load();
    } catch (e) {
      const msg = e instanceof Error ? e.message.split("\n")[0] : String(e);
      setSteps((s) => s?.map((x) => (x.state === "doing" ? { ...x, state: "fail", detail: msg } : x)));
    }
  }

  return (
    <>
      <PageHeader title="Billing" sub={prov ? `Paid in test USDC to provider #${sub?.providerId.toString()}.` : "Seats are paid to a staked provider. Self-hosting is free."} />
      <div className="max-w-xl rounded-2xl border border-line bg-panel p-6">
        {!sub ? (
          <p className="text-[14px] text-muted">Reading the subscription…</p>
        ) : sub.providerId === 0n ? (
          <p className="text-[14px] text-muted">No subscription. Self-hosting a provider is free.</p>
        ) : (
          <>
            <div className="flex items-baseline justify-between gap-3">
              <h2 className="text-[20px] font-semibold tracking-[-0.015em]">Paid until {dateFmt(sub.paidUntil)}</h2>
              {active ? <Badge tone="ok">Active</Badge> : <Badge tone="bad">Lapsed</Badge>}
            </div>
            <p className="mt-1 text-[14px] text-muted">{active ? `${daysLeft} days left` : "New devices can't join until you renew."}</p>

            <div className="mt-6 flex items-baseline justify-between text-[14px]">
              <span>Seats</span>
              <span>
                <b>{used}</b> <span className="text-muted">of {sub.seats}</span>
              </span>
            </div>
            <div className="mt-2 h-2 overflow-hidden rounded-full bg-panel-2">
              <div className={cx("h-full rounded-full", used > sub.seats ? "bg-bad" : "bg-accent")} style={{ width: `${Math.min(100, (used / Math.max(1, sub.seats)) * 100)}%` }} />
            </div>
            <p className="mt-2 text-[13px] text-muted">
              {devices} device{devices === 1 ? "" : "s"} and {agents} agent{agents === 1 ? "" : "s"}. Services are free.
            </p>

            {steps ? (
              <div className="mt-6 space-y-4">
                <Steps steps={steps} />
                {!steps.some((s) => s.state === "doing") && <Button onClick={() => (setSteps(undefined), setMode(null))}>Done</Button>}
              </div>
            ) : mode ? (
              <div className="mt-6 space-y-4">
                <Seg value={mode} onChange={setMode} options={[{ v: "seats", label: "Add seats" }, { v: "renew", label: "Renew" }]} />
                <div className="flex items-center gap-2">
                  <input type="number" min={1} className={cx(inputCls, "w-24 text-center")} value={mode === "seats" ? extra : months} onChange={(e) => (mode === "seats" ? setExtra : setMonths)(Math.max(1, +e.target.value))} />
                  <span className="text-[14px] text-muted">{mode === "seats" ? "seats" : "months"} · {usdc(cost)} USDC</span>
                </div>
                <div className="flex gap-2">
                  <Button kind="primary" disabled={!isOwner || (mode === "seats" && !active)} onClick={pay}>
                    Pay {usdc(cost)} USDC
                  </Button>
                  <Button kind="ghost" onClick={() => setMode(null)}>Cancel</Button>
                </div>
                {!isOwner && <p className="text-[13px] text-muted">Only the org owner can pay.</p>}
              </div>
            ) : (
              <div className="mt-6 flex gap-2">
                <Button kind="primary" onClick={() => setMode("seats")} disabled={!active}>Add seats</Button>
                <Button onClick={() => setMode("renew")}>Renew</Button>
              </div>
            )}
          </>
        )}
      </div>
      {prov && (
        <p className="mt-4 text-[13px] text-muted">
          {usdc(prov.pricePerSeatPerMonth)} USDC per seat per month, held in escrow. Unused months are refunded if you cancel. <Ext href={links.addr(SUBSCRIPTION)}>Contract</Ext>
        </p>
      )}
    </>
  );
}

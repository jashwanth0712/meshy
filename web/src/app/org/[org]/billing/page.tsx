"use client";
import { CreditCard, Plus, RefreshCw } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { namehash } from "viem";
import { type Step, Steps } from "@/components/Steps";
import { Badge, Button, Card, Ext, Field, Short, inputCls, links } from "@/components/ui";
import { send } from "@/lib/ens";
import { type Provider, SUBSCRIPTION, USDC, erc20Abi, registryAbi, REGISTRY, subscriptionAbi, usdc } from "@/lib/contracts";
import { useMeshy } from "@/lib/store";

export default function BillingPage() {
  const { org, tree, client, wallet, account, toast } = useMeshy();
  const node = namehash(org);
  const [sub, setSub] = useState<{ seats: number; paidUntil: number; providerId: bigint }>();
  const [prov, setProv] = useState<Provider>();
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

  const used = tree ? tree.agents.length + tree.networks.reduce((n, x) => n + x.resources.length, 0) : 0;
  const active = sub && sub.paidUntil > Date.now() / 1000;
  const isOwner = !!account && !!tree && tree.owner.toLowerCase() === account.toLowerCase();

  async function pay(kind: "seats" | "renew") {
    if (!wallet || !account || !sub || !prov) return;
    const price = prov.pricePerSeatPerMonth;
    const remainingMonths = Math.max(1, Math.ceil((sub.paidUntil - Date.now() / 1000) / (30 * 86400)));
    const cost = kind === "seats" ? price * BigInt(extra) * BigInt(remainingMonths) : price * BigInt(sub.seats) * BigInt(months);
    const up = (i: number, p: Partial<Step>) => setSteps((s) => s!.map((x, j) => (j === i ? { ...x, ...p } : x)));
    setSteps([
      { label: `Mint test USDC (MockUSDC is public on Sepolia)`, state: "doing" },
      { label: `Approve ${usdc(cost)} USDC`, state: "todo" },
      { label: kind === "seats" ? `Add ${extra} seats` : `Renew ${months} month(s)`, state: "todo" },
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
        kind === "seats"
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
    <div className="grid gap-5 lg:grid-cols-2">
      <Card title={<span className="flex items-center gap-2"><CreditCard size={15} /> Subscription</span>} actions={<Button kind="ghost" onClick={load}><RefreshCw size={14} /></Button>}>
        {!sub ? (
          <div className="text-sm text-muted">Reading MeshySubscription…</div>
        ) : sub.providerId === 0n ? (
          <div className="text-sm text-muted">No subscription. Self-hosting a provider is free; paid providers are for convenience.</div>
        ) : (
          <div className="space-y-4 text-sm">
            <div className="flex items-center gap-2">{active ? <Badge tone="ok">active</Badge> : <Badge tone="bad">lapsed</Badge>} paid until {new Date(sub.paidUntil * 1000).toLocaleDateString()}</div>
            <div>
              <div className="mb-1 flex justify-between text-xs text-muted">
                <span>Seats used (resources + agents)</span>
                <span>
                  {used} / {sub.seats}
                </span>
              </div>
              <div className="h-2 overflow-hidden rounded-full bg-panel-2">
                <div className={used > sub.seats ? "h-full bg-bad" : "h-full bg-accent"} style={{ width: `${Math.min(100, (used / Math.max(1, sub.seats)) * 100)}%` }} />
              </div>
              {used > sub.seats && <div className="mt-1 text-xs text-bad">Over your paid seats: the provider will refuse new device admissions.</div>}
            </div>
            {prov && (
              <div className="grid grid-cols-[110px_1fr] gap-y-1.5 text-xs">
                <span className="text-muted">provider</span>
                <span>#{sub.providerId.toString()} · <span className="mono">{prov.endpoint}</span></span>
                <span className="text-muted">price</span>
                <span>{usdc(prov.pricePerSeatPerMonth)} USDC / seat / month</span>
                <span className="text-muted">stake</span>
                <span>{(Number(prov.stake) / 1e18).toFixed(3)} ETH (slashable)</span>
                <span className="text-muted">contract</span>
                <Ext href={links.addr(SUBSCRIPTION)}><Short v={SUBSCRIPTION} n={4} /></Ext>
              </div>
            )}
          </div>
        )}
      </Card>
      <Card title="Change plan">
        {steps ? (
          <div className="space-y-4">
            <Steps steps={steps} />
            <Button onClick={() => setSteps(undefined)}>Done</Button>
          </div>
        ) : (
          <div className="space-y-5">
            <Field label="Add seats" hint="Prorated to your current period.">
              <div className="flex gap-2">
                <input type="number" min={1} className={inputCls} value={extra} onChange={(e) => setExtra(Math.max(1, +e.target.value))} />
                <Button kind="primary" disabled={!isOwner || !active} onClick={() => pay("seats")}>
                  <Plus size={14} /> Add
                </Button>
              </div>
            </Field>
            <Field label="Renew" hint="Funds are held in escrow and released to the provider month by month; cancelling refunds unused months.">
              <div className="flex gap-2">
                <input type="number" min={1} className={inputCls} value={months} onChange={(e) => setMonths(Math.max(1, +e.target.value))} />
                <Button disabled={!isOwner || !sub || sub.providerId === 0n} onClick={() => pay("renew")}>
                  Renew
                </Button>
              </div>
            </Field>
          </div>
        )}
      </Card>
    </div>
  );
}

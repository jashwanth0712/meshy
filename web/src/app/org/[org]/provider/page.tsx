"use client";
import { useCallback, useEffect, useState } from "react";
import { type Hex, parseEther } from "viem";
import { type Step, Steps } from "@/components/Steps";
import { Badge, Button, Card, Empty, Ext, Field, PageHeader, Row, Sheet, Short, cx, inputCls, links } from "@/components/ui";
import { PROVIDER_API } from "@/lib/config";
import { send } from "@/lib/ens";
import { type Provider, REGISTRY, STATUS, eth, registryAbi, usdc } from "@/lib/contracts";
import { useMeshy } from "@/lib/store";

type Health = { ok: boolean; attestor: string; world: string; devAttest: boolean; version?: string };

export default function ProviderPage() {
  const { client, wallet, account, toast } = useMeshy();
  const [list, setList] = useState<(Provider & { id: bigint })[]>();
  const [health, setHealth] = useState<Health | "down">();
  const [sheet, setSheet] = useState<"register" | "challenge" | null>(null);
  const [form, setForm] = useState({ endpoint: "https://", signer: "", price: "2", stake: "0.01" });
  const [att, setAtt] = useState("");
  const [steps, setSteps] = useState<Step[]>();
  const load = useCallback(async () => {
    const n = (await client.readContract({ address: REGISTRY, abi: registryAbi, functionName: "providerCount" })) as bigint;
    const out = [];
    for (let i = 1n; i <= n; i++) out.push({ id: i, ...((await client.readContract({ address: REGISTRY, abi: registryAbi, functionName: "getProvider", args: [i] })) as Provider) });
    setList(out);
  }, [client]);
  useEffect(() => {
    load().catch((e) => toast(String(e), "err"));
    fetch(`${PROVIDER_API}/v1/health`).then((r) => r.json()).then(setHealth).catch(() => setHealth("down"));
  }, [load, toast]);

  async function run(label: string, req: Parameters<typeof send>[2]) {
    if (!wallet) return;
    setSteps([{ label, state: "doing" }]);
    try {
      const r = await send(wallet, client, req, (h) => setSteps([{ label, state: "doing", tx: h }]));
      setSteps((s) => s!.map((x) => ({ ...x, state: "done", detail: `block ${r.blockNumber}` })));
      load();
    } catch (e) {
      const msg = e instanceof Error ? e.message.split("\n")[0] : String(e);
      setSteps((s) => s!.map((x) => ({ ...x, state: "fail", detail: msg })));
    }
  }
  const busy = steps?.some((s) => s.state === "doing");
  const close = () => (setSheet(null), setSteps(undefined));

  return (
    <>
      <PageHeader title="Provider" sub="The mesh your devices and agents connect through. Providers stake ETH and lose it if they cheat." />

      <Card>
        {!list ? (
          <Empty>Reading the provider registry…</Empty>
        ) : list.length === 0 ? (
          <Empty>No providers yet.</Empty>
        ) : (
          list.map((p) => (
            <Row key={p.id.toString()}>
              <div className="min-w-0 flex-1">
                <div className="mono truncate font-semibold">{p.endpoint.replace(/^https?:\/\//, "")}</div>
                <div className="mt-0.5 text-[13px] text-muted">
                  #{p.id.toString()} · {eth(p.stake)} ETH staked · {usdc(p.pricePerSeatPerMonth)} USDC per seat per month
                </div>
              </div>
              <Badge tone={p.status === 0 ? "ok" : p.status === 3 ? "bad" : "warn"}>{STATUS[p.status] ?? p.status}</Badge>
            </Row>
          ))
        )}
      </Card>

      <Card title="Status" className="mt-10">
        {health === "down" ? (
          <Row><span className="text-bad">meshy-sync is unreachable</span><span className="mono ml-auto text-[13px] text-muted">{PROVIDER_API}</span></Row>
        ) : !health ? (
          <Empty>Checking…</Empty>
        ) : (
          <>
            <Row><span className="flex-1">meshy-sync</span><span className="text-muted">Online{health.version ? ` · v${health.version}` : ""}</span></Row>
            <Row><span className="flex-1">Attestor</span><Short v={health.attestor} /></Row>
            <Row><span className="flex-1">World ID</span>{health.world === "disabled" ? <Badge tone="warn">Off</Badge> : <Badge tone="ok">On</Badge>}</Row>
            <Row><span className="flex-1">Dev attestation</span>{health.devAttest ? <Badge tone="warn">On · demo only</Badge> : <Badge tone="ok">Off</Badge>}</Row>
          </>
        )}
      </Card>

      <p className="mt-6 text-[13px] text-muted">
        Think a provider admitted a revoked name?{" "}
        <button onClick={() => setSheet("challenge")} className="font-medium text-accent hover:underline">Submit a fraud proof</button>. Want to run one?{" "}
        <button onClick={() => setSheet("register")} className="font-medium text-accent hover:underline">Register as a provider</button>.{" "}
        <Ext href={links.addr(REGISTRY)}>Registry contract</Ext>
      </p>

      {sheet === "register" && (
        <Sheet
          title="Register as a provider"
          onClose={close}
          locked={busy}
          footer={
            <>
              <span className="text-[13px] text-muted">Stakes {form.stake} ETH</span>
              <Button
                kind="primary"
                disabled={!wallet || busy}
                onClick={() =>
                  run("Register provider", {
                    address: REGISTRY,
                    abi: registryAbi as never,
                    functionName: "register",
                    args: [form.endpoint, (form.signer || account) as Hex, BigInt(Math.round(Number(form.price) * 1e6)), ""],
                    value: parseEther(form.stake),
                  } as never)
                }
              >
                {wallet ? "Stake and register" : "Connect a wallet"}
              </Button>
            </>
          }
        >
          {steps ? (
            <Steps steps={steps} />
          ) : (
            <>
              <Field label="Endpoint"><input className={inputCls} value={form.endpoint} onChange={(e) => setForm({ ...form, endpoint: e.target.value })} /></Field>
              <Field label="Signer address" hint="The key that signs admissions and World ID attestations.">
                <input className={inputCls} value={form.signer} onChange={(e) => setForm({ ...form, signer: e.target.value })} placeholder={account} />
              </Field>
              <div className="grid grid-cols-2 gap-3">
                <Field label="USDC per seat per month"><input className={inputCls} value={form.price} onChange={(e) => setForm({ ...form, price: e.target.value })} /></Field>
                <Field label="Stake (ETH, at least 0.01)"><input className={inputCls} value={form.stake} onChange={(e) => setForm({ ...form, stake: e.target.value })} /></Field>
              </div>
            </>
          )}
        </Sheet>
      )}

      {sheet === "challenge" && (
        <Sheet
          title="Submit a fraud proof"
          onClose={close}
          locked={busy}
          footer={
            <>
              <span className="text-[13px] text-muted">Half the stake goes to you</span>
              <Button
                kind="danger"
                disabled={!wallet || !att.trim() || busy}
                onClick={() => {
                  try {
                    const a = JSON.parse(att);
                    run("Submit fraud proof", {
                      address: REGISTRY,
                      abi: registryAbi,
                      functionName: "slash",
                      args: [{ providerId: BigInt(a.providerId), dnsName: a.dnsName, resource: BigInt(a.resource), issuedAt: BigInt(a.issuedAt), validUntil: BigInt(a.validUntil), peerKey: a.peerKey }, a.sig],
                    });
                  } catch {
                    toast("The attestation must be JSON", "err");
                  }
                }}
              >
                Submit
              </Button>
            </>
          }
        >
          {steps ? (
            <Steps steps={steps} />
          ) : (
            <Field label="Signed admission" hint="Paste what the provider signed. The contract checks ENS itself: if the name was already revoked at issuedAt, the stake is slashed.">
              <textarea className={cx(inputCls, "h-40 text-[12px]")} value={att} onChange={(e) => setAtt(e.target.value)} placeholder='{"providerId":1,"dnsName":"…","sig":"0x…"}' />
            </Field>
          )}
        </Sheet>
      )}
    </>
  );
}

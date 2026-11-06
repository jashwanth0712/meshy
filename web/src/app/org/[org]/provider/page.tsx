"use client";
import { Gavel, ShieldCheck } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { type Hex, parseEther } from "viem";
import { type Step, Steps } from "@/components/Steps";
import { Badge, Button, Card, Ext, Field, Short, inputCls, links } from "@/components/ui";
import { PROVIDER_API } from "@/lib/config";
import { send } from "@/lib/ens";
import { type Provider, REGISTRY, STATUS, eth, registryAbi, usdc } from "@/lib/contracts";
import { useMeshy } from "@/lib/store";

type Health = { ok: boolean; attestor: string; world: string; devAttest: boolean; version?: string };

export default function ProviderPage() {
  const { client, wallet, account, toast } = useMeshy();
  const [list, setList] = useState<(Provider & { id: bigint })[]>();
  const [health, setHealth] = useState<Health | "down">();
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

  return (
    <div className="space-y-5">
      <Card title={<span className="flex items-center gap-2"><ShieldCheck size={15} /> Staked providers</span>} actions={<Ext href={links.addr(REGISTRY)}>ProviderRegistry</Ext>}>
        <p className="mb-4 text-sm text-muted">
          Anyone can run a Meshy provider (NetBird mesh + meshy-sync). Paid providers stake ETH; if a provider attests access for a name that ENS shows was already revoked, anyone
          can submit the signed attestation as a fraud proof and the stake is slashed (50% to the reporter).
        </p>
        {!list ? (
          <div className="text-sm text-muted">Reading ProviderRegistry…</div>
        ) : (
          <div className="divide-y divide-line">
            {list.map((p) => (
              <div key={p.id.toString()} className="grid grid-cols-[50px_1fr_auto] items-center gap-4 py-3 text-sm">
                <span className="mono text-muted">#{p.id.toString()}</span>
                <div>
                  <div className="mono">{p.endpoint}</div>
                  <div className="text-xs text-muted">
                    owner <Short v={p.owner} n={4} /> · signer <Short v={p.signer} n={4} /> · {usdc(p.pricePerSeatPerMonth)} USDC/seat/mo
                  </div>
                </div>
                <div className="text-right">
                  <Badge tone={p.status === 0 ? "ok" : p.status === 3 ? "bad" : "warn"}>{STATUS[p.status] ?? p.status}</Badge>
                  <div className="mt-1 text-xs text-muted">{eth(p.stake)} ETH staked</div>
                </div>
              </div>
            ))}
          </div>
        )}
      </Card>
      <div className="grid gap-5 lg:grid-cols-2">
        <Card title="This dashboard's provider (meshy-sync)">
          {health === "down" ? (
            <p className="text-sm text-bad">Unreachable at {PROVIDER_API}</p>
          ) : !health ? (
            <p className="text-sm text-muted">…</p>
          ) : (
            <div className="grid grid-cols-[110px_1fr] gap-y-1.5 text-sm">
              <span className="text-muted">endpoint</span>
              <span className="mono">{PROVIDER_API}</span>
              <span className="text-muted">attestor</span>
              <Short v={health.attestor} />
              <span className="text-muted">World ID</span>
              <span>{health.world}</span>
              <span className="text-muted">dev attest</span>
              <span>{health.devAttest ? <Badge tone="warn">on</Badge> : <Badge tone="ok">off</Badge>}</span>
            </div>
          )}
        </Card>
        <Card title="Register as a provider">
          <div className="space-y-3">
            <Field label="Endpoint">
              <input className={inputCls} value={form.endpoint} onChange={(e) => setForm({ ...form, endpoint: e.target.value })} />
            </Field>
            <Field label="Signer address" hint="Key that signs admissions and World ID attestations (meshy-sync --signer-key-file).">
              <input className={inputCls} value={form.signer} onChange={(e) => setForm({ ...form, signer: e.target.value })} placeholder={account} />
            </Field>
            <div className="grid grid-cols-2 gap-3">
              <Field label="Price (USDC / seat / month)">
                <input className={inputCls} value={form.price} onChange={(e) => setForm({ ...form, price: e.target.value })} />
              </Field>
              <Field label="Stake (ETH, min 0.01)">
                <input className={inputCls} value={form.stake} onChange={(e) => setForm({ ...form, stake: e.target.value })} />
              </Field>
            </div>
            <Button
              kind="primary"
              disabled={!wallet}
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
              Stake & register
            </Button>
          </div>
        </Card>
      </div>
      <Card title={<span className="flex items-center gap-2"><Gavel size={15} /> Challenge a provider (fraud proof)</span>}>
        <p className="mb-3 text-sm text-muted">
          Paste a provider-signed admission attestation <span className="mono">{"{providerId, dnsName, resource, issuedAt, validUntil, peerKey, sig}"}</span>. The contract checks ENSv2
          on-chain: if the name was dead at <span className="mono">issuedAt</span>, the stake is slashed.
        </p>
        <textarea className={inputCls + " h-28"} value={att} onChange={(e) => setAtt(e.target.value)} />
        <div className="mt-3">
          <Button
            kind="danger"
            disabled={!wallet || !att.trim()}
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
                toast("Attestation must be JSON", "err");
              }
            }}
          >
            Submit slash
          </Button>
        </div>
        {steps && <div className="mt-4"><Steps steps={steps} /></div>}
      </Card>
    </div>
  );
}

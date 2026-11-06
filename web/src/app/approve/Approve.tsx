"use client";
import { Bot, CheckCircle2, Fingerprint, Network, ShieldCheck, Wallet } from "lucide-react";
import Link from "next/link";
import { useEffect, useRef, useState } from "react";
import { type Address, isAddress, zeroAddress } from "viem";
import { type Step, Steps } from "@/components/Steps";
import { Badge, Button, Card, Ext, Name, Short } from "@/components/ui";
import { PROVIDER_API } from "@/lib/config";
import { deployResolver, eacAbi, getState, recordCalls, registryAbi, resolverAbi, resolverOf, send, textKeySetter, yearFromNow } from "@/lib/ens";
import { useMeshy } from "@/lib/store";

type Health = { ok: boolean; attestor: string; world: string; devAttest: boolean };
type Flow = { pollId: string; userCode: string; verificationUri: string; verificationUriComplete?: string; interval: number };

async function sha256fp(key: string) {
  const b64 = key.trim().split(" ")[1];
  if (!b64) return "";
  const raw = Uint8Array.from(atob(b64), (c) => c.charCodeAt(0));
  const h = new Uint8Array(await crypto.subtle.digest("SHA-256", raw));
  return "SHA256:" + btoa(String.fromCharCode(...h)).replace(/=+$/, "");
}

export function Approve({ org, agent, addr, ssh }: { org: string; agent: string; addr: string; ssh: string }) {
  const { tree, account, connect, wallet, client, toast, error } = useMeshy();
  const name = `${agent}.agents.${org}`;
  const [fp, setFp] = useState("");
  const [health, setHealth] = useState<Health | null | "down">(null);
  const [flow, setFlow] = useState<Flow>();
  const [flowStatus, setFlowStatus] = useState<string>();
  const [attestation, setAttestation] = useState<string>();
  const [dev, setDev] = useState(false);
  const [steps, setSteps] = useState<Step[]>();
  const [done, setDone] = useState(false);
  const polling = useRef(false);
  const valid = /^[a-z0-9-]{1,63}$/.test(agent) && isAddress(addr) && ssh.startsWith("ssh-");
  const isOwner = !!account; // roles are enforced on-chain (owner or delegated admin)
  const already = tree?.agents.find((a) => a.label === agent);

  useEffect(() => {
    sha256fp(ssh).then(setFp).catch(() => setFp(""));
    fetch(`${PROVIDER_API}/v1/health`)
      .then((r) => r.json())
      .then(setHealth)
      .catch(() => setHealth("down"));
  }, [ssh]);

  async function startWorld() {
    setFlowStatus("starting");
    try {
      const r = await fetch(`${PROVIDER_API}/v1/agent/device-flow/start`, { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ org, agent, addr }) });
      if (!r.ok) throw new Error(await r.text());
      const f = (await r.json()) as Flow;
      setFlow(f);
      setFlowStatus("pending");
      if (polling.current) return;
      polling.current = true;
      for (;;) {
        await new Promise((x) => setTimeout(x, Math.max(2, f.interval || 5) * 1000));
        const p = await fetch(`${PROVIDER_API}/v1/agent/device-flow/poll`, { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ pollId: f.pollId }) }).then((x) => x.json());
        if (p.status === "ok") {
          setAttestation(p.attestation);
          setFlowStatus("ok");
          break;
        }
        if (p.status !== "pending") {
          setFlowStatus(p.status);
          toast(`World ID ${p.status}: the agent will not be admitted`, "err");
          break;
        }
      }
    } catch (e) {
      setFlowStatus("error");
      toast(e instanceof Error ? e.message : String(e), "err");
    } finally {
      polling.current = false;
    }
  }

  async function devAttest() {
    const r = await fetch(`${PROVIDER_API}/v1/agent/attest-dev`, { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ org, agent, addr }) });
    if (!r.ok) return toast("Provider has dev attestation disabled", "err");
    setAttestation((await r.json()).attestation);
    setDev(true);
  }

  async function approve() {
    if (!wallet || !tree || !account || !attestation) return;
    const up = (i: number, p: Partial<Step>) => setSteps((s) => s!.map((x, j) => (j === i ? { ...x, ...p } : x)));
    const records = {
      "meshy.ssh": ssh.trim(),
      "meshy.worldid": attestation,
      "agent-context": `Meshy agent ${agent}. Access is granted per resource via ENS subnames <agent>.<resource>.`,
    };
    const calls = recordCalls(name, records, addr as Address);
    try {
      const st = await getState(client, tree.agentsRegistry, agent);
      if (st.status === 2) {
        // Existing agent: update the records on whatever resolver it already uses.
        setSteps([{ label: `Update ${name}'s records (new keys + attestation)`, state: "doing" }]);
        const res = await resolverOf(client, tree.agentsRegistry, agent);
        await send(wallet, client, { address: res, abi: resolverAbi, functionName: "multicall", args: [calls] }, (h) => up(0, { tx: h }));
        up(0, { state: "done" });
      } else {
        setSteps([
          { label: "Deploy the agent's own PermissionedResolver (records written in the same tx)", state: "doing" },
          { label: `Register ${name} → that resolver`, state: "todo" },
          { label: "Let the agent rotate only its meshy.ssh key (EAC setter role)", state: "todo" },
        ]);
        const res = await deployResolver(wallet, client, account, calls);
        up(0, { state: "done", detail: res });
        up(1, { state: "doing" });
        await send(wallet, client, { address: tree.agentsRegistry, abi: registryAbi, functionName: "register", args: [agent, account, zeroAddress, res, 0n, yearFromNow()] }, (h) => up(1, { tx: h }));
        up(1, { state: "done" });
        up(2, { state: "doing" });
        await send(wallet, client, { address: res, abi: eacAbi, functionName: "grantSetterRoles", args: [textKeySetter("meshy.ssh"), addr as Address] }, (h) => up(2, { tx: h }));
        up(2, { state: "done", detail: "the org keeps membership (it can unregister the name); the agent's data is its own" });
      }
      setDone(true);
      toast(`${name} approved`, "ok");
    } catch (e) {
      const msg = e instanceof Error ? e.message.split("\n")[0] : String(e);
      setSteps((s) => s?.map((x) => (x.state === "doing" ? { ...x, state: "fail", detail: msg } : x)));
      toast(msg, "err");
    }
  }

  return (
    <main className="mx-auto max-w-2xl px-6 py-12">
      <div className="mb-8 flex items-center gap-3">
        <div className="grid h-10 w-10 place-items-center rounded-xl bg-accent/15 text-accent">
          <Network size={20} />
        </div>
        <div>
          <h1 className="text-xl font-semibold">Approve an agent</h1>
          <p className="text-sm text-muted">An AI agent is asking to join {org}</p>
        </div>
      </div>
      {!valid ? (
        <Card title="Invalid request">
          <p className="text-sm text-bad">This approval link is missing the agent&apos;s keys. Ask the agent to run <span className="mono">meshy join &lt;invite&gt;</span> and send you the link it prints.</p>
        </Card>
      ) : (
        <div className="space-y-5">
          <Card title={<span className="flex items-center gap-2"><Bot size={15} className="text-warn" /> Requesting agent</span>}>
            <div className="grid grid-cols-[120px_1fr] gap-y-2 text-sm">
              <span className="text-muted">ENS name</span>
              <Name name={name} />
              <span className="text-muted">address</span>
              <Short v={addr} />
              <span className="text-muted">SSH key</span>
              <span className="mono text-xs">{fp}</span>
              <span className="text-muted">status</span>
              <span>{already ? <Badge tone="warn">already exists — approving updates its keys</Badge> : <Badge>new</Badge>}</span>
            </div>
            <p className="mt-3 text-xs text-muted">Check that the SSH fingerprint matches what the agent printed in its terminal.</p>
          </Card>

          <Card title={<span className="flex items-center gap-2"><Wallet size={15} /> 1. Sign in as an org admin</span>}>
            {error && <p className="mb-2 text-sm text-bad">{error}</p>}
            {account ? (
              <div className="text-sm">
                <Short v={account} /> {tree && tree.owner.toLowerCase() === account.toLowerCase() ? <Badge tone="ok">org owner</Badge> : <Badge tone="info">must be the owner or a delegated admin</Badge>}
              </div>
            ) : (
              <Button kind="primary" onClick={connect}>
                <Wallet size={14} /> Connect wallet
              </Button>
            )}
          </Card>

          <Card title={<span className="flex items-center gap-2"><Fingerprint size={15} /> 2. Prove a human backs this agent (World ID)</span>}>
            {health === "down" ? (
              <p className="text-sm text-bad">Provider API unreachable at {PROVIDER_API}. Start meshy-sync.</p>
            ) : !health ? (
              <p className="text-sm text-muted">Contacting provider…</p>
            ) : attestation ? (
              <div className="flex items-center gap-2 text-sm text-accent">
                <ShieldCheck size={16} /> {dev ? "Dev attestation issued (demo mode — not a real World ID proof)" : "World ID verified — attestation signed by the provider"}
                <Badge>{health.attestor.slice(0, 10)}…</Badge>
              </div>
            ) : (
              <div className="space-y-3 text-sm">
                {health.world !== "disabled" && (
                  <>
                    {!flow ? (
                      <Button kind="primary" onClick={startWorld} disabled={flowStatus === "starting"}>
                        <Fingerprint size={14} /> Verify with World ID Agent Pass
                      </Button>
                    ) : (
                      <div className="rounded-lg border border-line bg-bg p-4">
                        <div className="text-xs uppercase tracking-wider text-muted">Your code</div>
                        <div className="mono mt-1 text-2xl tracking-widest text-accent">{flow.userCode}</div>
                        <div className="mt-2">
                          <Ext href={flow.verificationUriComplete ?? flow.verificationUri}>Open World ID verification</Ext>
                        </div>
                        <div className="mt-2 text-xs text-muted">Status: {flowStatus}</div>
                      </div>
                    )}
                  </>
                )}
                {health.devAttest && (
                  <div className="space-y-1">
                    <Button onClick={devAttest}>Use dev attestation (demo)</Button>
                    <div className="text-xs text-muted">Provider runs with --dev-attest. Clearly labelled; production uses World ID only.</div>
                  </div>
                )}
                {health.world === "disabled" && !health.devAttest && <p className="text-bad">Provider has World ID disabled.</p>}
              </div>
            )}
          </Card>

          <Card title={<span className="flex items-center gap-2"><CheckCircle2 size={15} /> 3. Approve</span>}>
            {done ? (
              <div className="space-y-2 text-sm">
                <p className="text-accent">Approved. The agent&apos;s terminal will now print MESHY_JOINED.</p>
                <Link href={`/org/${org}/grants`} className="text-accent-2 hover:underline">Grant it access to a resource →</Link>
              </div>
            ) : steps ? (
              <Steps steps={steps} />
            ) : (
              <Button kind="primary" disabled={!isOwner || !attestation || !tree || tree.agentsRegistry === zeroAddress} onClick={approve}>
                Approve {agent} (3 transactions)
              </Button>
            )}
          </Card>
        </div>
      )}
    </main>
  );
}

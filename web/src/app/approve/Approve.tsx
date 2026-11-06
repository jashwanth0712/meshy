"use client";
import Link from "next/link";
import { useEffect, useRef, useState } from "react";
import { type Address, isAddress, zeroAddress } from "viem";
import { type Step, Steps } from "@/components/Steps";
import { Logo } from "@/components/Shell";
import { Button, Ext, Short, cx } from "@/components/ui";
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

  const step = !account ? 1 : !attestation ? 2 : 3;
  return (
    <div className="min-h-screen">
      <header className="border-b border-line">
        <div className="mx-auto flex h-14 max-w-xl items-center justify-between px-6">
          <Logo />
          {account ? <Short v={account} /> : <Button className="h-8 px-3" onClick={connect}>Connect wallet</Button>}
        </div>
      </header>
      <main className="mx-auto max-w-xl px-6 pb-24 pt-12">
        <h1 className="text-[30px] font-semibold leading-tight tracking-[-0.025em]">Approve {agent || "an agent"}</h1>
        <p className="mt-2 text-[15px] leading-relaxed text-muted">
          An agent wants to join <span className="mono text-fg">{org}</span>. Approving gives it a name only. It can&apos;t reach anything until you give it access.
        </p>

        {!valid ? (
          <p className="mt-10 text-[14px] text-bad">
            This link is missing the agent&apos;s keys. Ask the agent to run <span className="mono">meshy join &lt;invite&gt;</span> and send you the link it prints.
          </p>
        ) : (
          <>
            <section className="mt-10">
              <h2 className="text-[14px] font-semibold">1 · Check the key</h2>
              <p className="mt-1 text-[13px] text-muted">It should match what the agent printed in its terminal.</p>
              <div className="mono mt-3 break-all rounded-xl bg-panel-2 px-4 py-3 text-[14px]">{fp || "…"}</div>
              <p className="mono mt-2 text-[13px] text-muted">{name} · <Short v={addr} /></p>
              {already && <p className="mt-2 text-[13px] text-warn">This agent already exists. Approving updates its keys.</p>}
            </section>

            <section className="mt-10">
              <h2 className={cx("text-[14px] font-semibold", step < 2 && "text-muted")}>2 · Prove a human is behind it</h2>
              {error && <p className="mt-2 text-[13px] text-bad">{error}</p>}
              {!account ? (
                <p className="mt-2 text-[13px] text-muted">Connect the owner&apos;s or an admin&apos;s wallet first.</p>
              ) : health === "down" ? (
                <p className="mt-2 text-[13px] text-bad">The provider is unreachable at {PROVIDER_API}.</p>
              ) : !health ? (
                <p className="mt-2 text-[13px] text-muted">Contacting the provider…</p>
              ) : attestation ? (
                <p className="mt-2 text-[14px]">{dev ? "Dev attestation issued (demo only)." : "World ID verified."}</p>
              ) : (
                <div className="mt-3 space-y-3">
                  {health.world !== "disabled" &&
                    (!flow ? (
                      <Button kind="primary" onClick={startWorld} disabled={flowStatus === "starting"}>Verify with World ID</Button>
                    ) : (
                      <div className="rounded-xl bg-panel-2 p-4">
                        <div className="mono text-[22px] font-semibold tracking-[0.2em]">{flow.userCode}</div>
                        <div className="mt-2 text-[13px]"><Ext href={flow.verificationUriComplete ?? flow.verificationUri}>Open World ID</Ext> <span className="text-muted">· {flowStatus}</span></div>
                      </div>
                    ))}
                  {health.devAttest && (
                    <button onClick={devAttest} className="block text-[14px] font-medium text-accent hover:underline">Use dev attestation (demo)</button>
                  )}
                  {health.world === "disabled" && !health.devAttest && <p className="text-[13px] text-bad">This provider has World ID turned off.</p>}
                </div>
              )}
            </section>

            <section className="mt-10">
              <h2 className={cx("text-[14px] font-semibold", step < 3 && "text-muted")}>3 · Approve</h2>
              <div className="mt-3">
                {done ? (
                  <div className="space-y-2 text-[14px]">
                    <p>Approved. The agent&apos;s terminal will now print MESHY_JOINED.</p>
                    <Link href={`/org/${org}?give=${agent}`} className="font-medium text-accent hover:underline">Give it access</Link>
                  </div>
                ) : steps ? (
                  <Steps steps={steps} />
                ) : (
                  <Button kind="primary" disabled={!isOwner || !attestation || !tree || tree.agentsRegistry === zeroAddress} onClick={approve}>
                    Approve {agent} · {already ? "1 transaction" : "3 transactions"}
                  </Button>
                )}
              </div>
            </section>
          </>
        )}
      </main>
    </div>
  );
}

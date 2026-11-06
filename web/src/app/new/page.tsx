"use client";
// Create an organization: <label>.meshy.eth, owned by your wallet, in one transaction.
import Link from "next/link";
import { useEffect, useState } from "react";
import { type Address, type Hex, type WalletClient } from "viem";
import { Logo } from "@/components/Shell";
import { Button, Field, inputCls } from "@/components/ui";
import { publicClient } from "@/lib/ens";
import { PROVIDER_API, ETHERSCAN } from "@/lib/config";
import { ORG_FACTORY, ORG_PARENT, deriveOrgKey, factoryAbi, labelOk, orgsLive } from "@/lib/orgs";
import { connectWallet, silentAccount } from "@/lib/wallet";

type Step = "name" | "key" | "create" | "done";

export default function NewOrg() {
  const client = publicClient();
  const [wallet, setWallet] = useState<WalletClient>();
  const [account, setAccount] = useState<Address>();
  const [label, setLabel] = useState("");
  const [avail, setAvail] = useState<"idle" | "checking" | "free" | "taken" | "invalid">("idle");
  const [step, setStep] = useState<Step>("name");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const [tx, setTx] = useState<Hex>();
  const name = label ? `${label}.${ORG_PARENT}` : "";

  useEffect(() => {
    silentAccount().then((a) => a && setAccount(a));
  }, []);

  // Live availability check (debounced).
  useEffect(() => {
    if (!label) return setAvail("idle");
    if (!labelOk(label)) return setAvail("invalid");
    setAvail("checking");
    const t = setTimeout(async () => {
      try {
        const ok = (await client.readContract({ address: ORG_FACTORY, abi: factoryAbi, functionName: "isAvailable", args: [label] })) as boolean;
        setAvail(ok ? "free" : "taken");
      } catch {
        setAvail("idle");
      }
    }, 350);
    return () => clearTimeout(t);
  }, [label, client]);

  async function ensureWallet() {
    if (wallet && account) return { wallet, account };
    const w = await connectWallet();
    setWallet(w.wallet);
    setAccount(w.account);
    return w;
  }

  async function create() {
    setErr("");
    setBusy(true);
    try {
      const { wallet, account } = await ensureWallet();
      setStep("key");
      const key = await deriveOrgKey(wallet, account, name);
      setStep("create");
      let attestor = "";
      try {
        attestor = ((await (await fetch(`${PROVIDER_API}/v1/health`)).json()) as { attestor?: string }).attestor ?? "";
      } catch {}
      const hash = await wallet.writeContract({
        account,
        chain: wallet.chain,
        address: ORG_FACTORY,
        abi: factoryAbi,
        functionName: "createOrg",
        args: [label, key.pub, attestor],
      });
      setTx(hash);
      const r = await client.waitForTransactionReceipt({ hash });
      if (r.status !== "success") throw new Error("The transaction reverted. The name may have just been taken.");
      localStorage.setItem(`meshy.orgkey.${name}`, key.priv);
      setStep("done");
    } catch (e) {
      setErr(e instanceof Error ? e.message.split("\n")[0] : String(e));
      setStep("name");
    } finally {
      setBusy(false);
    }
  }

  const hint = {
    idle: `Lowercase letters, numbers and dashes. Your org will be ${label || "acme"}.${ORG_PARENT}`,
    checking: "Checking…",
    free: `${name} is available`,
    taken: `${name} is taken`,
    invalid: "Use 1–32 lowercase letters, numbers or dashes (not at the start or end).",
  }[avail];

  return (
    <main className="mx-auto max-w-xl px-6 py-14">
      <Link href="/" aria-label="Meshy home">
        <Logo className="h-6" />
      </Link>
      <h1 className="mt-10 text-[32px] font-semibold leading-tight tracking-[-0.03em]">Create an organization</h1>
      <p className="mt-3 text-[15px] leading-relaxed text-muted">
        Your org is an ENS name owned by your wallet. People, agents, networks and devices live under it, and access is just more names.
      </p>

      {!orgsLive ? (
        <div className="mt-8 rounded-2xl border border-line bg-panel p-5 text-[14px] text-muted">Org creation is being deployed. Check back in a few minutes.</div>
      ) : step === "done" ? (
        <div className="mt-8 space-y-4 rounded-2xl border border-line bg-panel p-5">
          <div className="text-[17px] font-semibold">{name} is yours</div>
          <p className="text-[14px] text-muted">
            The org key was derived from your signature and saved in this browser. You can re-derive it anywhere by signing the same message.
          </p>
          {tx && (
            <a className="text-[13px] text-accent hover:underline" href={`${ETHERSCAN}/tx/${tx}`} target="_blank" rel="noreferrer">
              View transaction
            </a>
          )}
          <div className="flex flex-wrap gap-2 pt-2">
            <Link href={`/org/${name}/people`} className="inline-flex h-9 items-center rounded-full bg-accent px-4 text-[14px] font-semibold text-on-accent">
              Invite people
            </Link>
            <Link href={`/org/${name}/directory`} className="inline-flex h-9 items-center rounded-full border border-line-strong px-4 text-[14px] font-semibold">
              Add a network and devices
            </Link>
          </div>
        </div>
      ) : (
        <form
          className="mt-8 space-y-5 rounded-2xl border border-line bg-panel p-5"
          onSubmit={(e) => {
            e.preventDefault();
            if (avail === "free" && !busy) create();
          }}
        >
          <Field label="Organization name" hint={<span className={avail === "taken" || avail === "invalid" ? "text-bad" : avail === "free" ? "text-ok" : ""}>{hint}</span>}>
            <div className="flex items-center gap-2">
              <input
                autoFocus
                value={label}
                onChange={(e) => setLabel(e.target.value.toLowerCase().trim())}
                placeholder="acme"
                aria-label="Organization name"
                className={inputCls}
              />
              <span className="mono shrink-0 text-[14px] text-muted">.{ORG_PARENT}</span>
            </div>
          </Field>
          <ol className="space-y-1.5 text-[13px] text-muted">
            <li className={step === "key" ? "text-fg" : ""}>1. Sign once to derive your org&apos;s encryption key (free, nothing on-chain)</li>
            <li className={step === "create" ? "text-fg" : ""}>2. One transaction creates the org, its people and agents registries, and its records</li>
          </ol>
          {err && <div className="rounded-xl border border-bad/40 bg-bad/10 px-3.5 py-2.5 text-[13px] text-bad">{err}</div>}
          <div className="flex items-center justify-between gap-3">
            <span className="mono text-[12px] text-muted">{account ? `${account.slice(0, 6)}…${account.slice(-4)}` : "Wallet not connected"}</span>
            <Button type="submit" kind="primary" disabled={avail !== "free" || busy}>
              {busy ? (step === "key" ? "Sign in your wallet…" : "Creating…") : account ? "Create organization" : "Connect wallet and create"}
            </Button>
          </div>
          <p className="text-[12px] text-muted">
            Sepolia gas is paid by your wallet (about 1.5M gas). Need test ETH? Use a Sepolia faucet. Want your own <span className="mono">.eth</span> instead? That&apos;s coming next.
          </p>
        </form>
      )}
    </main>
  );
}

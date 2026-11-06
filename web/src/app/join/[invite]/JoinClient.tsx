"use client";
// Accept an invite: decode the signed blob, check it on-chain, claim the name in one tx.
import Link from "next/link";
import { use, useEffect, useState } from "react";
import { type Address, type Hex, type WalletClient } from "viem";
import { Logo } from "@/components/Shell";
import { Button, Field, inputCls } from "@/components/ui";
import { ETHERSCAN } from "@/lib/config";
import { publicClient } from "@/lib/ens";
import { INVITES, type InviteBlob, ZERO_ADDRESS, decodeInvite, invitesAbi, labelOk } from "@/lib/orgs";
import { connectWallet, silentAccount } from "@/lib/wallet";

export function JoinClient({ params }: { params: Promise<{ invite: string }> }) {
  const { invite } = use(params);
  const client = publicClient();
  const [blob, setBlob] = useState<InviteBlob | null>();
  const [wallet, setWallet] = useState<WalletClient>();
  const [account, setAccount] = useState<Address>();
  const [label, setLabel] = useState("");
  const [why, setWhy] = useState<string>();
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const [done, setDone] = useState<{ name: string; tx: Hex }>();

  useEffect(() => {
    try {
      const b = decodeInvite(invite);
      setBlob(b);
      if (b.inv.label) setLabel(b.inv.label);
    } catch {
      setBlob(null);
    }
    silentAccount().then((a) => a && setAccount(a));
  }, [invite]);

  // Live on-chain check for this claimer + name.
  useEffect(() => {
    if (!blob || !label || !labelOk(label)) return setWhy(undefined);
    const t = setTimeout(async () => {
      try {
        const r = (await client.readContract({
          address: INVITES,
          abi: invitesAbi,
          functionName: "check",
          args: [blob.inv, blob.sig, account ?? ZERO_ADDRESS, label],
        })) as string;
        setWhy(r === "invite is for another wallet" && !account ? "" : r);
      } catch (e) {
        setWhy(e instanceof Error ? e.message.split("\n")[0] : String(e));
      }
    }, 300);
    return () => clearTimeout(t);
  }, [blob, label, account, client]);

  async function claim() {
    if (!blob) return;
    setErr("");
    setBusy(true);
    try {
      let w = wallet;
      let a = account;
      if (!w || !a) {
        const c = await connectWallet();
        w = c.wallet;
        a = c.account;
        setWallet(w);
        setAccount(a);
      }
      const reason = (await client.readContract({ address: INVITES, abi: invitesAbi, functionName: "check", args: [blob.inv, blob.sig, a, label] })) as string;
      if (reason) throw new Error(`This invite can't be used: ${reason}.`);
      const tx = await w.writeContract({ account: a, chain: w.chain, address: INVITES, abi: invitesAbi, functionName: "claim", args: [blob.inv, blob.sig, label, ""] });
      const r = await client.waitForTransactionReceipt({ hash: tx });
      if (r.status !== "success") throw new Error("The claim transaction reverted.");
      setDone({ name: `${label}.${blob.inv.parentName}`, tx });
    } catch (e) {
      setErr(e instanceof Error ? e.message.split("\n")[0] : String(e));
    } finally {
      setBusy(false);
    }
  }

  const expires = blob ? new Date(Number(blob.inv.expiresAt) * 1000) : undefined;
  const expired = expires ? expires.getTime() < Date.now() : false;
  const role = blob?.role === "agent" ? "an agent" : "a member";

  return (
    <main className="mx-auto max-w-xl px-6 py-14">
      <Link href="/" aria-label="Meshy home">
        <Logo className="h-6" />
      </Link>
      {blob === null ? (
        <p className="mt-10 text-[15px] text-muted">This invite link is damaged. Ask for a new one.</p>
      ) : !blob ? null : done ? (
        <div className="mt-10 space-y-4">
          <h1 className="text-[30px] font-semibold tracking-[-0.03em]">Welcome to {blob.org}</h1>
          <p className="text-[15px] text-muted">
            You are <span className="mono text-fg">{done.name}</span>. Access to folders and ports is granted by the org&apos;s admins.
          </p>
          <a className="text-[13px] text-accent hover:underline" href={`${ETHERSCAN}/tx/${done.tx}`} target="_blank" rel="noreferrer">
            View transaction
          </a>
          <div className="rounded-2xl border border-line bg-panel p-5 text-[14px]">
            <div className="font-semibold">Next</div>
            <ul className="mt-2 space-y-1.5 text-muted">
              <li>
                Get <a href="/download" className="text-accent hover:underline">Meshy for Mac</a> and sign in with this wallet to see your access.
              </li>
              {blob.role === "agent" && (
                <li>
                  For an agent, run <span className="mono text-fg">meshy join</span> on its machine, then approve it from the dashboard.
                </li>
              )}
              <li>
                <Link href={`/org/${blob.org}`} className="text-accent hover:underline">
                  Open {blob.org}
                </Link>
              </li>
            </ul>
          </div>
        </div>
      ) : (
        <>
          <h1 className="mt-10 text-[30px] font-semibold leading-tight tracking-[-0.03em]">
            Join <span className="mono">{blob.org}</span>
          </h1>
          <p className="mt-3 text-[15px] text-muted">
            <span className="mono">
              {blob.by.slice(0, 6)}…{blob.by.slice(-4)}
            </span>{" "}
            invited you as {role}. {expires && (expired ? "This invite has expired." : `The invite expires ${expires.toLocaleString()}.`)}
          </p>
          <form
            className="mt-8 space-y-5 rounded-2xl border border-line bg-panel p-5"
            onSubmit={(e) => {
              e.preventDefault();
              if (!busy) claim();
            }}
          >
            <Field
              label="Your name in the org"
              hint={
                why ? <span className="text-bad">{why}</span> : label && labelOk(label) ? <span className="mono">{`${label}.${blob.inv.parentName}`}</span> : "Lowercase letters, numbers and dashes."
              }
            >
              <input value={label} readOnly={!!blob.inv.label} onChange={(e) => setLabel(e.target.value.toLowerCase().trim())} placeholder="alice" className={inputCls} />
            </Field>
            {blob.inv.invitee !== ZERO_ADDRESS && (
              <p className="text-[12px] text-muted">
                Only wallet <span className="mono">{blob.inv.invitee}</span> can accept this invite.
              </p>
            )}
            {err && <div className="rounded-xl border border-bad/40 bg-bad/10 px-3.5 py-2.5 text-[13px] text-bad">{err}</div>}
            <div className="flex items-center justify-between gap-3">
              <span className="mono text-[12px] text-muted">{account ? `${account.slice(0, 6)}…${account.slice(-4)}` : "Wallet not connected"}</span>
              <Button type="submit" kind="primary" disabled={busy || expired || !labelOk(label) || !!why}>
                {busy ? "Confirm in your wallet…" : account ? "Accept invite" : "Connect wallet and accept"}
              </Button>
            </div>
            <p className="text-[12px] text-muted">One Sepolia transaction, paid by your wallet. It registers your name; nobody else can take it or change it.</p>
          </form>
        </>
      )}
    </main>
  );
}

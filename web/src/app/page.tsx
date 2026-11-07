"use client";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { Logo } from "@/components/Shell";
import { DEMO_ORG, PROVIDER_API, isOrgName } from "@/lib/config";
import { forgetOrg, recentOrgs } from "@/lib/recent";
import { connectWallet, silentAccount } from "@/lib/wallet";

type MyOrg = { org: string; role: string; name?: string };

export default function Home() {
  const [org, setOrg] = useState("");
  const [recent, setRecent] = useState<string[]>([]);
  const [bad, setBad] = useState(false);
  const router = useRouter();
  const [account, setAccount] = useState<string>();
  const [mine, setMine] = useState<MyOrg[]>();
  const [finding, setFinding] = useState(false);
  // eslint-disable-next-line react-hooks/set-state-in-effect
  useEffect(() => setRecent(recentOrgs()), []);
  // Your orgs come from the chain (created, administered, or joined), found from the connected wallet.
  const find = async (a: string, auto: boolean) => {
    setFinding(true);
    try {
      const j = (await (await fetch(`${PROVIDER_API}/v1/orgs?addr=${a}`)).json()) as { orgs?: MyOrg[] };
      const orgs = j.orgs ?? [];
      setMine(orgs);
      if (auto && orgs.length === 1) router.push(`/org/${orgs[0].org}`);
    } catch {
      setMine([]);
    } finally {
      setFinding(false);
    }
  };
  useEffect(() => {
    silentAccount().then((a) => {
      if (!a) return;
      setAccount(a);
      find(a, true);
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  const connect = async () => {
    try {
      const c = await connectWallet();
      setAccount(c.account);
      find(c.account, true);
    } catch {}
  };
  const go = (o: string) => {
    const name = o.trim().toLowerCase();
    if (!isOrgName(name)) return setBad(true);
    router.push(`/org/${name}`);
  };
  return (
    <main className="mx-auto flex min-h-screen max-w-xl flex-col justify-center px-6 py-16">
      <Logo className="h-7" />
      <h1 className="mt-12 text-[40px] font-semibold leading-[1.05] tracking-[-0.035em]">Network access for agents, as ENS names.</h1>
      <p className="mt-4 text-[16px] leading-relaxed text-muted">
        Give an agent one folder or one port for an hour. Revoke it, and the session ends in the next block. Every action is logged on Arkiv.
      </p>
      <div className="mt-10 rounded-2xl border border-line bg-panel p-4">
        {!account ? (
          <div className="flex items-center justify-between gap-3">
            <span className="text-[14px] text-muted">Connect your wallet and Meshy finds your orgs.</span>
            <button onClick={connect} className="h-9 rounded-full bg-accent px-5 text-[14px] font-semibold text-on-accent hover:opacity-90">
              Connect wallet
            </button>
          </div>
        ) : finding || !mine ? (
          <p className="text-[14px] text-muted">Finding your orgs on chain…</p>
        ) : mine.length === 0 ? (
          <p className="text-[14px] text-muted">
            <span className="mono">{account.slice(0, 6)}…{account.slice(-4)}</span> isn&apos;t in an org yet. Create one, or ask an admin for an invite.
          </p>
        ) : (
          <>
            <p className="text-[13px] text-muted">
              Your orgs · <span className="mono">{account.slice(0, 6)}…{account.slice(-4)}</span>
            </p>
            <ul className="mt-2 flex flex-wrap gap-2">
              {mine.map((o) => (
                <li key={o.org}>
                  <Link href={`/org/${o.org}`} className="flex items-center gap-2 rounded-full border border-line-strong px-4 py-1.5 hover:border-accent">
                    <span className="mono text-[14px] font-medium">{o.org}</span>
                    <span className="text-[12px] text-muted">{o.role}</span>
                  </Link>
                </li>
              ))}
            </ul>
          </>
        )}
      </div>
      <form
        className="mt-4 flex items-center gap-2 rounded-full border border-line-strong p-1.5 pl-5 focus-within:border-accent"
        onSubmit={(e) => {
          e.preventDefault();
          go(org);
        }}
      >
        <input
          value={org}
          onChange={(e) => (setOrg(e.target.value), setBad(false))}
          placeholder="or type an org: your-org.eth"
          aria-label="Org name"
          className="mono h-9 flex-1 bg-transparent text-[15px] outline-none"
        />
        <button className="h-9 rounded-full bg-accent px-5 text-[14px] font-semibold text-on-accent hover:opacity-90">Open</button>
      </form>
      {bad && <p className="mt-3 text-[14px] text-bad">Type your org&apos;s ENS name, like acme.eth.</p>}
      <div className="mt-5 flex items-center gap-3">
        <Link href="/new" className="inline-flex h-9 items-center rounded-full border border-line-strong px-4 text-[14px] font-semibold hover:border-accent">
          Create an organization
        </Link>
        <span className="text-[13px] text-muted">Free name like acme.meshy.eth, owned by your wallet</span>
      </div>

      {recent.length > 0 ? (
        <div className="mt-8">
          <p className="text-[13px] text-muted">Your orgs on this browser</p>
          <ul className="mt-2 flex flex-wrap gap-2">
            {recent.map((o) => (
              <li key={o} className="group flex items-center rounded-full border border-line pl-4 pr-1.5 hover:border-line-strong">
                <Link href={`/org/${o}`} className="mono py-1.5 text-[14px] font-medium">{o}</Link>
                <button
                  aria-label={`Forget ${o}`}
                  onClick={() => (forgetOrg(o), setRecent(recentOrgs()))}
                  className="ml-1 h-6 w-6 rounded-full text-[14px] text-muted opacity-0 hover:text-fg group-hover:opacity-100"
                >
                  ×
                </button>
              </li>
            ))}
          </ul>
        </div>
      ) : (
        <div className="mt-6 inline-flex items-center gap-2 rounded-full border border-accent/40 bg-accent/5 px-4 py-2 text-[13px]">
          <span className="text-muted">Judging for ETHGlobal?</span>
          <Link href={`/org/${DEMO_ORG}`} className="font-semibold text-accent hover:underline">Explore the live demo org</Link>
          <span className="text-muted">— no wallet or setup needed, read-only.</span>
        </div>
      )}
      <p className="mt-6 text-[14px] text-muted">
        <a href="/download" className="text-accent hover:underline">Get Meshy for Mac, the CLI and the daemon</a>
      </p>
    </main>
  );
}

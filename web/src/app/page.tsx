"use client";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { LiveNetwork } from "@/components/LiveNetwork";
import { DEMO_ORG, HERO_ORG, PROVIDER_API, isOrgName } from "@/lib/config";
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
  const short = (a: string) => `${a.slice(0, 6)}…${a.slice(-4)}`;
  return (
    <main className="night-grid min-h-screen text-[#e8edff]">
      <header className="mx-auto flex h-16 max-w-6xl items-center gap-6 px-4 sm:px-6">
        {/* eslint-disable-next-line @next/next/no-img-element */}
        <img src="/meshy-lockup-white.svg" alt="Meshy" className="h-6 w-auto" />
        <nav className="ml-auto flex items-center gap-1 text-[14px]">
          <Link href={`/org/${DEMO_ORG}`} className="hidden rounded-full px-3 py-1.5 text-[#93a0c8] hover:text-white sm:inline">Demo org</Link>
          <a href="/download" className="rounded-full px-3 py-1.5 text-[#93a0c8] hover:text-white">Download</a>
        </nav>
      </header>

      <section className="mx-auto max-w-6xl px-4 pt-10 sm:px-6 sm:pt-14">
        <div className="mx-auto max-w-xl text-center">
          <h1 className="text-[34px] font-semibold leading-[1.05] tracking-[-0.035em] sm:text-[48px]">Network access for agents, as ENS names.</h1>
          <p className="mx-auto mt-4 text-[16px] leading-relaxed text-[#93a0c8]">
            Give an agent one folder or one port for an hour. Revoke it, and the session ends in the next block. Every action is logged on Arkiv.
          </p>
        </div>

        <div className="mx-auto mt-8 max-w-xl">
          <div className="rounded-2xl border border-[#1a2652] bg-[#0b1435]/80 p-4 text-left">
            {!account ? (
              <div className="flex flex-wrap items-center justify-between gap-3">
                <span className="text-[14px] text-[#93a0c8]">Connect your wallet and Meshy finds your orgs.</span>
                <button onClick={connect} className="h-9 rounded-full bg-[#8c9bff] px-5 text-[14px] font-semibold text-[#050b24] hover:opacity-90">
                  Connect wallet
                </button>
              </div>
            ) : finding || !mine ? (
              <p className="text-[14px] text-[#93a0c8]">Finding your orgs on chain…</p>
            ) : mine.length === 0 ? (
              <p className="text-[14px] text-[#93a0c8]">
                <span className="mono">{short(account)}</span> isn&apos;t in an org yet. Create one, or ask an admin for an invite.
              </p>
            ) : (
              <>
                <p className="text-[13px] text-[#93a0c8]">
                  Your orgs · <span className="mono">{short(account)}</span>
                </p>
                <ul className="mt-2 flex flex-wrap gap-2">
                  {mine.map((o) => (
                    <li key={o.org}>
                      <Link href={`/org/${o.org}`} className="flex items-center gap-2 rounded-full border border-[#26346a] px-4 py-1.5 hover:border-[#8c9bff]">
                        <span className="mono text-[14px] font-medium">{o.org}</span>
                        <span className="text-[12px] text-[#93a0c8]">{o.role}</span>
                      </Link>
                    </li>
                  ))}
                </ul>
              </>
            )}
          </div>

          <form
            className="mt-3 flex items-center gap-2 rounded-full border border-[#26346a] bg-[#0b1435]/80 p-1.5 pl-5 focus-within:border-[#8c9bff]"
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
              className="mono h-9 min-w-0 flex-1 bg-transparent text-[15px] outline-none placeholder:text-[#5b6787]"
            />
            <button className="h-9 rounded-full bg-[#8c9bff] px-5 text-[14px] font-semibold text-[#050b24] hover:opacity-90">Open</button>
          </form>
          {bad && <p className="mt-3 text-[14px] text-[#fb7185]">Type your org&apos;s ENS name, like acme.eth.</p>}

          <div className="mt-4 flex flex-wrap items-center justify-center gap-3">
            <Link href="/new" className="inline-flex h-9 items-center rounded-full border border-[#26346a] px-4 text-[14px] font-semibold hover:border-[#8c9bff]">
              Create an organization
            </Link>
            <span className="text-[13px] text-[#93a0c8]">Free name like acme.meshy.eth, owned by your wallet</span>
          </div>

          {recent.length > 0 ? (
            <div className="mt-6 text-center">
              <p className="text-[13px] text-[#93a0c8]">Your orgs on this browser</p>
              <ul className="mt-2 flex flex-wrap justify-center gap-2">
                {recent.map((o) => (
                  <li key={o} className="group flex items-center rounded-full border border-[#1a2652] pl-4 pr-1.5 hover:border-[#26346a]">
                    <Link href={`/org/${o}`} className="mono py-1.5 text-[14px] font-medium">{o}</Link>
                    <button
                      aria-label={`Forget ${o}`}
                      onClick={() => (forgetOrg(o), setRecent(recentOrgs()))}
                      className="ml-1 h-6 w-6 rounded-full text-[14px] text-[#93a0c8] opacity-0 hover:text-white group-hover:opacity-100"
                    >
                      ×
                    </button>
                  </li>
                ))}
              </ul>
            </div>
          ) : (
            <p className="mt-6 text-center text-[13px]">
              <span className="text-[#93a0c8]">Judging for ETHGlobal? </span>
              <Link href={`/org/${DEMO_ORG}`} className="font-semibold text-[#8c9bff] hover:underline">Explore the live demo org</Link>
              <span className="text-[#93a0c8]"> — no wallet or setup needed, read-only.</span>
            </p>
          )}
        </div>

        <div className="mt-12 rounded-3xl border border-[#1a2652] bg-[#050b24]/70 p-4 sm:p-6">
          <LiveNetwork org={HERO_ORG} />
        </div>
        <p className="py-8 text-center text-[14px] text-[#93a0c8]">
          This is <Link href={`/org/${HERO_ORG}`} className="text-[#8c9bff] hover:underline">{HERO_ORG}</Link>, read live from Sepolia ENSv2 through the Meshy provider.{" "}
          <a href="/download" className="text-[#8c9bff] hover:underline">Get Meshy for Mac, the CLI and the daemon</a>.
        </p>
      </section>
    </main>
  );
}

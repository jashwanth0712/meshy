"use client";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { Logo } from "@/components/Shell";
import { DEMO_ORG, isOrgName } from "@/lib/config";
import { forgetOrg, recentOrgs } from "@/lib/recent";

export default function Home() {
  const [org, setOrg] = useState("");
  const [recent, setRecent] = useState<string[]>([]);
  const [bad, setBad] = useState(false);
  const router = useRouter();
  // eslint-disable-next-line react-hooks/set-state-in-effect
  useEffect(() => setRecent(recentOrgs()), []);
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
      <form
        className="mt-10 flex items-center gap-2 rounded-full border border-line-strong p-1.5 pl-5 focus-within:border-accent"
        onSubmit={(e) => {
          e.preventDefault();
          go(org);
        }}
      >
        <input
          value={org}
          onChange={(e) => (setOrg(e.target.value), setBad(false))}
          placeholder="your-org.eth"
          aria-label="Org name"
          autoFocus
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
        <p className="mt-6 text-[14px] text-muted">
          New here? <Link href={`/org/${DEMO_ORG}`} className="text-accent hover:underline">Look around the demo org</Link>, read-only.
        </p>
      )}
      <p className="mt-6 text-[14px] text-muted">
        <a href="/download" className="text-accent hover:underline">Get Meshy for Mac, the CLI and the daemon</a>
      </p>
    </main>
  );
}

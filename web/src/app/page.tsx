"use client";
import { ArrowRight, Bot, KeyRound, Network, ScrollText, ShieldCheck } from "lucide-react";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { DEFAULT_ORG } from "@/lib/config";

export default function Home() {
  const [org, setOrg] = useState(DEFAULT_ORG);
  const router = useRouter();
  const go = () => org.trim() && router.push(`/org/${org.trim().toLowerCase()}`);
  return (
    <main className="mx-auto flex min-h-screen max-w-5xl flex-col justify-center px-6 py-16">
      <div className="mb-10 flex items-center gap-3">
        <div className="grid h-11 w-11 place-items-center rounded-xl bg-accent/15 text-accent">
          <Network size={22} />
        </div>
        <div>
          <h1 className="text-2xl font-semibold">Meshy</h1>
          <p className="text-sm text-muted">Decentralized network access for teams and their AI agents</p>
        </div>
      </div>
      <h2 className="max-w-3xl text-4xl font-semibold leading-tight">
        Give agents <span className="text-accent">exactly</span> the folders they need — and take it back on-chain in one block.
      </h2>
      <p className="mt-4 max-w-2xl text-muted">
        Every network, resource, agent and grant is an ENSv2 name. Grants expire on their own, revoking kills live sessions, and every file an agent reads or
        writes is audited on Arkiv.
      </p>
      <form
        className="mt-8 flex max-w-xl gap-2"
        onSubmit={(e) => {
          e.preventDefault();
          go();
        }}
      >
        <input
          value={org}
          onChange={(e) => setOrg(e.target.value)}
          placeholder="your-org.eth"
          className="mono flex-1 rounded-xl border border-line bg-panel px-4 py-3 outline-none focus:border-accent/60"
        />
        <button className="inline-flex items-center gap-2 rounded-xl border border-accent/50 bg-accent/15 px-5 py-3 text-accent hover:bg-accent/25">
          Open org <ArrowRight size={16} />
        </button>
      </form>
      <div className="mt-14 grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        {[
          { i: KeyRound, t: "Access is a name", d: "claude-1.db1.prod.acme.eth is a 1-hour, read-only grant. Revoke = unregister." },
          { i: ShieldCheck, t: "Folder sandbox", d: "Agents see only granted paths. Everything else is MESHY_BLOCKED." },
          { i: Bot, t: "Human-backed agents", d: "Agents join only after their human approves with World ID." },
          { i: ScrollText, t: "Audit on Arkiv", d: "Reads, writes, blocks and revocations — encrypted to your org key." },
        ].map(({ i: I, t, d }) => (
          <div key={t} className="rounded-xl border border-line bg-panel p-4">
            <I size={18} className="text-accent" />
            <div className="mt-3 text-sm font-semibold">{t}</div>
            <div className="mt-1 text-sm text-muted">{d}</div>
          </div>
        ))}
      </div>
    </main>
  );
}

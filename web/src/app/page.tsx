"use client";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { Logo } from "@/components/Shell";
import { DEFAULT_ORG } from "@/lib/config";

export default function Home() {
  const [org, setOrg] = useState(DEFAULT_ORG);
  const router = useRouter();
  const go = () => org.trim() && router.push(`/org/${org.trim().toLowerCase()}`);
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
          go();
        }}
      >
        <input value={org} onChange={(e) => setOrg(e.target.value)} placeholder="your-org.eth" aria-label="Org name" className="mono h-9 flex-1 bg-transparent text-[15px] outline-none" />
        <button className="h-9 rounded-full bg-accent px-5 text-[14px] font-semibold text-on-accent hover:opacity-90">Open</button>
      </form>
    </main>
  );
}

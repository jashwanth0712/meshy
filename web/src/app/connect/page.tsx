"use client";
// Sign in to Meshy for Mac. The menu-bar app opens
//   /connect?state=<nonce>&org=<org>
// in the default browser; this page asks the wallet for an address and a signature of a
// short message that includes the nonce, then hands both back through
//   meshy://signin?state=…&address=…&sig=…&org=…
// The app only accepts a callback whose state it issued, so a stray link can't set an identity.
// Nothing here writes on-chain, and no key ever reaches the app.
import { useEffect, useState } from "react";
import { Logo } from "@/components/Shell";

type Eth = { request: (a: { method: string; params?: unknown[] }) => Promise<unknown> };

export default function Connect() {
  const [q, setQ] = useState<{ state: string; org: string }>();
  const [status, setStatus] = useState<"idle" | "busy" | "done" | "error">("idle");
  const [msg, setMsg] = useState("");
  const [back, setBack] = useState("");

  useEffect(() => {
    const p = new URLSearchParams(window.location.search);
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setQ({ state: (p.get("state") ?? "").replace(/[^a-zA-Z0-9]/g, ""), org: (p.get("org") ?? "").toLowerCase() });
  }, []);

  async function signIn() {
    const eth = (window as unknown as { ethereum?: Eth }).ethereum;
    if (!eth) {
      setStatus("error");
      setMsg("No browser wallet found. Install MetaMask (or another wallet) in this browser, then reload.");
      return;
    }
    if (!q?.state) {
      setStatus("error");
      setMsg("This link is missing its sign-in code. Start again from the Meshy menu.");
      return;
    }
    setStatus("busy");
    try {
      const [address] = (await eth.request({ method: "eth_requestAccounts" })) as string[];
      const text = `Sign in to Meshy for Mac\norg: ${q.org || "-"}\nstate: ${q.state}`;
      const hex = "0x" + Array.from(new TextEncoder().encode(text), (b) => b.toString(16).padStart(2, "0")).join("");
      const sig = (await eth.request({ method: "personal_sign", params: [hex, address] })) as string;
      const url = `meshy://signin?state=${encodeURIComponent(q.state)}&address=${encodeURIComponent(address)}&sig=${encodeURIComponent(sig)}&org=${encodeURIComponent(q.org)}`;
      setBack(url);
      setStatus("done");
      window.location.href = url;
    } catch (e) {
      setStatus("error");
      setMsg(e instanceof Error ? e.message.split("\n")[0] : String(e));
    }
  }

  return (
    <main className="mx-auto flex min-h-screen max-w-md flex-col justify-center px-6 py-16">
      <Logo className="h-7" />
      <h1 className="mt-10 text-[30px] font-semibold leading-tight tracking-[-0.025em]">Sign in to Meshy for Mac</h1>
      <p className="mt-3 text-[15px] leading-relaxed text-muted">
        Your wallet signs a short message so the menu bar knows who you are{q?.org ? <> in <span className="mono text-fg">{q.org}</span></> : null}. Nothing is sent on-chain, and no key leaves your wallet.
      </p>
      {status === "done" ? (
        <div className="mt-8 space-y-3 text-[15px]">
          <p>Signed in. You can close this tab.</p>
          <a href={back} className="font-medium text-accent hover:underline">Didn&apos;t switch back? Open Meshy</a>
        </div>
      ) : (
        <button
          onClick={signIn}
          disabled={status === "busy"}
          className="mt-8 h-11 rounded-full bg-accent px-6 text-[15px] font-semibold text-on-accent hover:opacity-90 disabled:opacity-50"
        >
          {status === "busy" ? "Waiting for your wallet…" : "Sign in with wallet"}
        </button>
      )}
      {status === "error" && <p className="mt-4 text-[14px] text-bad">{msg}</p>}
    </main>
  );
}

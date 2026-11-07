"use client";
// Sign in to Meshy for Mac with Sign-In with Ethereum (EIP-4361).
//
// The menu-bar app opens   /connect?nonce=<nonce>&callback=meshy://auth
// in the default browser. This page asks the wallet (MetaMask) for an account and a
// personal_sign over a SIWE message carrying that nonce, this site's domain, Sepolia's
// chain ID and a 10-minute expiry, then redirects ONCE to
//   meshy://auth?message=<message>&sig=<signature>
// The app checks the nonce it issued and recovers the signer from the signature, so a
// stray or replayed link can't set an identity. Nothing is written on-chain and no key
// leaves the wallet. Reloading this page after success does not prompt or redirect again.
import { useEffect, useState } from "react";
import { toHex } from "viem";
import { Logo } from "@/components/Shell";
import { siweMessage } from "@/lib/siwe";

type Eth = { request: (a: { method: string; params?: unknown[] }) => Promise<unknown> };
type Q = { nonce: string; callback: string };

export default function Connect() {
  const [q, setQ] = useState<Q | null>();
  const [status, setStatus] = useState<"idle" | "busy" | "done" | "error">("idle");
  const [msg, setMsg] = useState("");
  const [back, setBack] = useState("");

  useEffect(() => {
    const p = new URLSearchParams(window.location.search);
    const nonce = p.get("nonce") ?? "";
    const callback = p.get("callback") ?? "meshy://auth";
    const ok = /^[a-zA-Z0-9]{8,64}$/.test(nonce) && /^meshy:\/\/[a-z]+$/.test(callback);
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setQ(ok ? { nonce, callback } : null);
    if (ok) {
      const prev = sessionStorage.getItem(`meshy.connect.${nonce}`);
      if (prev) {
        setBack(prev);
        setStatus("done");
      }
    }
  }, []);

  async function signIn() {
    if (!q) return;
    const eth = (window as unknown as { ethereum?: Eth }).ethereum;
    if (!eth) {
      setStatus("error");
      setMsg("No browser wallet found. Install MetaMask (or another wallet) in this browser, then reload this page.");
      return;
    }
    setStatus("busy");
    try {
      const [account] = (await eth.request({ method: "eth_requestAccounts" })) as string[];
      if (!account) throw new Error("The wallet didn't share an account.");
      const message = siweMessage({ domain: window.location.host, origin: window.location.origin, address: account, nonce: q.nonce, issuedAt: new Date(), ttlMs: 10 * 60_000 });
      const sig = (await eth.request({ method: "personal_sign", params: [toHex(message), account] })) as string;
      const url = `${q.callback}?message=${encodeURIComponent(message)}&sig=${encodeURIComponent(sig)}`;
      sessionStorage.setItem(`meshy.connect.${q.nonce}`, url);
      setBack(url);
      setStatus("done");
      window.location.href = url; // once; a reload shows the "done" state instead
    } catch (e) {
      setStatus("error");
      const m = e instanceof Error ? e.message.split("\n")[0] : String(e);
      setMsg(/reject|denied/i.test(m) ? "You cancelled in the wallet. Try again when you're ready." : m);
    }
  }

  return (
    <main className="mx-auto flex min-h-screen max-w-md flex-col justify-center px-6 py-16">
      <Logo className="h-7" />
      <h1 className="mt-10 text-[30px] font-semibold leading-tight tracking-[-0.025em]">Sign in to Meshy for Mac</h1>
      {q === null ? (
        <p className="mt-3 text-[15px] leading-relaxed text-muted">
          This page only works from the Meshy menu. Open Meshy in your menu bar and choose <b className="text-fg">Sign In with Ethereum…</b>
        </p>
      ) : (
        <>
          <p className="mt-3 text-[15px] leading-relaxed text-muted">
            Your wallet signs a one-time message so Meshy on your Mac knows which address is yours. Nothing is sent on-chain and no key leaves your wallet.
          </p>
          {status === "done" ? (
            <div className="mt-8 space-y-3 text-[15px]">
              <p>Signed in. You can close this tab and go back to the Meshy menu.</p>
              <a href={back} className="font-medium text-accent hover:underline">Didn&apos;t switch back? Open Meshy</a>
            </div>
          ) : (
            <button
              onClick={signIn}
              disabled={status === "busy" || !q}
              className="mt-8 h-11 rounded-full bg-accent px-6 text-[15px] font-semibold text-on-accent hover:opacity-90 disabled:opacity-50"
            >
              {status === "busy" ? "Waiting for your wallet…" : "Sign in with Ethereum"}
            </button>
          )}
          {status === "error" && <p className="mt-4 text-[14px] text-bad">{msg}</p>}
        </>
      )}
    </main>
  );
}

"use client";
// `meshy login`: sign this machine in to your orgs with your wallet. No invite link.
//
// The CLI opens /login?ssh=<key>&device=<addr>&cb=http://127.0.0.1:<port>/callback&state=<s>.
// Here you connect your wallet, see your orgs (read from chain via the provider's /v1/orgs),
// and for each one you tick:
//   - you already have a name (member): 1 tx sets meshy.ssh + addr on your own resolver
//   - you're the owner/admin with no name: sign a self-invite, claim it (1 tx), then link (1 tx)
// Then we return to the CLI's callback with the names. The CLI checks every name on ENS itself.
import Link from "next/link";
import { useEffect, useMemo, useState } from "react";
import { type Address, type WalletClient, decodeEventLog, keccak256, toBytes } from "viem";
import { Logo } from "@/components/Shell";
import { Badge, Button, inputCls } from "@/components/ui";
import { PROVIDER_API } from "@/lib/config";
import { firstLabel, parentRegistry, publicClient, recordCalls, resolverAbi, resolverOf } from "@/lib/ens";
import { INVITES, ORG_FACTORY, ORG_PARENT, ZERO_ADDRESS, factoryAbi, invitesAbi, labelOk, randomNonce, signInvite } from "@/lib/orgs";
import { connectWallet, silentAccount } from "@/lib/wallet";

type MyOrg = { org: string; role: "owner" | "admin" | "member" | "agent"; name?: string };
type Row = MyOrg & { pick: boolean; label: string; state: "" | "doing" | "done" | "fail"; note?: string };
type Q = { ssh: string; device: Address; cb: string; state: string };

export default function LoginPage() {
  const client = useMemo(() => publicClient(), []);
  const [q, setQ] = useState<Q | null>();
  const [wallet, setWallet] = useState<WalletClient>();
  const [account, setAccount] = useState<Address>();
  const [rows, setRows] = useState<Row[]>();
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");

  useEffect(() => {
    const p = new URLSearchParams(window.location.search);
    const ssh = p.get("ssh") ?? "", device = p.get("device") ?? "", cb = p.get("cb") ?? "", state = p.get("state") ?? "";
    const ok = ssh.startsWith("ssh-ed25519 ") && /^0x[0-9a-fA-F]{40}$/.test(device) && /^http:\/\/127\.0\.0\.1:\d+\/callback$/.test(cb) && /^[0-9a-f]{32}$/.test(state);
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setQ(ok ? { ssh, device: device as Address, cb, state } : null);
    silentAccount().then((a) => a && setAccount(a));
  }, []);

  // Load the wallet's orgs from chain state (provider: created / administered / joined).
  useEffect(() => {
    if (!account) return;
    (async () => {
      try {
        const r = await fetch(`${PROVIDER_API}/v1/orgs?addr=${account}`);
        const j = (await r.json()) as { orgs?: MyOrg[] };
        setRows(
          (j.orgs ?? []).map((o) => {
            const supported = (o.name && o.name.includes(".people.")) || ((o.role === "owner" || o.role === "admin") && o.org.endsWith(`.${ORG_PARENT}`));
            return {
              ...o,
              pick: !!supported,
              label: o.name ? firstLabel(o.name) : "",
              state: "",
              note: supported ? undefined : o.role === "agent" ? "agent identity: use `meshy join`" : "this org predates self-serve people; ask an admin to invite you",
            };
          }),
        );
      } catch (e) {
        setErr(`Couldn't read your orgs: ${e instanceof Error ? e.message : String(e)}`);
      }
    })();
  }, [account]);

  async function connect() {
    setErr("");
    try {
      const c = await connectWallet();
      setWallet(c.wallet);
      setAccount(c.account);
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    }
  }

  const upd = (i: number, p: Partial<Row>) => setRows((rs) => rs!.map((r, j) => (j === i ? { ...r, ...p } : r)));

  async function linkName(w: WalletClient, a: Address, name: string) {
    const parent = await parentRegistry(client, name);
    const res = await resolverOf(client, parent, firstLabel(name));
    const calls = recordCalls(name, { "meshy.ssh": q!.ssh }, q!.device);
    const h = await w.writeContract({ account: a, chain: w.chain, address: res, abi: resolverAbi, functionName: "multicall", args: [calls] });
    const r = await client.waitForTransactionReceipt({ hash: h });
    if (r.status !== "success") throw new Error("linking this machine reverted");
  }

  async function joinAsOwner(w: WalletClient, a: Address, org: string, label: string): Promise<string> {
    const o = (await client.readContract({ address: ORG_FACTORY, abi: factoryAbi, functionName: "orgs", args: [keccak256(toBytes(firstLabel(org)))] })) as readonly [Address, Address, Address, Address, Address, bigint];
    if (o[0] === ZERO_ADDRESS) throw new Error("not a Meshy-created org");
    const now = BigInt(Math.floor(Date.now() / 1000));
    const inv = { registry: o[3], parentName: `people.${org}`, otherRegistry: o[4], label, invitee: a, expiresAt: now + 3600n, nameExpiry: now + 365n * 86400n, nonce: randomNonce() };
    const sig = await signInvite(w, a, inv);
    const why = (await client.readContract({ address: INVITES, abi: invitesAbi, functionName: "check", args: [inv, sig, a, label] })) as string;
    if (why) throw new Error(why === "name taken" ? `${label} is taken in ${org}` : why);
    const h = await w.writeContract({ account: a, chain: w.chain, address: INVITES, abi: invitesAbi, functionName: "claim", args: [inv, sig, label, q!.ssh] });
    const r = await client.waitForTransactionReceipt({ hash: h });
    if (r.status !== "success") throw new Error("claiming your name reverted");
    let resolver: Address | undefined;
    for (const l of r.logs) {
      try {
        const ev = decodeEventLog({ abi: invitesAbi, data: l.data, topics: l.topics });
        if (ev.eventName === "Claimed") resolver = (ev.args as { resolver: Address }).resolver;
      } catch {}
    }
    const name = `${label}.people.${org}`;
    if (!resolver) throw new Error("claimed, but no resolver found");
    const h2 = await w.writeContract({ account: a, chain: w.chain, address: resolver, abi: resolverAbi, functionName: "multicall", args: [recordCalls(name, {}, q!.device)] });
    const r2 = await client.waitForTransactionReceipt({ hash: h2 });
    if (r2.status !== "success") throw new Error("linking this machine reverted");
    return name;
  }

  async function go() {
    if (!q || !rows) return;
    setBusy(true);
    setErr("");
    let w = wallet, a = account;
    try {
      if (!w || !a) {
        const c = await connectWallet();
        w = c.wallet;
        a = c.account;
        setWallet(w);
        setAccount(a);
      }
    } catch (e) {
      setBusy(false);
      return setErr(e instanceof Error ? e.message : String(e));
    }
    const names: string[] = [];
    for (let i = 0; i < rows.length; i++) {
      const r = rows[i];
      if (!r.pick) continue;
      upd(i, { state: "doing", note: undefined });
      try {
        if (r.name && r.name.includes(".people.")) {
          await linkName(w, a, r.name);
          names.push(r.name);
        } else {
          if (!labelOk(r.label)) throw new Error("pick a name (lowercase letters, numbers, dashes)");
          names.push(await joinAsOwner(w, a, r.org, r.label));
        }
        upd(i, { state: "done" });
      } catch (e) {
        upd(i, { state: "fail", note: e instanceof Error ? e.message.split("\n")[0] : String(e) });
      }
    }
    setBusy(false);
    if (names.length) {
      const u = new URL(q.cb);
      u.searchParams.set("state", q.state);
      u.searchParams.set("names", names.join(","));
      u.searchParams.set("wallet", a);
      window.location.href = u.toString();
    }
  }

  const picked = rows?.filter((r) => r.pick) ?? [];
  const txs = picked.reduce((n, r) => n + (r.name ? 1 : 2), 0);

  return (
    <main className="mx-auto max-w-xl px-6 py-14">
      <Link href="/" aria-label="Meshy home">
        <Logo className="h-6" />
      </Link>
      <h1 className="mt-10 text-[30px] font-semibold leading-tight tracking-[-0.03em]">Sign this machine in</h1>
      {q === null ? (
        <p className="mt-4 text-[15px] text-muted">
          Open this page from the terminal: run <span className="mono text-fg">meshy login</span>.
        </p>
      ) : !q ? null : (
        <>
          <p className="mt-3 text-[15px] leading-relaxed text-muted">
            <span className="mono text-fg">meshy login</span> wants to use device{" "}
            <span className="mono text-fg">
              {q.device.slice(0, 6)}…{q.device.slice(-4)}
            </span>{" "}
            in your orgs. Nothing leaves that machine but its public keys.
          </p>
          {!account ? (
            <div className="mt-8">
              <Button kind="primary" onClick={connect}>
                Connect wallet
              </Button>
            </div>
          ) : !rows ? (
            <p className="mt-8 text-[14px] text-muted">Finding your orgs on chain…</p>
          ) : rows.length === 0 ? (
            <p className="mt-8 text-[14px] text-muted">
              This wallet isn&apos;t in any org yet. <a href="/new" className="text-accent hover:underline">Create one</a>, or ask an admin for an invite.
            </p>
          ) : (
            <div className="mt-8 space-y-3">
              {rows.map((r, i) => {
                const supported = !r.note || r.state === "fail";
                return (
                  <div key={r.org} className="rounded-2xl border border-line bg-panel p-4">
                    <div className="flex items-center justify-between gap-3">
                      <label className="flex items-center gap-3">
                        <input type="checkbox" disabled={busy || (!supported && !r.pick)} checked={r.pick} onChange={(e) => upd(i, { pick: e.target.checked })} />
                        <span className="mono text-[15px] font-semibold">{r.org}</span>
                      </label>
                      <Badge tone={r.role === "owner" ? "ok" : r.role === "admin" ? "info" : "muted"}>{r.role}</Badge>
                    </div>
                    <div className="mt-2 pl-7 text-[13px] text-muted">
                      {r.name ? (
                        <>
                          as <span className="mono text-fg">{r.name}</span> · 1 transaction
                        </>
                      ) : r.note && r.state !== "fail" ? (
                        r.note
                      ) : (
                        <span className="flex items-center gap-2">
                          your name:
                          <input value={r.label} onChange={(e) => upd(i, { label: e.target.value.toLowerCase().trim() })} placeholder="you" className={`${inputCls} h-8 w-36 py-1`} disabled={busy} />
                          <span className="mono">.people.{r.org}</span> · sign + 2 transactions
                        </span>
                      )}
                      {r.state === "doing" && <div className="mt-1 text-fg">Confirm in your wallet…</div>}
                      {r.state === "done" && <div className="mt-1 text-ok">Linked to this machine</div>}
                      {r.state === "fail" && r.note && <div className="mt-1 text-bad">{r.note}</div>}
                    </div>
                  </div>
                );
              })}
              {err && <div className="rounded-xl border border-bad/40 bg-bad/10 px-3.5 py-2.5 text-[13px] text-bad">{err}</div>}
              <div className="flex items-center justify-between pt-2">
                <span className="mono text-[12px] text-muted">
                  {account.slice(0, 6)}…{account.slice(-4)} · {txs} transaction{txs === 1 ? "" : "s"}
                </span>
                <Button kind="primary" disabled={busy || picked.length === 0} onClick={go}>
                  {busy ? "Working…" : `Sign in to ${picked.length} org${picked.length === 1 ? "" : "s"}`}
                </Button>
              </div>
            </div>
          )}
          {err && !rows && <div className="mt-4 rounded-xl border border-bad/40 bg-bad/10 px-3.5 py-2.5 text-[13px] text-bad">{err}</div>}
        </>
      )}
    </main>
  );
}

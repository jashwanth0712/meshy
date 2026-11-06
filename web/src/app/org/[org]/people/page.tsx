"use client";
// People: members and agents of the org, and self-serve invites (link, QR, email, wallet-bound).
import QRCode from "qrcode";
import { useCallback, useEffect, useState } from "react";
import { type Address, isAddress, keccak256, toBytes } from "viem";
import { Badge, Button, Field, PageHeader, Seg, Sheet, Short, inputCls } from "@/components/ui";
import { childLabels, labelId, registryAbi, send } from "@/lib/ens";
import { useMeshy } from "@/lib/store";
import {
  INVITES,
  ORG_FACTORY,
  ORG_PARENT,
  type InviteBlob,
  type InviteRole,
  ZERO_ADDRESS,
  factoryAbi,
  inviteUrl,
  labelOk,
  orgsLive,
  randomNonce,
  signInvite,
} from "@/lib/orgs";

type OrgRegs = { owner: Address; peopleRegistry: Address; agentsRegistry: Address };
type Person = { label: string; kind: "member" | "agent"; name: string };

export default function PeoplePage() {
  const { org, client, wallet, account, connect, toast } = useMeshy();
  const [regs, setRegs] = useState<OrgRegs | null>();
  const [people, setPeople] = useState<Person[]>();
  const [inviting, setInviting] = useState(false);
  const [removing, setRemoving] = useState<string>();
  const isOwner = !!account && !!regs && regs.owner.toLowerCase() === account.toLowerCase();

  async function remove(p: Person) {
    if (!wallet || !regs) return connect();
    if (!window.confirm(`Remove ${p.name}? They lose every grant and their seat is freed. This takes one transaction.`)) return;
    setRemoving(p.name);
    try {
      await send(wallet, client, { address: p.kind === "member" ? regs.peopleRegistry : regs.agentsRegistry, abi: registryAbi, functionName: "unregister", args: [labelId(p.label)] });
      toast(`Removed ${p.label}`, "ok");
      await load();
    } catch (e) {
      toast(e instanceof Error ? e.message.split("\n")[0] : String(e), "err");
    } finally {
      setRemoving(undefined);
    }
  }
  const factoryOrg = org.endsWith(`.${ORG_PARENT}`) && org.split(".").length === 3;

  const load = useCallback(async () => {
    if (!factoryOrg || !orgsLive) return setRegs(null);
    const label = org.split(".")[0];
    const o = (await client.readContract({ address: ORG_FACTORY, abi: factoryAbi, functionName: "orgs", args: [keccak256(toBytes(label))] })) as readonly [Address, Address, Address, Address, Address, bigint];
    if (o[0] === ZERO_ADDRESS) return setRegs(null);
    const r = { owner: o[0], peopleRegistry: o[3], agentsRegistry: o[4] };
    setRegs(r);
    const [m, a] = await Promise.all([childLabels(client, r.peopleRegistry), childLabels(client, r.agentsRegistry)]);
    setPeople([
      ...m.filter((x) => !x.unregistered).map((x) => ({ label: x.label, kind: "member" as const, name: `${x.label}.people.${org}` })),
      ...a.filter((x) => !x.unregistered).map((x) => ({ label: x.label, kind: "agent" as const, name: `${x.label}.agents.${org}` })),
    ]);
  }, [client, org, factoryOrg]);

  useEffect(() => {
    load().catch((e) => toast(String(e), "err"));
  }, [load, toast]);

  if (regs === null)
    return (
      <div className="pt-10">
        <PageHeader title="People" sub="Members and agents of this organization." />
        <p className="mt-6 max-w-xl text-[14px] text-muted">
          Self-serve invites work for organizations created on Meshy ({`<name>.${ORG_PARENT}`}). This org was set up from the command line; invite agents from Directory, or{" "}
          <a href="/new" className="text-accent hover:underline">
            create a new organization
          </a>
          .
        </p>
      </div>
    );

  return (
    <div className="pt-10">
      <PageHeader
        title="People"
        sub={regs ? <>Owner <Short v={regs.owner} /></> : "Reading the org from ENS…"}
        action={
          <Button kind="primary" disabled={!regs} onClick={() => (account ? setInviting(true) : connect())}>
            {account ? "Invite" : "Connect wallet to invite"}
          </Button>
        }
      />
      <div className="mt-6 overflow-hidden rounded-2xl border border-line">
        {!people ? (
          <div className="px-5 py-6 text-[14px] text-muted">Loading members…</div>
        ) : people.length === 0 ? (
          <div className="px-5 py-8 text-center text-[14px] text-muted">No members yet. Invite someone: they claim their own name with one transaction.</div>
        ) : (
          people.map((p) => (
            <div key={p.name} className="flex items-center justify-between gap-3 border-b border-line px-5 py-3 last:border-0">
              <div>
                <div className="text-[15px] font-semibold">{p.label}</div>
                <div className="mono text-[12px] text-muted">{p.name}</div>
              </div>
              <div className="flex items-center gap-3">
                <Badge tone={p.kind === "agent" ? "info" : "muted"}>{p.kind}</Badge>
                {isOwner && (
                  <Button kind="danger" className="h-8 px-3.5" disabled={!!removing} onClick={() => remove(p)}>
                    {removing === p.name ? "Removing…" : "Remove"}
                  </Button>
                )}
              </div>
            </div>
          ))
        )}
      </div>
      {inviting && regs && wallet && account && (
        <InviteSheet org={org} regs={regs} wallet={wallet} account={account} onClose={() => setInviting(false)} taken={new Set((people ?? []).map((p) => p.label))} />
      )}
    </div>
  );
}

function InviteSheet({
  org,
  regs,
  wallet,
  account,
  onClose,
  taken,
}: {
  org: string;
  regs: OrgRegs;
  wallet: NonNullable<ReturnType<typeof useMeshy>["wallet"]>;
  account: Address;
  onClose: () => void;
  taken: Set<string>;
}) {
  const { toast } = useMeshy();
  const [role, setRole] = useState<InviteRole>("member");
  const [label, setLabel] = useState("");
  const [to, setTo] = useState("");
  const [days, setDays] = useState(7);
  const [email, setEmail] = useState("");
  const [link, setLink] = useState("");
  const [qr, setQr] = useState("");
  const [busy, setBusy] = useState(false);

  const labelErr = label && (!labelOk(label) ? "1–32 lowercase letters, numbers or dashes" : taken.has(label) ? "That name is taken" : "");
  const toErr = to && !isAddress(to) ? "Enter a 0x wallet address (or leave empty for anyone with the link)" : "";

  async function sign() {
    setBusy(true);
    try {
      const now = BigInt(Math.floor(Date.now() / 1000));
      const people = role === "member";
      const inv = {
        registry: people ? regs.peopleRegistry : regs.agentsRegistry,
        parentName: `${people ? "people" : "agents"}.${org}`,
        otherRegistry: people ? regs.agentsRegistry : regs.peopleRegistry,
        label,
        invitee: (to || ZERO_ADDRESS) as Address,
        expiresAt: now + BigInt(days * 86400),
        nameExpiry: now + 365n * 86400n,
        nonce: randomNonce(),
      };
      const sig = await signInvite(wallet, account, inv);
      const blob: InviteBlob = { v: 1, org, role, by: account, inv, sig };
      const url = inviteUrl(window.location.origin, blob);
      setLink(url);
      setQr(await QRCode.toDataURL(url, { margin: 1, width: 240 }));
    } catch (e) {
      toast(e instanceof Error ? e.message.split("\n")[0] : String(e), "err");
    } finally {
      setBusy(false);
    }
  }

  async function sendEmail() {
    const r = await fetch("/api/invite/email", { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ to: email, link, org, role }) });
    const j = (await r.json().catch(() => ({}))) as { error?: string };
    toast(r.ok ? `Invite sent to ${email}` : (j.error ?? "Couldn't send the email"), r.ok ? "ok" : "err");
  }

  return (
    <Sheet
      title={`Invite to ${org}`}
      onClose={onClose}
      footer={
        !link ? (
          <Button kind="primary" disabled={busy || !!labelErr || !!toErr} onClick={sign}>
            {busy ? "Sign in your wallet…" : "Create invite"}
          </Button>
        ) : (
          <Button onClick={onClose}>Done</Button>
        )
      }
    >
      {!link ? (
        <div className="space-y-5">
          <Field label="Role">
            <Seg value={role} onChange={setRole} options={[{ v: "member", label: "Member" }, { v: "agent", label: "Agent" }]} />
          </Field>
          <Field label="Name (optional)" hint={labelErr || `Leave empty to let them choose. They'll be <name>.${role === "member" ? "people" : "agents"}.${org}`}>
            <input value={label} onChange={(e) => setLabel(e.target.value.toLowerCase().trim())} placeholder="alice" className={inputCls} />
          </Field>
          <Field label="Only for this wallet (optional)" hint={toErr || "If set, nobody else can use the link."}>
            <input value={to} onChange={(e) => setTo(e.target.value.trim())} placeholder="0x…" className={inputCls} />
          </Field>
          <Field label="Link expires in">
            <Seg value={days} onChange={setDays} options={[{ v: 1, label: "1 day" }, { v: 7, label: "7 days" }, { v: 30, label: "30 days" }]} />
          </Field>
          <p className="text-[12px] text-muted">
            Signing is free and sends nothing on-chain. The person who opens the link claims their name with one transaction, checked by <Short v={INVITES} />.
          </p>
        </div>
      ) : (
        <div className="space-y-5">
          <div className="flex justify-center rounded-2xl bg-white p-3">
            {/* eslint-disable-next-line @next/next/no-img-element */}
            {qr && <img src={qr} alt="Invite QR code" width={240} height={240} />}
          </div>
          <Field label="Invite link">
            <div className="flex gap-2">
              <input readOnly value={link} className={inputCls} onFocus={(e) => e.target.select()} />
              <Button
                onClick={() => {
                  navigator.clipboard.writeText(link);
                  toast("Link copied", "ok");
                }}
              >
                Copy
              </Button>
            </div>
          </Field>
          <Field label="Email it (Resend)" hint="Sends the same link from invites@meshy.live. Meshy doesn't store the address.">
            <div className="flex gap-2">
              <input value={email} onChange={(e) => setEmail(e.target.value.trim())} placeholder="name@company.com" className={inputCls} type="email" />
              <Button disabled={!/^[^@\s]+@[^@\s]+\.[^@\s]+$/.test(email)} onClick={sendEmail}>
                Send
              </Button>
            </div>
          </Field>
        </div>
      )}
    </Sheet>
  );
}


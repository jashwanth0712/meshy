"use client";
import { useState } from "react";
import { type Address, isAddress } from "viem";
import { type Step, Steps } from "@/components/Steps";
import { Badge, Button, Card, Field, Row, Sheet, inputCls } from "@/components/ui";
import { ADMIN_ROLES, eacAbi, send, textKeySetter } from "@/lib/ens";
import { useMeshy } from "@/lib/store";

/** Delegated network admins via ENSv2 Enhanced Access Control. */
export function Admins() {
  const { tree, wallet, client, account, toast, org } = useMeshy();
  const [open, setOpen] = useState(false);
  const [net, setNet] = useState(tree?.networks[0]?.name ?? "");
  const [addr, setAddr] = useState("");
  const [status, setStatus] = useState<Record<string, boolean>>();
  const [steps, setSteps] = useState<Step[]>();
  if (!tree) return null;
  const isOwner = !!account && tree.owner.toLowerCase() === account.toLowerCase();
  const network = tree.networks.find((n) => n.name === net) ?? tree.networks[0];
  // Registries an admin of this network needs roles on: the network, each device, each service.
  const regs = network
    ? [
        { name: network.name, reg: network.registry },
        ...network.resources.map((r) => ({ name: r.name, reg: r.registry })),
        ...network.resources.flatMap((r) => r.services.map((s) => ({ name: s.name, reg: s.registry }))),
      ]
    : [];
  const busy = steps?.some((s) => s.state === "doing");

  async function check() {
    if (!isAddress(addr)) return;
    const out: Record<string, boolean> = {};
    for (const r of regs) out[r.name] = (await client.readContract({ address: r.reg, abi: eacAbi, functionName: "hasRootRoles", args: [ADMIN_ROLES, addr as Address] })) as boolean;
    setStatus(out);
  }

  async function apply(revoke: boolean) {
    if (!wallet || !isAddress(addr)) return;
    const list: Step[] = regs.map((r) => ({ label: `${revoke ? "Remove" : "Grant"} admin roles on ${r.name.replace(`.${org}`, "")}`, state: "todo" as const }));
    if (!revoke) list.push({ label: "Allow writing grant policies (only meshy.policy)", state: "todo" });
    setSteps(list);
    const up = (i: number, p: Partial<Step>) => setSteps((s) => s!.map((x, j) => (j === i ? { ...x, ...p } : x)));
    try {
      for (let i = 0; i < regs.length; i++) {
        up(i, { state: "doing" });
        await send(wallet, client, { address: regs[i].reg, abi: eacAbi, functionName: revoke ? "revokeRootRoles" : "grantRootRoles", args: [ADMIN_ROLES, addr as Address] }, (h) => up(i, { tx: h }));
        up(i, { state: "done" });
      }
      if (!revoke) {
        const i = regs.length;
        up(i, { state: "doing" });
        await send(wallet, client, { address: tree!.resolver, abi: eacAbi, functionName: "grantSetterRoles", args: [textKeySetter("meshy.policy"), addr as Address] }, (h) => up(i, { tx: h }));
        up(i, { state: "done" });
      }
      toast(revoke ? "Admin removed" : `Admin added for ${network?.label}`, "ok");
      check();
    } catch (e) {
      const msg = e instanceof Error ? e.message.split("\n")[0] : String(e);
      setSteps((s) => s?.map((x) => (x.state === "doing" ? { ...x, state: "fail", detail: msg } : x)));
    }
  }

  return (
    <>
      <Card
        title="Admins"
        className="mt-10"
        actions={
          <button onClick={() => setOpen(true)} className="text-[13px] font-medium text-accent hover:underline">
            Add or check an admin
          </button>
        }
      >
        <Row>
          <span className="flex-1 text-muted">An admin can give and revoke access inside one network, without owning the org or touching the org key.</span>
        </Row>
      </Card>

      {open && (
        <Sheet
          title="Network admin"
          onClose={() => (setOpen(false), setSteps(undefined), setStatus(undefined))}
          locked={busy}
          footer={
            steps && !busy ? (
              <>
                <span />
                <Button kind="primary" onClick={() => (setOpen(false), setSteps(undefined))}>Done</Button>
              </>
            ) : (
              <>
                <Button kind="ghost" disabled={!isAddress(addr) || busy} onClick={check}>Check</Button>
                <div className="flex gap-2">
                  <Button kind="danger" disabled={!isOwner || !isAddress(addr) || busy} onClick={() => apply(true)}>Remove</Button>
                  <Button kind="primary" disabled={!isOwner || !isAddress(addr) || busy} onClick={() => apply(false)}>
                    Make admin · {regs.length + 1} tx
                  </Button>
                </div>
              </>
            )
          }
        >
          {steps ? (
            <Steps steps={steps} />
          ) : (
            <>
              <Field label="Network">
                <select className={inputCls} value={network?.name} onChange={(e) => (setNet(e.target.value), setStatus(undefined))}>
                  {tree.networks.map((n) => (
                    <option key={n.name} value={n.name}>{n.label}</option>
                  ))}
                </select>
              </Field>
              <Field label="Wallet address">
                <input className={inputCls} value={addr} onChange={(e) => (setAddr(e.target.value.trim()), setStatus(undefined))} placeholder="0x…" />
              </Field>
              {status && (
                <div className="flex flex-wrap gap-2">
                  {Object.entries(status).map(([n, ok]) => (
                    <Badge key={n} tone={ok ? "ok" : "muted"}>
                      {n.replace(`.${org}`, "")}: {ok ? "admin" : "no roles"}
                    </Badge>
                  ))}
                </div>
              )}
              {!isOwner && <p className="text-[13px] text-muted">Only the org owner can add or remove admins.</p>}
            </>
          )}
        </Sheet>
      )}
    </>
  );
}

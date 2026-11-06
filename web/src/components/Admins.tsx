"use client";
import { ShieldPlus, UserMinus } from "lucide-react";
import { useState } from "react";
import { type Address, isAddress } from "viem";
import { type Step, Steps } from "@/components/Steps";
import { Badge, Button, Card, Field, inputCls } from "@/components/ui";
import { ADMIN_ROLES, eacAbi, send, textKeySetter } from "@/lib/ens";
import { useMeshy } from "@/lib/store";

/** Delegated network admins via ENSv2 Enhanced Access Control. */
export function Admins() {
  const { tree, wallet, client, account, toast } = useMeshy();
  const [net, setNet] = useState(tree?.networks[0]?.name ?? "");
  const [addr, setAddr] = useState("");
  const [status, setStatus] = useState<Record<string, boolean>>();
  const [steps, setSteps] = useState<Step[]>();
  if (!tree) return null;
  const isOwner = !!account && tree.owner.toLowerCase() === account.toLowerCase();
  const network = tree.networks.find((n) => n.name === net);
  // Registries an admin of this network needs roles on: the network, each device, each service.
  const regs = network
    ? [
        { name: network.name, reg: network.registry },
        ...network.resources.map((r) => ({ name: r.name, reg: r.registry })),
        ...network.resources.flatMap((r) => r.services.map((s) => ({ name: s.name, reg: s.registry }))),
      ]
    : [];

  async function check() {
    if (!isAddress(addr)) return;
    const out: Record<string, boolean> = {};
    for (const r of regs) out[r.name] = (await client.readContract({ address: r.reg, abi: eacAbi, functionName: "hasRootRoles", args: [ADMIN_ROLES, addr as Address] })) as boolean;
    setStatus(out);
  }

  async function apply(revoke: boolean) {
    if (!wallet || !isAddress(addr)) return;
    const list: Step[] = regs.map((r) => ({ label: `${revoke ? "Revoke" : "Grant"} REGISTRAR|UNREGISTER|RENEW on ${r.name}`, state: "todo" as const }));
    if (!revoke) list.push({ label: "Grant setter role: may write ONLY meshy.policy on the org resolver", state: "todo" });
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
      toast(revoke ? "Admin removed" : `Admin delegated for ${net}`, "ok");
      check();
    } catch (e) {
      const msg = e instanceof Error ? e.message.split("\n")[0] : String(e);
      setSteps((s) => s?.map((x) => (x.state === "doing" ? { ...x, state: "fail", detail: msg } : x)));
    }
  }

  return (
    <Card title={<span className="flex items-center gap-2"><ShieldPlus size={15} /> Delegated admins (ENSv2 Enhanced Access Control)</span>} className="lg:col-span-2">
      <p className="mb-4 text-sm text-muted">
        Give a teammate admin rights over one network without handing over the org: they can create, grant, revoke and renew names inside it (EAC root roles on that network&apos;s
        registries), and write <b>only</b> the <span className="mono">meshy.policy</span> text record on the org resolver (a setter-scoped role) — they cannot touch{" "}
        <span className="mono">meshy.orgkey</span>, the attestor list, or other networks.
      </p>
      <div className="grid gap-3 md:grid-cols-[200px_1fr_auto_auto_auto]">
        <Field label="Network">
          <select className={inputCls} value={net} onChange={(e) => (setNet(e.target.value), setStatus(undefined))}>
            {tree.networks.map((n) => (
              <option key={n.name}>{n.name}</option>
            ))}
          </select>
        </Field>
        <Field label="Admin address">
          <input className={inputCls} value={addr} onChange={(e) => (setAddr(e.target.value.trim()), setStatus(undefined))} placeholder="0x…" />
        </Field>
        <div className="flex items-end">
          <Button disabled={!isAddress(addr)} onClick={check}>Check</Button>
        </div>
        <div className="flex items-end">
          <Button kind="primary" disabled={!isOwner || !isAddress(addr)} onClick={() => apply(false)}>
            <ShieldPlus size={14} /> Delegate ({regs.length + 1} tx)
          </Button>
        </div>
        <div className="flex items-end">
          <Button kind="danger" disabled={!isOwner || !isAddress(addr)} onClick={() => apply(true)}>
            <UserMinus size={14} /> Remove
          </Button>
        </div>
      </div>
      {status && (
        <div className="mt-4 flex flex-wrap gap-2">
          {Object.entries(status).map(([n, ok]) => (
            <Badge key={n} tone={ok ? "ok" : "muted"}>
              {n}: {ok ? "admin" : "no roles"}
            </Badge>
          ))}
        </div>
      )}
      {steps && <div className="mt-4"><Steps steps={steps} /></div>}
    </Card>
  );
}

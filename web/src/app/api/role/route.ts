// GET /api/role?org=acme.eth&address=0x…  →  what this address can do in the org, read from ENSv2.
//   { org, exists, owner, role: "owner" | "admin" | "member", adminOf: ["prod.acme.eth", …] }
// "admin" = delegated network admin (ENSv2 Enhanced Access Control root roles on a network registry).
// Meshy for Mac calls this after Sign-In with Ethereum, when the user adds an org. Public data only.
import { type Address, type PublicClient, createPublicClient, http, isAddress, zeroAddress } from "viem";
import { sepolia } from "viem/chains";
import { ENS, isOrgName } from "@/lib/config";
import { ADMIN_ROLES, childLabels, eacAbi, exactOwner, firstLabel, getState, subregistryOf } from "@/lib/ens";

export async function GET(req: Request) {
  const u = new URL(req.url);
  const org = (u.searchParams.get("org") ?? "").trim().toLowerCase();
  const address = (u.searchParams.get("address") ?? "").trim();
  if (!isOrgName(org) || !isAddress(address)) return Response.json({ error: "org (an .eth name) and address are required" }, { status: 400 });

  // Our own RPC proxy: it holds the provider key and routes eth_getLogs to a node that allows wide ranges.
  const c = createPublicClient({ chain: sepolia, batch: { multicall: { wait: 10 } }, transport: http(new URL("/api/rpc", u.origin).toString(), { batch: { wait: 10 }, retryCount: 2 }) }) as PublicClient;
  try {
    const orgRegistry = await subregistryOf(c, ENS.ethRegistry, firstLabel(org));
    if (orgRegistry === zeroAddress) return Response.json({ org, exists: false, role: null, adminOf: [] }, { headers: { "cache-control": "s-maxage=30" } });
    const owner = await exactOwner(c, org);
    const me = address.toLowerCase();
    const adminOf: string[] = [];
    if (owner.toLowerCase() !== me) {
      const now = Math.floor(Date.now() / 1000);
      const labels = (await childLabels(c, orgRegistry)).filter((l) => l.label !== "agents" && !l.unregistered);
      await Promise.all(
        labels.map(async ({ label }) => {
          const [st, reg] = await Promise.all([getState(c, orgRegistry, label), subregistryOf(c, orgRegistry, label)]);
          if (st.status !== 2 || Number(st.expiry) <= now || reg === zeroAddress) return;
          const ok = (await c.readContract({ address: reg, abi: eacAbi, functionName: "hasRootRoles", args: [ADMIN_ROLES, address as Address] })) as boolean;
          if (ok) adminOf.push(`${label}.${org}`);
        }),
      );
    }
    const role = owner.toLowerCase() === me ? "owner" : adminOf.length ? "admin" : "member";
    return Response.json({ org, exists: true, owner, role, adminOf: adminOf.sort() }, { headers: { "cache-control": "s-maxage=30" } });
  } catch (e) {
    return Response.json({ error: e instanceof Error ? e.message.split("\n")[0] : String(e) }, { status: 502 });
  }
}

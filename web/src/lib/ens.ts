// ENSv2 (Sepolia) reads + writes for Meshy. Mirrors meshy/enskit.
import {
  type Address,
  type Hex,
  type PublicClient,
  type WalletClient,
  createPublicClient,
  decodeEventLog,
  decodeFunctionResult,
  encodeFunctionData,
  getAddress,
  http,
  keccak256,
  labelhash,
  namehash,
  parseAbi,
  stringToBytes,
  toHex,
  zeroAddress,
} from "viem";
import { sepolia } from "viem/chains";
import { ENS, FROM_BLOCK } from "./config";

export const helperAbi = parseAbi([
  "function findExactOwner(bytes name) view returns (address)",
  "function findParentRegistry(bytes name) view returns (address)",
]);

export const registryAbi = parseAbi([
  "struct State { uint8 status; uint64 expiry; address latestOwner; uint256 tokenId; uint256 resource; }",
  "function getState(uint256 anyId) view returns (State)",
  "function getResolver(string label) view returns (address)",
  "function getSubregistry(string label) view returns (address)",
  "function register(string label, address owner, address registry, address resolver, uint256 roleBitmap, uint64 expiry) returns (uint256)",
  "function unregister(uint256 anyId)",
  "function renew(uint256 anyId, uint64 newExpiry)",
  "function initialize((address account, uint256 roleBitmap)[] grants)",
  "event LabelRegistered(uint256 indexed tokenId, bytes32 indexed labelHash, string label, address owner, uint64 expiry, address indexed sender)",
  "event LabelUnregistered(uint256 indexed tokenId, address indexed sender)",
  "event ExpiryUpdated(uint256 indexed tokenId, uint64 indexed newExpiry, address indexed sender)",
]);

export const resolverAbi = parseAbi([
  "function resolve(bytes name, bytes data) view returns (bytes)",
  "function text(bytes32 node, string key) view returns (string)",
  "function addr(bytes32 node) view returns (address)",
  "function setText(bytes name, string key, string value)",
  "function setAddress(bytes name, uint256 coinType, bytes addressBytes)",
  "function multicall(bytes[] data) returns (bytes[])",
  "function initialize((address account, uint256 roleBitmap)[] grants, bytes[] calls)",
]);

export const factoryAbi = parseAbi([
  "function deployProxy(address implementation, uint256 salt, bytes data) returns (address)",
  "event ProxyDeployed(address indexed sender, address indexed proxyAddress, uint256 salt, address implementation)",
]);

export const ALL_ROLES = BigInt("0x" + "1".repeat(64));

export const STATUS = ["available", "reserved", "registered"] as const;

export function dnsEncode(name: string): Hex {
  const out: number[] = [];
  for (const l of name.split(".")) {
    const b = stringToBytes(l);
    if (b.length === 0 || b.length > 255) throw new Error(`bad label in ${name}`);
    out.push(b.length, ...b);
  }
  out.push(0);
  return toHex(new Uint8Array(out));
}

export const labelId = (label: string) => BigInt(labelhash(label));
export const firstLabel = (name: string) => name.split(".")[0];
export const parentName = (name: string) => name.split(".").slice(1).join(".");

export function publicClient(): PublicClient {
  // RPC goes through our server-side proxy so the provider key stays private.
  const url = typeof window === "undefined" ? process.env.SEPOLIA_RPC! : "/api/rpc";
  return createPublicClient({ chain: sepolia, transport: http(url, { batch: true }) }) as PublicClient;
}

export type State = { status: number; expiry: bigint; latestOwner: Address; tokenId: bigint; resource: bigint };

export async function exactOwner(c: PublicClient, name: string): Promise<Address> {
  return c.readContract({ address: ENS.universalHelper, abi: helperAbi, functionName: "findExactOwner", args: [dnsEncode(name)] });
}

export async function parentRegistry(c: PublicClient, name: string): Promise<Address> {
  return c.readContract({ address: ENS.universalHelper, abi: helperAbi, functionName: "findParentRegistry", args: [dnsEncode(name)] });
}

export async function getState(c: PublicClient, registry: Address, label: string): Promise<State> {
  return (await c.readContract({ address: registry, abi: registryAbi, functionName: "getState", args: [labelId(label)] })) as State;
}

export async function resolverOf(c: PublicClient, registry: Address, label: string): Promise<Address> {
  return c.readContract({ address: registry, abi: registryAbi, functionName: "getResolver", args: [label] });
}

export async function subregistryOf(c: PublicClient, registry: Address, label: string): Promise<Address> {
  return c.readContract({ address: registry, abi: registryAbi, functionName: "getSubregistry", args: [label] });
}

/** ENSIP-10 resolve() read (PermissionedResolver has no direct getters). */
export async function text(c: PublicClient, resolver: Address, name: string, key: string): Promise<string> {
  if (resolver === zeroAddress) return "";
  const data = encodeFunctionData({ abi: resolverAbi, functionName: "text", args: [namehash(name), key] });
  try {
    const out = await c.readContract({ address: resolver, abi: resolverAbi, functionName: "resolve", args: [dnsEncode(name), data] });
    return decodeFunctionResult({ abi: resolverAbi, functionName: "text", data: out });
  } catch {
    return "";
  }
}

export async function addrRecord(c: PublicClient, resolver: Address, name: string): Promise<Address> {
  if (resolver === zeroAddress) return zeroAddress;
  const data = encodeFunctionData({ abi: resolverAbi, functionName: "addr", args: [namehash(name)] });
  try {
    const out = await c.readContract({ address: resolver, abi: resolverAbi, functionName: "resolve", args: [dnsEncode(name), data] });
    return decodeFunctionResult({ abi: resolverAbi, functionName: "addr", data: out });
  } catch {
    return zeroAddress;
  }
}

const regEvents = registryAbi.filter(
  (x) => x.type === "event" && (x.name === "LabelRegistered" || x.name === "LabelUnregistered"),
);

/** Labels ever registered in a registry, with whether the latest registration was unregistered (revoked). */
export async function childLabels(
  c: PublicClient,
  registry: Address,
): Promise<{ label: string; tokenId: bigint; block: bigint; unregistered: boolean }[]> {
  const latest = await c.getBlockNumber();
  const logs: { data: Hex; topics: [Hex, ...Hex[]] | []; blockNumber: bigint | null }[] = [];
  // Chunk to stay within RPC log-range limits.
  for (let from = FROM_BLOCK; from <= latest; from += 5000n) {
    const to = from + 4999n > latest ? latest : from + 4999n;
    logs.push(...((await c.getLogs({ address: registry, events: regEvents as never, fromBlock: from, toBlock: to })) as never[]));
  }
  const latestByLabel = new Map<string, { label: string; tokenId: bigint; block: bigint; unregistered: boolean }>();
  const labelByToken = new Map<string, string>();
  for (const log of logs) {
    const ev = decodeEventLog({ abi: registryAbi, data: log.data, topics: log.topics as never }) as {
      eventName: string;
      args: { label?: string; tokenId: bigint };
    };
    if (ev.eventName === "LabelRegistered" && ev.args.label) {
      latestByLabel.set(ev.args.label, { label: ev.args.label, tokenId: ev.args.tokenId, block: log.blockNumber!, unregistered: false });
      labelByToken.set(ev.args.tokenId.toString(), ev.args.label);
    } else if (ev.eventName === "LabelUnregistered") {
      const l = labelByToken.get(ev.args.tokenId.toString());
      const cur = l ? latestByLabel.get(l) : undefined;
      if (cur && cur.tokenId === ev.args.tokenId) cur.unregistered = true;
    }
  }
  return [...latestByLabel.values()];
}

// ---------- org tree ----------

export type GrantNode = {
  name: string;
  agent: string;
  registry: Address;
  owner: Address;
  expiry: number;
  status: "live" | "expired" | "revoked";
  tokenId: bigint;
  policySet: boolean;
};

export type ResourceNode = {
  name: string;
  label: string;
  network: string;
  registry: Address; // grants registry (subregistry of the resource)
  parentRegistry: Address;
  addr: Address;
  encKey: string;
  sshHost: string;
  roots: string[];
  endpoint: string;
  grants: GrantNode[];
};

export type AgentNode = {
  name: string;
  label: string;
  addr: Address;
  ssh: string;
  owner: string;
  worldid: string;
  agentContext: string;
};

export type NetworkNode = { name: string; label: string; registry: Address; resources: ResourceNode[] };

export type OrgTree = {
  org: string;
  owner: Address;
  orgRegistry: Address;
  agentsRegistry: Address;
  resolver: Address;
  orgKey: string;
  attestors: string[];
  networks: NetworkNode[];
  agents: AgentNode[];
  block: bigint;
  blockTime: number;
};

export async function loadOrg(c: PublicClient, org: string): Promise<OrgTree> {
  const label = firstLabel(org);
  const [orgRegistry, resolver, owner, block] = await Promise.all([
    subregistryOf(c, ENS.ethRegistry, label),
    resolverOf(c, ENS.ethRegistry, label),
    exactOwner(c, org),
    c.getBlock(),
  ]);
  if (orgRegistry === zeroAddress) throw new Error(`${org} has no subregistry: is it a Meshy org?`);
  const [orgKey, attestor] = await Promise.all([text(c, resolver, org, "meshy.orgkey"), text(c, resolver, org, "meshy.attestor")]);
  const tree: OrgTree = {
    org,
    owner,
    orgRegistry,
    agentsRegistry: zeroAddress,
    resolver,
    orgKey,
    attestors: attestor.split(",").map((s) => s.trim()).filter(Boolean),
    networks: [],
    agents: [],
    block: block.number,
    blockTime: Number(block.timestamp),
  };
  const now = Number(block.timestamp);
  const top = await childLabels(c, orgRegistry);
  await Promise.all(
    top.map(async ({ label: l }) => {
      const name = `${l}.${org}`;
      const st = await getState(c, orgRegistry, l);
      if (st.status !== 2 || Number(st.expiry) <= now) return;
      const sub = await subregistryOf(c, orgRegistry, l);
      if (l === "agents") {
        tree.agentsRegistry = sub;
        if (sub === zeroAddress) return;
        const agents = await childLabels(c, sub);
        tree.agents = (
          await Promise.all(
            agents.map(async ({ label: a }) => {
              const an = `${a}.${name}`;
              const ast = await getState(c, sub, a);
              if (ast.status !== 2) return null;
              const res = await resolverOf(c, sub, a);
              const [addr, ssh, ownerTxt, worldid, ctx] = await Promise.all([
                addrRecord(c, res, an),
                text(c, res, an, "meshy.ssh"),
                text(c, res, an, "meshy.owner"),
                text(c, res, an, "meshy.worldid"),
                text(c, res, an, "agent-context"),
              ]);
              return { name: an, label: a, addr, ssh, owner: ownerTxt, worldid, agentContext: ctx } as AgentNode;
            }),
          )
        ).filter(Boolean) as AgentNode[];
        return;
      }
      const net: NetworkNode = { name, label: l, registry: sub, resources: [] };
      if (sub !== zeroAddress) {
        const res = await childLabels(c, sub);
        net.resources = (
          await Promise.all(
            res.map(async ({ label: r }) => {
              const rn = `${r}.${name}`;
              const rst = await getState(c, sub, r);
              if (rst.status !== 2) return null;
              const [rres, grantsReg] = await Promise.all([resolverOf(c, sub, r), subregistryOf(c, sub, r)]);
              const [addr, encKey, sshHost, roots, endpoint] = await Promise.all([
                addrRecord(c, rres, rn),
                text(c, rres, rn, "meshy.enckey"),
                text(c, rres, rn, "meshy.ssh-host"),
                text(c, rres, rn, "meshy.roots"),
                text(c, rres, rn, "meshy.endpoint"),
              ]);
              const node: ResourceNode = {
                name: rn,
                label: r,
                network: name,
                registry: grantsReg,
                parentRegistry: sub,
                addr,
                encKey,
                sshHost,
                roots: safeJSON<string[]>(roots, []),
                endpoint,
                grants: [],
              };
              if (grantsReg !== zeroAddress) {
                const gl = await childLabels(c, grantsReg);
                node.grants = await Promise.all(
                  gl.map(async ({ label: g, unregistered }) => {
                    const gn = `${g}.${rn}`;
                    const gst = await getState(c, grantsReg, g);
                    const exp = Number(gst.expiry);
                    const live = gst.status === 2 && exp > now;
                    const status: GrantNode["status"] = live ? "live" : unregistered ? "revoked" : "expired";
                    const gres = live ? await resolverOf(c, grantsReg, g) : zeroAddress;
                    const pol = live ? await text(c, gres, gn, "meshy.policy") : "";
                    return { name: gn, agent: g, registry: grantsReg, owner: gst.latestOwner, expiry: exp, status, tokenId: gst.tokenId, policySet: !!pol };
                  }),
                );
              }
              return node;
            }),
          )
        ).filter(Boolean) as ResourceNode[];
      }
      tree.networks.push(net);
    }),
  );
  tree.networks.sort((a, b) => a.name.localeCompare(b.name));
  return tree;
}

function safeJSON<T>(s: string, d: T): T {
  try {
    return s ? (JSON.parse(s) as T) : d;
  } catch {
    return d;
  }
}

// ---------- writes (browser wallet) ----------

export type Tx = { label: string; hash?: Hex; status: "pending" | "sent" | "mined" | "failed"; block?: bigint; error?: string; at?: number };

export async function send(
  w: WalletClient,
  c: PublicClient,
  req: { address: Address; abi: readonly unknown[]; functionName: string; args: readonly unknown[] },
  onSent?: (hash: Hex) => void,
) {
  const [account] = await w.getAddresses();
  const hash = await w.writeContract({ ...req, account, chain: sepolia } as never);
  onSent?.(hash);
  const receipt = await c.waitForTransactionReceipt({ hash, pollingInterval: 1000 });
  if (receipt.status !== "success") throw new Error(`tx ${hash} reverted`);
  return receipt;
}

export function recordCalls(name: string, texts: Record<string, string>, addr?: Address): Hex[] {
  const dns = dnsEncode(name);
  const calls: Hex[] = [];
  if (addr) calls.push(encodeFunctionData({ abi: resolverAbi, functionName: "setAddress", args: [dns, 60n, addr] }));
  for (const [k, v] of Object.entries(texts)) calls.push(encodeFunctionData({ abi: resolverAbi, functionName: "setText", args: [dns, k, v] }));
  return calls;
}

export function randomSalt(): bigint {
  const b = crypto.getRandomValues(new Uint8Array(32));
  return BigInt(keccak256(b));
}

export async function deployRegistry(w: WalletClient, c: PublicClient, admin: Address): Promise<Address> {
  const init = encodeFunctionData({ abi: registryAbi, functionName: "initialize", args: [[{ account: admin, roleBitmap: ALL_ROLES }]] });
  const r = await send(w, c, { address: ENS.factory, abi: factoryAbi, functionName: "deployProxy", args: [ENS.userRegistryImpl, randomSalt(), init] });
  for (const l of r.logs) {
    try {
      const ev = decodeEventLog({ abi: factoryAbi, data: l.data, topics: l.topics });
      if (ev.eventName === "ProxyDeployed") return getAddress(ev.args.proxyAddress);
    } catch {}
  }
  throw new Error("ProxyDeployed not found");
}

export const yearFromNow = () => BigInt(Math.floor(Date.now() / 1000) + 365 * 86400);

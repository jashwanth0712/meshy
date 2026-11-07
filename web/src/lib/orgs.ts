// Self-serve orgs and invites (docs/orgs/PLAN.md).
//
//   MeshyOrgFactory.createOrg(label, orgKey, attestor)   one tx -> <label>.meshy.eth, owned by the caller
//   MeshyInvites.claim(invite, sig, label, sshKey)        one tx -> <label>.people.<org> (or .agents.)
//
// The org key (X25519, for encrypted policies and audit) is derived from a wallet
// signature, so the owner can re-derive it on any device and never has to back it up.
import { type Address, type Hex, type PublicClient, type WalletClient, bytesToHex, encodeAbiParameters, hexToBytes, keccak256, parseAbi, recoverTypedDataAddress, toBytes, toHex } from "viem";
import { x25519 } from "@noble/curves/ed25519.js";
import { hkdf } from "@noble/hashes/hkdf.js";
import { sha256 } from "@noble/hashes/sha2.js";
import { b64 } from "./envelope";
import dep from "./abi/orgs-deployment.json";

export const ORG_PARENT = dep.parentName as string; // "meshy.eth"
export const ORG_FACTORY = dep.MeshyOrgFactory as Address;
export const INVITES = dep.MeshyInvites as Address;
export const ORGS_FROM_BLOCK = BigInt(dep.deployBlock);
export const orgsLive = /^0x[0-9a-fA-F]{40}$/.test(ORG_FACTORY) && ORG_FACTORY !== "0x0000000000000000000000000000000000000000";

export const factoryAbi = parseAbi([
  "function isAvailable(string label) view returns (bool)",
  "function orgsOf(address owner) view returns (string[])",
  "function orgs(bytes32) view returns (address owner, address orgRegistry, address resolver, address peopleRegistry, address agentsRegistry, uint64 createdAt)",
  "function createOrg(string label, string orgKey, string attestor) returns ((address owner, address orgRegistry, address resolver, address peopleRegistry, address agentsRegistry, uint64 createdAt))",
  "event OrgCreated(string indexed labelIndexed, string label, string name, address indexed owner, address orgRegistry, address resolver, address peopleRegistry, address agentsRegistry)",
]);

export const invitesAbi = parseAbi([
  "function check((address registry, string parentName, address otherRegistry, string label, address invitee, uint64 expiresAt, uint64 nameExpiry, uint256 nonce) inv, bytes sig, address claimer, string label) view returns (string)",
  "function claim((address registry, string parentName, address otherRegistry, string label, address invitee, uint64 expiresAt, uint64 nameExpiry, uint256 nonce) inv, bytes sig, string label, string sshKey) returns (string)",
  "function cancel(uint256 nonce)",
  "function used(address issuer, uint256 nonce) view returns (bool)",
  "event Claimed(address indexed issuer, uint256 indexed nonce, address indexed claimer, string name, address resolver)",
]);

export const labelOk = (s: string) => /^[a-z0-9](?:[a-z0-9-]{0,30}[a-z0-9])?$/.test(s);

// ---- org key derived from a wallet signature ----

export const orgKeyMessage = (org: string) =>
  `Meshy org key v1\n\nOrg: ${org}\n\nSigning this derives your org's encryption key on this device. It costs nothing and sends nothing on-chain. Only sign it on meshy.live.`;

/** Deterministic X25519 key from a personal_sign signature (RFC 6979 signatures are deterministic). */
export function orgKeyFromSignature(org: string, sig: Hex): { priv: string; pub: string } {
  const seed = hkdf(sha256, hexToBytes(sig), new TextEncoder().encode(org), new TextEncoder().encode("meshy-orgkey-v1"), 32);
  return { priv: b64(seed), pub: b64(x25519.getPublicKey(seed)) };
}

export async function deriveOrgKey(wallet: WalletClient, account: Address, org: string) {
  const sig = await wallet.signMessage({ account, message: orgKeyMessage(org) });
  return orgKeyFromSignature(org, sig);
}

// ---- invites (EIP-712, matches contracts/src/MeshyInvites.sol) ----

export type Invite = {
  registry: Address;
  parentName: string;
  otherRegistry: Address;
  label: string;
  invitee: Address;
  expiresAt: bigint;
  nameExpiry: bigint;
  nonce: bigint;
};

export type InviteRole = "member" | "agent";

/** What travels in the link: the invite, its signature, and display hints. */
export type InviteBlob = { v: 1; org: string; role: InviteRole; by: Address; inv: Invite; sig: Hex };

export const inviteTypes = {
  Invite: [
    { name: "registry", type: "address" },
    { name: "parentName", type: "string" },
    { name: "otherRegistry", type: "address" },
    { name: "label", type: "string" },
    { name: "invitee", type: "address" },
    { name: "expiresAt", type: "uint64" },
    { name: "nameExpiry", type: "uint64" },
    { name: "nonce", type: "uint256" },
  ],
} as const;

export const inviteDomain = { name: "MeshyInvites", version: "1", chainId: 11155111, verifyingContract: INVITES } as const;

export function randomNonce(): bigint {
  const b = crypto.getRandomValues(new Uint8Array(16));
  return BigInt(toHex(b));
}

export async function signInvite(wallet: WalletClient, account: Address, inv: Invite): Promise<Hex> {
  return wallet.signTypedData({ account, domain: inviteDomain, types: inviteTypes, primaryType: "Invite", message: inv });
}

const ZERO = "0x0000000000000000000000000000000000000000" as Address;
export { ZERO as ZERO_ADDRESS };

const b64url = (u: Uint8Array) => btoa(String.fromCharCode(...u)).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
const unb64url = (s: string) => Uint8Array.from(atob(s.replace(/-/g, "+").replace(/_/g, "/") + "===".slice((s.length + 3) % 4)), (c) => c.charCodeAt(0));
const u32 = (n: bigint) => [Number((n >> 24n) & 255n), Number((n >> 16n) & 255n), Number((n >> 8n) & 255n), Number(n & 255n)];

/**
 * Compact invite link (v2, ~160 characters instead of ~900): only what can't be read back.
 *   [0]=2  [1]=flags (1: agent, 2: has invitee)  [2..6) expiresAt  [6..10) nameExpiry  [10..26) nonce
 *   [26..91) signature  [20 bytes invitee if flagged]  then "org\0label" in UTF-8.
 * The registries come from MeshyOrgFactory.orgs(org) and the inviter from the signature, so the
 * link stays short and still can't be tampered with (MeshyInvites checks the signature).
 */
export function encodeInvite(b: InviteBlob): string {
  const sig = hexToBytes(b.sig);
  const nonce = hexToBytes(toHex(b.inv.nonce, { size: 16 }));
  const hasInvitee = b.inv.invitee !== ZERO;
  const tail = new TextEncoder().encode(`${b.org}\0${b.inv.label}`);
  return b64url(
    Uint8Array.from([2, (b.role === "agent" ? 1 : 0) | (hasInvitee ? 2 : 0), ...u32(b.inv.expiresAt), ...u32(b.inv.nameExpiry), ...nonce, ...sig, ...(hasInvitee ? hexToBytes(b.inv.invitee) : []), ...tail]),
  );
}

/** Reads a link: v2 (compact, needs the chain) or v1 (the old JSON links). */
export async function decodeInvite(s: string, client?: PublicClient): Promise<InviteBlob> {
  const raw = unb64url(s);
  if (raw[0] !== 2) {
    const o = JSON.parse(new TextDecoder().decode(raw));
    o.inv.expiresAt = BigInt(o.inv.expiresAt);
    o.inv.nameExpiry = BigInt(o.inv.nameExpiry);
    o.inv.nonce = BigInt(o.inv.nonce);
    return o as InviteBlob;
  }
  if (!client) throw new Error("reading a compact invite needs a chain client");
  const num = (i: number) => BigInt(((raw[i] << 24) >>> 0) + (raw[i + 1] << 16) + (raw[i + 2] << 8) + raw[i + 3]);
  const role: InviteRole = raw[1] & 1 ? "agent" : "member";
  let at = 91;
  const invitee = raw[1] & 2 ? (bytesToHex(raw.slice(at, (at += 20))) as Address) : ZERO;
  const [org, label = ""] = new TextDecoder().decode(raw.slice(at)).split("\0");
  const o = (await client.readContract({ address: ORG_FACTORY, abi: factoryAbi, functionName: "orgs", args: [keccak256(toBytes(org.split(".")[0]))] })) as readonly Address[];
  const people = o[3], agents = o[4];
  const inv: Invite = {
    registry: role === "member" ? people : agents,
    parentName: `${role === "member" ? "people" : "agents"}.${org}`,
    otherRegistry: role === "member" ? agents : people,
    label,
    invitee,
    expiresAt: num(2),
    nameExpiry: num(6),
    nonce: BigInt(bytesToHex(raw.slice(10, 26))),
  };
  const sig = bytesToHex(raw.slice(26, 91));
  const by = await recoverTypedDataAddress({ domain: inviteDomain, types: inviteTypes, primaryType: "Invite", message: inv, signature: sig });
  return { v: 1, org, role, by, inv, sig };
}

export const inviteUrl = (origin: string, b: InviteBlob) => `${origin}/join/${encodeInvite(b)}`;

/** Short stable id for an invite (for lists / dedupe). */
export const inviteId = (b: InviteBlob) =>
  keccak256(encodeAbiParameters([{ type: "address" }, { type: "uint256" }], [b.by, b.inv.nonce])).slice(0, 14);

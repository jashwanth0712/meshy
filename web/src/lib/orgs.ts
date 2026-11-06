// Self-serve orgs and invites (docs/orgs/PLAN.md).
//
//   MeshyOrgFactory.createOrg(label, orgKey, attestor)   one tx -> <label>.meshy.eth, owned by the caller
//   MeshyInvites.claim(invite, sig, label, sshKey)        one tx -> <label>.people.<org> (or .agents.)
//
// The org key (X25519, for encrypted policies and audit) is derived from a wallet
// signature, so the owner can re-derive it on any device and never has to back it up.
import { type Address, type Hex, type WalletClient, encodeAbiParameters, hexToBytes, keccak256, parseAbi, toHex } from "viem";
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

export function encodeInvite(b: InviteBlob): string {
  const json = JSON.stringify(b, (_, v) => (typeof v === "bigint" ? v.toString() : v));
  return btoa(unescape(encodeURIComponent(json))).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

export function decodeInvite(s: string): InviteBlob {
  const pad = s.replace(/-/g, "+").replace(/_/g, "/") + "===".slice((s.length + 3) % 4);
  const o = JSON.parse(decodeURIComponent(escape(atob(pad))));
  o.inv.expiresAt = BigInt(o.inv.expiresAt);
  o.inv.nameExpiry = BigInt(o.inv.nameExpiry);
  o.inv.nonce = BigInt(o.inv.nonce);
  return o as InviteBlob;
}

export const inviteUrl = (origin: string, b: InviteBlob) => `${origin}/join/${encodeInvite(b)}`;

/** Short stable id for an invite (for lists / dedupe). */
export const inviteId = (b: InviteBlob) =>
  keccak256(encodeAbiParameters([{ type: "address" }, { type: "uint256" }], [b.by, b.inv.nonce])).slice(0, 14);

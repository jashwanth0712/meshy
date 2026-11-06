// TypeScript port of meshy/policy/envelope.go (must stay byte-compatible):
// multi-recipient X25519 -> HKDF-SHA256 -> XChaCha20-Poly1305, AAD = grant
// name (policies) or schema name (Arkiv payloads). base64 = RawStd (no pad).
import { x25519 } from "@noble/curves/ed25519.js";
import { xchacha20poly1305 } from "@noble/ciphers/chacha.js";
import { hkdf } from "@noble/hashes/hkdf.js";
import { sha256 } from "@noble/hashes/sha2.js";
import { randomBytes } from "@noble/hashes/utils.js";

const ALG = "x25519-hkdf-sha256-xchacha20poly1305";
const enc = new TextEncoder();
const dec = new TextDecoder();

export function b64(bytes: Uint8Array): string {
  let s = "";
  for (const b of bytes) s += String.fromCharCode(b);
  return btoa(s).replace(/=+$/, "");
}

export function unb64(s: string): Uint8Array {
  const pad = s.length % 4 === 0 ? "" : "=".repeat(4 - (s.length % 4));
  const bin = atob(s.replace(/-/g, "+").replace(/_/g, "/") + pad);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

const hex = (b: Uint8Array) => Array.from(b, (x) => x.toString(16).padStart(2, "0")).join("");

export function keyId(pub: Uint8Array): string {
  return hex(sha256(pub).slice(0, 8));
}

function concat(...parts: Uint8Array[]): Uint8Array {
  const out = new Uint8Array(parts.reduce((n, p) => n + p.length, 0));
  let o = 0;
  for (const p of parts) {
    out.set(p, o);
    o += p.length;
  }
  return out;
}

function kek(priv: Uint8Array, peer: Uint8Array, epk: Uint8Array, rcpt: Uint8Array): Uint8Array {
  const shared = x25519.getSharedSecret(priv, peer);
  return hkdf(sha256, shared, undefined, concat(enc.encode("meshy-policy-v1"), epk, rcpt), 32);
}

export type Envelope = {
  v: 1;
  alg: string;
  r: { kid: string; epk: string; n: string; wk: string }[];
  n: string;
  c: string;
};

export function generateKey(): { priv: string; pub: string } {
  const priv = x25519.utils.randomSecretKey();
  return { priv: b64(priv), pub: b64(x25519.getPublicKey(priv)) };
}

export function publicKeyOf(privB64: string): string {
  return b64(x25519.getPublicKey(unb64(privB64)));
}

/** Seal plaintext to base64 X25519 public keys. Returns the record JSON. */
export function sealBytes(plain: Uint8Array, aad: string, recipientsB64: string[]): string {
  if (recipientsB64.length === 0) throw new Error("no recipients");
  const ad = enc.encode(aad);
  const cek = randomBytes(32);
  const nonce = randomBytes(24);
  const env: Envelope = {
    v: 1,
    alg: ALG,
    r: [],
    n: b64(nonce),
    c: b64(xchacha20poly1305(cek, nonce, ad).encrypt(plain)),
  };
  for (const r of recipientsB64) {
    const pub = unb64(r);
    const eph = x25519.utils.randomSecretKey();
    const epk = x25519.getPublicKey(eph);
    const wn = randomBytes(24);
    const wk = xchacha20poly1305(kek(eph, pub, epk, pub), wn, ad).encrypt(cek);
    env.r.push({ kid: keyId(pub), epk: b64(epk), n: b64(wn), wk: b64(wk) });
  }
  return JSON.stringify(env);
}

/** Open a record sealed with SealBytes/Seal using a base64 X25519 private key. */
export function openBytes(record: string, aad: string, privB64: string): Uint8Array {
  const env = JSON.parse(record) as Envelope;
  if (env.v !== 1 || env.alg !== ALG) throw new Error(`unsupported envelope ${env.alg}`);
  const priv = unb64(privB64);
  const pub = x25519.getPublicKey(priv);
  const kid = keyId(pub);
  const ad = enc.encode(aad);
  const r = env.r.find((x) => x.kid === kid);
  if (!r) throw new Error("not a recipient of this envelope");
  const epk = unb64(r.epk);
  const cek = xchacha20poly1305(kek(priv, epk, epk, pub), unb64(r.n), ad).decrypt(unb64(r.wk));
  return xchacha20poly1305(cek, unb64(env.n), ad).decrypt(unb64(env.c));
}

export type PathRule = { p: string; m: "ro" | "rw" };
export type Policy = { v: 1; grant: string; grantTokenId: string; paths: PathRule[]; cmds?: string[] };

export function sealPolicy(p: Policy, recipientsB64: string[]): string {
  const body: Policy = { v: 1, grant: p.grant, grantTokenId: p.grantTokenId, paths: p.paths };
  if (p.cmds && p.cmds.length) body.cmds = p.cmds;
  return sealBytes(enc.encode(JSON.stringify(body)), p.grant, recipientsB64);
}

export function openPolicy(record: string, grant: string, privB64: string): Policy {
  return JSON.parse(dec.decode(openBytes(record, grant, privB64))) as Policy;
}

export function openJSON<T = unknown>(record: string, aad: string, privB64: string): T {
  return JSON.parse(dec.decode(openBytes(record, aad, privB64))) as T;
}

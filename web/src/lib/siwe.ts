// EIP-4361 (Sign-In with Ethereum) message for Meshy for Mac. Verified on the Mac by
// `meshy verify-siwe` (meshy/cmd/meshy/siwe.go), which parses exactly these fields.
import { getAddress } from "viem";
import { SEPOLIA_CHAIN_ID } from "./config";

export function siweMessage(o: { domain: string; origin: string; address: string; nonce: string; issuedAt: Date; ttlMs: number }) {
  return [
    `${o.domain} wants you to sign in with your Ethereum account:`,
    getAddress(o.address),
    "",
    "Sign in to Meshy for Mac.",
    "",
    `URI: ${o.origin}/connect`,
    "Version: 1",
    `Chain ID: ${SEPOLIA_CHAIN_ID}`,
    `Nonce: ${o.nonce}`,
    `Issued At: ${o.issuedAt.toISOString().replace(/\.\d{3}Z$/, "Z")}`,
    `Expiration Time: ${new Date(o.issuedAt.getTime() + o.ttlMs).toISOString().replace(/\.\d{3}Z$/, "Z")}`,
  ].join("\n");
}

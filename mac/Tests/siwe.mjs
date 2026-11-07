// Test helper: sign a Meshy SIWE message like /connect does and print the meshy://auth callback.
//   node Tests/siwe.mjs <nonce> [domain] [privateKey]
// Uses viem from web/node_modules (run `npm ci` in web/ first).
import { createRequire } from "node:module";
const req = createRequire(new URL("../../web/package.json", import.meta.url));
const { privateKeyToAccount } = await import(req.resolve("viem/accounts"));
const [nonce, domain = "meshy.live", pk = "0x" + "11".repeat(32)] = process.argv.slice(2);
const acct = privateKeyToAccount(pk);
const iso = (d) => d.toISOString().replace(/\.\d{3}Z$/, "Z");
const now = new Date();
const message = [
  `${domain} wants you to sign in with your Ethereum account:`,
  acct.address,
  "",
  "Sign in to Meshy for Mac.",
  "",
  `URI: https://${domain}/connect`,
  "Version: 1",
  "Chain ID: 11155111",
  `Nonce: ${nonce}`,
  `Issued At: ${iso(now)}`,
  `Expiration Time: ${iso(new Date(now.getTime() + 600_000))}`,
].join("\n");
const sig = await acct.signMessage({ message });
console.log(`meshy://auth?message=${encodeURIComponent(message)}&sig=${sig}`);
console.error(acct.address);

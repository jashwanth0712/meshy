"use client";
// Minimal browser-wallet helper for pages that live outside an org (create org, join).
import { type Address, type WalletClient, createWalletClient, custom } from "viem";
import { sepolia } from "viem/chains";

type Eth = { request: (a: { method: string; params?: unknown[] }) => Promise<unknown> };
const eth = () => (typeof window !== "undefined" ? (window as unknown as { ethereum?: Eth }).ethereum : undefined);

export const hasWallet = () => !!eth();

export async function connectWallet(): Promise<{ wallet: WalletClient; account: Address }> {
  const e = eth();
  if (!e) throw new Error("No browser wallet found. Install MetaMask in this browser, then reload.");
  const wallet = createWalletClient({ chain: sepolia, transport: custom(e) });
  const [account] = await wallet.requestAddresses();
  if (!account) throw new Error("The wallet didn't share an account.");
  try {
    await e.request({ method: "wallet_switchEthereumChain", params: [{ chainId: "0xaa36a7" }] });
  } catch {
    throw new Error("Switch your wallet to Sepolia and try again.");
  }
  return { wallet, account };
}

export async function silentAccount(): Promise<Address | undefined> {
  const e = eth();
  if (!e) return undefined;
  const a = (await e.request({ method: "eth_accounts" })) as Address[];
  return a[0];
}

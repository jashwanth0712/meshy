import type { Address } from "viem";

// ENSv2 on Sepolia (docs.ens.domains/learn/deployments, 2026-10-01).
export const ENS = {
  universalHelper: "0xd453e5bdb62cc3bea84341b1e306319c8ffd7dfe" as Address,
  rootRegistry: "0xb458d6a3a77919449d03e7a6903c26827c1ec43f" as Address,
  ethRegistry: "0xd4ebcbbdf463c9c45784603db0ddd499bc44a8b4" as Address,
  ethRegistrar: "0xf633e7fc17e2bbe0d0965d18ec1821dcb754a3d3" as Address,
  factory: "0xda70306c98e97ece36f997a21368e53298572991" as Address,
  userRegistryImpl: "0x9bd8a88719068d09ecee662f36c0e3856708366a" as Address,
  resolverImpl: "0x115eb53f0c60696633855f90b138178fb40b2b2c" as Address,
  mockUSDC: "0x240b0316df57887dbbe58b586508b19e633a14aa" as Address,
};

export const SEPOLIA_CHAIN_ID = 11155111;
export const ETHERSCAN = "https://sepolia.etherscan.io";
export const ENS_APP = "https://sepolia.app.ens.domains";
export const ARKIV_EXPLORER = "https://rabdi.explorer.arkiv.network";

/** Earliest block to scan for registry events (before meshydemo.eth existed). */
export const FROM_BLOCK = BigInt(process.env.NEXT_PUBLIC_FROM_BLOCK ?? "12072900");

/** meshy-sync (provider API). */
export const PROVIDER_API = process.env.NEXT_PUBLIC_MESHY_SYNC ?? "https://api.meshy.live";

/** The public demo org, offered as an explicit link on the home page (never a silent default). */
export const DEMO_ORG = process.env.NEXT_PUBLIC_DEMO_ORG ?? "meshydemo.eth";

/** ENS org names: lowercase labels ending in .eth. */
export const isOrgName = (s: string) => /^([a-z0-9-]+\.)+eth$/.test(s);

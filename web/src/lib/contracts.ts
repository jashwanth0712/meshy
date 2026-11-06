import type { Abi, Address } from "viem";
import subAbi from "./abi/MeshySubscription.json";
import regAbi from "./abi/ProviderRegistry.json";
import dep from "./abi/deployment.json";

export const REGISTRY = dep.ProviderRegistry as Address;
export const SUBSCRIPTION = dep.MeshySubscription as Address;
export const USDC = dep.usdc as Address;
export const registryAbi = regAbi as Abi;
export const subscriptionAbi = subAbi as Abi;
export const DEPLOYMENT = dep;

export const erc20Abi = [
  { type: "function", name: "approve", stateMutability: "nonpayable", inputs: [{ name: "s", type: "address" }, { name: "a", type: "uint256" }], outputs: [{ type: "bool" }] },
  { type: "function", name: "balanceOf", stateMutability: "view", inputs: [{ name: "a", type: "address" }], outputs: [{ type: "uint256" }] },
  { type: "function", name: "mint", stateMutability: "nonpayable", inputs: [{ name: "to", type: "address" }, { name: "a", type: "uint256" }], outputs: [] },
] as const;

export type Provider = {
  owner: Address;
  signer: Address;
  endpoint: string;
  pricePerSeatPerMonth: bigint;
  stake: bigint;
  unbondTime: bigint;
  status: number;
  metadataURI?: string;
};

export const STATUS = ["active", "unbonding", "withdrawn", "slashed"];
export const usdc = (v: bigint) => (Number(v) / 1e6).toLocaleString(undefined, { maximumFractionDigits: 2 });
export const eth = (v: bigint) => (Number(v) / 1e18).toLocaleString(undefined, { maximumFractionDigits: 4 });

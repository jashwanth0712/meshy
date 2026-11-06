"use client";
import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import { type Address, type PublicClient, type WalletClient, createWalletClient, custom } from "viem";
import { sepolia } from "viem/chains";
import { loadOrg, publicClient, type OrgTree } from "./ens";

type Eth = { request: (a: { method: string; params?: unknown[] }) => Promise<unknown>; on?: (e: string, f: (...a: unknown[]) => void) => void };

declare global {
  interface Window {
    ethereum?: Eth;
  }
}

type Ctx = {
  client: PublicClient;
  wallet?: WalletClient;
  account?: Address;
  chainOk: boolean;
  connect: () => Promise<void>;
  org: string;
  setOrg: (o: string) => void;
  tree?: OrgTree;
  loading: boolean;
  error?: string;
  refresh: () => Promise<void>;
  orgKey?: string;
  setOrgKey: (k?: string) => void;
  toast: (msg: string, kind?: "ok" | "err" | "info") => void;
  toasts: { id: number; msg: string; kind: string }[];
};

const C = createContext<Ctx | null>(null);

export function useMeshy() {
  const c = useContext(C);
  if (!c) throw new Error("useMeshy outside MeshyProvider");
  return c;
}

export function MeshyProvider({ org: initialOrg, children }: { org: string; children: ReactNode }) {
  const client = useMemo(() => publicClient(), []);
  const [wallet, setWallet] = useState<WalletClient>();
  const [account, setAccount] = useState<Address>();
  const [chainOk, setChainOk] = useState(false);
  const [org, setOrg] = useState(initialOrg);
  const [tree, setTree] = useState<OrgTree>();
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string>();
  const [orgKey, setOrgKeyState] = useState<string>();
  const [toasts, setToasts] = useState<{ id: number; msg: string; kind: string }[]>([]);

  const toast = useCallback((msg: string, kind: "ok" | "err" | "info" = "info") => {
    const id = Date.now() + Math.random();
    setToasts((t) => [...t, { id, msg, kind }]);
    setTimeout(() => setToasts((t) => t.filter((x) => x.id !== id)), 6000);
  }, []);

  useEffect(() => {
    setOrgKeyState(localStorage.getItem(`meshy.orgkey.${org}`) ?? undefined);
  }, [org]);

  const setOrgKey = useCallback(
    (k?: string) => {
      if (k) localStorage.setItem(`meshy.orgkey.${org}`, k);
      else localStorage.removeItem(`meshy.orgkey.${org}`);
      setOrgKeyState(k);
    },
    [org],
  );

  const refresh = useCallback(async () => {
    setLoading(true);
    setError(undefined);
    try {
      setTree(await loadOrg(client, org));
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setLoading(false);
    }
  }, [client, org]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  const syncChain = useCallback(async () => {
    if (!window.ethereum) return;
    const id = (await window.ethereum.request({ method: "eth_chainId" })) as string;
    setChainOk(parseInt(id, 16) === sepolia.id);
  }, []);

  const connect = useCallback(async () => {
    if (!window.ethereum) {
      toast("No browser wallet found. Install MetaMask.", "err");
      return;
    }
    const w = createWalletClient({ chain: sepolia, transport: custom(window.ethereum) });
    const [a] = await w.requestAddresses();
    try {
      await window.ethereum.request({ method: "wallet_switchEthereumChain", params: [{ chainId: "0xaa36a7" }] });
    } catch {
      toast("Switch your wallet to Sepolia", "err");
    }
    setWallet(w);
    setAccount(a);
    await syncChain();
  }, [syncChain, toast]);

  useEffect(() => {
    const eth = window.ethereum;
    if (!eth?.on) return;
    eth.on("accountsChanged", (a) => setAccount((a as Address[])[0]));
    eth.on("chainChanged", () => syncChain());
    (eth.request({ method: "eth_accounts" }) as Promise<Address[]>).then((a) => {
      if (a[0]) {
        setWallet(createWalletClient({ chain: sepolia, transport: custom(eth) }));
        setAccount(a[0]);
        syncChain();
      }
    });
  }, [syncChain]);

  return (
    <C.Provider
      value={{ client, wallet, account, chainOk, connect, org, setOrg, tree, loading, error, refresh, orgKey, setOrgKey, toast, toasts }}
    >
      {children}
    </C.Provider>
  );
}

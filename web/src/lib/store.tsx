"use client";
import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import { rememberOrg } from "./recent";
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
  /** The connected wallet's role in the org, read from ENSv2 (/api/role). */
  role?: { role: "owner" | "admin" | "member"; adminOf: string[] };
  /** Owner or delegated admin: may give, revoke, approve, invite and remove. Members only request. */
  canManage: boolean;
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
  const [loading, setLoading] = useState(true);
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
      const t = await loadOrg(client, org);
      setTree(t);
      rememberOrg(org);
      sessionStorage.setItem(`meshy.tree.${org}`, JSON.stringify(t, (_, v) => (typeof v === "bigint" ? { $big: v.toString() } : v)));
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setLoading(false);
    }
  }, [client, org]);

  useEffect(() => {
    // Paint the last known tree instantly, then refresh from chain.
    const cached = sessionStorage.getItem(`meshy.tree.${org}`);
    if (cached) {
      try {
        setTree(JSON.parse(cached, (_, v) => (v && typeof v === "object" && "$big" in v ? BigInt(v.$big) : v)) as OrgTree);
      } catch {}
    }
    refresh();
  }, [org, refresh]);

  const [role, setRole] = useState<Ctx["role"]>();
  useEffect(() => {
    setRole(undefined);
    if (!account) return;
    let stop = false;
    fetch(`/api/role?org=${encodeURIComponent(org)}&address=${account}`)
      .then((r) => r.json())
      .then((j: { role?: "owner" | "admin" | "member"; adminOf?: string[] }) => !stop && j.role && setRole({ role: j.role, adminOf: j.adminOf ?? [] }))
      .catch(() => {});
    return () => {
      stop = true;
    };
  }, [account, org]);
  // The owner is known from the tree right away; admins once /api/role answers.
  const isOwner = !!account && !!tree && tree.owner.toLowerCase() === account.toLowerCase();
  const canManage = isOwner || role?.role === "owner" || role?.role === "admin";

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
      value={{ client, wallet, account, chainOk, connect, org, setOrg, tree, loading, error, refresh, orgKey, setOrgKey, toast, toasts, role, canManage }}
    >
      {children}
    </C.Provider>
  );
}

"use client";
import { Activity, CreditCard, KeyRound, Network, RefreshCw, Settings, ShieldCheck, Wallet } from "lucide-react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import type { ReactNode } from "react";
import { useMeshy } from "@/lib/store";
import { Badge, Button, Short, cx } from "./ui";

const NAV = [
  { href: "", label: "Access", icon: KeyRound },
  { href: "/directory", label: "Directory", icon: Network },
  { href: "/activity", label: "Activity", icon: Activity },
];
const ORG_NAV = [
  { href: "/billing", label: "Billing", icon: CreditCard },
  { href: "/provider", label: "Provider", icon: ShieldCheck },
  { href: "/settings", label: "Settings", icon: Settings },
];

export function Shell({ children }: { children: ReactNode }) {
  const { org, tree, account, chainOk, connect, refresh, loading, toasts, orgKey } = useMeshy();
  const path = usePathname();
  const base = `/org/${org}`;
  const isOwner = !!account && !!tree && tree.owner.toLowerCase() === account.toLowerCase();
  return (
    <div className="flex min-h-screen">
      <aside className="sticky top-0 flex h-screen w-60 shrink-0 flex-col border-r border-line bg-panel">
        <Link href="/" className="flex items-center gap-2 px-5 py-5">
          <div className="grid h-8 w-8 place-items-center rounded-lg bg-accent/15 text-accent">
            <Network size={18} />
          </div>
          <div>
            <div className="text-base font-semibold">Meshy</div>
            <div className="text-[11px] text-muted">ENS-native mesh for agents</div>
          </div>
        </Link>
        <div className="mx-3 mb-3 rounded-lg border border-line bg-bg px-3 py-2">
          <div className="text-[10px] uppercase tracking-wider text-muted">Organization</div>
          <div className="mono truncate text-sm text-fg">{org}</div>
        </div>
        <nav className="flex-1 space-y-0.5 px-2">
          {[NAV, ORG_NAV].map((group, gi) => (
            <div key={gi} className={gi ? "mt-6 border-t border-line pt-4" : ""}>
              {gi === 1 && <div className="px-3 pb-1 text-[10px] uppercase tracking-wider text-muted">Org</div>}
              {group.map(({ href, label, icon: I }) => {
                const to = base + href;
                const active = href === "" ? path === base : path.startsWith(to);
                return (
                  <Link
                    key={label}
                    href={to}
                    className={cx("flex items-center gap-2.5 rounded-lg px-3 py-2 text-sm", active ? "bg-panel-2 text-fg" : "text-muted hover:bg-panel-2 hover:text-fg")}
                  >
                    <I size={16} />
                    {label}
                  </Link>
                );
              })}
            </div>
          ))}
        </nav>
        <div className="space-y-1 border-t border-line p-4 text-[11px] text-muted">
          <div>Source of truth: ENSv2 · Sepolia</div>
          <div>Audit log: Arkiv · Rabdi</div>
          <div>{orgKey ? <span className="text-accent">org key loaded</span> : "org key not loaded"}</div>
        </div>
      </aside>
      <div className="flex min-w-0 flex-1 flex-col">
        <header className="sticky top-0 z-10 flex items-center justify-between gap-3 border-b border-line bg-bg/80 px-6 py-3 backdrop-blur">
          <div className="flex items-center gap-3 text-xs text-muted">
            {tree ? (
              <>
                <span className="inline-flex items-center gap-1.5">
                  <span className="h-1.5 w-1.5 rounded-full bg-accent live-dot" /> Sepolia #{tree.block.toString()}
                </span>
                <span>
                  owner <Short v={tree.owner} n={4} />
                </span>
              </>
            ) : (
              <span>{loading ? "Loading from ENS…" : "—"}</span>
            )}
            <Button kind="ghost" onClick={refresh} disabled={loading}>
              <RefreshCw size={14} className={loading ? "animate-spin" : ""} /> Refresh
            </Button>
          </div>
          <div className="flex items-center gap-2">
            {account ? (
              <>
                {!chainOk && <Badge tone="bad">wrong network</Badge>}
                {isOwner && <Badge tone="ok">owner</Badge>}
                <span className="mono rounded-lg border border-line px-2.5 py-1.5 text-xs">
                  <Short v={account} n={4} />
                </span>
              </>
            ) : (
              <Button kind="primary" onClick={connect}>
                <Wallet size={14} /> Connect wallet
              </Button>
            )}
          </div>
        </header>
        <main className="min-w-0 flex-1 p-6">{children}</main>
      </div>
      <div className="fixed bottom-4 right-4 z-50 space-y-2">
        {toasts.map((t) => (
          <div
            key={t.id}
            className={cx(
              "max-w-md rounded-lg border px-4 py-3 text-sm shadow-xl",
              t.kind === "ok" && "border-accent/40 bg-panel text-accent",
              t.kind === "err" && "border-bad/40 bg-panel text-bad",
              t.kind === "info" && "border-line bg-panel text-fg",
            )}
          >
            {t.msg}
          </div>
        ))}
      </div>
    </div>
  );
}

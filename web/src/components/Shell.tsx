"use client";
import Link from "next/link";
import { usePathname } from "next/navigation";
import type { ReactNode } from "react";
import { useMeshy } from "@/lib/store";
import { Short, cx } from "./ui";

const NAV = [
  { href: "", label: "Access" },
  { href: "/directory", label: "Directory" },
  { href: "/activity", label: "Activity" },
  { href: "/billing", label: "Billing" },
  { href: "/provider", label: "Provider" },
  { href: "/settings", label: "Settings" },
];

export function Logo({ className = "h-6" }: { className?: string }) {
  return (
    <>
      {/* eslint-disable-next-line @next/next/no-img-element */}
      <img src="/meshy-lockup.svg" alt="Meshy" className={cx(className, "w-auto dark:hidden")} />
      {/* eslint-disable-next-line @next/next/no-img-element */}
      <img src="/meshy-lockup-white.svg" alt="Meshy" className={cx(className, "hidden w-auto dark:block")} />
    </>
  );
}

export function Shell({ children }: { children: ReactNode }) {
  const { org, tree, account, chainOk, connect, toasts } = useMeshy();
  const path = usePathname();
  const base = `/org/${org}`;
  const isOwner = !!account && !!tree && tree.owner.toLowerCase() === account.toLowerCase();
  return (
    <div className="min-h-screen">
      <header className="sticky top-0 z-40 border-b border-line bg-bg/85 backdrop-blur-md">
        <div className="mx-auto flex h-14 max-w-5xl items-center gap-6 px-6">
          <Link href={base} className="shrink-0">
            <Logo />
          </Link>
          <nav className="hidden items-center gap-0.5 md:flex" aria-label="Main">
            {NAV.map(({ href, label }) => {
              const to = base + href;
              const active = href === "" ? path === base : path.startsWith(to);
              return (
                <Link
                  key={label}
                  href={to}
                  aria-current={active ? "page" : undefined}
                  className={cx("rounded-full px-3 py-1.5 text-[14px]", active ? "bg-panel-2 font-semibold text-fg" : "text-muted hover:text-fg")}
                >
                  {label}
                </Link>
              );
            })}
          </nav>
          <div className="ml-auto flex items-center gap-3 text-[13px]">
            {tree && <span className="hidden text-muted lg:inline">{org}</span>}
            {account ? (
              <span className="inline-flex h-8 items-center gap-2 rounded-full border border-line px-3">
                {!chainOk && <span className="text-bad">wrong network</span>}
                <Short v={account} n={4} />
                {isOwner && <span className="text-muted">owner</span>}
              </span>
            ) : (
              <button onClick={connect} className="inline-flex h-8 items-center rounded-full border border-line-strong px-3 font-semibold hover:bg-panel-2">
                Connect wallet
              </button>
            )}
          </div>
        </div>
      </header>
      <main className="mx-auto max-w-5xl px-6 pb-24 pt-10">{children}</main>
      <div className="fixed bottom-4 right-4 z-[60] space-y-2">
        {toasts.map((t) => (
          <div
            key={t.id}
            role="status"
            className={cx("fade-up max-w-sm rounded-xl border bg-panel px-4 py-3 text-[14px] shadow-[0_12px_32px_-12px_rgb(11_21_51/0.3)]", t.kind === "err" ? "border-bad/40 text-bad" : "border-line")}
          >
            {t.msg}
          </div>
        ))}
      </div>
    </div>
  );
}

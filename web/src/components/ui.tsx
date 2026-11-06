"use client";
import { Check, Copy, ExternalLink } from "lucide-react";
import { type ReactNode, useEffect, useState } from "react";
import { ARKIV_EXPLORER, ENS_APP, ETHERSCAN } from "@/lib/config";

export function cx(...c: (string | false | undefined | null)[]) {
  return c.filter(Boolean).join(" ");
}

export function Card({ title, actions, children, className }: { title?: ReactNode; actions?: ReactNode; children: ReactNode; className?: string }) {
  return (
    <section className={cx("rounded-xl border border-line bg-panel", className)}>
      {(title || actions) && (
        <header className="flex items-center justify-between gap-3 border-b border-line px-4 py-3">
          <h2 className="text-sm font-semibold tracking-wide text-fg">{title}</h2>
          <div className="flex items-center gap-2">{actions}</div>
        </header>
      )}
      <div className="p-4">{children}</div>
    </section>
  );
}

export function Badge({ children, tone = "muted" }: { children: ReactNode; tone?: "ok" | "bad" | "warn" | "muted" | "info" }) {
  const t = {
    ok: "border-accent/40 text-accent bg-accent/10",
    bad: "border-bad/40 text-bad bg-bad/10",
    warn: "border-warn/40 text-warn bg-warn/10",
    info: "border-accent-2/40 text-accent-2 bg-accent-2/10",
    muted: "border-line text-muted bg-panel-2",
  }[tone];
  return <span className={cx("inline-flex items-center gap-1 rounded-md border px-1.5 py-0.5 text-[11px] font-medium", t)}>{children}</span>;
}

export function Button({
  children,
  onClick,
  kind = "default",
  disabled,
  type = "button",
  className,
}: {
  children: ReactNode;
  onClick?: () => void;
  kind?: "default" | "primary" | "danger" | "ghost";
  disabled?: boolean;
  type?: "button" | "submit";
  className?: string;
}) {
  const k = {
    default: "border-line bg-panel-2 hover:bg-line text-fg",
    primary: "border-accent/50 bg-accent/15 hover:bg-accent/25 text-accent",
    danger: "border-bad/50 bg-bad/10 hover:bg-bad/20 text-bad",
    ghost: "border-transparent hover:bg-panel-2 text-muted hover:text-fg",
  }[kind];
  return (
    <button
      type={type}
      onClick={onClick}
      disabled={disabled}
      className={cx("inline-flex shrink-0 items-center gap-1.5 whitespace-nowrap rounded-lg border px-3 py-1.5 text-sm transition disabled:cursor-not-allowed disabled:opacity-40", k, className)}
    >
      {children}
    </button>
  );
}

export function Name({ name, className }: { name: string; className?: string }) {
  const [ok, setOk] = useState(false);
  return (
    <span className={cx("mono inline-flex items-center gap-1 text-[13px]", className)}>
      <span className="break-all">{name}</span>
      <button
        title="Copy"
        className="text-muted hover:text-fg"
        onClick={(e) => {
          e.stopPropagation();
          navigator.clipboard.writeText(name);
          setOk(true);
          setTimeout(() => setOk(false), 1200);
        }}
      >
        {ok ? <Check size={12} /> : <Copy size={12} />}
      </button>
    </span>
  );
}

export function Short({ v, n = 6 }: { v: string; n?: number }) {
  if (!v) return <span className="text-muted">—</span>;
  return <span className="mono" title={v}>{v.length > 2 * n + 2 ? `${v.slice(0, n + 2)}…${v.slice(-n)}` : v}</span>;
}

export function Ext({ href, children }: { href: string; children: ReactNode }) {
  return (
    <a href={href} target="_blank" rel="noreferrer" className="inline-flex items-center gap-1 text-accent-2 hover:underline">
      {children}
      <ExternalLink size={11} />
    </a>
  );
}

export const links = {
  tx: (h: string) => `${ETHERSCAN}/tx/${h}`,
  addr: (a: string) => `${ETHERSCAN}/address/${a}`,
  ens: (n: string) => `${ENS_APP}/${n}`,
  arkivEntity: (k: string) => `${ARKIV_EXPLORER}/entity/${k}`,
  arkivAddr: (a: string) => `${ARKIV_EXPLORER}/address/${a}`,
};

export function OnChain({ name, addr }: { name?: string; addr?: string }) {
  return (
    <span className="inline-flex gap-2 text-[11px]">
      {name && <Ext href={links.ens(name)}>ENS</Ext>}
      {addr && <Ext href={links.addr(addr)}>Etherscan</Ext>}
    </span>
  );
}

export function useNow(ms = 1000) {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), ms);
    return () => clearInterval(t);
  }, [ms]);
  return now;
}

export function fmtLeft(sec: number) {
  if (sec <= 0) return "expired";
  const h = Math.floor(sec / 3600);
  const m = Math.floor((sec % 3600) / 60);
  const s = Math.floor(sec % 60);
  return h ? `${h}h ${m}m` : m ? `${m}m ${s.toString().padStart(2, "0")}s` : `${s}s`;
}

export function Countdown({ until }: { until: number }) {
  const now = useNow();
  const left = until - now / 1000;
  return <span className={cx("mono tabular-nums", left < 300 ? "text-warn" : "text-accent")}>{fmtLeft(left)}</span>;
}

export function Ring({ until, total, size = 26 }: { until: number; total: number; size?: number }) {
  const now = useNow();
  const left = Math.max(0, until - now / 1000);
  const f = total > 0 ? Math.min(1, left / total) : 0;
  const r = size / 2 - 3;
  const c = 2 * Math.PI * r;
  return (
    <svg width={size} height={size} className="-rotate-90">
      <circle cx={size / 2} cy={size / 2} r={r} stroke="var(--color-line)" strokeWidth={3} fill="none" />
      <circle cx={size / 2} cy={size / 2} r={r} stroke={f < 0.1 ? "var(--color-warn)" : "var(--color-accent)"} strokeWidth={3} fill="none" strokeDasharray={c} strokeDashoffset={c * (1 - f)} strokeLinecap="round" />
    </svg>
  );
}

export function Empty({ children }: { children: ReactNode }) {
  return <div className="rounded-lg border border-dashed border-line p-6 text-center text-sm text-muted">{children}</div>;
}

export function Field({ label, hint, children }: { label: string; hint?: ReactNode; children: ReactNode }) {
  return (
    <label className="block space-y-1.5">
      <span className="text-xs font-medium uppercase tracking-wider text-muted">{label}</span>
      {children}
      {hint && <span className="block text-xs text-muted">{hint}</span>}
    </label>
  );
}

export const inputCls = "w-full rounded-lg border border-line bg-bg px-3 py-2 text-sm outline-none focus:border-accent/60 mono";

export function ago(ms: number, now = Date.now()) {
  const s = Math.max(0, Math.round((now - ms) / 1000));
  if (s < 60) return `${s}s ago`;
  if (s < 3600) return `${Math.floor(s / 60)}m ago`;
  if (s < 86400) return `${Math.floor(s / 3600)}h ago`;
  return `${Math.floor(s / 86400)}d ago`;
}

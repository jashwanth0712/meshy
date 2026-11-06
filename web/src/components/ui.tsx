"use client";
import { Check, Copy } from "lucide-react";
import { type ReactNode, useEffect, useState } from "react";
import { ARKIV_EXPLORER, ENS_APP, ETHERSCAN } from "@/lib/config";

export function cx(...c: (string | false | undefined | null)[]) {
  return c.filter(Boolean).join(" ");
}

/** Page title, one line of context, and at most one primary action. */
export function PageHeader({ title, sub, action }: { title: ReactNode; sub?: ReactNode; action?: ReactNode }) {
  return (
    <div className="mb-8 flex flex-wrap items-end justify-between gap-4">
      <div className="min-w-0">
        <h1 className="text-[30px] font-semibold leading-tight tracking-[-0.025em]">{title}</h1>
        {sub && <p className="mt-1.5 text-[15px] text-muted">{sub}</p>}
      </div>
      {action}
    </div>
  );
}

export function Section({ title, action, children, className }: { title?: ReactNode; action?: ReactNode; children: ReactNode; className?: string }) {
  return (
    <section className={className}>
      {(title || action) && (
        <div className="mb-3 flex items-baseline justify-between gap-3">
          <h2 className="text-[15px] font-semibold">{title}</h2>
          {action}
        </div>
      )}
      {children}
    </section>
  );
}

/** A bordered list or panel. Children are usually <Row>s. */
export function Card({ title, actions, children, className }: { title?: ReactNode; actions?: ReactNode; children: ReactNode; className?: string }) {
  return (
    <Section title={title} action={actions} className={className}>
      <div className="overflow-hidden rounded-2xl border border-line bg-panel">{children}</div>
    </Section>
  );
}

export function Row({ children, className, onClick }: { children: ReactNode; className?: string; onClick?: () => void }) {
  return (
    <div onClick={onClick} className={cx("flex items-center gap-4 border-line px-5 py-3.5 text-[14px] [&+&]:border-t", onClick && "cursor-pointer hover:bg-panel-2", className)}>
      {children}
    </div>
  );
}

const CHIP = {
  ok: "bg-[var(--chip-lime)] text-[var(--chip-lime-fg)]",
  warn: "bg-[var(--chip-sun)] text-[var(--chip-sun-fg)]",
  bad: "bg-[var(--chip-pink)] text-[var(--chip-pink-fg)]",
  info: "bg-[var(--chip-cyan)] text-[var(--chip-cyan-fg)]",
  muted: "bg-[var(--chip-mist)] text-[var(--chip-mist-fg)]",
};

export function Badge({ children, tone = "muted" }: { children: ReactNode; tone?: keyof typeof CHIP }) {
  return <span className={cx("inline-flex h-[22px] items-center gap-1 whitespace-nowrap rounded-full px-2.5 text-[12px] font-semibold", CHIP[tone])}>{children}</span>;
}

export function Button({
  children,
  onClick,
  kind = "default",
  disabled,
  type = "button",
  className,
  title,
}: {
  children: ReactNode;
  onClick?: () => void;
  kind?: "default" | "primary" | "danger" | "ghost";
  disabled?: boolean;
  type?: "button" | "submit";
  className?: string;
  title?: string;
}) {
  const k = {
    default: "border-line-strong bg-panel text-fg hover:bg-panel-2",
    primary: "border-transparent bg-accent text-on-accent hover:opacity-90",
    danger: "border-bad/40 bg-transparent text-bad hover:bg-bad/10",
    ghost: "border-transparent bg-transparent text-muted hover:bg-panel-2 hover:text-fg",
  }[kind];
  return (
    <button
      type={type}
      onClick={onClick}
      disabled={disabled}
      title={title}
      className={cx("inline-flex h-9 shrink-0 items-center justify-center gap-1.5 whitespace-nowrap rounded-full border px-4 text-[14px] font-semibold transition disabled:cursor-not-allowed disabled:opacity-40", k, className)}
    >
      {children}
    </button>
  );
}

export function Name({ name, className }: { name: string; className?: string }) {
  const [ok, setOk] = useState(false);
  return (
    <span className={cx("mono inline-flex items-center gap-1.5 text-[13px]", className)}>
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

export function Short({ v, n = 4 }: { v: string; n?: number }) {
  if (!v) return <span className="text-muted">—</span>;
  return <span className="mono" title={v}>{v.length > 2 * n + 2 ? `${v.slice(0, n + 2)}…${v.slice(-n)}` : v}</span>;
}

export function Ext({ href, children }: { href: string; children: ReactNode }) {
  return (
    <a href={href} target="_blank" rel="noreferrer" className="font-medium text-accent hover:underline">
      {children}
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
    <span className="inline-flex gap-3 text-[13px]">
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
  return <span className={cx("mono tnum", left < 300 && "text-warn")}>{fmtLeft(left)}</span>;
}

/** Smallest standard grant length (15 min, 1 h, 4 h) that covers what's left: a good guess at the total. */
export function grantTotal(leftSec: number) {
  return [900, 3600, 14400].find((t) => t >= leftSec) ?? 14400;
}

export function Ring({ until, total, size = 34 }: { until: number; total?: number; size?: number }) {
  const now = useNow();
  const left = Math.max(0, until - now / 1000);
  const tot = total ?? grantTotal(left);
  const f = tot > 0 ? Math.min(1, left / tot) : 0;
  const r = size / 2 - 3;
  const c = 2 * Math.PI * r;
  return (
    <svg width={size} height={size} className="shrink-0 -rotate-90" aria-hidden="true">
      <circle cx={size / 2} cy={size / 2} r={r} stroke="var(--line)" strokeWidth={3} fill="none" />
      <circle cx={size / 2} cy={size / 2} r={r} stroke={left < 300 ? "var(--warn)" : "var(--accent)"} strokeWidth={3} fill="none" strokeDasharray={c} strokeDashoffset={c * (1 - f)} strokeLinecap="round" />
    </svg>
  );
}

export function Empty({ children }: { children: ReactNode }) {
  return <div className="px-5 py-8 text-center text-[14px] text-muted">{children}</div>;
}

export function Field({ label, hint, children }: { label: string; hint?: ReactNode; children: ReactNode }) {
  return (
    <label className="block space-y-2">
      <span className="text-[13px] font-semibold">{label}</span>
      {children}
      {hint && <span className="block text-[12px] text-muted">{hint}</span>}
    </label>
  );
}

export const inputCls = "w-full rounded-xl border border-line-strong bg-panel px-3.5 py-2.5 text-[14px] outline-none focus:border-accent mono";

/** Pill toggle group. */
export function Seg<T extends string | number>({ value, options, onChange }: { value: T; options: { v: T; label: string }[]; onChange: (v: T) => void }) {
  return (
    <div className="inline-flex gap-0.5 rounded-full bg-panel-2 p-[3px]">
      {options.map((o) => (
        <button
          key={String(o.v)}
          type="button"
          onClick={() => onChange(o.v)}
          className={cx("h-8 whitespace-nowrap rounded-full px-3.5 text-[13px] font-semibold", value === o.v ? "bg-panel text-fg shadow-[0_0_0_1px_var(--line)]" : "text-muted hover:text-fg")}
        >
          {o.label}
        </button>
      ))}
    </div>
  );
}

/** Underline tabs. */
export function Tabs<T extends string>({ value, tabs, onChange }: { value: T; tabs: { v: T; label: string; count?: number }[]; onChange: (v: T) => void }) {
  return (
    <div className="mb-4 flex gap-6 border-b border-line">
      {tabs.map((t) => (
        <button
          key={t.v}
          onClick={() => onChange(t.v)}
          className={cx("relative -mb-px flex items-center gap-2 border-b-2 py-2.5 text-[14px]", value === t.v ? "border-accent font-semibold text-fg" : "border-transparent text-muted hover:text-fg")}
        >
          {t.label}
          {t.count !== undefined && <span className={cx("grid h-5 min-w-5 place-items-center rounded-full px-1.5 text-[11px] font-bold", value === t.v ? "bg-accent text-on-accent" : "bg-panel-2 text-muted")}>{t.count}</span>}
        </button>
      ))}
    </div>
  );
}

/** Centered modal; `onClose` is ignored while `locked` (e.g. a transaction is in flight). */
export function Modal({ children, onClose, locked, wide }: { children: ReactNode; onClose: () => void; locked?: boolean; wide?: boolean }) {
  useEffect(() => {
    const k = (e: KeyboardEvent) => e.key === "Escape" && !locked && onClose();
    window.addEventListener("keydown", k);
    return () => window.removeEventListener("keydown", k);
  }, [locked, onClose]);
  return (
    <div className="fixed inset-0 z-50 grid place-items-start bg-[rgb(5_11_36/0.38)] pt-[16vh] backdrop-blur-[2px]" onClick={() => !locked && onClose()}>
      <div role="dialog" aria-modal="true" className={cx("fade-up mx-auto w-[min(92vw,420px)] rounded-2xl border border-line bg-panel p-6 shadow-[0_18px_48px_-12px_rgb(11_21_51/0.35)]", wide && "w-[min(92vw,520px)]")} onClick={(e) => e.stopPropagation()}>
        {children}
      </div>
    </div>
  );
}

/** Right-side sheet. */
export function Sheet({ title, children, footer, onClose, locked }: { title: ReactNode; children: ReactNode; footer?: ReactNode; onClose: () => void; locked?: boolean }) {
  useEffect(() => {
    const k = (e: KeyboardEvent) => e.key === "Escape" && !locked && onClose();
    window.addEventListener("keydown", k);
    return () => window.removeEventListener("keydown", k);
  }, [locked, onClose]);
  return (
    <div className="fixed inset-0 z-50 bg-[rgb(5_11_36/0.38)] backdrop-blur-[2px]" onClick={() => !locked && onClose()}>
      <aside role="dialog" aria-modal="true" className="fade-up fixed inset-y-2 right-2 flex w-[min(480px,calc(100vw-1rem))] flex-col overflow-hidden rounded-2xl border border-line bg-panel shadow-[0_18px_48px_-12px_rgb(11_21_51/0.35)]" onClick={(e) => e.stopPropagation()}>
        <header className="flex items-center justify-between px-6 pb-2 pt-6">
          <h2 className="text-[20px] font-semibold tracking-[-0.015em]">{title}</h2>
          <button onClick={() => !locked && onClose()} className="grid h-8 w-8 place-items-center rounded-full text-muted hover:bg-panel-2 hover:text-fg" aria-label="Close">
            ✕
          </button>
        </header>
        <div className="flex-1 space-y-6 overflow-y-auto px-6 py-4">{children}</div>
        {footer && <footer className="flex items-center justify-between gap-3 border-t border-line px-6 py-4">{footer}</footer>}
      </aside>
    </div>
  );
}

export function ago(ms: number, now = Date.now()) {
  const s = Math.max(0, Math.round((now - ms) / 1000));
  if (s < 60) return `${s}s ago`;
  if (s < 3600) return `${Math.floor(s / 60)}m ago`;
  if (s < 86400) return `${Math.floor(s / 3600)}h ago`;
  return `${Math.floor(s / 86400)}d ago`;
}

export const dateFmt = (sec: number) => new Intl.DateTimeFormat("en-GB", { day: "numeric", month: "short", year: "numeric" }).format(new Date(sec * 1000));

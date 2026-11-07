"use client";
import Link from "next/link";
import { cx } from "@/components/ui";

/** Feed ⇄ Access history, as links so each view has its own shareable URL. */
export function ViewSwitch({ org, current }: { org: string; current: "feed" | "access" }) {
  const items = [
    { v: "feed", label: "Feed", href: `/org/${org}/activity` },
    { v: "access", label: "Access history", href: `/org/${org}/activity/access` },
  ] as const;
  return (
    <nav aria-label="Activity views" className="inline-flex gap-0.5 rounded-full bg-panel-2 p-[3px]">
      {items.map((i) => (
        <Link
          key={i.v}
          href={i.href}
          aria-current={current === i.v ? "page" : undefined}
          className={cx(
            "inline-flex h-8 items-center whitespace-nowrap rounded-full px-3.5 text-[13px] font-semibold",
            current === i.v ? "bg-panel text-fg shadow-[0_0_0_1px_var(--line)]" : "text-muted hover:text-fg",
          )}
        >
          {i.label}
        </Link>
      ))}
    </nav>
  );
}

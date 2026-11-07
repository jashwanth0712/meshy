"use client";
import { Check, Loader2, X } from "lucide-react";
import { Ext, cx, links } from "./ui";

export type Step = { label: string; state: "todo" | "doing" | "done" | "fail"; detail?: string; tx?: string; at?: number };

export function Steps({ steps }: { steps: Step[] }) {
  return (
    <ol className="space-y-3">
      {steps.map((s, i) => (
        <li key={i} className="flex items-start gap-3 text-[14px]">
          <span
            className={cx(
              "mt-px grid h-5 w-5 shrink-0 place-items-center rounded-full border",
              s.state === "done" && "border-fg bg-fg text-bg",
              s.state === "doing" && "border-accent text-accent",
              s.state === "fail" && "border-bad bg-bad text-white",
              s.state === "todo" && "border-line-strong",
            )}
          >
            {s.state === "done" ? <Check size={12} /> : s.state === "doing" ? <Loader2 size={12} className="animate-spin" /> : s.state === "fail" ? <X size={12} /> : null}
          </span>
          <div className="min-w-0">
            <div className={cx(s.state === "todo" && "text-muted")}>{s.label}</div>
            {(s.detail || s.tx) && (
              <div className="mt-0.5 break-all text-[12px] text-muted">
                {s.detail}
                {s.tx && (
                  <>
                    {s.detail ? " · " : ""}
                    <Ext href={links.tx(s.tx)}>{s.tx.slice(0, 10)}…</Ext>
                  </>
                )}
              </div>
            )}
          </div>
        </li>
      ))}
    </ol>
  );
}

export function useSteps(set: (f: (s: Step[]) => Step[]) => void) {
  return (i: number, patch: Partial<Step>) => set((s) => s.map((x, j) => (j === i ? { ...x, ...patch } : x)));
}

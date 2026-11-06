"use client";
import { Check, Circle, Loader2, X } from "lucide-react";
import { Ext, cx, links } from "./ui";

export type Step = { label: string; state: "todo" | "doing" | "done" | "fail"; detail?: string; tx?: string; at?: number };

export function Steps({ steps }: { steps: Step[] }) {
  return (
    <ol className="space-y-2">
      {steps.map((s, i) => (
        <li key={i} className="flex items-start gap-3">
          <span
            className={cx(
              "mt-0.5 grid h-5 w-5 shrink-0 place-items-center rounded-full border",
              s.state === "done" && "border-accent/60 bg-accent/15 text-accent",
              s.state === "doing" && "border-accent-2/60 text-accent-2",
              s.state === "fail" && "border-bad/60 bg-bad/10 text-bad",
              s.state === "todo" && "border-line text-muted",
            )}
          >
            {s.state === "done" ? <Check size={12} /> : s.state === "doing" ? <Loader2 size={12} className="animate-spin" /> : s.state === "fail" ? <X size={12} /> : <Circle size={6} />}
          </span>
          <div className="min-w-0 text-sm">
            <div className={cx(s.state === "todo" ? "text-muted" : "text-fg")}>{s.label}</div>
            {s.detail && <div className="mono break-all text-xs text-muted">{s.detail}</div>}
            {s.tx && (
              <div className="text-xs">
                <Ext href={links.tx(s.tx)}>{s.tx.slice(0, 10)}…</Ext>
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

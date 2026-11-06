"use client";
import { useState } from "react";

export function CopyLine({ text }: { text: string }) {
  const [done, setDone] = useState(false);
  return (
    <button
      type="button"
      onClick={() => {
        navigator.clipboard?.writeText(text);
        setDone(true);
        setTimeout(() => setDone(false), 1200);
      }}
      className="mono mt-3 flex w-full items-center justify-between gap-3 rounded-xl border border-line-strong bg-bg px-3.5 py-2.5 text-left text-[13px] hover:border-accent"
      aria-label={`Copy: ${text}`}
    >
      <span className="truncate">{text}</span>
      <span className="shrink-0 text-[12px] text-muted">{done ? "Copied" : "Copy"}</span>
    </button>
  );
}

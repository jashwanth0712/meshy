"use client";
import { type MouseEvent, type ReactNode, useCallback, useState } from "react";

type T = { x: number; y: number; c: ReactNode };

/** Hover/focus tooltip for chart marks: `bind(content)` spreads onto a mark, `el` renders once per chart. */
export function useTip() {
  const [t, setT] = useState<T>();
  const bind = useCallback(
    (c: ReactNode) => ({
      tabIndex: 0,
      onMouseEnter: (e: MouseEvent) => setT({ x: e.clientX, y: e.clientY, c }),
      onMouseMove: (e: MouseEvent) => setT({ x: e.clientX, y: e.clientY, c }),
      onMouseLeave: () => setT(undefined),
      onFocus: (e: { currentTarget: Element }) => {
        const r = e.currentTarget.getBoundingClientRect();
        setT({ x: r.left + r.width / 2, y: r.bottom, c });
      },
      onBlur: () => setT(undefined),
    }),
    [],
  );
  const el = t ? (
    <div
      role="tooltip"
      className="pointer-events-none fixed z-[70] max-w-[22rem] rounded-xl border border-line bg-panel px-3 py-2 text-[12px] leading-relaxed text-fg shadow-[0_18px_48px_-12px_rgb(11_21_51/0.28)]"
      style={{
        left: Math.min(t.x + 14, (typeof window !== "undefined" ? window.innerWidth : 1600) - 360),
        top: t.y + 14,
      }}
    >
      {t.c}
    </div>
  ) : null;
  return { bind, el };
}

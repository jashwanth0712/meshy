"use client";
import { links } from "@/components/ui";
import { type Kill, median } from "@/lib/insights";
import { useTip } from "./Tip";

const fmtMs = (ms: number) => (ms >= 1000 ? `${(ms / 1000).toFixed(ms >= 10_000 ? 0 : 1)} s` : `${Math.round(ms)} ms`);

/**
 * Revoke latency strip: every session.revoke on Arkiv, placed by `latencyMs` (meshyd saw the
 * chain head → session dead) on a log axis. With a handful of points a histogram would lie, so
 * each kill is one dot; fail-closed kills (stale chain view, not a revoke) are flagged.
 * docs/insights/PLAN.md V6.
 */
export function KillLatency({ kills }: { kills: Kill[] }) {
  const { bind, el } = useTip();
  const pts = kills.filter((k) => typeof k.latencyMs === "number" && k.latencyMs > 0).sort((a, b) => a.latencyMs! - b.latencyMs!);
  if (pts.length === 0) return null;
  const real = pts.filter((k) => !k.failClosed).map((k) => k.latencyMs!);
  const lo = Math.pow(10, Math.min(2, Math.floor(Math.log10(pts[0].latencyMs!))));
  const hi = Math.pow(10, Math.max(4, Math.ceil(Math.log10(pts[pts.length - 1].latencyMs! * 1.05))));
  const x = (v: number) => (100 * (Math.log10(v) - Math.log10(lo))) / (Math.log10(hi) - Math.log10(lo));
  const ticks: number[] = [];
  for (let d = lo; d <= hi; d *= 10) {
    ticks.push(d);
    if (d * 3 < hi) ticks.push(d * 3);
  }
  // Direct labels only where they don't collide; every dot keeps its tooltip.
  let lastX = -100;
  const labeled = new Set<string>();
  for (const k of pts) {
    const px = x(k.latencyMs!);
    if (px - lastX > 9) {
      labeled.add(k.key);
      lastX = px;
    }
  }
  const p50 = median(real);
  const fc = pts.length - real.length;

  return (
    <section>
      <div className="mb-3 flex flex-wrap items-baseline justify-between gap-3">
        <h2 className="text-[15px] font-semibold">How fast was each kill?</h2>
        <span className="text-[12px] text-muted">
          meshyd saw the block → session dead (<code className="mono">latencyMs</code>, public on Arkiv) · log scale
        </span>
      </div>
      <div className="rounded-2xl border border-line bg-panel p-5">
        <div className="relative mx-2 h-[4.5rem]">
          <div className="absolute inset-x-0 top-9 h-px bg-line-strong" />
          {ticks.map((t) => (
            <span
              key={t}
              className="mono absolute top-12 whitespace-nowrap text-[10px] text-muted"
              style={{ left: `${x(t)}%`, transform: x(t) >= 99 ? "translateX(-100%)" : x(t) <= 1 ? "none" : "translateX(-50%)" }}
            >
              {fmtMs(t)}
            </span>
          ))}
          {pts.map((k) => (
            <a
              key={k.key}
              href={links.arkivEntity(k.key)}
              target="_blank"
              rel="noreferrer"
              aria-label={`${fmtMs(k.latencyMs!)} kill at block ${k.block}`}
              className="absolute top-9 h-3 w-3 -translate-x-1/2 -translate-y-1/2 rounded-full shadow-[0_0_0_2px_var(--panel)]"
              style={{ left: `${x(k.latencyMs!)}%`, background: k.failClosed ? "var(--bad)" : "var(--accent)" }}
              {...bind(
                <>
                  <b>{fmtMs(k.latencyMs!)}</b> {k.failClosed ? "· fail closed" : "· revoke"}
                  <div>
                    {new Date(k.ts).toLocaleString([], { dateStyle: "medium", timeStyle: "medium" })} · block {k.block?.toLocaleString()}
                  </div>
                  <div className="mono text-[11px]">{k.grant}</div>
                  <div className="text-muted">{k.reason}</div>
                </>,
              )}
            />
          ))}
          {pts
            .filter((k) => labeled.has(k.key))
            .map((k) => (
              <span
                key={`l-${k.key}`}
                className="mono absolute top-1 -translate-x-1/2 whitespace-nowrap text-[11px]"
                style={{ left: `${x(k.latencyMs!)}%`, color: k.failClosed ? "var(--bad)" : undefined }}
              >
                {fmtMs(k.latencyMs!)}
                {k.failClosed && " fail closed"}
              </span>
            ))}
        </div>
        <div className="mt-4 grid gap-4 text-[13px] sm:grid-cols-3">
          <div>
            <div className="mono text-[11px] uppercase tracking-[0.06em] text-muted">Median of {real.length} revoke{real.length === 1 ? "" : "s"}</div>
            <div className="mono text-[18px] font-semibold tabular-nums">{p50 !== undefined ? fmtMs(p50) : "—"}</div>
          </div>
          <div>
            <div className="mono text-[11px] uppercase tracking-[0.06em] text-muted">Slowest revoke</div>
            <div className="mono text-[18px] font-semibold tabular-nums">{real.length ? fmtMs(Math.max(...real)) : "—"}</div>
          </div>
          <div>
            <div className="mono text-[11px] uppercase tracking-[0.06em] text-muted">Fail-closed kills</div>
            <div className="mono text-[18px] font-semibold tabular-nums">{fc}</div>
            {fc > 0 && <div className="text-[12px] text-muted">meshyd couldn&apos;t verify the chain and ended the session to be safe</div>}
          </div>
        </div>
        <p className="mt-4 text-[12px] text-muted">Sepolia inclusion time (tx sent → mined) isn&apos;t recorded yet and is usually several seconds more.</p>
      </div>
      {el}
    </section>
  );
}

"use client";
import type { Address } from "viem";
import { cx, links } from "@/components/ui";
import { type Axis, type GrantInterval, buildAxis, fmtDur, grantEnd, usedMs } from "@/lib/insights";
import { useTip } from "./Tip";

const hm = (t: number) => new Date(t).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" });
const short = (a?: string) => (a ? `${a.slice(0, 6)}…${a.slice(-4)}` : "");

export function who(a: Address | undefined, owner?: Address, account?: Address) {
  if (!a) return "";
  if (account && a.toLowerCase() === account.toLowerCase()) return "you";
  if (owner && a.toLowerCase() === owner.toLowerCase()) return "org owner";
  return short(a);
}

/** Shared x-axis for grant windows and the sessions inside them. */
export function grantAxis(grants: GrantInterval[], collapse: boolean, now: number): Axis {
  const spans: [number, number][] = [];
  for (const g of grants) {
    spans.push([g.from, grantEnd(g, now)]);
    for (const s of g.sessions) spans.push([s.start, s.end ?? now]);
  }
  return buildAxis(spans, collapse);
}

function Gaps({ axis }: { axis: Axis }) {
  return (
    <>
      {axis.gaps.map((g, i) => (
        <div
          key={i}
          className="absolute inset-y-0 border-x border-dashed border-line-strong"
          style={{ left: `${g.x0}%`, width: `${g.w}%`, background: "repeating-linear-gradient(135deg, transparent 0 5px, var(--line) 5px 6px)" }}
        />
      ))}
    </>
  );
}

const ROW = "grid grid-cols-[minmax(11rem,15rem)_1fr_8.5rem] items-center";

/**
 * Access Gantt: one row per grant instance (ENS), the window it was valid for, and the sessions that
 * actually ran inside it (Arkiv). Answers "which grants were used, by how much, who issued and who
 * ended them". docs/insights/PLAN.md V2.
 */
export function AccessGantt({
  grants,
  audited,
  owner,
  account,
  collapse,
  now,
}: {
  grants: GrantInterval[];
  audited: Set<string>;
  owner?: Address;
  account?: Address;
  collapse: boolean;
  now: number;
}) {
  const { bind, el } = useTip();
  const axis = grantAxis(grants, collapse, now);
  const nth = new Map<string, number>();
  const count = new Map<string, number>();
  for (const g of grants) count.set(g.grant, (count.get(g.grant) ?? 0) + 1);

  return (
    <div className="overflow-x-auto rounded-2xl border border-line bg-panel">
      <div className="min-w-[760px]">
        {grants.map((g) => {
          const n = (nth.get(g.grant) ?? 0) + 1;
          nth.set(g.grant, n);
          const end = grantEnd(g, now);
          const unknown = !audited.has(g.resource);
          const used = usedMs(g, now);
          const total = Math.max(1, end - g.from);
          const pct = (100 * used) / total;
          const x0 = axis.x(g.from);
          const x1 = axis.x(end);
          const shortTarget = g.target.split(".").slice(0, -2).join(".");
          return (
            <div key={g.key} className={cx(ROW, "min-h-[3.5rem] border-t border-line first:border-t-0")}>
              <div className="min-w-0 px-4 py-2 text-[13px] leading-snug">
                <div className="mono truncate">
                  {g.agent}
                  <span className="text-muted">.{shortTarget}</span>
                  {count.get(g.grant)! > 1 && <span className="ml-1.5 rounded-full bg-panel-2 px-1.5 py-0.5 text-[11px] font-semibold text-muted">#{n}</span>}
                </div>
                <div className="truncate text-[12px] text-muted">
                  {g.kind === "service" ? "port" : "folders"} · by {who(g.granter, owner, account)}
                  {g.endCause === "revoked" && g.revoker && g.revoker.toLowerCase() !== g.granter.toLowerCase() && <> · ended by {who(g.revoker, owner, account)}</>}
                </div>
              </div>
              <div className="relative h-14">
                <Gaps axis={axis} />
                <a
                  href={links.tx(g.txs.registered)}
                  target="_blank"
                  rel="noreferrer"
                  aria-label={`${g.grant} window`}
                  className={cx("absolute top-1/2 h-[18px] -translate-y-1/2 rounded-[5px]", g.endCause === "live" && "ring-1 ring-accent")}
                  style={{
                    left: `${x0}%`,
                    width: `max(${x1 - x0}%, 4px)`,
                    background:
                      unknown
                        ? "repeating-linear-gradient(135deg, color-mix(in srgb, var(--muted) 18%, transparent) 0 5px, color-mix(in srgb, var(--muted) 45%, transparent) 5px 7px)"
                        : g.endCause === "expired"
                          ? "repeating-linear-gradient(135deg, color-mix(in srgb, var(--accent) 22%, transparent) 0 6px, color-mix(in srgb, var(--accent) 8%, transparent) 6px 10px)"
                          : "color-mix(in srgb, var(--accent) 22%, transparent)",
                  }}
                  {...bind(
                    <>
                      <b className="mono">{g.grant}</b>
                      <div>
                        {hm(g.from)} → {g.ended ? hm(g.ended) : `live until ${hm(g.until)}`} · {fmtDur(end - g.from)}
                      </div>
                      <div>
                        granted by <span className="mono">{who(g.granter, owner, account)}</span> in block {g.blocks.registered.toLocaleString()}
                      </div>
                      {g.endCause === "revoked" && (
                        <div>
                          revoked by <span className="mono">{who(g.revoker, owner, account)}</span> in block {g.blocks.unregistered?.toLocaleString()}
                        </div>
                      )}
                      {g.endCause === "expired" && <div>expired (never revoked)</div>}
                      {g.endCause === "reissued" && <div>replaced by a new grant with the same name</div>}
                      {g.blocks.renewed.length > 0 && <div>renewed {g.blocks.renewed.length}×</div>}
                      <div className="text-muted">tokenId …{g.tokenId.slice(-6)}</div>
                      {unknown && <div className="text-muted">This device has written no audit to Arkiv, so use is unknown.</div>}
                    </>,
                  )}
                />
                {g.sessions.map((s) => {
                  const a = axis.x(Math.max(s.start, g.from));
                  const b = axis.x(Math.min(s.end ?? now, end));
                  return (
                    <a
                      key={s.session}
                      href={links.arkivEntity(s.keys[0])}
                      target="_blank"
                      rel="noreferrer"
                      aria-label={`session ${s.session}`}
                      className={cx("absolute top-1/2 h-[10px] -translate-y-1/2 rounded-[3px] bg-accent shadow-[0_0_0_2px_var(--panel)]", s.live && "animate-pulse")}
                      style={{ left: `${a}%`, width: `max(${b - a}%, 4px)` }}
                      {...bind(
                        <>
                          <b>{g.kind === "service" ? "Port forward" : "Session"}</b> {hm(s.start)} → {s.end ? hm(s.end) : "live"} · {fmtDur((s.end ?? now) - s.start)}
                          {s.argv && <div className="mono text-[11px]">{s.argv.join(" ")}</div>}
                          {s.exit && <div>{s.exit}</div>}
                          {s.kill && (
                            <div>
                              killed {s.kill.latencyMs?.toLocaleString()} ms after block {s.kill.block?.toLocaleString()}
                              {s.kill.failClosed && " (fail closed: the chain view went stale)"}
                            </div>
                          )}
                        </>,
                      )}
                    />
                  );
                })}
                {g.sessions
                  .filter((s) => s.kill?.failClosed)
                  .map((s) => (
                    <span
                      key={`fc-${s.session}`}
                      className="absolute top-1/2 grid h-4 w-4 -translate-x-1/2 -translate-y-1/2 place-items-center rounded-full bg-bad text-[10px] font-extrabold text-white shadow-[0_0_0_2px_var(--panel)]"
                      style={{ left: `${axis.x(s.kill!.ts)}%` }}
                      {...bind(
                        <>
                          <b>Fail-closed kill</b> {hm(s.kill!.ts)} · {s.kill!.latencyMs?.toLocaleString()} ms
                          <div>{s.kill!.reason}</div>
                          <div className="text-muted">The grant was still valid on ENS.</div>
                        </>,
                      )}
                    >
                      !
                    </span>
                  ))}
                {g.endCause === "revoked" && (
                  <span
                    className="absolute top-1/2 h-4 w-4 -translate-x-1/2 -translate-y-1/2 rounded-full bg-[var(--chip-sun)] shadow-[0_0_0_2px_var(--panel)]"
                    style={{ left: `${x1}%` }}
                    {...bind(
                      <>
                        <b>Revoked</b> in block {g.blocks.unregistered?.toLocaleString()} by <span className="mono">{who(g.revoker, owner, account)}</span>
                        {g.sessions.find((s) => s.kill && !s.kill.failClosed) && (
                          <div>live session killed {g.sessions.find((s) => s.kill && !s.kill.failClosed)!.kill!.latencyMs?.toLocaleString()} ms after meshyd saw the block</div>
                        )}
                      </>,
                    )}
                  />
                )}
              </div>
              <div className="whitespace-nowrap px-4 text-right text-[13px] tabular-nums">
                {unknown ? (
                  <span className="text-muted">no audit</span>
                ) : g.sessions.length === 0 ? (
                  <span className="rounded-full bg-[var(--chip-sun)] px-2 py-0.5 text-[12px] font-semibold text-[var(--chip-sun-fg)]">never used</span>
                ) : (
                  <>
                    <span className={cx(pct < 5 && "rounded-full bg-[var(--chip-sun)] px-2 py-0.5 font-semibold text-[var(--chip-sun-fg)]")}>{pct < 0.1 ? "<0.1" : pct < 1 ? pct.toFixed(1) : Math.round(pct)}%</span>{" "}
                    <span className="text-muted">of {fmtDur(total)}</span>
                  </>
                )}
              </div>
            </div>
          );
        })}
        <div className={cx(ROW, "border-t border-line")}>
          <div />
          <div className="relative h-11">
            {axis.ticks.map((t, i) => (
              <span key={i} className="mono absolute top-1.5 -translate-x-1/2 whitespace-nowrap text-[11px] text-muted" style={{ left: `${t.x}%` }}>
                {t.label}
              </span>
            ))}
            {axis.gapLabels.map((g, i) => (
              <span key={`g${i}`} className="absolute top-6 -translate-x-1/2 whitespace-nowrap text-[10px] italic text-muted" style={{ left: `${g.x}%` }}>
                {g.label}
              </span>
            ))}
          </div>
          <div />
        </div>
      </div>
      {el}
    </div>
  );
}

export function GanttLegend() {
  const sw = "inline-block h-2.5 w-4 rounded-[3px]";
  return (
    <div className="flex flex-wrap items-center gap-4 text-[12px] text-muted">
      <span className="inline-flex items-center gap-1.5"><i className={sw} style={{ background: "color-mix(in srgb, var(--accent) 22%, transparent)" }} />grant window (ENS)</span>
      <span className="inline-flex items-center gap-1.5"><i className={cx(sw, "bg-accent")} />session (Arkiv)</span>
      <span className="inline-flex items-center gap-1.5"><i className={sw} style={{ background: "repeating-linear-gradient(135deg, color-mix(in srgb, var(--accent) 30%, transparent) 0 3px, transparent 3px 6px)" }} />expired</span>
      <span className="inline-flex items-center gap-1.5"><i className="inline-block h-3 w-3 rounded-full bg-[var(--chip-sun)]" />revoked</span>
      <span className="inline-flex items-center gap-1.5"><i className="grid h-3 w-3 place-items-center rounded-full bg-bad text-[8px] font-bold text-white">!</i>fail-closed kill</span>
    </div>
  );
}

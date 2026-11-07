"use client";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { AccessGantt, GanttLegend } from "@/components/insights/AccessGantt";
import { KillLatency } from "@/components/insights/KillLatency";
import { useInsights } from "@/components/insights/useInsights";
import { ViewSwitch } from "@/components/insights/ViewSwitch";
import { PageHeader, Seg, useNow } from "@/components/ui";
import { fmtDur, grantEnd, usedMs } from "@/lib/insights";
import { useMeshy } from "@/lib/store";

const DAY = 86_400_000;

export default function AccessHistoryPage() {
  const { org, tree, account } = useMeshy();
  const { grants, kills, audited, orphans, err } = useInsights();
  const now = useNow(30_000);
  const sp = useSearchParams();
  const router = useRouter();
  const pathname = usePathname();

  // Facets live in the URL, so a view can be shared as a link.
  const agent = sp.get("agent") ?? "all";
  const network = sp.get("network") ?? "all";
  const range = sp.get("range") ?? "all";
  const collapse = sp.get("idle") !== "linear";
  const set = (k: string, v: string, dflt: string) => {
    const p = new URLSearchParams(sp.toString());
    if (v === dflt) p.delete(k);
    else p.set(k, v);
    const q = p.toString();
    router.replace(q ? `${pathname}?${q}` : pathname, { scroll: false });
  };

  const since = range === "24h" ? now - DAY : range === "7d" ? now - 7 * DAY : 0;
  const shown = (grants ?? [])
    .filter((g) => (agent === "all" || g.agent === agent) && (network === "all" || g.network === network) && grantEnd(g, now) >= since)
    .sort((a, b) => a.from - b.from);
  const netOf = (resource: string) => resource.split(".")[1];
  const shownKills = (kills ?? []).filter((k) => (agent === "all" || k.agent === agent) && (network === "all" || netOf(k.resource) === network) && k.ts >= since);

  const aud = audited ?? new Set<string>();
  const handed = shown.reduce((s, g) => s + (grantEnd(g, now) - g.from), 0);
  const known = shown.filter((g) => aud.has(g.resource));
  const knownTotal = known.reduce((s, g) => s + (grantEnd(g, now) - g.from), 0);
  const knownUsed = known.reduce((s, g) => s + usedMs(g, now), 0);
  const never = known.filter((g) => g.sessions.length === 0 && g.endCause !== "live").length;
  const unknownDevices = [...new Set(shown.filter((g) => !aud.has(g.resource)).map((g) => g.resource.split(".")[0]))];

  const agents = tree?.agents.map((a) => a.label) ?? [];
  const networks = tree?.networks.map((n) => n.label) ?? [];

  return (
    <>
      <PageHeader
        title="Access history"
        sub={
          grants
            ? `${shown.length} grant${shown.length === 1 ? "" : "s"} · windows from ENS, use from Arkiv, joined on the grant name`
            : "Reading ENS and Arkiv…"
        }
        action={<ViewSwitch org={org} current="access" />}
      />

      <div className="mb-6 flex flex-wrap items-center gap-3">
        {agents.length > 1 && <Seg value={agent} onChange={(v) => set("agent", v, "all")} options={[{ v: "all", label: "All agents" }, ...agents.map((a) => ({ v: a, label: a }))]} />}
        {networks.length > 1 && <Seg value={network} onChange={(v) => set("network", v, "all")} options={[{ v: "all", label: "All networks" }, ...networks.map((n) => ({ v: n, label: n }))]} />}
        <Seg value={range} onChange={(v) => set("range", v, "all")} options={[{ v: "24h", label: "24 h" }, { v: "7d", label: "7 days" }, { v: "all", label: "All" }]} />
        <div className="ml-auto">
          <Seg value={collapse ? "collapse" : "linear"} onChange={(v) => set("idle", v, "collapse")} options={[{ v: "collapse", label: "Collapse idle" }, { v: "linear", label: "Linear" }]} />
        </div>
      </div>

      {err && <p className="mb-4 text-[14px] text-bad">{err}</p>}

      <div className="mb-8 grid gap-3 sm:grid-cols-4">
        {[
          { v: grants ? String(shown.length) : undefined, l: "grants issued" },
          { v: grants ? fmtDur(handed) : undefined, l: "of access handed out" },
          {
            v: grants ? (knownTotal ? `${((100 * knownUsed) / knownTotal).toFixed(knownUsed / knownTotal < 0.1 ? 1 : 0)}%` : "—") : undefined,
            l: `of granted time used${unknownDevices.length ? ` (${unknownDevices.join(", ")} excluded)` : ""}`,
          },
          { v: grants ? String(never) : undefined, l: "ended without ever being used" },
        ].map((t, i) => (
          <div key={i} className="rounded-2xl border border-line bg-panel px-5 py-4">
            {t.v === undefined ? <div className="h-7 w-16 animate-pulse rounded-md bg-panel-2" /> : <div className="text-[24px] font-semibold leading-tight tracking-[-0.02em] tabular-nums">{t.v}</div>}
            <div className="text-[13px] text-muted">{t.l}</div>
          </div>
        ))}
      </div>

      <section className="mb-10">
        <div className="mb-3 flex flex-wrap items-baseline justify-between gap-3">
          <h2 className="text-[15px] font-semibold">Granted vs. used</h2>
          <GanttLegend />
        </div>
        {!grants ? (
          <div className="overflow-hidden rounded-2xl border border-line bg-panel" aria-busy="true" aria-label="Reading grant history from ENS">
            {[0, 1, 2, 3].map((i) => (
              <div key={i} className="grid grid-cols-[minmax(11rem,15rem)_1fr_8.5rem] items-center gap-4 border-t border-line px-4 py-4 first:border-t-0">
                <div className="space-y-1.5">
                  <div className="h-3.5 w-36 animate-pulse rounded bg-panel-2" />
                  <div className="h-3 w-24 animate-pulse rounded bg-panel-2" />
                </div>
                <div className="h-[18px] animate-pulse rounded-[5px] bg-panel-2" style={{ marginLeft: `${8 + i * 18}%`, width: `${30 - i * 4}%` }} />
                <div className="ml-auto h-3.5 w-16 animate-pulse rounded bg-panel-2" />
              </div>
            ))}
          </div>
        ) : shown.length === 0 ? (
          <div className="rounded-2xl border border-line bg-panel px-5 py-10 text-center text-[14px] text-muted">No grants in this range.</div>
        ) : (
          <AccessGantt grants={shown} audited={aud} owner={tree?.owner} account={account} collapse={collapse} now={now} />
        )}
        <p className="mt-3 text-[13px] text-muted">
          Hover a bar for who granted and revoked it, and in which block. Hatched gaps are quiet stretches, collapsed to save room.
          {unknownDevices.length > 0 && <> {unknownDevices.join(", ")} writes no audit to Arkiv, so its use is unknown.</>}
          {orphans && orphans.length > 0 && <> {orphans.length} session{orphans.length === 1 ? "" : "s"} on Arkiv matched no grant window.</>}
        </p>
      </section>

      {shownKills.length > 0 && <KillLatency kills={shownKills} />}
    </>
  );
}

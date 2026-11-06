"use client";
import { useCallback, useEffect, useMemo, useState } from "react";
import { type Row, decryptRow, orgHistory } from "@/lib/arkiv";
import {
  type GrantInterval,
  type InsightEvent,
  type Kill,
  type SessionSpan,
  grantHistory,
  insightEvents,
  joinSessions,
  kills,
  sessionSpans,
} from "@/lib/insights";
import { useMeshy } from "@/lib/store";

export type Insights = {
  grants?: GrantInterval[];
  rows?: Row[];
  spans?: SessionSpan[];
  orphans?: SessionSpan[]; // sessions with no matching ENS grant window
  kills?: Kill[];
  events?: InsightEvent[];
  audited?: Set<string>; // resources with at least one Arkiv row from their daemon
  err?: string;
  at?: number;
};

type Cached = { grants: GrantInterval[]; raw: Row[] };
const cacheKey = (org: string) => `meshy.insights.${org}`;

/** Join the raw sources. Decryption happens here, in the browser, and plaintext is never stored. */
function derive(grants: GrantInterval[], raw: Row[], orgKey?: string): Insights {
  const rows = raw.map((r) => decryptRow(r, orgKey));
  const spans = sessionSpans(rows);
  const orphans = joinSessions(grants, spans);
  return { grants, rows, spans, orphans, kills: kills(rows), events: insightEvents(grants, rows), audited: new Set(rows.map((r) => r.resource)), at: Date.now() };
}

function readCache(org: string): Cached | undefined {
  try {
    const s = sessionStorage.getItem(cacheKey(org));
    return s ? (JSON.parse(s) as Cached) : undefined;
  } catch {
    return undefined;
  }
}

/** ENS grant history + Arkiv history for the current org, joined; refreshed every `refreshMs`.
 *  Like the org tree, the last result paints instantly from sessionStorage (ciphertext only), then refreshes. */
export function useInsights(refreshMs = 30_000) {
  const { client, org, tree, orgKey } = useMeshy();
  // Only trust entities written by the device daemons' ENS addresses (spoof-proof reads).
  const writers = useMemo(() => tree?.networks.flatMap((n) => n.resources).map((r) => r.addr as string) ?? [], [tree]);
  const [s, setS] = useState<Insights>({});

  // Paint the last result after hydration (never during render, so server and client HTML match).
  useEffect(() => {
    const c = readCache(org);
    if (c) Promise.resolve().then(() => setS((p) => (p.at ? p : derive(c.grants, c.raw, orgKey))));
  }, [org, orgKey]);

  const load = useCallback(async () => {
    if (!tree) return;
    try {
      const [grants, raw] = await Promise.all([grantHistory(client, tree), orgHistory(org, writers)]);
      // grant sessions are rebuilt by the join, so cache them empty
      const cached: Cached = { grants: grants.map((g) => ({ ...g, sessions: [] })), raw };
      try {
        sessionStorage.setItem(cacheKey(org), JSON.stringify(cached));
      } catch {}
      setS(derive(grants, raw, orgKey));
    } catch (e) {
      setS((p) => ({ ...p, err: e instanceof Error ? e.message : String(e) }));
    }
  }, [client, org, tree, orgKey, writers]);

  useEffect(() => {
    load();
    const t = setInterval(load, refreshMs);
    return () => clearInterval(t);
  }, [load, refreshMs]);

  return { ...s, reload: load };
}

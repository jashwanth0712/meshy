// Server-side JSON-RPC proxy for Sepolia: keeps the provider key off the client.
// eth_getLogs is routed to LOGS_RPC (default: public node) because Alchemy's
// free tier caps log queries at a 10-block range.
type Call = { jsonrpc: string; id: number | string; method: string; params?: unknown[] };

const LOGS = new Set(["eth_getLogs"]);

async function forward(url: string, payload: Call | Call[]) {
  const res = await fetch(url, { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify(payload), cache: "no-store" });
  return res.json();
}

export async function POST(req: Request) {
  const main = process.env.SEPOLIA_RPC;
  const logs = process.env.LOGS_RPC ?? "https://ethereum-sepolia-rpc.publicnode.com";
  if (!main) return Response.json({ error: { code: -32000, message: "SEPOLIA_RPC not configured" } }, { status: 500 });
  const body = (await req.json()) as Call | Call[];
  if (!Array.isArray(body)) return Response.json(await forward(LOGS.has(body.method) ? logs : main, body));
  const a = body.filter((c) => !LOGS.has(c.method));
  const b = body.filter((c) => LOGS.has(c.method));
  const [ra, rb] = await Promise.all([a.length ? forward(main, a) : [], b.length ? forward(logs, b) : []]);
  const byId = new Map<string | number, unknown>();
  for (const r of [...(Array.isArray(ra) ? ra : [ra]), ...(Array.isArray(rb) ? rb : [rb])]) byId.set((r as { id: string | number }).id, r);
  return Response.json(body.map((c) => byId.get(c.id) ?? { jsonrpc: "2.0", id: c.id, error: { code: -32603, message: "no response" } }));
}

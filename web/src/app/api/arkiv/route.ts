// Server-side JSON-RPC proxy for Arkiv Rabdi (adds the access key; read-only methods only).
const ALLOWED = new Set(["arkiv_query", "arkiv_getEntity", "arkiv_getBlockTiming", "eth_blockNumber", "eth_chainId"]);

export async function POST(req: Request) {
  const base = process.env.ARKIV_RPC ?? "https://rpc.rabdi.db-chain.testnet.arkiv.network";
  const body = await req.json();
  const calls = Array.isArray(body) ? body : [body];
  if (!calls.every((c) => ALLOWED.has(c?.method))) {
    return Response.json({ jsonrpc: "2.0", id: body?.id ?? null, error: { code: -32601, message: "method not allowed" } }, { status: 400 });
  }
  const res = await fetch(base, { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify(body), cache: "no-store" });
  return new Response(await res.text(), { status: res.status, headers: { "content-type": "application/json" } });
}

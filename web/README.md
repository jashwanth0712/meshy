# web/: Meshy dashboard (Next.js 16)

This is an access-first dashboard for a Meshy org. Everything is read **live from ENSv2 on Sepolia** and from **Arkiv**, so it's viewable without a wallet. Writes go through the browser wallet, and the chain enforces roles (the owner or a delegated admin).

> **Next.js 16 has breaking changes.** Read `AGENTS.md` and `node_modules/next/dist/docs/` before changing routing or data fetching. See also [docs/LEARNINGS.md](../docs/LEARNINGS.md) #35–39.

## Run
```bash
cp .env.example .env.local   # fill SEPOLIA_RPC (Alchemy), ARKIV_RPC (+ access key), NEXT_PUBLIC_MESHY_SYNC
pnpm install && pnpm dev     # http://localhost:3000/org/meshydemo.eth
npx next typegen && npx tsc --noEmit
```

## Screens (`src/app/org/[org]/…`)
| Route | What |
|---|---|
| `/` **Access** | Every grant as a line (agent → target, scope, countdown, Revoke), plus **Give access** (`GrantDrawer`) and "Happening now" from Arkiv (sentences) |
| `/directory` | Networks → devices (kind/meta) → services, plus agents. Add network, device (join code) and service; invite an agent (`components/Devices.tsx`, `Agents.tsx`) |
| `/activity` | Full Arkiv audit feed: live sessions, blocks, revocation latency, decrypt with the org key, runnable query |
| `/billing` | `MeshySubscription` seats used vs paid, add seats, renew |
| `/provider` | `ProviderRegistry` providers, register (stake), fraud-proof `slash` form, meshy-sync health |
| `/settings` | Org records, import the org key (localStorage only), **delegated admins** (EAC) |
| `/approve?org&agent&addr&ssh` | Approve an agent: World ID (or dev attestation) via meshy-sync, then a per-agent resolver |

## Libraries (`src/lib`)
| File | What |
|---|---|
| `ens.ts` | ENSv2 reads (via `resolve()`), org tree loader (registry event logs + Multicall3 batching), write helpers (`deployRegistry`, `deployResolver`, `recordCalls`, EAC `eacAbi`, `textKeySetter`) |
| `envelope.ts` | TS port of the Go policy/payload envelope. **Must stay byte-compatible** with `meshy/policy/envelope.go` (verified both ways) |
| `arkiv.ts` | `arkiv_query`, `$owner`-filtered org feed, payload decryption, live sessions |
| `contracts.ts` + `abi/` | ProviderRegistry / MeshySubscription ABIs and addresses (copied from `contracts/`) |
| `store.tsx` | Wallet (injected EIP-1193), org tree (sessionStorage cache), org key, toasts |
| `config.ts` | ENSv2 Sepolia addresses, `FROM_BLOCK`, provider URL |

## Server routes
- `/api/rpc`: Sepolia JSON-RPC proxy. It keeps the key server-side and routes `eth_getLogs` to `LOGS_RPC` (the public node), because Alchemy's free tier caps logs at 10 blocks.
- `/api/arkiv`: read-only Arkiv proxy that adds the access key; only query methods are allowed.

## Known gaps
- No org-creation wizard (`meshy-admin org init` only).
- Activity polls instead of using `watchEntityEvents`.
- Privy isn't integrated (browser wallet only).
- No deployed URL yet.

# Arkiv schema: Meshy audit log

Each resource daemon (meshyd) writes audit entities to **Arkiv Rabdi** (chain 7794095). The daemon's key is the writer; its address is the `addr` record of the resource's ENS name.

- **How it writes:** raw transactions to `0x4400…0044` `execute((uint8,bytes)[])`, in atomic batches. A batch goes out every 5 s, at 100 events, or immediately on a block or revocation.
- **Code:** [`meshy/arkivkit`](../meshy/arkivkit).
- **Who reads it:** the dashboard's Activity and Blocks views, live via `watchEntityEvents`.

## Entity types

Owner of every entity is **the resource daemon** — the EVM address stored in the ENS
`addr` record of the resource name (e.g. `db1.prod.acme.eth`). For the demo:
`0x7Dec34A86907eeF26c70A6578df7bAaE7F223384`.

The dashboard filters all queries with `$owner = addr(<daemon-address>)` to prevent
spoofed writes (anyone can write `org = 'meshydemo.eth'`, but only the real daemon is
the `$owner`).

All entities are created with `FlagReadOnly` (`creationFlags = 0x01`). No patch, extend,
transfer or delete is accepted.

| `arkiv.schema` | Written when | `MinLifetime` (blocks at 2 s) | Owner | Flags |
|---|---|---|---|---|
| `meshy.session` (start / end) | session start or clean end | grant lifetime + 1 day (falls back to 30 days / 1,296,000 blocks when `GrantExpiry` not set) | resource daemon `addr` | read-only |
| `meshy.session` (revoke) | on grant unregister detected | 90 days (3,888,000 blocks) — dispute / slashing evidence window | resource daemon `addr` | read-only |
| `meshy.blocked` | every blocked file op, blocked command, or denied login | 30 days (1,296,000 blocks) — compliance window | resource daemon `addr` | read-only |
| `meshy.audit-chunk` | each flush, one per session | 30 days (1,296,000 blocks) — compliance window | resource daemon `addr` | read-only |

## Attributes

| Name | Type | Indexed | On | Notes |
|---|---|---|---|---|
| `arkiv.schema` | str | ✓ | all | entity type |
| `org` | str | ✓ | all | e.g. `acme.eth` |
| `resource` | str | ✓ | all | e.g. `db1.prod.acme.eth` |
| `ts` | u64 | ✓ | all | ms since epoch (sortable; `$createdAt` isn't) |
| `agent` | str | ✓ | all | agent label, e.g. `claude-1` |
| `grant` | str | ✓ | all | ENS grant name, e.g. `claude-1.db1.prod.acme.eth` |
| `session` | str | ✓ | all | session id |
| `kind` | str | ✓ | session, blocked | `session.start`, `session.end`, `session.revoke`, `blocked`, `denied` |
| `op` | str | ✓ | blocked | `open`, `open-write`, `create`, `unlink`, `rename`, `exec`, … |
| `reason` | str | ✓ | blocked, revoke | `outside grant`, `read-only grant`, revoke reason |
| `block` | u64 | ✓ | revoke | Sepolia block that carried the `unregister` |
| `latencyMs` | u64 | ✓ | revoke | from the chain head being seen to the session being killed |
| `tsEnd` | u64 | ✓ | chunk | last op in the chunk |
| `nOps` | u64 | ✓ | chunk | number of ops in the chunk |
| `payload` | bytes | ✗ | all | the full event, sealed to the org key (below) |

## Privacy: what is and isn't public
Arkiv is public. **File paths, command lines and byte counts never appear in clear.** They live only in `payload`, which is a multi-recipient envelope (X25519 → HKDF-SHA256 → XChaCha20-Poly1305) sealed to the org's ENS `meshy.orgkey`, with `aad` set to the schema name. Indexed attributes carry only who, what kind, when and why. The dashboard decrypts payloads client-side with the org key.

## Queries the app runs
```text
# Blocks feed (Blocks tab), newest first
arkiv.schema = str('meshy.blocked') AND org = str('acme.eth') ORDER BY ts:u64 DESC

# One agent's activity on one resource
arkiv.schema = str('meshy.audit-chunk') AND agent = str('claude-1') AND resource = str('db1.prod.acme.eth') ORDER BY ts:u64 DESC

# Revocations and their measured kill latency
arkiv.schema = str('meshy.session') AND kind = str('session.revoke') AND org = str('acme.eth') ORDER BY ts:u64 DESC
```

Runnable (judges), on the public RPC:
```bash
curl -s https://rpc.rabdi.db-chain.testnet.arkiv.network -H 'content-type: application/json' -d \
 '{"jsonrpc":"2.0","id":1,"method":"arkiv_query","params":["arkiv.schema = str('"'"'meshy.blocked'"'"') ORDER BY ts:u64 DESC",{"select":{"key":true,"attributes":true},"limit":"0x14"}]}'
```
## Live data: real Sepolia demo (`meshydemo.eth`)
Same writer, `0x7Dec34A86907eeF26c70A6578df7bAaE7F223384`. The run made 12 batches with no failures. Two queries return its data:

- **Blocks** (`org = str('meshydemo.eth')`): 5 entities, from denied logins and outside-grant and read-only blocks.
- **Revocations:** 3 entities, at Sepolia blocks 12073043, 12073047 and 12073050, with `latencyMs` 120, 192 and 132.

```bash
curl -s https://rpc.rabdi.db-chain.testnet.arkiv.network -H 'content-type: application/json' -d \
 '{"jsonrpc":"2.0","id":1,"method":"arkiv_query","params":["arkiv.schema = str('"'"'meshy.session'"'"') AND kind = str('"'"'session.revoke'"'"') AND org = str('"'"'meshydemo.eth'"'"') ORDER BY ts:u64 DESC",{"select":{"key":true,"attributes":true},"limit":"0x14"}]}'
```

## Live data (Rabdi, fork run)
These came from `e2e/run-local.sh` with live Arkiv writes. ENS ran on a Sepolia fork; Arkiv was the real Rabdi testnet.

- **Writer (resource daemon):** `0x7Dec34A86907eeF26c70A6578df7bAaE7F223384`, [explorer](https://rabdi.explorer.arkiv.network/address/0x7Dec34A86907eeF26c70A6578df7bAaE7F223384)
- **Demo org:** `meshy923891.eth`, resource `db1.prod.meshy923891.eth`, agent `claude-1`
- **Batches:**
  - `0xea43f4252dae32f62325a693d6311124b42b8a6dde229ebe593df6ec83e9b351` (15 entities)
  - `0xfb0efb2123d29afba8263173630fa21a25b460c8411e54044440f43f328647a3` (3)
  - `0xb442321a2c160b8a10e51a75ce98b47ea2be94aaba46b7ea8a86dc462d78c557` (3)
- **Result:** the blocks query returns 5 entities (4 blocked file accesses, 1 denied login). The revocations query returns 2, with Sepolia blocks 12072955 and 12072958.

Runnable query for these:
```bash
curl -s https://rpc.rabdi.db-chain.testnet.arkiv.network -H 'content-type: application/json' -d \
 '{"jsonrpc":"2.0","id":1,"method":"arkiv_query","params":["arkiv.schema = str('"'"'meshy.blocked'"'"') AND org = str('"'"'meshy923891.eth'"'"') ORDER BY ts:u64 DESC",{"select":{"key":true,"attributes":true},"limit":"0x14"}]}'
```

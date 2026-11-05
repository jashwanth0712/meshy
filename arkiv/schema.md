# Arkiv schema: Meshy audit log

Each resource daemon (meshyd) writes audit entities to **Arkiv Rabdi** (chain 7794095). The daemon's key is the writer; its address is the `addr` record of the resource's ENS name.

- **How it writes:** raw transactions to `0x4400…0044` `execute((uint8,bytes)[])`, in atomic batches. A batch goes out every 5 s, at 100 events, or immediately on a block or revocation.
- **Code:** [`meshy/arkivkit`](../meshy/arkivkit).
- **Who reads it:** the dashboard's Activity and Blocks views, live via `watchEntityEvents`.

## Entity types

| `arkiv.schema` | Written when | Lifetime | Owner |
|---|---|---|---|
| `meshy.session` | session start, end, revoke | 30 days (1,296,000 blocks), read-only | resource daemon |
| `meshy.blocked` | every blocked file op, blocked command, or denied login | 30 days, read-only | resource daemon |
| `meshy.audit-chunk` | each flush, one per session | 30 days, read-only | resource daemon |

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
_The writer addresses and a demo org name will be filled in after the live Rabdi run._

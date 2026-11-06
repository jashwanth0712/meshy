# How we use Arkiv

> "Do the filtered fields, entity edit permissions, and expiration settings fit
> what the app does? Does each Arkiv feature used change something users see?"

---

## Filtered fields → dashboard features

Every entity carries a core set of indexed attributes that drive specific UI
elements. Here is what each attribute does:

| Attribute | Indexed | Used by | UI effect |
|---|---|---|---|
| `arkiv.schema` | ✓ | all queries | selects entity type (session / blocked / chunk) |
| `org` | ✓ | all queries + `$owner` filter | scopes feed to one organisation; combined with `$owner` = resource daemon addr (ENS `addr` record) to prevent spoofed writes |
| `resource` | ✓ | Activity feed filter, Blocks tab | shows which resource the event is on; `resource.split('.')[0]` is the short label shown in the row |
| `ts` | ✓ | `ORDER BY ts:u64 DESC` | chronological feed ordering (Arkiv's `$createdAt` is not sortable, so we store this ourselves) |
| `agent` | ✓ | Activity feed, Blocks tab | the `<agent>` label in every row |
| `grant` | ✓ | Revocations query | scopes revocation lookup to one specific grant |
| `session` | ✓ | `liveSessions()` | cross-join: start rows with no matching end/revoke → "Live now" section |
| `kind` | ✓ | Activity feed filter, stat tiles | `session.revoke` → kill-latency stat; `session.start` without end → live-sessions count; `blocked`/`denied` → blocked-attempts count |
| `op` | ✓ | Blocks tab | file-operation name shown in the blocked row |
| `reason` | ✓ | Blocks tab, revoke rows | "outside grant" / "read-only grant" / revoke reason in the row description |
| `block` | ✓ | Revocation rows | "killed at block N" in the revoke row |
| `latencyMs` | ✓ | Avg kill latency stat | avg across all revocations: `Math.round(sum / revokes.length)` ms |
| `tsEnd` | ✓ | Chunk rows | last op timestamp (future: time-range filter) |
| `nOps` | ✓ | Chunk rows | "N file ops" count in the row |
| `payload` | ✗ | Payload decryption | gzip(JSONL) for chunks; event JSON for sessions/blocked; decrypted client-side with the org key to show paths and argv |

---

## Entity edit permissions

All entities are created with `FlagReadOnly` (bit 0 set, `creationFlags = 0x01`).
This means:

- No patch, transfer, or delete operation is accepted by the Arkiv contract.
- The dashboard shows an **uneditable** audit trail: a user seeing the Activity
  feed cannot mistake it for a live-editable record.
- Judges can verify immutability: attempting a patch op on any of our entity
  keys returns a contract error.

Code: `batcher.go`, every `Create{..., Flags: FlagReadOnly, ...}`.

---

## Expiration settings and why they fit

| Entity type | `MinLifetime` | Blocks (2 s each) | Rationale |
|---|---|---|---|
| `meshy.blocked` | 30 days | 1,296,000 | Standard infosec compliance window for denied access attempts. Matches the 30-day default assumed by most SOC 2 controls. |
| `meshy.audit-chunk` | 30 days | 1,296,000 | Operational file-access logs; same 30-day window. No dispute arises from an op that completed successfully. |
| `meshy.session` start/end | grant lifetime + 1 day | computed per session | The evidence is relevant for exactly as long as the grant was (or could have been) active, plus one day's buffer for clock drift. If a grant expires in 2 hours, the entity lives for `(2×3600 + 86400) / 2 = 46,800 blocks`. Falls back to 30 days when `GrantExpiry` is not set. |
| `meshy.session` revoke | 90 days | 3,888,000 | Revocation evidence is used in dispute resolution and (future) slashing. 90 days gives a full quarter for a governance vote or legal process to reference the on-chain record. |

Each type's lifetime is set at write time by `batcher.go`'s `sessionLife()` and
the per-type branches in `build()` — not a single global default. Unit tests in
`batcher_test.go` (`TestSessionLifetimes`) verify each branch.

---

## Spoof-proof owner filter

Arkiv entities are public and anyone can write `org = 'meshydemo.eth'`. The
dashboard adds an owner filter so only entities written by the legitimate
resource daemon (whose address is the ENS `addr` record) are shown:

```typescript
// arkiv.ts – ownerClause()
AND ($owner = addr(0x7Dec34A86907eeF26c70A6578df7bAaE7F223384))
```

The activity page derives this address from `tree.networks.flatMap(n=>n.resources).map(r=>r.addr)`
via `useMeshy()`. The filter is shown to the user as a "Verified writers" note
listing the truncated addresses. Without the filter, a malicious actor could
write fake `meshy.blocked` entities claiming the daemon blocked a legitimate
agent, and the dashboard would show them.

---

## What is not done (honest)

- **`watchEntityEvents` subscriptions:** the dashboard polls every 5 s instead.
  Live latency is therefore 0–5 s above the actual kill time. We plan to switch
  to subscriptions after the hackathon.
- **Swarm payloads:** large file contents stay off Arkiv (see `why-arkiv.md`).
  A Swarm CID in the sealed payload is the planned extension.

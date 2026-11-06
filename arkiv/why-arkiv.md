# Why Arkiv

> "What would users lose if the app used Postgres or another database instead?
> What did the team keep off Arkiv, and why?"

---

## What Postgres cannot give us

### 1. Tamper-evidence the org's own admins can't defeat

The audit trail exists to protect the *org's users* from a compromised provider
or a rogue admin. If the same team that runs meshyd also controls the Postgres
instance, they can quietly delete a row. Arkiv entities are immutable once
written: `FlagReadOnly` (bit 0 set) means no patch or delete operation is
accepted. A judge or auditor can verify every row independently with a curl
command against the public RPC — there is no server to trust.

### 2. Independent public verifiability with no backend

The dashboard reads Arkiv directly via a Next.js proxy route — there is no
Meshy-controlled backend that could filter or rewrite query results. Anyone can
run the same query:

```bash
curl -s https://rpc.rabdi.db-chain.testnet.arkiv.network \
  -H 'content-type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"arkiv_query","params":[
    "arkiv.schema = str('"'"'meshy.blocked'"'"') AND org = str('"'"'meshydemo.eth'"'"') ORDER BY ts:u64 DESC",
    {"select":{"key":true,"attributes":true},"limit":"0x14"}]}'
```

They see exactly what the dashboard sees, keyed to the same writer address.

### 3. Queryable, indexed attributes without a schema migration

Arkiv lets us query `kind = str('session.revoke') AND org = str('acme.eth') ORDER BY ts:u64 DESC`
without declaring a schema ahead of time and without running `ALTER TABLE`. We
added `latencyMs` mid-hackathon and the query just worked.

Postgres would need a migration, a primary key, an index on `(org, kind, ts)`,
and a connection pool — all of which are infrastructure the org admin controls
and could tamper with.

### 4. Per-type expiry that fits compliance windows

Arkiv lets us set `MinLifetime` per entity:
- `meshy.blocked` / `meshy.audit-chunk`: 30 days (compliance window)
- `meshy.session` start/end: grant lifetime + 1 day (evidence for the grant's
  active period, automatically expires after)
- `meshy.session` revoke: 90 days (evidence for disputes and slashing)

A Postgres `DELETE WHERE ts < now() - interval '30 days'` could be run
accidentally — or intentionally — before a dispute is resolved. Arkiv's
per-entity `MinLifetime` is enforced by the chain.

### 5. Live data: 12 real batches, 3 real revocations

Writer `0x7Dec34A86907eeF26c70A6578df7bAaE7F223384` has written 12 successful
batches (0 failures) to Rabdi. Three revocation entities carry real `latencyMs`
values (120, 192, 132 ms) that the dashboard reads and averages. These numbers
come from Arkiv, not from a database we control.

Estimated gas per batch: ~127,000 (observed from `eth_estimateGas` calls during
the fork run). At Rabdi testnet this is effectively free; on a production chain
the cost would be a small fraction of the value of a tamper-evident audit trail.

---

## What we kept off Arkiv, and why

| Data | Stored where | Why not Arkiv |
|---|---|---|
| File paths, command lines, byte counts | `payload` field (sealed to org key) | Arkiv is fully public. Paths and commands are sensitive. They live only in an X25519→XChaCha20-Poly1305 envelope; the indexed attributes carry only who, what kind, when and why. |
| ACL source of truth (who can access what) | ENS subnames + `meshy.policy` records | ENS *is* the access-control plane. Arkiv is an append-only audit log. Reading grants from Arkiv rather than ENS would create a secondary, mutable-looking view of the access policy. |
| Org X25519 key, SSH keys, daemon keys | ENS text records (`meshy.orgkey`, `meshy.enckey`), local `~/.meshy/secrets.env` | Key material is not an audit event and must stay in systems designed for secret management. |
| Session-to-session continuity (live session state) | In-process ring buffer (`audit.Ring`) in meshyd, plus Arkiv start/end pairs | Arkiv is the durable evidence; the ring is the fast in-process cache. We derive "live sessions" from Arkiv (start without a matching end) but the daemon tracks liveness itself. |
| Arbitrary Swarm files | Not implemented | Planned but out of scope for this sprint. File-content storage is a natural extension: the sealed payload could include a Swarm CID. |

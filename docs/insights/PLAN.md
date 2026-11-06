# Meshy Insights: turning every interaction into something you can ask about

> **Status:** plan only, no code changed. Written Fri 6 Nov 2026 for ETHGlobal Mumbai, where judging starts Sat 7 Nov 09:00 IST.
> **Mockups:** [`mockups/index.html`](mockups/index.html). There are five pages, built with the ui-revamp tokens and shell, and the data is shaped like the real `meshydemo.eth` data.
> **Evidence:** every Arkiv claim below was checked against the public Rabdi RPC on 6 Nov. The queries are in §3.4 and can be pasted as they are.

## Contents
- [0. Summary](#0-summary)
- [1. Signal inventory](#1-signal-inventory)
- [2. The questions people will ask](#2-the-questions-people-will-ask)
- [3. Data model and query layer](#3-data-model-and-query-layer)
- [4. Filters and search](#4-filters-and-search)
- [5. Visualizations](#5-visualizations)
- [6. Privacy and security](#6-privacy-and-security)
- [7. Exports and integrations](#7-exports-and-integrations)
- [8. Roadmap](#8-roadmap)
- [9. Decisions needed](#9-decisions-needed)

---

## 0. Summary

Meshy already writes more than the dashboard shows. ENS has the full life of every grant: who registered it, for whom, until when, who revoked it and in which block. Arkiv has every session start, end and revoke, plus every refusal, all signed by the device daemon. What's missing is a **join**. Nothing today puts the ENS intervals and the Arkiv events on one timeline, so nobody can ask "who could read `/srv/reports` at 14:00" or "which grants were never used".

**Top findings:**

1. **The join key already exists.** It's the grant name, plus the grant's time window. Arkiv events carry `grant`, and ENS `LabelRegistered`/`LabelUnregistered`/`ExpiryUpdated` events carry the tokenId, the expiry and the **`sender`** (the granter and the revoker). The revoke event's `block` attribute anchors Arkiv time to chain time exactly. A first insight layer needs no daemon change.
2. **No allowed file op has reached Arkiv on the demo org.** Live count on 6 Nov: `meshy.session` 29, `meshy.blocked` 7, **`meshy.audit-chunk` 0**. The causes:
   - `db1` runs the macOS sandbox, which audits blocks only; allowed reads need eslogger, which needs root.
   - `rig1`, which runs the Linux GateFS, has Arkiv turned off (its key is unfunded).

   So the file heatmap and "what did it touch" have no live data until one Linux daemon writes to Arkiv. That is an ops task taking about an hour, not code.
3. **Revocation reasons are ambiguous.**
   - The watcher writes `revoked or expired (grant or an ancestor is no longer registered)` for both an unregister and an expiry.
   - The local expiry timer and `meshyd kill` don't emit `session.revoke` at all; they only end with `exit 137`.
   - The good news: the fail-closed path is distinct. One real revoke on 6 Nov was `cannot verify grant on chain (fail closed)`, with `latencyMs` 5002. That is a genuine anomaly worth flagging in the UI.
4. **The retention windows don't line up.** A `session.start`/`end` lives for the grant's remaining time plus one day. Its `audit-chunk`s live 30 days. For a 1 h grant, the file ops outlive the session they belong to after about 25 h. Fix it by giving sessions at least the chunk lifetime (decision D4).
5. **Commands typed inside an interactive shell are invisible.** Only the SSH exec argv is recorded (`/bin/sh -l` for a login). Capturing **exec events** inside the sandbox fixes this without keylogging: eBPF or `cn_proc` on Linux, and eslogger's `exec` event on macOS.
6. **Audit drops are silent.** Under backpressure the batcher drops the oldest allowed ops (`dropOps`, at 10,000 events) and records nothing. A noisy agent could push its own trail out. Add a `dropped` counter and a per-session `seq`.
7. **Arkiv's query engine is good enough for the dashboard,** but you have to know its rules (§3.4):
   - The page size is capped at 100.
   - A cursor works only at the same head block, so use keyset pagination on `ts`.
   - Numeric literals must be typed `u64(…)`. `u256(…)` against a u64 attribute **silently returns 0 rows**.
   - Supported: `STARTSWITH`, `BETWEEN`, `OR`, `$owner`, and `select.count`.
   - Not supported: `!=`; `NOT (…)` returns `NotImplemented`.

**Top visualizations, in demo order:**
1. **Access Gantt.** Grant lifetimes from ENS, with sessions from Arkiv drawn inside them, ⚡ revoke markers, and "never used" grants greyed out.
2. **Agent timeline** with facets in the URL.
3. **"Who can reach what" at time T,** with a scrubber.
4. **Revoke latency strip.** It plots the 5 real kills: 120, 132, 192, 1316 and 5002 ms, the last flagged as fail-closed.
5. **Session replay:** argv, then the ops, then the `MESHY_BLOCKED` lines, then the kill.

**Decisions needed** are in §9. The first is whether to build the Timeline, Gantt and latency views before judging. That's about 4 to 5 h of web-only work with no daemon change.

---

## 1. Signal inventory

**Legend**
- **Public:** anyone can read it.
- **Sealed:** encrypted to `meshy.orgkey` (and to the device's `meshy.enckey` for policies).
- **Local:** only on the machine that produced it.

**Retention** is how long the source keeps the data. "Chain" means forever, since ENS on Sepolia is permanent.

### 1.1 ENS (Sepolia, ENSv2): who holds what access, when, and at what level

| Signal | Where it lives | Visibility | Retention | Granularity | Notes |
|---|---|---|---|---|---|
| **Grant registered** | `LabelRegistered(tokenId, labelHash, label, owner, expiry, sender)` on the device registry (folder grants) or the service registry (port grants) | Public | Chain | One per grant instance, per block | `label` = agent label. `owner` = agent address. `expiry` = until when. **`sender` = the granter** (owner or a delegated admin). Read today by `web/src/lib/ens.ts` and `enskit/children.go`, but `sender` is thrown away. |
| **Grant revoked** | `LabelUnregistered(tokenId, sender)` | Public | Chain | Per block | **`sender` = the revoker.** The block timestamp is the revoke time. |
| **Grant renewed** | `ExpiryUpdated(tokenId, newExpiry, sender)` | Public | Chain | Per block | The ABI is in `ens.ts`, but nothing reads the event yet. "+1h" in the ui-revamp plan would emit it. |
| **Grant expired** | Derived: `expiry` < now with no unregister | Public | Chain | Second | Not an event. Compute it from the registered expiry plus the renewals. |
| **Grant re-issued** | A new `LabelRegistered` for the same label with a new tokenId | Public | Chain | Per block | meshyd treats a tokenId change as a revoke (`grant was re-issued`). |
| **Access level (paths, ro/rw, cmds)** | The `meshy.policy` text record on the org PermissionedResolver | **Sealed** (device + org key) | Chain, current value only | Per write | The envelope's `aad` is the grant name, and the policy is bound to `grantTokenId`. **History:** walk the resolver's `setText` transactions, or its text-changed logs if PermissionedResolver emits them. *Verify the event name; the repo has no resolver event ABI.* Decrypting each historical value gives the path history of a grant. |
| **Service grant level** | No policy. It's the service's `meshy.service` `{proto, target}` | Public | Chain, current value | Per write | One port; the level is always "connect". |
| **Delegated admin role changes** | EAC `grantRootRoles` / `revokeRootRoles` on network, device and service registries; `grantSetterRoles` on the resolver | Public | Chain | Per tx | Today: tx calldata, plus `hasRootRoles` reads (`Admins.tsx`). Index the EAC role-change event. *Verify its name in the ENSv2 contracts; not in the repo.* |
| **Structure** | `LabelRegistered` / `SubregistrySet` on the org → network → device → service registries | Public | Chain | Per tx | Devices and services added or removed, and by whom (`sender`). For example, `rig1` was added by `0xf846…Fc98`. |
| **Device identity** | `addr`, `meshy.enckey`, `meshy.ssh-host`, `meshy.roots`, `meshy.kind`, `meshy.meta` on the device name | Public | Chain, current | Per write | `addr` is the Arkiv writer. **Its history matters:** an Arkiv entity is trusted only if `$owner` equalled the device's `addr` *when it was written*. |
| **Agent identity** | `addr`, `meshy.ssh`, `agent-context` on the agent's own resolver; the ENS owner is the approving human | Public | Chain | Per write | SSH key rotations (the agent holds the setter role for `meshy.ssh`). |
| **World ID attestation** | `meshy.worldid` = base64url JSON `{agent, addr, human, acr, authTime, iat, exp, attestor, sig}` | Public (`human` is an opaque per-org hash) | Chain | Per approval | When the agent was human-backed, by which attestor, and until when (`exp`). Today the demo is a dev signature. |
| **Org settings** | `meshy.orgkey`, `meshy.attestor`, `meshy.provider` | Public | Chain | Per write | An org-key rotation changes what can be decrypted (see §6). |

### 1.2 Arkiv (Rabdi, chain 7794095): what happened

Common indexed attributes: `arkiv.schema`, `org`, `resource`, `ts` (u64 ms), `agent`, `grant`, `session`. The `payload` is not indexed and is sealed to the org key with `aad` = schema. Every entity is read-only (`creationFlags` 1, checked live), and its `$owner` is the device daemon.

| Entity / kind | Written when | Indexed (public) | Payload (sealed) | Retention | Live count, 6 Nov |
|---|---|---|---|---|---|
| `meshy.session` `session.start` | After the sandboxed process starts (`server.go:264`), or when a forward opens (`forward.go:72`) | common, `kind` | `argv` (the exec command, or `["connect", service, proto://target]`), `grantExpiry` | Grant's remaining time + 1 d (falls back to 30 d) | 12 |
| `meshy.session` `session.end` | When the process exits, the client disconnects, or after a kill (`server.go:298`) | common, `kind` | `reason` = `exit N` (0, 255 for client gone, 137 for killed). For forwards: `bytes`, `closed (X B in, Y B out)` | Same | 12 |
| `meshy.session` `session.revoke` | The watcher sees an unregister, an owner change, a re-issue or a stale chain (`server.go:357`, `forward.go:113`) | common, `kind`, `block`, `latencyMs`, `reason` | The same fields | 90 d | 5 |
| `meshy.blocked` `blocked` | A GateFS block, a Seatbelt EPERM, a command outside `cmds`, or a forward refused | common (but **`grant` is absent on a forward refusal**), `kind`, `op`, `reason` | `path`, `op`, and for a refused exec the raw command | 30 d | 4 |
| `meshy.blocked` `denied` | Login or session authorization refused (`server.go:168–199`) | `agent`, `kind`, `reason` (contains `MESHY_NO_GRANT grant=…`) and **no `grant` attribute** | The same | 30 d | 3 |
| `meshy.audit-chunk` | Each flush (every 5 s, at 100 events, or urgently on a block or revoke), one per session | common, `tsEnd`, `nOps` | gzip JSONL of `op` events: `{op, path, bytes, ts}` | 30 d | **0** |

`select` can also return `owner`, `creator`, `createdAt`, `expiresAt` and `creationFlags`, so retention is visible per entity (§3.4).

### 1.3 meshyd: the session layer (`meshy/meshyd`)

| Signal | Captured? | Where | Granularity |
|---|---|---|---|
| Agent login (identity check) | Only on **failure** (`denied`). A successful login with no session leaves no record. | Arkiv | Per connection |
| Exec argv | Yes, in `session.start` | Sealed payload | Per SSH exec |
| Interactive shell commands | **No.** A login is recorded as `/bin/sh -l` | — | — |
| Session start and end | Yes, wall-clock `ts` from the daemon | Arkiv | ms |
| Duration | Derived: end `ts` − start `ts`. Not indexed. | Arkiv | ms |
| Exit code | Yes, as text in the sealed end payload (`exit 0`) | Sealed | Per session |
| Kill cause | Partly. Revoke → `session.revoke` with a reason. Expiry and `meshyd kill` → only `exit 137` | Arkiv | Per session |
| Revoke latency | Yes: `latencyMs`, from the head being seen to the session being killed | Public | ms |
| Revoke block | Yes: `block` | Public | Block |
| Inclusion latency (tx sent → mined) | **No** (in STATUS it's measured by hand) | — | — |
| SSH channel type (`session`/`exec`/`shell`/`pty`/`direct-tcpip`) | **No.** Only inferable from argv. | — | — |
| Forward bytes in and out | Yes, in the forward's `session.end` payload | Sealed | Per forward |
| Service request count | **No** (it's a TCP pipe, with no L7 view) | — | — |
| Policy snapshot used | **No.** The session doesn't record which policy version or tokenId it ran under. | — | — |
| Local copy | An in-memory `audit.Ring{N:1000}` (feeds `meshyd status -w`), and plaintext JSONL on **stdout** | **Local, plaintext** | Per event |

### 1.4 GateFS, the Linux FUSE gate (`meshy/sandbox/gatefs.go`)

| Op | Allowed → `op` event | Blocked → `blocked` event | Bytes |
|---|---|---|---|
| `open` (read) / `open-write` | One event **per file handle at `Release`**: `read N`, `write N`, or `open` when there was no I/O | `open` / `open-write`, with reason `outside grant` or `read-only grant` | Yes: bytes read **or** written per handle (write wins when both happen) |
| `create` | Yes | Yes | — |
| `readdir` | Yes | Yes | — |
| `unlink`, `rmdir`, `mkdir`, `symlink`, `link` | Yes (`mutate`) | Yes. `link` also needs write access to the source. | — |
| `rename` | Yes, `from -> to` | Yes (either side) | — |
| `mknod` | — | Always blocked | — |
| `lookup` / `getattr` (stat) | **Not audited** | Hidden (not visible) | — |
| Reads under the system allow-list (`/etc/…`) | **Not audited**, on purpose ("keep the log about the data") | — | — |

Events are stamped with session, agent and grant by meshyd's per-session sink.

### 1.5 macOS sandbox (`seatbelt_darwin.go`, `eslogger_darwin.go`)

- **Blocks.** `BlockTap` scans the child's stderr for EPERM/EACCES and emits `blocked` with `op="access"`. There's no read/write distinction and no syscall. This only works if the tool prints the error. A silent `open()` failure in a program that swallows errors is **not** recorded.
- **Allowed ops.** The eslogger monitor subscribes to `fork, exit, open, close, create, rename, unlink`, and needs root plus Full Disk Access. It **has never run live** (STATUS P2-9), which is why `db1` produces no chunks. **`exec` is not subscribed**, even though eslogger supports it, and it's the cheapest way to capture typed commands on macOS.

### 1.6 Provider (meshy-sync on AWS) and NetBird

| Signal | Where | Visibility | Retention | Granularity |
|---|---|---|---|---|
| Admission attestation `{providerId, name, resource, issuedAt, validUntil, peerKey, sig}` | `attestations.jsonl` on the provider; `GET /v1/attestations?name=` (newest first, at most 200) | Public endpoint | Until the file is rotated | Per mesh join |
| Admission refusals (`MESHY_NOT_ON_ENS`, `MESHY_EXPIRED`, `MESHY_SEATS_EXCEEDED`, …) | HTTP response plus an `slog` line | **Provider logs only** | Log rotation | Per request |
| Seats `{seats, used, paidUntil}` | `GET /v1/seats`; `MeshySubscription` events on Sepolia | Public | Chain | Per change |
| Reconciler pass `{groups, policies, peersMatched, peersEvicted, policiesAdded, policiesRemoved, err}` | `GET /v1/health` → `mesh.lastSync` (**the last pass only**) | Public | None (overwritten every 10 s) | 10 s |
| NetBird policy and peer changes | NetBird management's own activity log (`/api/events`: peer added, policy created or deleted, peer login) | Provider admin token | NetBird DB | Per change |
| Peer connectivity (`connected`, `last_seen`, IP) | NetBird `/api/peers` | Provider admin token | Current only | Poll |
| Slashing | `ProviderRegistry` `Slashed(...)` events | Public | Chain | Per tx |
| World device flow (start, poll status ok/denied/expired) | Provider memory and logs | Provider | None | Per approval |

### 1.7 Mac app, menu bar, CLI and plugin

| Source | What it knows | Persisted? |
|---|---|---|
| Mac app (`mac/`, WKWebView of meshy-bay) | Nothing of its own; it shows the dashboard | No |
| Menu bar (`menubar/`) | Local identities, grants, local meshyd sessions, Arkiv blocks and revocations (as notifications) | No |
| `meshy` CLI | The `MESHY_*` lines the agent received (`BLOCKED`, `REVOKED`, `NO_GRANT`, …), the commands it ran, and how long connecting took | **No.** This is the agent's own view and is lost. |
| Claude Code plugin (MCP: `meshy_join/whoami/grants/ssh`) | Which tool calls the agent made and why (the prompt context) | No |

### 1.8 Gaps worth capturing next, with their Arkiv cost

Baseline cost, measured on 6 Nov: a sealed session or blocked payload is **1.2–1.6 KB** (hex in the RPC, so about 600–800 raw bytes), with 9–12 attributes. A session today costs 2–3 entities plus one chunk per 5 s flush while files are being touched. The limits per entity: 32 attributes, `str` ≤ 128 B, `payload` ≤ 128 KiB. Rabdi doesn't publish quota numbers (`arkiv/friction.md` §4), so cost is given below in entities and bytes.

| # | Gap | Capture it as | Arkiv cost | Public or sealed | Value |
|---|---|---|---|---|---|
| G1 | **Exact kill cause** | Indexed `cause` ∈ `unregistered · expired · reissued · owner-changed · fail-closed · manual-kill · client-exit · exit` on `session.end`. Also emit `session.revoke` for expiry and manual kill. | +1 attribute (~40 B); +1 entity on expiry or kill | Public (it's already inferable from ENS) | Revoke vs expiry vs fail-closed charts; the anomaly flag |
| G2 | Exit code | Indexed `exitCode` u64 on `session.end` | +1 attribute | Public metadata (decision D3) | "Sessions that failed" facet |
| G3 | Command duration | Indexed `durMs` u64 on `session.end` | +1 attribute | Public | Duration histograms with no join |
| G4 | Bytes read and written per session | Indexed `bytesR`, `bytesW` on `session.end` (summed from ops) | +2 attributes | Public magnitude, sealed paths | Exfiltration-volume anomaly; a working "bytes read" facet |
| G5 | Both read and write bytes per handle | `auditFile.Release` emits both counts (today write wins) | +~10 B per op in the chunk | Sealed | Correct totals |
| G6 | SSH channel and request type | Indexed `chan` ∈ `exec · shell · pty · direct-tcpip · subsystem` on `session.start` | +1 attribute | Public | Distinguishes an interactive shell from a one-shot exec |
| G7 | **Exec tree inside the sandbox** (what was actually typed and run) | Linux: `cn_proc` netlink or an eBPF `sched_process_exec` filtered by the session cgroup or pidns. macOS: add `exec` to `esEvents`. Write `op=exec` events (argv, truncated to 512 B) into the chunk. | ~60–120 B gzipped per exec, in the existing chunk; no new entities | Sealed | **"Which commands they typed"** without keylogging |
| G8 | Policy version on each session | Indexed `tokenId` (the grant instance) and `policyHash` (`sha256(sealed record)[:16]`) on `session.start` | +2 attributes | Public (hashes) | Exact join to the ENS policy write; makes "as of T" exact |
| G9 | `grant` on denied and refused-forward events | Set `grant` from the parsed `MESHY_NO_GRANT grant=…` | +1 attribute | Public (it's already in `reason`) | "What did it ask for and not get?" without regex |
| G10 | **Audit integrity** | `seq` u64 per session on each chunk; `dropped` u64 when `dropOps` fires; `prevKey` (the previous chunk's entity key) | +2–3 attributes per chunk | Public | Detects gaps and drops; the base for signed reports (§7) |
| G11 | Successful login (no session yet) | A `meshy.session` with `kind=login` | +1 entity per connection | Public | "Agent probed but never used" |
| G12 | Service request counts | For `proto:"http"`: count requests and status classes in the forward (light parsing) → `nReq`, `n2xx/4xx/5xx` on the forward's end | +3–4 attributes | Public | Service usage charts |
| G13 | Inclusion latency | The dashboard records tx-sent time and mined block for revokes it sends; the daemon already has head-seen to killed | 0 on Arkiv (client side), or +1 attribute `sentAt` if the revoker writes a `meshy.revoke-intent` entity | Public | Full revoke-latency breakdown (inclusion, propagation, daemon) |
| G14 | Session lifetime alignment | `sessionLife` = max(grant + 1 d, 30 d) | Same entities, but they live longer | — | Joins don't break after a day (finding 4) |
| G15 | Provider admission refusals | Provider writes `meshy.admission` entities (ok and refused) under its own key | +1 entity per join attempt | Public | Provider persona; seat-limit insight |
| G16 | Reconciler history | Provider writes `meshy.mesh-sync` only **when something changed** (policies added or removed, peers evicted) | Low: about 1 entity per grant change | Public | "ENS → NetBird lag" chart (STATUS saw about 27 s) |

---

## 2. The questions people will ask

"Answerable today?" means answerable with existing data and only a join (✅), with the org key (🔑), or not until a gap is filled (G#).

### Org owner (`0x45e8…E0EA`)
| Question | Answerable today? |
|---|---|
| What did `claude-1` do on `db1` in the last hour? | ✅ sessions, blocks; 🔑 argv; G7 for typed commands; file ops need chunks (finding 2) |
| What can each agent reach **right now**, and until when? | ✅ ENS |
| Which grants were never used, or used once and left open? | ✅ ENS intervals × Arkiv `session.start` by grant |
| Who granted this, and who revoked it? | ✅ `LabelRegistered.sender`, `LabelUnregistered.sender` |
| How much access did I hand out this week, in grant-hours, by network? | ✅ |
| Did anything get blocked, and should I grant it? | ✅, plus "Grant this" (ui-revamp innovation 9) |

### Security reviewer / auditor
| Question | Answerable today? |
|---|---|
| Who could read `/srv/reports` at 14:00 on 6 Nov? | 🔑 ENS intervals at T, decrypted policy at T, longest-prefix match |
| Show every access to `/srv/reports/q3.csv`, with bytes | 🔑 + chunks (finding 2) |
| What got blocked, why, and by which agent? | ✅ public `op`/`reason`; 🔑 for paths |
| Was every session killed after its grant ended? Show the time from revoke to kill. | ✅ `session.revoke.latencyMs` + block; G13 for inclusion time |
| Were any events dropped or missing? | G10 |
| Is this audit trail genuine, and written by the device? | ✅ `$owner` = the device's ENS `addr` at the time; `creationFlags.readonly` |
| Has any session run under a policy other than the current one? | G8 |
| Did the daemon ever fail closed, and why? | ✅ `reason` contains `fail closed` |

### Delegated admin (`0xf846…Fc98`, admin of `home`)
| Question | Answerable today? |
|---|---|
| Which grants did **I** make, and which are still live? | ✅ filter by `sender` |
| What are agents doing on my network (`home`) only? | ✅ facet `network=home` (resource suffix) |
| Are there agents blocked on `rig1` who need `datasets`? | ✅ (once rig1 writes to Arkiv) |
| Can I see `prod`? | By design, yes: ENS is public. Paths need the org key, which delegated admins **don't hold** (decision D2). |

### The agent itself (`claude-1`)
| Question | Answerable today? |
|---|---|
| What am I allowed to touch, and until when? | ✅ `meshy grants` |
| Why was I blocked just now, and who do I ask? | ✅ in the `MESHY_BLOCKED` line; not persisted (§1.7) |
| What did I do in my last session (to resume work)? | Needs `meshy history` (§7.4) |
| Was my session killed by a revoke or by expiry? | Partly (`MESHY_REVOKED`/`MESHY_EXPIRED`); G1 makes the record match |

### Provider (meshy-sync operator)
| Question | Answerable today? |
|---|---|
| Which names did I admit, and when? Can any admission be slashed? | ✅ `/v1/attestations` |
| How long does ENS → NetBird policy take? | G16 (one sample in STATUS: about 27 s) |
| Which orgs are near their seat limit? Who was refused? | ✅ seats; G15 for refusals |

### Hackathon judge
| Question | Answerable today? |
|---|---|
| Is the audit trail really on Arkiv, unspoofable, and verifiable without your server? | ✅ the runnable query, `$owner`, read-only flag |
| How fast is revocation, really? | ✅ 5 measured kills: 120, 132, 192, 1316, 5002 ms |
| Does each Arkiv feature change something users see? | ✅ every attribute maps to a facet or chart (§4) |
| Can I search it myself? | ✅ shareable URL (§4.4) plus the "Show query" panel |

---

## 3. Data model and query layer

### 3.1 Canonical keys

| Key | Form | Source | Joins |
|---|---|---|---|
| **Org** | `meshydemo.eth` | all | everything |
| **Network** | `prod`, `home` | ENS. In Arkiv, take the second label of `resource` | `resource STARTSWITH`; suffix match |
| **Resource (device)** | `db1.prod.meshydemo.eth` | ENS name; Arkiv `resource` | sessions ↔ device ↔ writer `addr` |
| **Service** | `camera.pi.home.meshydemo.eth` | ENS; Arkiv carries it in `grant` (`claude-1.camera.pi…`) and the payload `path` | — |
| **Agent** | ENS `claude-1.agents.meshydemo.eth`; Arkiv `agent = claude-1` (label only) | both | `agent` label ↔ ENS `<label>.agents.<org>` |
| **Grant name** | `claude-1.db1.prod.meshydemo.eth` | ENS name; Arkiv `grant` | **the main join** |
| **Grant instance** | `(grant name, tokenId)` | ENS `LabelRegistered.tokenId` | Arkiv by time window today; exact with G8 |
| **Session** | random id, from meshyd | Arkiv `session` | start ↔ end ↔ revoke ↔ chunks ↔ blocked |
| **Entity** | Arkiv entity key (bytes32) | Arkiv | the immutable cache key; the proof link |
| **Block** | Sepolia block number | ENS logs; Arkiv `session.revoke.block` | **chain time ↔ daemon time anchor** |
| **Granter / revoker** | EVM address | `LabelRegistered.sender` / `LabelUnregistered.sender` / `ExpiryUpdated.sender` | resolve with ENS reverse lookup, or label owner as "you" / "admin of home" |

### 3.2 Two derived tables (everything else is a view)

```ts
// One row per grant instance. Built from ENS logs alone (public).
type GrantInterval = {
  grant: string;            // claude-1.db1.prod.meshydemo.eth
  tokenId: bigint;          // from LabelRegistered
  kind: "device" | "service";
  agent: string;            // claude-1
  target: string;           // db1.prod.meshydemo.eth | camera.pi.home.meshydemo.eth
  network: string;          // prod
  granter: `0x${string}`;   // LabelRegistered.sender
  from: number;             // block timestamp (ms)
  until: number;            // expiry, updated by ExpiryUpdated
  ended?: number;           // LabelUnregistered block ts, or a later re-register, or `until` if past
  endCause: "live" | "revoked" | "expired" | "reissued";
  revoker?: `0x${string}`;  // LabelUnregistered.sender
  blocks: { registered: number; unregistered?: number; renewed: number[] };
  policy?: PolicyVersion[]; // 🔑 decrypted meshy.policy writes in [from, ended]
};
type PolicyVersion = { at: number; tx: string; paths: { p: string; m: "ro" | "rw" }[]; cmds: string[] };

// One row per thing that happened. Built from Arkiv (+ ENS events re-cast as rows).
type InsightEvent = {
  t: number;                // ms. Arkiv `ts`; ENS block timestamp
  src: "arkiv" | "ens" | "provider";
  kind:
    | "grant.created" | "grant.renewed" | "grant.revoked" | "grant.expired"
    | "admin.role"
    | "session.start" | "session.end" | "session.revoke"
    | "op" | "blocked" | "denied"
    | "admission";
  org: string; network: string; resource: string; agent?: string; grant?: string; session?: string;
  actor?: string;           // granter/revoker address for ENS rows
  op?: string; path?: string; bytes?: number; mode?: "ro" | "rw";            // path 🔑
  outcome: "ok" | "blocked" | "denied" | "revoked" | "expired" | "failed";
  reason?: string; block?: number; latencyMs?: number; exitCode?: number; argv?: string[];  // argv 🔑
  proof: { arkivKey?: string; tx?: string; owner?: string };   // click-through
  sealed: boolean;          // true when a payload exists but no org key is loaded
};
```

**Join rules:**
1. `session.*`, `blocked`, `op` → `GrantInterval`. Match `grant` equal, then `iv.from ≤ t ≤ (iv.ended ?? +∞) + 5 s`. The 5 s slack covers the Arkiv batch lag and the daemon's wall clock. When several match (a re-issue in the same minute), prefer the one whose `ended` block equals the event's `block` (for a revoke), else the latest `from`. With G8 the match becomes exact on `tokenId`.
2. `denied` rows have no `grant`. Parse `/grant=(\S+)/` from `reason` (G9 removes the need). These map to **no** interval, by definition: they are "asked for and not held".
3. **Clock alignment.** For each `session.revoke`, `offset = arkiv.ts − (blockTs(block) + latencyMs)`. Keep the median offset per resource. If |offset| > 2 s, show a "daemon clock skew" warning. Today's data can do this.
4. **Trust.** Keep an Arkiv row only if `owner` ∈ the set of `addr` values the `resource` had at `t` (address history from resolver writes). Today we use the current `addr` only; that's fine until a daemon key rotates.
5. **Op rows.** Expand each `audit-chunk` (🔑) into its `op` events, inheriting `session`, `agent` and `grant` from the chunk's attributes.

**"As of T" access reconstruction**, `reachAt(T, opts)`:
```
live = GrantInterval where from ≤ T < (ended ?? until)
for each g in live:
  if g.kind == "service": cell(g.agent, g.target) = "connect"
  else: pv = last g.policy with at ≤ T           // 🔑; without key → "folders (sealed)"
        for each path rule: cell(g.agent, g.target, rule.p) = rule.m
admins(T) = EAC role intervals live at T           // who could have *granted* access at T
```
"Who could read `/srv/reports` on `db1` at 14:00" is the agents whose rules on `db1` have the longest prefix covering `/srv/reports` with `m ∈ {ro, rw}`. That's the same rule as `policy.Check`, so port `policy.go` matching to TS once. Add `admins(T)` and the owner as "could have granted it".

### 3.3 Where the joins run: three options

| | **A. Query Arkiv and ENS directly from the browser** | **B. A browser index (IndexedDB)** | **C. A provider-side indexer** |
|---|---|---|---|
| How | On each view: `arkiv_query` (≤ 100 a page), ENS `getLogs`, join in memory | Same sources, but every immutable entity is cached by key in IndexedDB; incremental sync is `ts > u64(lastTs)`; ENS logs cached by block range | meshy-sync (or a separate `meshy-index`) tails Rabdi and Sepolia, stores to Postgres or SQLite, and serves `/v1/insights/*` |
| Decryption | Client side, org key in memory | Client side. **Store ciphertext only**, decrypt per view (or keep plaintext in memory) | Indexer stores **public attributes only**. The browser fetches payloads and decrypts. (Variant C′: an org-hosted indexer that holds the org key.) |
| Latency | 3 parallel queries ≈ 0.5–1 s; joins are instant at demo scale | First load like A; then ~100 ms | ~50 ms; pushes over SSE |
| Scale ceiling | ~1–2k events per view (≤ 100 per page) | ~100k events (an IndexedDB budget of tens of MB) | Unbounded; aggregates in SQL |
| Trust | Best: no server; the user can verify every row | Same as A | The server can omit rows. **Mitigate:** the client spot-checks entity keys against Arkiv, and reports carry signed roots (§7.3) |
| Search | In-memory filter | Indexed by facet, plus a path trigram index over decrypted ops (in memory) | Full-text over public attributes only; paths only in C′ |
| Effort | **S.** Extend `web/src/lib/arkiv.ts` | M | L |
| Fits the hackathon | ✅ | after | after |

**Recommendation:** A now, B next, C only for org-wide aggregates that don't need plaintext (seat usage, latency stats, provider views).
- Keep decryption in the browser by default; that's decision D1.
- **Arkiv entities are read-only and never change,** so caching them by key never needs invalidation, only eviction at `expiresAt`. That makes B unusually simple.

### 3.4 Arkiv queries that work today (checked on Rabdi, 6 Nov)

The endpoint is `https://rpc.rabdi.db-chain.testnet.arkiv.network`, method `arkiv_query`, with params `[query, options]`. These are the rules we found:

| Rule | Detail |
|---|---|
| `limit` ≤ `0x64` (100) | `limit 200 exceeds the node maximum of 100` |
| Numeric literals are typed | `ts > 1762300000000` → `value is out of range for i32`. Write `u64(…)`. **Using `u256(…)` on a u64 attribute returns 0 rows with no error.** |
| Operators | `= < <= > >= BETWEEN STARTSWITH`, plus `AND`/`OR` with parentheses. `&& \|\| ~` were removed. `!=` doesn't exist. `NOT (…)` returns `store: NotImplemented` on Rabdi. |
| Count | `{"select":{"count":true}}` returns `{"count":"0x24","data":[]}` (36 for `meshydemo.eth`) |
| Metadata | `select` keys: `key, owner, creator, createdAt, updatedAt, expiresAt, creationFlags, attributeSchema, attributes, count` |
| Pagination | The response carries `cursor` (`b64:…`) and `blockNumber`. A cursor is valid **only at the same head block**: `cursor belongs to a different query, block or select`. `atBlock` for older blocks fails with `state for this block is not retained`. **Use keyset pagination:** `ORDER BY ts:u64 DESC` with `ts < u64(<last ts>)`. |

```bash
RPC=https://rpc.rabdi.db-chain.testnet.arkiv.network
aq() { curl -s $RPC -H 'content-type: application/json' -d "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"arkiv_query\",\"params\":[\"$1\",${2:-{\"select\":{\"key\":true,\"attributes\":true},\"limit\":\"0x64\"}}]}"; }
DB1='$owner = addr(0x7Dec34A86907eeF26c70A6578df7bAaE7F223384)'
PI='$owner = addr(0x6E36ec35e3bbec78aC17d0e5bE66FEB829Cf5B7F)'

# 1. How much audit does the org have? (count only)  -> 0x24
aq "org = str('meshydemo.eth') AND ($DB1 OR $PI)" '{"select":{"count":true}}'

# 2. Everything claude-1 did on db1 in a time window (keyset page 1)
aq "org = str('meshydemo.eth') AND agent = str('claude-1') AND resource = str('db1.prod.meshydemo.eth') AND ts BETWEEN u64(1793925000000) AND u64(1793941200000) AND $DB1 ORDER BY ts:u64 DESC"

# 3. Next page: same query, ts below the last row's ts
aq "org = str('meshydemo.eth') AND agent = str('claude-1') AND ts < u64(1793925180000) AND $DB1 ORDER BY ts:u64 DESC"

# 4. Every grant held by claude-1 on any device or service (prefix search over grant names)
aq "org = str('meshydemo.eth') AND grant STARTSWITH str('claude-1.') AND kind = str('session.start')"

# 5. Everything on the home network (prefix search over resource)
aq "org = str('meshydemo.eth') AND resource STARTSWITH str('pi.home.') AND $PI"

# 6. The block/deny funnel: refusals of either kind
aq "org = str('meshydemo.eth') AND (kind = str('blocked') OR kind = str('denied')) ORDER BY ts:u64 DESC"

# 7. Slow kills (anomaly): revokes where the daemon took > 1 s  -> 2 rows (1316, 5002)
aq "org = str('meshydemo.eth') AND kind = str('session.revoke') AND latencyMs > u64(1000)"

# 8. One session, end to end (start, end, revoke, blocks, chunks)
aq "org = str('meshydemo.eth') AND session = str('<session id>') ORDER BY ts:u64 ASC"

# 9. Retention and proof for each revoke: who wrote it, when it expires, read-only flag
aq "org = str('meshydemo.eth') AND kind = str('session.revoke')" '{"select":{"key":true,"owner":true,"createdAt":true,"expiresAt":true,"creationFlags":true},"limit":"0x14"}'

# 10. File ops for one grant (empty today, see finding 2)
aq "arkiv.schema = str('meshy.audit-chunk') AND grant = str('claude-1.rig1.home.meshydemo.eth') ORDER BY ts:u64 DESC"
```

ENS side (viem). This already runs in `ens.ts`; the change is to keep `sender` and `tokenId`, and to add `ExpiryUpdated`:
```ts
const logs = await client.getLogs({
  address: deviceRegistry,              // db1's UserRegistry: its grant children
  events: parseAbi([
    "event LabelRegistered(uint256 indexed tokenId, bytes32 indexed labelHash, string label, address owner, uint64 expiry, address indexed sender)",
    "event LabelUnregistered(uint256 indexed tokenId, address indexed sender)",
    "event ExpiryUpdated(uint256 indexed tokenId, uint64 indexed newExpiry, address indexed sender)",
  ]),
  fromBlock: FROM_BLOCK, toBlock: "latest",
});
// + getBlock(ts) per distinct blockNumber (batch with Multicall-free JSON-RPC batch; cache forever)
```

---

## 4. Filters and search

### 4.1 Facets (one row above every view, shadcn `Combobox` chips)

| Facet | Values come from | Applies to | Arkiv pushdown? |
|---|---|---|---|
| **Agent** | ENS `agents.<org>` children | all | `agent = str()` |
| **Device** | ENS devices | all | `resource = str()` |
| **Network** | ENS networks | all | `resource STARTSWITH` per device, OR-ed (no suffix operator) |
| **Service** | ENS services | sessions, forwards | `grant = str('<agent>.<service>')` |
| **Grant** | `GrantInterval` list | all | `grant = str()` |
| **Granter / revoker** | `LabelRegistered.sender` / `LabelUnregistered.sender` | grants, Gantt | ENS side only |
| **Event kind** | `session.start/end/revoke · blocked · denied · op · grant.*` | timeline, table | `kind = str()` OR-ed |
| **Outcome** | ok · blocked · denied · revoked · expired · failed (exit ≠ 0, G2) | all | `kind`; G1/G2 for the rest |
| **Access mode** | ro · rw · connect | Gantt, matrix, ops | 🔑 client side |
| **Path prefix** | Free text with autocomplete from decrypted policy paths and ops | ops, blocks, matrix | 🔑 client side (paths are sealed) |
| **Op** | open · read · write · create · unlink · rename · readdir · exec · connect · access | ops, blocks | `op = str()` (blocks only; ops live inside chunks) |
| **Time range** | Presets (15 m, 1 h, 24 h, 7 d, 30 d), custom, or "around block N" | all | `ts BETWEEN u64() AND u64()` |
| **Proof** | verified writer · unverified (shown only when toggled) | all | `$owner` clause |

### 4.2 Free text and path search
- **One box, parsed into tokens.** `claude-1 db1 /srv/reports blocked since:1h`:
  - Known ENS labels become facets.
  - A leading `/` becomes a path-prefix filter.
  - `since:` / `until:` / `block:` become time filters.
  - A `0x…` address becomes granter or revoker.
  - Anything left over is a substring match over the decrypted argv, path and reason.
- **Path search over decrypted data** (🔑):
  - Build an in-memory trie of every path seen (policies, ops, blocks).
  - Prefix query, and show the matching subtree in the treemap.
  - Without the org key, the box says "Paths are sealed. Import the org key to search them".
- **The ⌘K palette** (ui-revamp innovation 1) gains "Search activity for …" and "Show access at …" actions.

### 4.3 Saved views
- These ship as defaults, read-only:
  - **Live now**
  - **Refused (24 h)**
  - **Revocations**
  - **Never-used grants**
  - **Slow kills (> 1 s)**
  - **Writes to prod**
- User views are stored in `localStorage` under `meshy.views.<org>` as `{name, url}`. There's no server and no wallet signature needed.
- **Later:** pin a view for the whole org as an ENS text record `meshy.views` on the org (owner only). That's public, so it may only hold facet URLs with no paths.

### 4.4 Shareable URLs
Every view is a pure function of its URL. For example:
```
/org/meshydemo.eth/activity/timeline?agent=claude-1&device=db1.prod&kind=blocked,session.revoke&from=2026-11-06T06:00+05:30&to=2026-11-06T10:30+05:30
/org/meshydemo.eth/activity/access?at=2026-11-06T10:23:54+05:30&path=/srv/reports
/org/meshydemo.eth/activity/session/<sessionId>
```
- **Paths in a URL are a leak,** because URLs end up in logs and chat. So by default `path=` is replaced with `pathh=<sha256(path)[:12]>`, and the client resolves the hash against the decrypted path set. A "Copy link with plain paths" option is offered behind a warning.
- `at=` also accepts `block:12074331`.

### 4.5 "As of time T"
- A **time scrubber**, shared by the matrix, Gantt and reach map, sets `at=` in the URL.
- It snaps to interesting moments: grant created, revoked, expired; first and last session.
- The answer card reads: *"At 10:23:54 IST, 1 agent could read `/srv/reports` on db1: claude-1 (ro, granted by 0x45e8…E0EA at 10:19, revoked in block 12,074,331). 2 wallets could have granted it: the owner and no delegated admin (prod has none)."*

---

## 5. Visualizations

Each entry below says which question it answers, what data it needs, and how it's built. The colour rules (validated with the dataviz checker):
- **Categorical:**
  - light theme: cobalt `#1e3fd8` (read), cyan `#0ea5e9` (list/meta), fuchsia `#c026d3` (write). Direct labels are always on, because cyan is 2.8:1 on white.
  - dark theme: `#5e70f5`, `#0891b2` and `#c026d3`. The CVD ΔE is 6.5, which is legal only with direct labels, so they stay on.
- **Status colours are reserved:**
  - pink/destructive = refused
  - sun/amber = revoked or expiring
  - lime = live
- **Sequential** (heatmap): a single cobalt ramp.

| # | Visualization | Answers | Data | Notes | Mockup |
|---|---|---|---|---|---|
| V1 | **Per-agent activity timeline** | "What did claude-1 do on db1 in the last hour?" | Arkiv sessions, blocks and revokes (✅); ops 🔑 | Swimlanes per device. A session is a bar from start to end. ✕ marks a refusal and ⚡ a revoke. Hover shows argv (🔑), exit and latency. Click opens replay. | [timeline.html](mockups/timeline.html) |
| V2 | **Access Gantt** (grant lifetime vs actual use) | "Which grants were never used? Who granted it, who revoked it?" | `GrantInterval` (ENS) plus sessions (Arkiv) | A light bar for the granted window, solid segments for sessions inside it, ⚡ at the revoke block, a dashed tail when it expired unused. Utilization % per row. | [access.html](mockups/access.html) |
| V3 | **Path treemap and heatmap** | "Which files did it touch, and how much?" | Chunks 🔑 (G5 for byte accuracy), plus blocks | Treemap sized by bytes and coloured by op mix. A cell outlined in pink was refused. Next to it, a heatmap of path × hour. | [files.html](mockups/files.html) |
| V4 | **"Who can reach what" matrix at T** | "Who could read /srv/reports at 14:00?" | `reachAt(T)` | Rows = agents (plus "admins who could grant"); columns = devices and services. Cells: `ro`, `rw`, `connect`, or blank. Path drill-down 🔑. Uses the shared time scrubber. | [reach.html](mockups/reach.html) |
| V5 | **Block/deny funnel** | "What got blocked, and why?" | Arkiv blocked and denied | Stages: connected → identity OK → grant held → path allowed → write allowed. Each drop is a reason bucket (`MESHY_NO_GRANT`, `outside grant`, `read-only grant`, `command not allowed`). Each bucket gets **Grant this**. | [files.html](mockups/files.html) |
| V6 | **Revoke latency distribution** | "How fast is revocation, really?" | `session.revoke.latencyMs`, block; G13 for inclusion | A strip plot (n = 5 today, too few for a histogram) on a log axis, with fail-closed flagged. Later, a stacked breakdown (inclusion, propagation, daemon) per revoke. | [access.html](mockups/access.html) |
| V7 | **Session replay** | "Show me exactly what happened." | session.start (argv 🔑), chunks 🔑, blocks, revoke | A terminal-style transcript reconstructed from events (no keystrokes), plus a scrubber. G7 adds the exec lines. It ends with the kill banner and the latency breakdown. | [replay.html](mockups/replay.html) |
| V8 | **Unused-privilege detection** | "What should I take away?" | `GrantInterval` × sessions × ops 🔑 | Per grant: granted paths vs touched paths (🔑), granted rw vs any write, granted hours vs used hours. Suggests "narrow to `/srv/reports` ro" or "shorten to 30 min". | [access.html](mockups/access.html) (panel) |
| V9 | **Anomaly flags** | "Is anything weird?" | Rules over `InsightEvent` | A list of chips; each links to a filtered timeline. Rules in §5.1. | [timeline.html](mockups/timeline.html) (rail) |

### 5.1 Anomaly rules (start simple, explainable, no ML)

| Rule | Data | Today? |
|---|---|---|
| A revoke where `latencyMs` > 1000, or `reason` contains `fail closed` | Arkiv | ✅ (1316 ms, and 5002 ms fail-closed, on 6 Nov) |
| A session with no `session.end` more than 60 s after its grant ended | ENS × Arkiv | ✅ |
| ≥ 3 refusals in 60 s from one agent (probing) | Arkiv | ✅ |
| A refusal on a path outside every root in `meshy.roots` (looking for secrets) | 🔑 + ENS | ✅ (with the key) |
| Bytes read in a session > 10× the agent's median | G4 | — |
| A grant used for the first time more than 30 min after it was issued (stale grant) | ENS × Arkiv | ✅ |
| A grant issued by someone other than the usual granter for that network | ENS | ✅ |
| Arkiv rows whose `owner` isn't the device's `addr` (spoof attempt, shown in a separate "unverified" lane) | Arkiv + ENS | ✅ |
| Daemon clock skew > 2 s (§3.2 rule 3) | ENS × Arkiv | ✅ |
| `dropped > 0` in a chunk (the audit was under pressure) | G10 | — |

---

## 6. Privacy and security

**What stays encrypted:**
- Paths, argv, exit text, byte counts in payloads, and policy contents. These are sealed to `meshy.orgkey` (and policies to the device key too).
- The **indexed attributes are public by design:**
  - who: agent, grant, resource
  - what kind: kind, op, reason
  - when: ts, block
  - how fast: latencyMs

  Every new indexed attribute (G2–G4, G6, G12) is new public metadata. `bytesR` and `durMs` say how much an agent read and for how long, without saying what. That's decision D3.

**Decryption stays client-side.**
- The org private key is imported in Settings and kept in memory. Today it's the `useMeshy` store, so check it isn't written to `localStorage`.
- Insight views decrypt per row, on demand, and never send plaintext anywhere.
- IndexedDB (option B) stores **ciphertext only**. Plaintext lives in memory and is gone on reload.
- Exports (§7) are generated in the browser.

**Redaction:**
- **Path masking modes** in the UI:
  - full
  - basename only (`…/q3.csv`)
  - hashed (`h:3f9a…`), for screen-sharing during the demo
- **Command redaction:** argv tokens that look like secrets are masked before display and export (`--password=…`, `AKIA…`, `ghp_…`, `sk-…`, long base64 strings).
- **Shareable URLs** carry path hashes by default (§4.4).

**Retention:**
- Arkiv lifetimes are fixed at write time. Entities are read-only, so they can't be extended later.
- The current windows (30 d for blocks and chunks, 90 d for revokes) are sensible. Sessions should become max(grant + 1 d, 30 d) (G14, decision D4).
- Anything longer must be **exported, or summarized into a signed report** (§7.3) before it expires.
- The dashboard should show `expiresAt` as a "kept until" date on every row.

**Delegated admins and the org key:**
- Today there is one org key, so anyone who can see paths can see **all** paths.
- Per-network keys would let `0xf846…` read `home` but not `prod`. That means sealing to `meshy.netkey` on the network as a third recipient: one more envelope recipient (about 100 B per payload) plus a key-distribution step. Decision D2.

**Risks of agents reading their own audit trail:**
1. **Prompt injection through filenames.** Paths and argv are attacker-controlled strings; an agent can create `IGNORE PREVIOUS INSTRUCTIONS.md`. Anything that feeds audit text back to an LLM (`meshy history`, MCP tools, an "explain this session" feature) must:
   - quote it as data
   - truncate it
   - strip control characters

   The dashboard must render paths as text, never HTML (they're XSS vectors).
2. **Learning the policy from refusals.** An agent can map the edges of its grant by probing, and it already gets `allowed=[…]` in `MESHY_BLOCKED`. That's intended, but **probing rates are an anomaly signal** (§5.1).
3. **Hiding activity.** An agent can try to flood ops so `dropOps` evicts its earlier trail. Fixes:
   - G10 (`dropped`, `seq`)
   - never drop `blocked` or `session.*`, which is already the case
4. **Scoping.** An agent's history API must return only its own sessions, served by meshyd, which authenticates the agent's SSH key. It must not depend on the agent holding any org key. The public Arkiv attributes for other agents are visible anyway; that's accepted, since ENS grants are public too.
5. **Plaintext on the device.** meshyd writes plaintext JSONL to stdout, which is fine for the device owner. Docker log drivers and journald ship it elsewhere, so document it in RUNBOOK and add `--audit-log=off|file`.

---

## 7. Exports and integrations

### 7.1 CSV and JSON export (browser, S)
- **Export** on every view writes the **currently filtered** `InsightEvent[]` or `GrantInterval[]`.
- It respects the redaction mode, and includes `arkivKey`, `owner`, `tx` and `block` columns so each row stays verifiable.
- JSON Lines for machines, CSV for spreadsheets.
- The filename carries the org, the view and the time range.

### 7.2 SIEM webhook (M)
Two routes; they're not exclusive.
- **On the device: a meshyd sink** with `--audit-webhook https://siem.example/hook` (plus an HMAC secret).
  - It sends plaintext JSON events before sealing, so this is the device operator's choice.
  - It maps to OCSF classes: File System Activity 1001 (ops and blocks), Authentication 3002 (denied/login), Network Activity 4001 (forwards), Process Activity 1007 (G7 exec).
  - It has its own retry queue, separate from Arkiv.
- **On the provider: a public-metadata relay.** meshy-sync tails Arkiv for the org's verified writers and posts the public attributes plus entity key (no payload) to a webhook, with optional Splunk HEC and Datadog formats. That gives near-real-time alerts on refusals and revokes without key custody.

### 7.3 Signed audit reports anchored on chain (M)
- **Build the report in the browser** for a time range and scope:
  - every entity key in scope, sorted
  - its public attributes
  - `sha256(decrypted payload)` per entity
  - the `GrantInterval`s
  - summary stats
- **Root:** a Merkle tree over `(entityKey, sha256(payload ciphertext))`.
- **Sign and anchor:**
  - The owner or an admin signs EIP-712 `AuditReport{org, from, to, root, count, generatedAt}`.
  - Anchor it as an Arkiv entity `meshy.report`, owned by the signer's address. Give it `MaxLifetime` (~182 d) and read-only.
  - Optionally, also put the latest root in the ENS text record `meshy.audit-root` (one tx).
- **Verify:** anyone recomputes the root from public Arkiv data. A holder of the org key also checks the plaintext hashes. A missing entity changes the root, and with G10 `seq`, gaps are provable.
- **Why it matters:** it outlives Arkiv retention. A PDF or JSON copy plus the on-chain root is the long-term evidence, and fits SOC 2–style controls.

### 7.4 Agent-facing tools (M)
- **CLI.** `meshy history [--resource db1.prod…] [--since 1h] [--json]` returns the agent's own recent sessions, ops and blocks.
  - **Source:** a meshyd SSH subsystem `meshy-history`, served from the ring buffer or the local log, scoped to the authenticated agent and its grants.
  - Arkiv is the fallback, for public attributes only.
- **MCP tools** in `agent/claude-plugin/mcp/server.mjs`:
  - `meshy_history`: the agent's own trail (untrusted text is quoted)
  - `meshy_why_blocked`: the last `MESHY_BLOCKED` line, plus who can grant it (admins of the network from ENS) and a pre-filled approve link
  - `meshy_my_access_at`: what it held at time T, from ENS only
- **Persist the agent's own view.** The CLI appends every `MESHY_*` line it receives to `~/.meshy/events.jsonl`, giving a client-side history with no server.

---

## 8. Roadmap

Effort is for one agent working in this repo: **S** ≤ 2 h, **M** ≈ half a day, **L** ≥ 1 day. Before judging means before Sat 7 Nov 09:00 IST.

### 8.1 Before judging: web only, no daemon change, demoable

| # | Item | Effort | Files | Demo value |
|---|---|---|---|---|
| J1 | `lib/insights.ts`: `GrantInterval` from ENS logs (keep `sender`, `tokenId`; add `ExpiryUpdated`; block timestamps), `InsightEvent` from Arkiv rows, join rules 1–3 | M (2 h) | `web/src/lib/ens.ts` (expose grant events with sender and blocks), `web/src/lib/arkiv.ts` (`orgHistory`: keyset pagination on `ts`, `u64` literals, `count`), new `web/src/lib/insights.ts` | Foundation |
| J2 | **Access Gantt plus the "never used" badge plus granter and revoker** | S (1.5 h) | new `web/src/components/insights/AccessGantt.tsx`, a route `web/src/app/org/[org]/activity/access/page.tsx` (or a tab) | Highest: ENS × Arkiv on one picture |
| J3 | **Revoke latency strip** (5 real points, fail-closed flagged) | S (30 min) | `components/insights/LatencyStrip.tsx` on Activity | The headline claim, shown measured |
| J4 | **Agent timeline plus URL facets** (agent, device, kind, from/to) | S (1.5 h) | `components/insights/Timeline.tsx`, `activity/page.tsx` (tabs) | "What did claude-1 do" |
| J5 | Export JSON/CSV of the current view | S (30 min) | `components/insights/Export.tsx` | Judges can take the data |
| J6 | **Ops: put one Linux GateFS daemon on Arkiv,** so `audit-chunk`s exist (fund rig1's key and restart with `--arkiv`, or run `deploy/meshyd.Dockerfile` with `/dev/fuse`) | S (1 h), no code | runbook only; **coordinate with the deploy/release sessions** | Unlocks file views with real data |

Total: about 6 h of work, or about 4.5 h for J1 to J4. The ui-revamp work is merged, so these go into the new shadcn Activity page. Coordinate with the ui-revamp session so two writers don't touch `activity/page.tsx`.

### 8.2 Next (the week after)

| # | Item | Effort | Files |
|---|---|---|---|
| N1 | Daemon gaps G1, G2, G3, G6, G8, G9, G14 (attributes only, one PR) | M | `meshy/audit/audit.go` (Event: `Cause`, `ExitCode`, `DurMs`, `Chan`, `TokenID`, `PolicyHash`), `meshy/meshyd/server.go` and `forward.go` (emit them; emit revoke on expiry and kill), `meshy/arkivkit/batcher.go` (attributes, `sessionLife`), `arkiv/schema.md`, `docs/INTERFACES.md` §5 |
| N2 | G10 integrity (`seq`, `dropped`, `prevKey`) | S | `arkivkit/batcher.go`, tests |
| N3 | G4 and G5 byte totals | S | `sandbox/gatefs.go` (`Release`), `meshyd/server.go` |
| N4 | "As of T" matrix plus scrubber; port `policy.Check` to TS | M | `web/src/lib/policy.ts` (new), `components/insights/ReachMatrix.tsx` |
| N5 | Session replay | M | `web/src/app/org/[org]/activity/session/[id]/page.tsx` |
| N6 | Path treemap, heatmap and funnel | M | `components/insights/PathTreemap.tsx`, `Funnel.tsx` |
| N7 | IndexedDB cache (option B) | M | `web/src/lib/insights-cache.ts` (`idb-keyval` or raw IDB) |
| N8 | Anomaly rules plus rail | S | `web/src/lib/anomalies.ts` |
| N9 | Saved views plus the search-box parser | S | `web/src/lib/views.ts` |

### 8.3 Later

| # | Item | Effort | Files |
|---|---|---|---|
| L1 | G7 exec capture (Linux eBPF or `cn_proc`; macOS eslogger `exec`) | L | `meshy/sandbox/bwrap_linux.go` (new `exectrace_linux.go`), `eslogger_darwin.go` |
| L2 | `meshy history` plus MCP tools; CLI event log | M | `meshy/cmd/meshy`, `meshy/meshyd/server.go` (subsystem), `agent/claude-plugin/mcp/server.mjs` |
| L3 | Signed audit reports (`meshy.report`) | M | `web/src/lib/report.ts`, `components/insights/Report.tsx`; optional ENS record |
| L4 | SIEM: meshyd webhook sink plus provider metadata relay | M + M | `meshy/audit/webhook.go`, `meshy/provider/relay.go` |
| L5 | Provider entities G15 and G16, plus the provider insights page | M | `meshy/provider/admit.go`, `provider/netbird/reconciler.go` |
| L6 | Provider-side indexer (option C) for aggregates | L | new `meshy/cmd/meshy-index` |
| L7 | Per-network keys (D2) | L | `meshy/policy/envelope.go`, batcher, dashboard sealing, ENS record `meshy.netkey` |
| L8 | G12 HTTP request counts on service forwards | M | `meshy/meshyd/forward.go` |

---

## 9. Decisions needed

| # | Decision | Options | Recommendation |
|---|---|---|---|
| **D0** | Build insight views before judging? | (a) J1–J4 (~4.5 h), (b) J2 + J3 only (~3.5 h including J1), (c) none: show these mockups | **(b)** if time is short. The Gantt and the latency strip tell the ENS × Arkiv story in one screen. |
| **D1** | Where do joins and decryption run? | Browser only · provider indexer on public metadata · org-hosted indexer that holds the org key | **Browser only** now. Add a provider indexer for public aggregates later. Never send the key to the provider. |
| **D2** | Should delegated admins see paths? | All-or-nothing org key (today) · per-network keys | Keep the org key for the hackathon; per-network keys later (L7). |
| **D3** | Which new metadata becomes **public** indexed attributes? | `cause`, `exitCode`, `durMs`, `chan`, `bytesR/W`, `tokenId`, `policyHash`, `seq` | Index `cause`, `chan`, `tokenId`, `policyHash`, `seq`, `dropped`. Keep **bytes** and **exitCode** sealed unless you want them as facets. |
| **D4** | Session retention | Keep it at grant + 1 d · raise it to max(grant + 1 d, 30 d) | **Raise it,** so sessions don't vanish before their file ops. |
| **D5** | Capture commands typed inside shells? | No · exec events (G7) · full TTY recording | **Exec events only.** No keystrokes: they leak secrets and cost a lot. |
| **D6** | Agent self-history | Off · own sessions via meshyd · own sessions via Arkiv public attributes | Own sessions via meshyd, with untrusted-text quoting. |
| **D7** | Fund rig1's Arkiv key, or run a Docker GateFS daemon for the demo? | — | Yes. It's the only way to show real file-level data. **This needs the deploy/release session or you,** since this session doesn't touch infra. |
| **D8** | SIEM route | Device webhook (plaintext) · provider relay (metadata) · both | Both, later. Neither is needed for judging. |

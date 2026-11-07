# Status: done, improved, pending

Last updated Fri 6 Nov 2026, about 11:00 IST, after the provider mesh went live on AWS (NetBird + meshy-sync) and the first revoke over WireGuard. "Verified" means we ran it and saw the result. The evidence is next to each claim.

## Headline results (verified)
- **Revocation on real Sepolia:** a live agent session is killed in the block that revokes the grant.
  - Dashboard revoke by a delegated admin: session dead **1.6 s after the block was mined**, and meshyd reacted **216 ms** after seeing the block (block 12073941, tx `0x6479d2fc…d57c`).
  - Three CLI revokes earlier: 9.46 / 15.75 / 6.82 s from tx sent to session dead. meshyd reacted in 120–192 ms each time; the rest is Sepolia inclusion time.
  - Fork, 12 s blocks, 5 runs: p50 11.71 s, p95 12.19 s.
- **The full lifecycle with a human wallet on Sepolia:**
  1. Delegate the `home` network to `0xf846…Fc98` (EAC).
  2. That wallet adds `rig1` from the dashboard.
  3. It grants `claude-1` read-only `models`. The policy is sealed in the browser and decrypted by rig1's daemon.
  4. The agent reads `models`. `datasets` and writes are `MESHY_BLOCKED`, and the sandbox has no network.
  5. The wallet revokes from the dashboard, and the live session is killed.
- **The provider mesh is live, with ENS enforced on NetBird** (`https://16-4-10-3.sslip.io`, meshy-sync at `https://sync.16-4-10-3.sslip.io`):
  1. `db1` joined as its own WireGuard peer through `meshyd mesh-join` (admitted with the daemon key, then an EIP-712 attestation).
  2. `claude-1` joined as its own peer through `meshy mesh-join`.
  3. The ENS grant became the NetBird policy `meshy:grant:claude-1.db1…` (agent group → db1 group, tcp/22). The default allow-all policy is deleted.
  4. `meshy ssh db1.prod.meshydemo.eth -- ls …/reports` ran **over the mesh**.
  5. An on-chain revoke (block 12074344) killed the streaming session **9.03 s after the tx was sent** (0.75 s after mining). The mesh policy was gone at about 27 s.
- **Slashing has real evidence:** every admission is signed as a `ProviderRegistry` EIP-712 `Attestation`, verified against the deployed contract with `eth_call`:
  - an honest one → `no fraud detected`
  - a tampered one → `bad signature`
  - one for a revoked name → `Slashed(1, 0.01 ETH, "expired at issuance")`
- **Seats enforced at admission:** `meshydemo.eth` uses 4 of 5 paid seats (`GET /v1/seats`). Over the limit, no subscription, or the wrong provider each return 402.
- **Devices and services as names:** `camera.pi.home.meshydemo.eth` → `http 127.0.0.1:8001`. A service grant opens that one port (`meshy curl`) but not a shell.
- **Arkiv:**
  - Live audit from 2 daemons; 12+ batches with 0 failures on the demo org.
  - The owner filter is proven: 5 / 5 / 0 results for no owner, the right owner and a wrong owner.

---

## Done, by component

### meshyd: resource daemon (`meshy/meshyd`, `meshy/cmd/meshyd`)
- [x] **SSH server (gliderlabs).** Auth is split three ways:
  - `Authenticate`: agent identity from ENS (`meshy.ssh` key, `addr`, World ID attestation)
  - `AuthorizeSession`: device grant plus sealed folder policy bound to the grant's token ID
  - `AuthorizeService`: service grant, with the target taken from `meshy.service`
  - Tests: `meshyd/server_test.go`
- [x] **ENS-gated port forwarding.** A `direct-tcpip` channel's destination host is the service's ENS name; meshyd dials the ENS-declared target, and is watched and cut on revoke or expiry. Tests: 3 forward tests, plus a live stream cut in `e2e/services-local.sh`.
- [x] **Revocation watcher** (`enskit/watcher.go`):
  - re-checks every live session's grant on every block with `findExactOwner` plus `getState` (token ID)
  - fails closed on a stale head (45 s) or repeated RPC errors
  - has a local expiry timer
- [x] **Linux sandbox:** bubblewrap plus **GateFS** (a go-fuse loopback that checks every path against the policy and audits ops and blocks). All Linux tests pass in Docker: 9 escape attempts blocked, including symlink and `..`.
- [x] **macOS sandbox:** `sandbox-exec` SBPL profile plus BlockTap (EPERM on stderr becomes an audited block) plus an eslogger monitor for allowed reads (needs root). Tests: `sandbox/seatbelt_darwin_test.go`.
- [x] **Machine-readable agent errors:** `MESHY_BLOCKED path=… allowed=[…] grant=… ask=…`, `MESHY_REVOKED block=N`, `MESHY_NO_GRANT`, `MESHY_NOT_HUMAN_BACKED`.
- [x] **Commands:** `init` (join code), `run`, `status -w` (live view), `sessions`, `kill`, `logs -f`, `doctor`.
- [x] **Docker image:** `deploy/meshyd.Dockerfile` plus an entrypoint that runs first-boot init.

### Agent side
- [x] **`meshy` CLI:**
  - `join`: keygen, approval URL, then waits on ENS
  - `whoami`, `grants`
  - `ssh`, with the host key pinned from ENS `meshy.ssh-host`
  - `connect` (local tunnel to a service) and `curl` (one HTTP request to a service)
- [x] **Claude Code plugin** (`agent/claude-plugin`): a zero-dependency stdio MCP server (`meshy_join/whoami/grants/ssh`), a skill, and `/meshy:join` and `/meshy:status`. The MCP handshake and tools were tested over stdio.
- [x] **`worldid` package:** EIP-191 provider-signed attestation bound to the agent's name, address and an opaque per-org human hash. Verified against the org's `meshy.attestor`. Tests cover tampering, expiry and untrusted signers.

### ENSv2 (the access-control plane)
- [x] **Hierarchy:** org → network → device → service, plus `agents`. Each level is a UserRegistry deployed through VerifiableFactory.
- [x] **Grants are subnames:** `<agent>.<device>` (folders) and `<agent>.<service>` (one port). `roleBitmap = 0`, so a grant can't be moved or renewed by the agent. Expiry is native, and revoke is `unregister`.
- [x] **Delegated admins (EAC).**
  - `grantRootRoles(REGISTRAR|UNREGISTER|RENEW)` on the network, device and service registries.
  - Setter-scoped `grantSetterRoles` lets the admin write only `meshy.policy`. With `--devices`, it can also write the device/service record keys, `addr`, and approve agents.
  - Verified refused on `meshy.orgkey` (fork). Verified live: `0xf846…` is admin on `home` and `agents` and has no rights on `prod`.
- [x] **A Permissioned Resolver per agent.** The approver holds every role; the agent may set only `meshy.ssh`. Verified: the agent is refused on `meshy.worldid` (fork).
- [x] **Tools:** `meshy-admin` (org init with commit/reveal, network/resource/service/agent add, grant, revoke, admin add) and the `enskit` admin helpers.
- [x] **Record audit:** redundant records removed (see [ONCHAIN.md](ONCHAIN.md)).

### Arkiv (audit log)
- [x] **Raw-transaction writer** (`arkivkit`): `execute((uint8,bytes)[])` with the create op encoded as a single tuple. Verified against `cast` and live Rabdi.
- [x] **Batcher:**
  - Session and blocked entities are written individually; ops are rolled into per-session gzip chunks.
  - Payloads are sealed to `meshy.orgkey`.
  - Urgent flush on a block or revoke, and retry on failure.
- [x] **Per-type lifetimes:** blocked 30 d, chunks 30 d, sessions = grant expiry + 1 d, revocations 90 d.
- [x] **Reads that can't be spoofed:** the dashboard and menu bar filter by `$owner` = the device daemon's addresses from ENS.
- [x] **Prize files:** `arkiv/schema.md`, `friction.md`, `reach.md`, `why-arkiv.md`, `how-you-use-arkiv.md`.

### Provider (`meshy/provider`, `meshy/cmd/meshy-sync`, `deploy/provider`)
- [x] **meshy-sync HTTP API:**
  - `/v1/health`
  - `/v1/agent/device-flow/start|poll` (World IdP OIDC device flow, with ID-token validation via JWKS)
  - `/v1/agent/attest-dev` (behind `--dev-attest`)
  - `/v1/map` (org tree from ENS logs)
  - `/v1/admit`: EIP-191 by the name's addr record (daemon or agent key) or owner. It checks the name is live and the seats (`seatsOf`, 402), then returns a one-off setup key into the name's group plus a signed **EIP-712 attestation** (`provider/admit.go`).
  - `/v1/attestations` (slashing evidence, persisted to `attestations.jsonl`) and `/v1/seats`.
  - `/v1/map` **classifies** device children: services (those with a `meshy.service` record) are listed under their device, and each grant carries `kind`, `agent`, `target` and `device`.
- [x] **NetBird reconciler, running live** (`provider/netbird/reconciler.go`, every 10 s per `--orgs`):
  - one group per ENS device or agent, with the peer matched by name
  - one tcp/22 policy per live grant
  - deletes the allow-all `Default` policy
  - evicts peers whose ENS name is gone (after 2 passes)
  - tests: 4 tests against a fake NetBird, including the real API's object-shaped `sources`
- [x] **Deployed on AWS** (EC2 t3.medium, ap-south-1, elastic IP 16.4.10.3):
  - NetBird's official `getting-started.sh`: combined server, embedded IdP, Traefik with Let's Encrypt
  - meshy-sync on NetBird's network at `sync.<domain>`
  - reproducible with `deploy/provider/bootstrap.sh`
  - provider #1's on-chain endpoint updated to the live URL (tx `0x636a7150…`)

### Contracts (`contracts/`, Sepolia)
- [x] **`ProviderRegistry`:** stake ≥ 0.01 ETH, unbond and withdraw, EIP-712 `slash()` fraud proof checked against ENSv2 on-chain, 50% to the reporter.
- [x] **`MeshySubscription`:** per-seat USDC with escrow and claim, prorated `addSeats`, renew, and cancel with a refund.
- [x] 31 forge tests pass. Both contracts are verified on Sourcify, Blockscout and Routescan. Demo provider #1; `meshydemo.eth` has 5 seats.

### Dashboard (`web/`)
- [x] **Access** (home): every grant as a line (agent → target, scope, countdown, Revoke), **Give access**, and "Happening now" from Arkiv.
- [x] **Directory:** networks → devices (kind and metadata) → services, plus agents. Add network, device (from a join code) and service; invite an agent.
- [x] **Activity:** full Arkiv feed, live sessions, kill-latency stat, decryption with the org key, and a runnable query.
- [x] **Billing, Provider** (registry, register, fraud-proof form), **Settings** (org key import, delegated admins).
- [x] **Approve page:** World device flow or dev attestation, then a per-agent resolver plus a setter role.
- [x] **In-browser policy sealing** (`lib/envelope.ts`), **verified byte-compatible with Go in both directions.**
- [x] **Write path verified with a real wallet:** add device, grant, revoke.
- [x] Server-side RPC proxies keep keys off the client.

### macOS menu bar (`mac/`)
- [x] Shows agent identities on this Mac, live grants with countdowns, local meshyd sessions and Arkiv blocks/revocations (with notifications). Packaged as `Meshy.app` (LSUIElement).

### Mesh transport (`meshnet/`)
- [x] `meshy-mesh serve | proxy | status | keygen` on NetBird `client/embed` (userspace, no root). **Verified on the live provider.**
  - The WireGuard key is seeded into NetBird's `config.json` before the first join, so its public key is what gets signed in `/v1/admit`.
  - The peer name is the ENS name with dashes.
  - `proxy` resolves an ENS name to the mesh IP of the peer admitted under it.
- [x] `meshyd mesh-join` (admit with the daemon key, then exec `meshy-mesh serve`) and `meshy mesh-join` (the agent becomes its own peer; `ssh`/`connect`/`curl` then dial over the mesh). The shared client is `meshy/meshadmit`.

---

## Improved (what changed after first versions, and why)
| Area | Before | After | Why |
|---|---|---|---|
| ENS resolver reads | `text()` / `addr()` calls | ENSIP-10 `resolve(name, data)` | ENSv2 PermissionedResolver has no direct getters, so those calls reverted |
| Auth model | Login required a device grant | Identity is authenticated at login; each session or forward is authorized separately | Agents with only a service grant must be able to connect |
| Arkiv create encoding | Five flat ABI params | A single tuple | The node reverts flat params with `NonCanonicalOperationData` |
| Arkiv reads | Filtered by `org` attribute only | Also `$owner` ∈ device daemons from ENS | Anyone could write fake entities with our org tag |
| Arkiv lifetimes | Everything 30 d | Per type (session = grant + 1 d, revoke 90 d) | Expiry should match what the data is for (a judging criterion) |
| Priority fee | Suggested tip × 2 | Clamped to 1–10 gwei | On quiet chains the suggestion reflects our own transactions, so the fee compounded and drained 100 ETH on a fork |
| Dashboard loading | 144 RPC requests, about 40 s | 13 requests (Multicall3 + parallel node reads + one `getLogs` per registry), about 10 s, then cached | Rate limits and sequential depth |
| `eth_getLogs` | Through Alchemy | Routed to the public node by the `/api/rpc` proxy | Alchemy's free tier caps logs at a 10-block range |
| UI | Tailscale-like pages plus a React Flow graph | Access-first (Access / Directory / Activity) | It was cluttered and looked like an imitation |
| On-chain records | `meshy.owner`; service `kind/url/description`; org `description` | Removed | They duplicated what ENS (owner) and `meshy.service` already record |
| Mesh in meshyd | `client/embed` inside meshyd | Separate `meshy-mesh` module | NetBird and go-ethereum need incompatible `pion/stun/v3` |
| Linux `/proc` | `--proc /proc` always | Falls back to an empty `/proc` when Docker masks procfs | Never bind the container's real `/proc`, which would expose meshyd's environment |
| Write gating in the UI | Owner-only buttons | Any connected wallet; the chain enforces roles | Delegated admins must be able to act |

---

## Pending, by priority
### P0: needed for a credible submission
1. **World ID Agent Pass for real.** It needs `WORLD_CLIENT_ID/SECRET` from `sandbox.auth.world.org/portal` (a human must do this). Then:
   - run `meshy-sync --world-client-id … --world-client-secret …` without `--dev-attest`
   - approve an agent via `/approve` and record the success path, plus a denied or expired path (`MESHY_NOT_HUMAN_BACKED`)
   - write the integration debrief (time to first success, friction, missing docs, one improvement)

   The current attestation for `claude-1` is a **dev signature**.
2. **Public repo + live demo URL + video.**
   - ~~Make the repo public~~ **done** (6 Nov).
   - ~~Deploy `web/`~~ **done:** https://meshy-bay.vercel.app (Vercel project `meshy`, deployed from the CLI, so a push doesn't redeploy). It uses a public Sepolia RPC; the Alchemy and Arkiv keys aren't uploaded. meshy-sync `--cors` allows that origin.
   - **UI revamp merged** (ETHGlobal style, shadcn): plan and mockups in `docs/ui-revamp/`, the new UI in `web/src`, and a native Mac app in `mac/` that loads the live dashboard. The new UI is live at https://meshy-next.vercel.app. Still to check: a live grant and revoke through the new UI on Sepolia.
   - The video should be 2:30–3:00. `e2e/live-demo.sh` is the script.

### P1: depth gaps
3. ~~NetBird mesh~~ **done**: live on AWS, revoke verified over WireGuard. What's left:
   - only `db1` and `claude-1` are joined; pi and rig1 are not
   - a cross-machine run (agent on another host)
   - `meshy-mesh proxy` starts a fresh embed client per connection (about 8–25 s to connect). A long-running local agent peer would make that instant.
4. ~~Admission attestations~~ **done**. A **real `slash` tx** hasn't been sent (only simulated) because it would burn the demo provider's stake. If wanted, send one against a throwaway provider.
5. ~~Seat enforcement~~ **done**, but the 402 path is unit-logic only and hasn't been triggered live: the org is at 4/5 seats.
6. **People and devices for humans** (`alice.people.<org>`, `meshy.wg` keys) aren't implemented. Only agents and resources exist.
7. ~~Classify services in `/v1/map`~~ **done**; the menu bar now shows service grants (🔌, copies `meshy connect`). The dashboard still reads ENS directly, so it isn't affected.

### P2: polish and verification
8. **The Claude Code plugin** has never run inside a real Claude Code session (only stdio tests).
9. **macOS eslogger** read-audit has never run live (needs root and Full Disk Access).
10. **Simulation suite from the plan:** toxiproxy RPC chaos, provider kill, clock skew, load tests. Not done; fail-closed is unit-tested only.
11. **Menu bar:** no wallet sign-in or device registration; it's status-only. Privy isn't integrated (browser wallet only).
12. **Dashboard:** no org-creation wizard (`meshy-admin org init` only). The Activity page polls instead of using `watchEntityEvents`.
13. **Teams** (`eng.teams.<org>`) skipped.
14. **The Arkiv owner filter for `rig1`:** rig1's daemon runs with `--arkiv ""` (unfunded key). Fund it and restart with Arkiv to include it.

## Running processes on the dev Mac (at time of writing)
See [RUNBOOK.md](RUNBOOK.md#what-is-running).

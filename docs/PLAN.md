# Meshy: ETHGlobal Mumbai build plan

## Context
Meshy is a decentralized, agent-friendly alternative to Tailscale and Twingate, built by jashwanth0712, Ronnieraj37 and aizahs31.

- **First users:** dev teams that give AI agents access to their infrastructure. Claude Code is the first agent we support.
- **Source of truth:** ENSv2 (Sepolia). It records who and what is on an org's network, and which folders each agent may touch.
- **Revocation:** revoking a grant, or letting it expire, kills the agent's live SSH session within 15 seconds.
- **Providers:** staked providers run tunnel infrastructure forked from NetBird. Org owners pay them per seat. Anyone can self-host a provider for free.
- **Audit:** every file an agent reads or writes, and every blocked attempt, is logged to Arkiv.

**Deadline:** Saturday 7 Nov, 09:00 IST, with no late entries. Planning started Friday 01:45 IST, which leaves about 31 hours.
- The demo video must be 2–4 minutes. We aim for 2:30–3:00.
- All three members build their lanes in parallel. There are integration checkpoints at H+10 and H+20.
- **ETHGlobal rules:** work must start after the event begins. The NetBird fork counts as a library, so the README must say clearly what is new. The repo needs steady commits. We must disclose AI use and commit this plan to `docs/`.

### Prize targets (3 partner picks)
| Partner | Track | Qualification checklist |
|---|---|---|
| **ENS** (main) | Best Use of ENSv2, $7k (1st place $3k) | ENSv2 on Sepolia is central to the product: hierarchical subregistries; Enhanced Access Control (EAC) for delegated admins; per-member Permissioned Resolvers; expiring, revocable, non-transferable grants; **agents as namespaces** (a bonus point they named); ENSIP-26 agent records. No hard-coded values. Live link, open source. |
| **World** | Agent Pass, $5k; IDKit, $2.5k (stretch) | Agent Pass is the World IdP OIDC **device flow** at auth.world.org (sandbox). The flow runs request → human approves → backend validates the ID token → agent is admitted. A **denied or expired path** blocks admission. All validation is on the backend. Submit an integration debrief. |
| **Arkiv** | Build on Arkiv, $4k ($3k) | The daemon writes to Rabdi testnet and the dashboard **reads it back** (live audit and blocks feed). README lists writer addresses and one runnable query. Files: `/arkiv/{schema,friction,reach,why-arkiv,how-you-use-arkiv}.md`. |

- **Payments:** USDC on Sepolia (Circle Sepolia USDC or the ENSv2 MockUSDC). This doesn't target a payment prize.
- **Dropped:** Whitechain (USDT is hard to obtain, and it needs a drop-in widget) and EF CROPS (the "no trackers" rule conflicts with Privy).

## Key research findings that shape the design
**ENSv2 Sepolia** (addresses from docs.ens.domains/learn/deployments; don't use the stale `main` branch):

| Contract | Address |
|---|---|
| RootRegistry | `0xb458…c43f` |
| ETHRegistry | `0xd4eb…a8b4` |
| ETHRegistrar | `0xf633…d3d3` |
| VerifiableFactory | `0xda70…2991` |
| UserRegistryImpl | `0x9bd8…366a` |
| PermissionedResolverImpl | `0x115e…2b2c` |
| UniversalResolver proxy | `0xeEeE…EeEe` |
| UniversalHelper | `0xd453…fdfe` |
| MockUSDC | `0x240b…14aa` (public `mint`) |

- A 5+ character `.eth` costs about 8 MockUSDC/yr. Registration takes a 60 s commit, a 28-day minimum duration, and `register`.
- UserRegistry subnames accept **any future expiry**, so 1 hour is fine.
- **A plain expiry emits no event.** The daemon needs its own timers, driven by block timestamps.
- `unregister` emits `LabelUnregistered`, but **resolver records persist**. A re-registered label would serve the old policy again. So we write the policy fresh at grant time and bind it to the grant's tokenId.
- Resolver roles cover every name on that resolver, not one name. So each member gets their **own resolver** for their devices, and org-controlled records sit on the org resolver.
- Each role allows at most 15 holders per resource, which caps delegated admins at 15 per registry.
- Token IDs regenerate on role changes, so always key by labelhash.
- ensjs ≥5.2.5 `wallet/v2` handles writes (`createSubname`, `deploySubregistry`, `grantRoles`, `setRecords`, …).
- In Go there is no v2 write SDK, so we use `abigen` bindings from the contracts-v2 ABIs.
- Gas per operation: register about 450k, unregister about 100k, first setText about 300k, later setText about 50k.

**NetBird (v0.82, Go 1.26.10):**
- `management/`, `signal/`, `relay/`, `combined/` and `proxy/` are AGPL. `client/*` (embed, ssh, ui) is BSD-3.
- We bootstrap with no IdP: `NB_SETUP_PAT_ENABLED=true` → `POST /api/setup` returns a personal access token.
- **Custom DNS zones** (`/api/dns/zones/{id}/records`) push `db1.prod.acme.eth → 100.x` to every peer. That gives working `ssh db1.prod.acme.eth` with no custom resolver.
- **`POST /api/peers/{id}/temporary-access`** pre-registers an agent's WireGuard pubkey with a policy to one target. The agent then runs `embed.New(Options{PrivateKey})`, which is how `proxy/` does it.
- Policy pushes reach peers in under 2 s. Removing a peer **does not reset open TCP connections**, so meshyd must kill sessions itself.
- `client/ssh/server` has no public-key auth hook and no command-wrap hook. We write our own ~300-line `gliderlabs/ssh` server on `embedClient.ListenTCP(":22")`. Peer identity comes from `IdentityForIP`.
- The Mac UI is Wails v3 beta plus React. It talks gRPC to a root launchd daemon over `unix:///var/run/netbird.sock`. No Network Extension is needed.

**Sandboxing:**
- **Linux:** bwrap in Docker needs `seccomp=unconfined`, `apparmor=unconfined`, `--device /dev/fuse`, `CAP_SYS_ADMIN`, and the flags `--new-session --die-with-parent`. go-fuse loopback with a wrapped node and file gives a per-op log, and the caller PID is available.
- **Mac:**
  - `sandbox-exec` works on 15.7 (deprecated, but works) with an SBPL profile of `deny default` plus approved subpaths.
  - `eslogger` (root plus Full Disk Access; events open, close, rename, unlink, create, exec, fork) emits JSONL that we filter by session process tree.
  - Denials come from `log stream --predicate 'sender=="Sandbox"'`.
  - Fallback: run meshyd Linux inside Lima.

**World Agent Pass:**
- Device authorization lives at `https://sandbox.auth.world.org/api/v1/device_authorization`.
- Poll the token endpoint, then validate the RS256 ID token against JWKS. Check `iss`, `aud`, `exp`, `auth_time` (must be after the request), and `acr`/`amr`.
- The `sub` is pairwise. Device codes last 20 minutes and are single-use.
- Testing uses the sandbox World App with "manual verify", which takes **about 1 hour to activate. Start it at H0.** We also keep locally-signed fixture tokens as a fallback demo.

**Arkiv (Rabdi):**
- Chain 7794095, `https://rpc.rabdi.db-chain.testnet.arkiv.network` (wss also available), 2 s blocks, tGLM faucet, access key from hub.arkiv.network.
- There is no Go SDK. We write raw transactions to `0x44…44` `execute((uint8,bytes)[])`. The TS SDK is `@arkiv-network/sdk` 1.0.1.
- Every entity requires an expiry. Up to 32 attributes; `str` ≤128 B, `bytes` ≤128 KiB. Everything is public, so we encrypt paths.
- `$createdAt` can't be sorted on, so we store our own `ts:u64`. No `NOT`/`!=` operators on Rabdi.

**Privy:**
- The device flow for CLIs and agents fits the Mac app. The web uses `@privy-io/react-auth`.
- Go verifies ES256 JWTs with the per-app key.
- Session signers plus policies (allowed `to` = ENS contracts, chain = Sepolia) let the Mac app sign ENS writes for the user.

## Architecture

### ENSv2 namespace
```
acme.eth                                   org root. Owner wallet holds the 2LD. resolver = org resolver
│  text meshy.provider = <providerId>      text meshy.orgkey = org X25519 pubkey (for encrypted policies)
├─ prod.acme.eth                           NETWORK (UserRegistry). Delegated admins hold root
│  │                                       REGISTRAR|UNREGISTER|RENEW via EAC grantRootRoles
│  └─ db1.prod.acme.eth                    RESOURCE. addr = daemon key; text meshy.wg, meshy.ssh-host, meshy.roots
│     ├─ claude-1.db1.prod.acme.eth        AGENT GRANT. owner = agent addr; roleBitmap = 0 (it can't change or move itself)
│     │                                    expiry = now+1h; text meshy.policy = ECIES-encrypted
│     │                                    {grantTokenId, paths:[{p:"/srv/reports",m:"ro"}], cmds?}
│     └─ alice.db1.prod.acme.eth           human grant (or network-wide alice.prod.acme.eth)
├─ people.acme.eth
│  └─ alice.people.acme.eth                MEMBER. resolver = alice's own PermissionedResolver
│     └─ mbp.alice.people.acme.eth         DEVICE. text meshy.wg = WireGuard pubkey (written by alice)
└─ agents.acme.eth
   └─ claude-1.agents.acme.eth             AGENT IDENTITY. Reserved at invite (LabelReserved), promoted on join.
                                           addr = agent key; meshy.wg, meshy.ssh, meshy.owner = alice.people.acme.eth,
                                           meshy.worldid = provider-signed attestation; ENSIP-26 agent-context
```
**Rules:**
- **Access is a name.** A grant is a subname under the resource. Revoking it is `unregister`, and the 1-hour default limit is native ENSv2 expiry.
- Agents are **deny-by-default**: they get resource-level, path-scoped grants only. People can get network-level grants.
- Names are public. Folder paths and commands are encrypted to `{daemon key, org key}`.

### Components (monorepo `jashwanth0712/meshy`, AGPL-3.0)
```
netbird/            fork of netbirdio/netbird (management, signal, relay, client, ui). meshyd and the CLI are built inside it
  meshy/            new Go code inside the fork module, so replace directives keep working
    sync/           meshy-sync (provider sidecar)
    meshyd/         resource daemon
    cli/            `meshy` CLI for agents and humans
    enskit/         abigen bindings + indexer + namespace helpers (shared)
    arkivkit/       Arkiv raw-tx writer + entity encoders
    policy/         policy schema, ECIES, sandbox compilers (bwrap profile, SBPL)
contracts/          Foundry: ProviderRegistry (stake/slash), MeshySubscription (USDC seats)
web/                Next.js dashboard (Privy, viem, ensjs v2, Arkiv TS SDK, react-flow)
agent/claude-plugin/  Claude Code plugin: MCP server (TS) wrapping `meshy` CLI + skill + slash commands
deploy/             provider docker-compose, meshyd Dockerfile, launchd plist, e2e compose
e2e/                scenario runner + simulation harness
docs/               this plan, ENS schema, API spec, AI-use disclosure
arkiv/              prize markdown files
```

1. **Provider = NetBird `combined` + meshy-sync.** It runs on a VPS with a domain and TLS (Traefik compose generated by `infrastructure_files/getting-started.sh`). meshy-sync does the following:
   - **ENS indexer.** One Sepolia websocket subscribes to `newHeads` and to logs from the org's registries and resolvers. Watched events: `LabelRegistered`, `LabelReserved`, `LabelUnregistered`, `ExpiryUpdated`, `SubregistryUpdated`, `ResolverUpdated`, `EACRolesChanged`, `TextUpdated`, `Linked`, plus `VerifiableFactory.ProxyDeployed`. It builds an in-memory tree, with expiry evaluated against the latest block timestamp.
   - **Compiler.** It turns ENS state into NetBird objects through the REST API:
     - Groups: `net:<network>`, `res:<resource>`, `agent:<name>`.
     - Policies: humans get network → resources; agents get tcp/22 to the granted resource only.
     - DNS zone records for each network.
     - `temporary-access` peers for agents.
     - Peer deletion when a device or agent name dies.
   - **HTTP API** (`/v1`):
     - `POST /admit`: SIWE or Privy JWT, plus the WireGuard key, which must match `meshy.wg`. Returns a one-off setup key.
     - `POST /agent/device-flow/start|poll`: the World Agent Pass proxy, a confidential client. The `client_secret` never leaves the provider.
     - `POST /agent/attest`: returns a provider-signed `meshy.worldid` attestation over `(agentAddr, H(iss,sub), auth_time)`.
     - `GET /map`: live topology for the dashboard.
     - `GET /health`.
   - **Seat enforcement:** paid seats in `MeshySubscription` must cover the active ENS seats. Over-limit admissions return 402.
   - **Attestations:** every admission is signed as `(peerKey, name, resourceVersion, validUntil)`. A slashing challenge can use them as evidence.
2. **meshyd (per-resource daemon).** Linux runs it in Docker; Mac runs it under launchd as root.
   - It joins the mesh through `embed.New(SetupKey)`, with `BlockInbound=false`.
   - **SSH server** (`gliderlabs/ssh`) listens on `ListenTCP(":22")`. The `PublicKeyHandler` maps a key to an agent name through ENS `meshy.ssh` and cross-checks the source IP with `IdentityForIP`. It then requires a live grant and a valid `meshy.worldid` attestation for agents, and decrypts the policy. Port forwarding is disabled. SFTP goes through `pkg/sftp` RequestServer with per-op logging.
   - **Linux sandbox:** each session runs `bwrap --new-session --die-with-parent --unshare-all --share-net=no` over a minimal read-only rootfs (`/usr`, `/bin`, `/lib`). Each approved path is bind-mounted from a per-session **go-fuse loopback** mount (ro or rw enforced in FUSE) that logs `open`, `read`, `write`, `readdir`, `unlink` and `rename`. cgroup limits cap pids, memory and CPU.
   - **Mac sandbox:** each session runs `sandbox-exec -p <SBPL>`: `deny default`, allow system read paths, and allow approved subpaths ro or rw. `eslogger open close create rename unlink exec fork` is piped into a filter keyed on the session's process tree. Denials come from `log stream` with the Sandbox predicate.
   - **Revocation watcher:**
     - It has its own websocket subscription and per-grant timers set to `expiry`, checked against the block head timestamp.
     - **Ancestor watch:** if the resource, network or agent name dies, all grants under it die too.
     - On revoke it sends SIGKILL to the process group, closes the SSH conn, unmounts FUSE, and emits `meshy.session` revoked with the measured latency.
     - **Fail-closed:** if the chain head is older than 45 s or the websocket stays down for more than 30 s, it kills all agent sessions.
   - **Arkiv writer:** batches flush every 5 s or at 100 events, as one `execute` transaction.
   - **CLI/TUI:** see the UI/UX section.
3. **Mac menu-bar app.** A fork of `client/ui` (Wails v3 plus React), rebranded as Meshy.
   - Sign-in uses the Privy device flow, which opens the browser at `web/approve`.
   - A device key is generated and `mbp.alice.people.acme.eth` is published through a Privy session signer (policy limited to ENS contracts on Sepolia).
   - The app calls `/admit`, then the daemon gRPC `Login{setupKey}` and `Up`.
   - It shows the network map, agents on this Mac, and blocks.
4. **`meshy` CLI and the Claude Code plugin.**
   - The CLI runs the agent as its **own peer** (embed with PrivateKey via temporary-access) and provides `meshy ssh <ens-name> -- <cmd>` (`ProxyCommand` style).
   - The plugin's MCP server exposes `meshy_join`, `meshy_status`, `meshy_ls`, `meshy_ssh`, `meshy_blocks`, plus a skill that teaches Claude to prefer `meshy ssh`.
   - Plain `ssh db1.prod.acme.eth` also works through the human's Mac DNS. That path only works when the human's device has a grant.
5. **Contracts (Foundry, Sepolia).**
   - **`ProviderRegistry`:**
     - `register(endpoint, pubkey, pricePerSeat)` payable, with a minimum stake.
     - `unbond` with a delay.
     - `slash(attestation, sig)` succeeds if, on-chain via `UniversalHelper.findParentRegistry` and `getExpiry`/`getResource`, the name was already expired or unregistered at `attestation.issuedAt`, or `resourceVersion` ≠ the current version (covers re-registration). The reporter gets 50% and the rest is burned.
     - Providers are mirrored as `<id>.providers.meshy-net.eth` (name TBD).
   - **`MeshySubscription`:**
     - `subscribe(orgNode, provider, seats, months)` takes a USDC `transferFrom` split between the provider and a 5% Meshy treasury fee.
     - Also `addSeats`, `cancel` (prorated refund), and `seatsOf(orgNode)`.
6. **Web dashboard** (Next.js, shadcn/ui, viem, ensjs v2, Privy, Arkiv SDK). Screens are listed below.

## UI/UX
**Principles:**
- The ENS name is the primary identifier everywhere, in monospace with a copy button.
- Every on-chain object has a chip linking to Sepolia Etherscan or the ENS app, and Arkiv entities link to the Rabdi explorer.
- Each write shows its tx steps as a progress stepper. "Pending on Sepolia… ~12s" states are explicit.
- The UI uses a dark theme, and is minimal and terminal-flavoured.
- Every error is actionable for **both humans and agents**.

### Web dashboard (`web/`)
1. **Onboarding wizard:**
   - Sign in with Privy (wallet or Google).
   - Pick an org name: use an existing Sepolia `.eth` or register one. The wizard mints MockUSDC → commit → 60 s countdown → register.
   - Choose a provider from a card list read from `ProviderRegistry`: stake, price per seat, region, uptime from `/health`, and a "self-host (free)" option.
   - Pick seats and pay USDC.
   - **Create namespace:** one batched stepper deploys the subregistries and the org resolver and creates `prod`, `people` and `agents`.
2. **Network map (home):**
   - react-flow graph of org → networks → resources; people → devices; agents.
   - Each grant edge shows a **countdown ring** for its time left.
   - Live sessions pulse green. A revoke flashes red, then the edge disappears.
   - A side panel shows the selected node's ENS records, decoded.
3. **Resources:**
   - List with daemon status (online, chain lag, active sessions).
   - "Add resource" generates a `meshyd init` command and a one-time join code. An admin approves it, which registers `db1.prod.acme.eth` with the daemon key.
4. **Grant access drawer:**
   - Choose who: an agent or person, with search over `agents.`/`people.` names.
   - Choose the resource, then pick folders from the daemon's advertised roots tree (`meshy.roots`) and set ro or rw per path.
   - Choose a duration: 15m, **1h (default)** or 4h; agents are capped at 4h.
   - A preview reads: "Creates `claude-1.db1.prod.acme.eth`, expires 14:32 IST, policy encrypted to db1 + org".
   - Confirm sends one multicall transaction.
5. **Revoke:**
   - The button opens a confirm step, then a **live revocation timeline**: tx sent → mined (block #) → daemon killed session (Δt) → peer removed → Arkiv `revoked` entity.
   - It ends with "**Access revoked in 11.8 s**". This is the headline demo moment.
6. **Agents:**
   - Each agent card shows its World "human-backed" badge (attestation present and valid), its owner human, ENSIP-26 records and active grants.
   - Invite: reserve `claude-1.agents.acme.eth` (`LabelReserved`), then get a magic link plus an email draft.
7. **Activity (reads from Arkiv):**
   - Live feed via `watchEntityEvents`, filterable by agent, resource or allowed/blocked.
   - A **Blocks** tab.
   - Each row has a "View on Arkiv" link and a copyable runnable query.
   - Encrypted chunks are decrypted client-side with the org key.
8. **Billing:** seats used vs paid, provider, renewal date, add seats, cancel.
9. **Provider console:** stake, register, endpoint health, earnings, and slashing events. It also has a "challenge a provider" form that submits an attestation as a fraud proof.
10. **Approve page (`/approve?code=`)** serves both the Mac app and agent joins:
    - It shows the requesting device or agent, its key fingerprints and the requested name.
    - For agents, it embeds the World Agent Pass step (QR code or user code) and then an **Accept invite** button, which sends the ENS promote and records tx.

### Mac menu-bar app
- **Signed out:** "Sign in to Meshy", which opens the browser approve page.
- **Signed in, top to bottom:**
  - Header: `acme.eth`, a connection dot, and the provider name.
  - **This Mac:** `mbp.alice.people.acme.eth` and its mesh IP.
  - **Networks:** a toggle per network.
  - **Resources:** each row copies `ssh db1.prod.acme.eth` or opens Terminal.
  - **Agents on this Mac:** e.g. `claude-1 → db1 (42m left)` with a Revoke button.
  - **Recent blocks (3).**
  - Open dashboard, Preferences and Quit.
- **Notifications:** "claude-1's access to db1 was revoked; session terminated in 11.8 s", and "Blocked: claude-1 tried /etc/shadow on db1".

### Headless daemon (`meshyd`) and provider CLI
- `meshyd init --name db1.prod.acme.eth --provider <id> --roots /srv` prints a join code and a QR code for an admin to approve in the dashboard.
- `meshyd status` is a live TUI (bubbletea) showing:
  - identity, mesh IP, provider and chain head lag
  - active sessions with a countdown, agent, paths and op counts
  - the last 10 blocks
- `meshyd sessions`, `meshyd kill <id>`, `meshyd logs -f`, `meshyd policy show` (decrypted locally).
- `meshyd doctor` checks FUSE and bwrap, eslogger Full Disk Access, websocket RPC, clock skew and the Arkiv key.
- Docker one-liner: `docker run -d --device /dev/fuse --cap-add SYS_ADMIN --security-opt seccomp=unconfined --security-opt apparmor=unconfined -v /srv:/srv -e MESHY_NAME=db1.prod.acme.eth ghcr.io/jashwanth0712/meshyd`
- `meshy-provider up | register --stake 0.05 | status`

### Agent UX (Claude Code)
- **Join:** `/meshy:join <invite-link>`. Claude prints "Open this link to approve me: … (code ABCD-1234)", the browser opens the approve page, and the human completes World ID and accepts. Claude then says "Joined as `claude-1.agents.acme.eth`".
- **Use:** `meshy ssh db1.prod.acme.eth -- cat /srv/reports/q3.csv`, or the MCP tool `meshy_ssh`.
- **Agent-friendly errors** are one line, machine-parseable:
  - `MESHY_BLOCKED path=/etc/shadow allowed=[/srv/reports:ro] grant=claude-1.db1.prod.acme.eth expires=14:32 ask=alice.people.acme.eth`
  - `MESHY_REVOKED grant=… at block 9123456; session terminated`
  - `MESHY_NOT_HUMAN_BACKED: complete World Agent Pass via /meshy:join`

## Demo script (2:30–3:00)
1. 0:00 — Problem in one line; the network map is on screen.
2. 0:15 — Invite `claude-1`. In Claude Code, run `/meshy:join`, approve with World Agent Pass, and `claude-1.agents.acme.eth` appears.
   - Also show the **denied path**: a second agent that cancels World verification gets `MESHY_NOT_HUMAN_BACKED`.
3. 0:50 — Grant ro `/srv/reports` on `db1.prod.acme.eth` for 1h. Show the ENS app with the subname and its expiry.
4. 1:10 — Claude reads the report successfully, then tries `/etc/shadow` and `../secrets`. Both return `MESHY_BLOCKED`, and the Arkiv feed updates live.
5. 1:40 — Revoke during a long-running command (`tail -f`). The revocation timeline ends at "revoked in ~12 s" and Claude receives `MESHY_REVOKED`.
6. 2:15 — Architecture slide: ENSv2 = control plane, staked providers, Arkiv = audit. Close with the runnable Arkiv query and the repo link.

## Parallel lanes (3 people) and timeline
H0 = start. All three lanes run at the same time.

- **H0–H1 (everyone):**
  - Create the repo and invite collaborators. Fork NetBird.
  - Freeze **interface contracts** in `docs/`: ENS schema and record keys, the policy JSON plus ECIES format, Arkiv entity schema, the meshy-sync HTTP API, and the error strings.
  - Accounts: **World sandbox identity (1 h activation, start now)**, World IdP sandbox client, Arkiv access key and tGLM, Privy app, Sepolia ETH, MockUSDC, a VPS with domain, and a Sepolia websocket RPC (Alchemy/Infura).
- **Lane A: contracts, ENS kit and dashboard.**
  - `contracts/` with forge tests.
  - `web/` onboarding, namespace creation, grant and revoke, map, activity, billing and approve page.
  - A TS ENS helper lib (ensjs v2) shared with the plugin.
- **Lane B: provider and Mac.**
  - Deploy `combined` on the VPS.
  - meshy-sync: indexer, compiler, admit, World device-flow proxy, attestations, `/map`.
  - Mac UI fork and rebrand, Privy device flow, root daemon install.
- **Lane C: daemon and agent.**
  - meshyd SSH server, Linux sandbox and FUSE logging, revocation watcher, Arkiv writer, TUI.
  - Mac sandbox.
  - `meshy` CLI and the Claude plugin.
- **Checkpoint 1 at H+10:** thin end-to-end on an **anvil Sepolia fork**: grant → `meshy ssh` read → blocked path → revoke kill. Fix blockers before adding anything new.
- **Checkpoint 2 at H+20:** the full demo on real Sepolia, Rabdi and World sandbox, with the Mac app and a Mac resource.
- **H+20–26:** simulation suite, hardening, UI polish.
- **H+26–29:** README (architecture diagram, what's new vs NetBird, AI disclosure), the `/arkiv` files, the World debrief, and recording the video. Record a **backup video at H+26**.
- **H+29–31:** buffer. Submit by 08:00 IST.
- Sleep is taken in staggered 3 h shifts so one person is always on integration.

## Testing and simulation plan
**Unit tests:**
- **Go:** ENS event decoding and tree building (with expiry, ancestor death and token regeneration); policy ECIES round-trip; SBPL and bwrap argument compilers; Arkiv tx encoding against a TS SDK golden vector; World ID token validation using locally signed fixtures (wrong aud, stale `auth_time`, wrong acr, expired); setup-key admission logic.
- **Solidity (`forge test`):** slash on an expired name; slash on an unregistered name; slash on a re-registered name (version mismatch); no slash on a valid name; subscription split, fee, prorated cancel and seat math.
- **TS:** grant tx builder (calldata snapshot) and Arkiv query builders.

**Local chain simulation:**
- Run `anvil --fork-url $SEPOLIA --block-time 12`, which has the real ENSv2 contracts and realistic block cadence.
- `evm_increaseTime` tests the 1 h expiry without waiting.
- The whole end-to-end suite runs against this fork first, then against real Sepolia.

**End-to-end harness** (`deploy/e2e-compose.yml` + `e2e/` Go scenario runner):
- Services: anvil-fork, provider (`combined` + meshy-sync), `meshyd-db1` and `meshyd-web1` (Linux), an `agent` container running the `meshy` CLI as a simulated Claude Code, and toxiproxy in front of the RPC websocket.
- The runner asserts:
  1. A valid agent reads an allowed file, and the op appears in the Arkiv batch.
  2. **Escape attempts are all blocked and logged:**
     - absolute paths outside the grant; `..` traversal
     - a symlink pointing outside; hardlinks
     - `/proc/self/root`, `/proc/1/environ`
     - writing in a ro grant
     - `curl` to a metadata IP (no network inside the sandbox)
     - a fork bomb (pids cgroup)
     - SSH port-forward and SFTP outside the root
  3. Expiry auto-kill after warp: the session dies and no new session is allowed.
  4. **Revocation latency:** 20 runs. Report p50/p95 from tx-mined to kill and from tx-sent to kill. The target is p95 under 15 s from tx-sent.
  5. Killing an ancestor (unregister the resource) kills all grants under it.
  6. An agent without a `meshy.worldid` attestation is refused, which is the World denied path.
  7. A seat over the paid limit gets 402 at admission.
  8. A provider attestation for an expired name is slashed on-chain.
  9. **Chaos:**
     - Websocket drop, or 10 s of latency via toxiproxy: fail-closed kills sessions after 30 s.
     - Provider container killed: existing P2P sessions continue, new admissions fail, and the daemon still enforces revocation from its own watcher.
     - Restarting meshyd mid-session recovers state, and the stale session is not resumed.
     - Clock skew of +5 min on the daemon: decisions follow block time.
  10. Re-registering a revoked label does **not** revive the old policy (tokenId binding).
- **Load:** 10 agents × 3 concurrent sessions with heavy reads. Measure Arkiv tx count and cost units per minute, plus FUSE throughput overhead. Tune the batch size.

**Mac checks (manual checklist plus a scripted run):**
- The SBPL profile works for `ls`, `cat`, `grep`, `python3`, `git status` in an approved folder and denies `~/.ssh` and `/etc`.
- eslogger events are attributed to the right session.
- The menu-bar sign-in → admit → `ssh db1.prod.acme.eth` works through the DNS zone.
- The revoke notification fires.

**Test–fix loop:**
- Each failing scenario becomes a GitHub issue labelled `e2e` with the scenario ID.
- At each checkpoint we triage by demo impact: P0 is the demo path; P1 is prize-checklist items; P2 is the rest.
- `make e2e` and `forge test` run in CI (GitHub Actions) on every push to `main`.

## Risks and fallbacks
- **World sandbox flakiness** (another team saw broken TestFlight and a broken simulator):
  - Activate the sandbox identity at H0.
  - Keep a fixture-token mode, clearly labelled, so the denied path still demos.
  - Ask at the World booth.
- **Revocation over 15 s:** Sepolia blocks are 12 s. Send with a high priority fee, watch logs on the same websocket, and show the measured number honestly.
- **bwrap or FUSE blocked in Docker:** fall back to Landlock (`go-landlock`) for restriction and fanotify for logging.
- **Mac eslogger or Full Disk Access trouble:** fall back to meshyd Linux inside Lima on the Mac.
- **NetBird build pain** (CGO for `combined` and the UI; replace directives): build the server through `combined/Dockerfile.multistage`, and keep all new Go code inside the fork module.
- **Wails v3 beta friction:** if the Mac UI fork stalls by H+14, ship a minimal Wails tray app that talks to the daemon gRPC (same features, less polish).
- **Arkiv quota and rate limits:** keep batching, use an access key, and record the numbers in `friction.md`.
- **ENS gotchas:** key everything by labelhash, use a per-member resolver for device keys, and write the policy fresh and bound to tokenId.

## Verification (definition of done)
- [ ] `forge test` is green. Contracts are deployed and verified on Sepolia, with addresses in the README.
- [ ] `make e2e` passes all 10 scenarios against the anvil fork. Scenarios 1, 2, 4 and 6 also pass on real Sepolia.
- [ ] The revocation latency table (p50/p95) is in the README.
- [ ] The live dashboard URL works with a fresh wallet through onboarding.
- [ ] The Mac app and `meshy` CLI and plugin join and reach `db1.prod.acme.eth` by name.
- [ ] The Arkiv runnable query returns demo data. All 5 `/arkiv` files are present.
- [ ] The World debrief is written, and both the success and denied paths are recorded.
- [ ] The video is 2:30–3:00 and uploaded. Submission is in before 08:00 IST.

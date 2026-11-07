# Meshy

**A decentralized, agent-friendly mesh VPN.** ENSv2 is the access-control plane, staked providers run the WireGuard mesh, and AI agents get folder- or port-scoped, time-limited access that dies in the block that revokes it. Every file an agent touches is logged to Arkiv.

> Built at ETHGlobal Mumbai 2026 by [@jashwanth0712](https://github.com/jashwanth0712), [@Ronnieraj37](https://github.com/Ronnieraj37) and [@aizahs31](https://github.com/aizahs31).

- **Dashboard:** [meshy.live](https://meshy.live) (try the read-only demo org at [meshy.live/org/meshydemo.eth](https://meshy.live/org/meshydemo.eth))
- **Pitch with live scenes:** [meshy.live/demo](https://meshy.live/demo)
- **Install the tools:** `curl -fsSL https://get.meshy.live | sh`
- **Mac app:** [meshy.live/download](https://meshy.live/download) (`Meshy.dmg`, from GitHub Releases)
- **Provider API:** `https://api.meshy.live`

## How it works
Everything in an org is an ENS name on Sepolia ENSv2:

```
acme.eth                            org (owner wallet, org key, trusted attestors)
├─ prod.acme.eth                    network
│  └─ db1.prod.acme.eth             device (kind, meta, SSH host key, folders)
│     ├─ camera.db1.prod.acme.eth   service: one port, declared in meshy.service
│     └─ claude-1.db1.prod.acme.eth grant: one agent on one device, with an expiry
├─ agents.acme.eth
│  └─ claude-1.agents.acme.eth      agent (its own resolver, SSH key, World ID attestation)
└─ people.acme.eth                  members
```

1. **A grant is a subname.** `claude-1.db1.prod.acme.eth` lets one agent reach one device (or `claude-1.camera…` one port) until it expires. Its folder policy is sealed in the browser to the device's key. The agent can't move or renew it.
2. **Revoking is `unregister()`.** Each device's daemon (`meshyd`) re-checks every live session's grant on every block and kills the session in the block that carries the revoke. It fails closed if the chain head goes stale.
3. **Folder sandbox.** Sessions see only the granted folders, read-only or read-write: bubblewrap with a policy-checking FUSE on Linux, Seatbelt on macOS. Blocked attempts return machine-readable errors (`MESHY_BLOCKED`, `MESHY_REVOKED`, `MESHY_NO_GRANT`).
4. **The mesh follows ENS.** The provider (`meshy-sync`, built on NetBird) admits peers only for live names, turns each live grant into a mesh policy, and evicts peers whose name is gone.
5. **Everything is audited on Arkiv.** Sessions, blocks and file operations are written by the device's own key. Paths and commands are sealed to the org key. Readers trust only entities signed by the devices' ENS `addr` keys.
6. **Agents are human-backed.** An agent joins only after a person approves it (World ID Agent Pass, via the provider's attestation).
7. **Delegated admins.** With ENSv2 Enhanced Access Control, an owner can make someone admin of one network: they can grant and revoke there and nowhere else.
8. **Decentralized providers.** Anyone can stake ETH in `ProviderRegistry` to run a provider. Signed admissions are fraud proofs that can slash a dishonest one. Orgs pay per seat in USDC through `MeshySubscription`.

## Measured
| Run | Result |
|---|---|
| Dashboard revoke on Sepolia, delegated admin | Session dead **1.6 s after the block was mined**; meshyd reacted in 216 ms |
| CLI revokes on Sepolia (3 runs) | 9.46 / 15.75 / 6.82 s from tx sent to session dead; meshyd reacted in 120–192 ms, the rest is block inclusion |
| Revoke over the live WireGuard mesh | Session killed 0.75 s after mining; the mesh policy removed at about 27 s |
| `e2e/run-local.sh`, Sepolia fork, 12 s blocks, 5 runs | p50 **11.71 s**, p95 12.19 s |
| `e2e/services-local.sh` | 21/21 checks |

The latency figures come back from the `latencyMs` attribute of the revoke entities on Arkiv. Details and evidence: [docs/STATUS.md](docs/STATUS.md).

## Use it
**Admins** create an org at [meshy.live/new](https://meshy.live/new) (a free `acme.meshy.eth` in one transaction from your wallet), add networks and devices, invite people and agents from **People**, and give or revoke access from **Access**. Every write is signed by your wallet and enforced on-chain.

**Share a device:**
```bash
curl -fsSL https://get.meshy.live | MESHY_TOOLS="meshyd meshy-mesh" sh
meshyd init --name db1.prod.acme.eth --roots /srv     # prints a join code; paste it in Directory › Add device
```
On a Mac, **This Mac › Share this Mac…** in the menu-bar app does the same and installs the daemon.

**Agents and people:**
```bash
curl -fsSL https://get.meshy.live | sh                 # meshy + meshy-mesh
meshy join '<invite link>'                             # or `meshy login` to sign this machine in with your wallet
meshy grants db1.prod.acme.eth
meshy ssh db1.prod.acme.eth -- cat /srv/reports/q3.csv
meshy mount db1.prod.acme.eth                          # the granted folders as a Finder volume
meshy curl camera.pi.home.acme.eth /snapshot           # one HTTP request to a service, by name
meshy request access db1.prod.acme.eth --path /srv/reports --mode ro --dur 3600
meshy top                                              # live terminal dashboard
```
For Claude Code, load the plugin with `claude --plugin-dir agent/claude-plugin` and run `/meshy:join <invite link>`.

## Repo layout
| Path | What | Docs |
|---|---|---|
| `meshy/` | Go: `meshyd` (device daemon), `meshy` (agent CLI), `meshy-admin`, `meshy-sync` (provider API), and the `enskit`, `policy`, `sandbox`, `arkivkit`, `worldid`, `provider`, `tui` packages | [meshy/README.md](meshy/README.md) |
| `meshnet/` | `meshy-mesh`: userspace WireGuard peer on NetBird's `client/embed` (no root). A separate module because of a dependency conflict | [meshnet/README.md](meshnet/README.md) |
| `web/` | Next.js 16 dashboard: create an org, Access, Directory, People, Activity, Billing, Provider, Settings, Approve, sign-in for the CLI and Mac app | [web/README.md](web/README.md) |
| `mac/` | Meshy for macOS: a menu-bar app with Sign-In with Ethereum, role-based menus, Share this Mac, Request access and Finder volumes. Bundles the CLI tools | [mac/README.md](mac/README.md) |
| `contracts/` | Foundry: `ProviderRegistry` (stake and slash with an ENS fraud proof) and `MeshySubscription` (per-seat USDC), deployed on Sepolia | [contracts/README.md](contracts/README.md) |
| `agent/claude-plugin/` | Claude Code plugin: zero-dependency MCP server, skill, `/meshy:join` | [agent/claude-plugin/README.md](agent/claude-plugin/README.md) |
| `demo/` | The pitch presentation with canvas scenes, built into `web/public/demo.html` | [demo/README.md](demo/README.md) |
| `deploy/` | meshyd Docker image, Linux sandbox test image, provider bootstrap (NetBird + meshy-sync on AWS) | [deploy/README.md](deploy/README.md) |
| `e2e/` | End-to-end scripts on a Sepolia fork and on live Sepolia | [e2e/README.md](e2e/README.md) |
| `arkiv/` | Arkiv schema and prize write-ups | [arkiv/](arkiv/) |
| `brand/` | Logo, favicons, design principles | [brand/README.md](brand/README.md) |
| `docs/` | **Start here:** [docs/index.md](docs/index.md): status, learnings, interfaces, on-chain records, runbook, plans | |
| `scripts/`, `.github/` | Release script and the GitHub Actions release (CLI for macOS and Linux, signed `Meshy.dmg`) | |

## Build and test
```bash
# Go components
cd meshy && go build ./cmd/... && go test ./...
../e2e/run-local.sh                       # full flow on an anvil Sepolia fork (set FORK_RPC)

# Dashboard
cd web && cp .env.example .env.local      # SEPOLIA_RPC, ARKIV_RPC, NEXT_PUBLIC_MESHY_SYNC
pnpm install && pnpm dev                  # http://localhost:3000

# Contracts
cd contracts && forge test

# Mac app
mac/build.sh && mac/test.sh --live
```
Releases: push a `v*` tag and `.github/workflows/release.yml` builds and publishes the CLI and the Mac app.

## On Sepolia
| What | Address / name |
|---|---|
| Demo org | `meshydemo.eth` (networks `prod`, `home`; devices `db1`, `pi` with service `camera`, `rig1`; agent `claude-1`) |
| `ProviderRegistry` | `0x8B93557b315E825C657D48A4CB962A3fF7E4eEec` |
| `MeshySubscription` | `0x83989608D93858c1D835216B44B44469Dc8204Df` |
| Arkiv | Rabdi testnet; writers are each device's `addr` (`db1` `0x7Dec…3384`, `pi` `0x6E36…5B7F`) |

## Branches
`main` has everything that has been merged, including the UI revamp, insights (J1–J3) and the demo presentation. Open work:

| Branch | What |
|---|---|
| `feat/live-network-hero` | A live map of the demo org's mesh on the landing page: agents, grant check, provider, networks, and devices by kind |
| `brand/meshy-logo` | The original logo commit (the logo itself is already on `main` under `brand/`) |
| `submission-assets` | A placeholder demo video for the ETHGlobal submission |

## What's new vs NetBird
NetBird ([netbirdio/netbird](https://github.com/netbirdio/netbird)) is used as a library and base for the mesh: WireGuard, signal, relay and management. Everything under `meshy/`, `meshnet/`, `contracts/`, `web/`, `mac/`, `agent/`, `demo/`, `deploy/` and `e2e/` was written during the hackathon. See the commit history.

## License
AGPL-3.0. NetBird's client code is BSD-3, and its management, signal and relay code is AGPL-3.0.

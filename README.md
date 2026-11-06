# Meshy

**A decentralized, agent-friendly mesh VPN.** ENSv2 is the access-control plane. Staked providers run the tunnels. AI agents get folder-scoped, time-limited access, and every file they touch is logged to Arkiv.

> Built at ETHGlobal Mumbai 2026 by [@jashwanth0712](https://github.com/jashwanth0712), [@Ronnieraj37](https://github.com/Ronnieraj37) and [@aizahs31](https://github.com/aizahs31).

## What it does
- **Access is an ENS name.** `claude-1.db1.prod.acme.eth` is a 1-hour, revocable grant for one agent on one resource.
- **Revoke on-chain, and the session dies.** Each resource daemon watches ENSv2 on Sepolia and kills live agent sessions within ~15 s.
- **Folder sandboxing.** Agents see only the approved folders (read-only or read-write). Every read and write is logged, and every blocked attempt is recorded.
- **Decentralized providers.** Anyone can stake ETH and run a Meshy provider (forked from NetBird). Self-hosting is free.
- **Devices and services are ENS names.** `pi.home.acme.eth` is a device with kind and metadata records. `camera.pi.home.acme.eth` is a port on that device, declared in its `meshy.service` record. `claude-1.camera.pi.home.acme.eth` is a port-level grant. Agents reach a service with `meshy connect` or `meshy curl` using the clean name, never an IP address and port, and only while the grant is live.
- **Delegated admins (ENSv2 Enhanced Access Control).** A network admin can grant and revoke inside that network and write only `meshy.policy` records. Each agent owns its own Permissioned Resolver.
- **Human-backed agents.** Agents join only after the human behind them approves through World ID Agent Pass.

## Measured: revocation latency
`e2e/run-local.sh` runs on an anvil fork of Sepolia with the **real ENSv2 contracts** and 12 s blocks (Sepolia's cadence). It revokes a grant while the agent has a live command running, and times how long until the session is killed.

| Runs | p50 | p95 | max | Target |
|---|---|---|---|---|
| 5 | **11.71 s** | 12.19 s | 12.19 s | ≤ 15 s |

Timing starts when the `unregister()` tx is sent and stops when the agent's SSH session is dead (`MESHY_REVOKED block=N`, exit 137). meshyd kills the session in the same block that carries the revocation. The same run also checks, against real ENSv2 state: the join flow, `MESHY_NO_GRANT` before any grant, the granted read, blocks on sibling, `..` and read-only writes, no network inside the sandbox, and reconnect refused after revoke.

### Live on Sepolia (real network)
`e2e/live-demo.sh` runs against `db1.prod.meshydemo.eth` on Sepolia ENSv2, with meshyd auditing to live Arkiv.

| Revoke | tx mined after | session dead after | meshyd: block seen → killed |
|---|---|---|---|
| block 12073043 | 7.8 s | 9.46 s | 120 ms |
| block 12073047 | 12.9 s | 15.75 s | 192 ms |
| block 12073050 | 4.4 s | 6.82 s | 132 ms |

meshyd reacts in about 150 ms once it sees the block. The rest of the total is Sepolia block inclusion (one run missed the next block) plus head delivery from the RPC. The 120–192 ms figures are read back from the `latencyMs` attribute of the `meshy.session` revoke entities on Arkiv.

- **On-chain names:** `meshydemo.eth`, `prod.meshydemo.eth`, `db1.prod.meshydemo.eth`, `claude-1.agents.meshydemo.eth`, and grants as `claude-1.db1.prod.meshydemo.eth`. Admin: `0x45e8747861B2E5B59b651A7d80Bc14d9B7A1E0EA`.

## Repo layout
| Path | What |
|---|---|
| `meshy/` | Go: `meshyd` (resource daemon), `meshy` (agent CLI), `meshy-admin`, `enskit`, `policy`, `sandbox`, `arkivkit`, `worldid`. See [meshy/README.md](meshy/README.md). |
| `meshnet/` | `meshy-mesh`: userspace WireGuard mesh bridge (NetBird `client/embed`) |
| `contracts/` | Foundry: `ProviderRegistry` (stake/slash), `MeshySubscription` (USDC seats) |
| `web/` | Next.js dashboard: onboarding, network map, grants, revocation timeline, Arkiv activity |
| `agent/claude-plugin/` | Claude Code plugin (MCP server + skill) |
| `deploy/` | Provider compose, meshyd Dockerfile, launchd plist, e2e compose |
| `e2e/` | Scenario runner + simulation harness |
| `arkiv/` | Arkiv prize docs (schema, friction, reach, why-arkiv, how-you-use-arkiv) |
| `docs/` | [Build plan](docs/PLAN.md), [interface contracts](docs/INTERFACES.md), [AI usage](docs/AI_USAGE.md) |

## What's new vs NetBird
NetBird ([netbirdio/netbird](https://github.com/netbirdio/netbird)) is used as a library and base for the mesh: WireGuard, signal, relay and management. Everything under `meshy/`, `contracts/`, `web/`, `agent/`, `deploy/` and `e2e/` was written during the hackathon. See the commit history.

## License
AGPL-3.0. NetBird's client code is BSD-3, and its management, signal and relay code is AGPL-3.0.

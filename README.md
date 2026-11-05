# Meshy

**A decentralized, agent-friendly mesh VPN.** ENSv2 is the access-control plane. Staked providers run the tunnels. AI agents get folder-scoped, time-limited access, and every file they touch is logged to Arkiv.

> Built at ETHGlobal Mumbai 2026 by [@jashwanth0712](https://github.com/jashwanth0712), [@Ronnieraj37](https://github.com/Ronnieraj37) and [@aizahs31](https://github.com/aizahs31).

## What it does
- **Access is an ENS name.** `claude-1.db1.prod.acme.eth` is a 1-hour, revocable grant for one agent on one resource.
- **Revoke on-chain, and the session dies.** Each resource daemon watches ENSv2 on Sepolia and kills live agent sessions within ~15 s.
- **Folder sandboxing.** Agents see only the approved folders (read-only or read-write). Every read and write is logged, and every blocked attempt is recorded.
- **Decentralized providers.** Anyone can stake ETH and run a Meshy provider (forked from NetBird). Self-hosting is free.
- **Human-backed agents.** Agents join only after the human behind them approves through World ID Agent Pass.

## Repo layout
| Path | What |
|---|---|
| `meshy/` | Go: `sync` (provider sidecar), `meshyd` (resource daemon), `cli` (`meshy`), `enskit`, `arkivkit`, `policy`. These live inside the NetBird fork module. |
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

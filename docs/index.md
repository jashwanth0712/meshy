# Meshy docs: start here

Meshy is a decentralized, agent-friendly alternative to Tailscale and Twingate. ENSv2 on Sepolia is the access-control plane: every network, device, service, agent and **grant** is an ENS name. Resource daemons (meshyd) sandbox agent sessions to the granted folders or ports, and kill live sessions in the block that revokes the grant. Every file operation and every block is audited on Arkiv.

If you're an agent picking this up, read in this order: **STATUS → LEARNINGS → INTERFACES → RUNBOOK**.

| Doc | What it answers |
|---|---|
| [STATUS.md](STATUS.md) | What is done (with evidence), what was improved and why, and what is pending, by component and in priority order |
| [LEARNINGS.md](LEARNINGS.md) | Every non-obvious gotcha we hit (ENSv2, Arkiv, NetBird, Next.js 16, Docker, RPCs) and the fix |
| [INTERFACES.md](INTERFACES.md) | The contracts between components: ENS names and records, policy envelope, approve flow, World attestation, Arkiv schema, agent-facing strings |
| [ONCHAIN.md](ONCHAIN.md) | Every record we write, who reads it, why it exists, and what we removed |
| [RUNBOOK.md](RUNBOOK.md) | How to run everything locally and live on Sepolia: processes, ports, state dirs, demo scripts, troubleshooting |
| [AUDIT.md](AUDIT.md) | Honest plan-vs-built and prize-depth audit (ENS / World / Arkiv), with the fix list |
| [PLAN.md](PLAN.md) | The original build plan: architecture, UI/UX, demo script, test plan. Partly superseded; see STATUS |
| [ui-revamp/PLAN.md](ui-revamp/PLAN.md) | Planned web UI revamp (cobalt + ice palette, ASCII horizon hero, shadcn/ui, new logo): audit, tokens, component map, step plan, innovations; [mockups](ui-revamp/mockups/index.html) |
| [SETUP_CHECKLIST.md](SETUP_CHECKLIST.md) | Accounts, keys and funds that need a human (faucets, World sandbox, VPS, repo visibility) |
| [AI_USAGE.md](AI_USAGE.md) | AI-use disclosure required by ETHGlobal and Arkiv |
| [../arkiv/](../arkiv/) | Arkiv prize files: schema, friction, reach, why-arkiv, how-you-use-arkiv |

## Repo map
| Path | What | README |
|---|---|---|
| `meshy/` | Go module: `meshyd` (resource daemon), `meshy` (agent CLI), `meshy-admin`, `meshy-sync` (provider API), and packages `enskit`, `policy`, `sandbox`, `arkivkit`, `worldid`, `audit`, `provider` | [meshy/README.md](../meshy/README.md) |
| `meshnet/` | `meshy-mesh`: userspace WireGuard bridge (NetBird `client/embed`); a separate module because of a dependency conflict | [meshnet/README.md](../meshnet/README.md) |
| `web/` | Next.js 16 dashboard: Access, Directory, Activity, Billing, Provider, Settings, Approve | [web/README.md](../web/README.md) |
| `contracts/` | Foundry: `ProviderRegistry` (stake/slash with an ENS fraud proof), `MeshySubscription` (per-seat USDC); deployed on Sepolia | [contracts/README.md](../contracts/README.md) |
| `menubar/` | macOS menu-bar app (Go systray) | [menubar/README.md](../menubar/README.md) |
| `agent/claude-plugin/` | Claude Code plugin: zero-dependency MCP server + skill + `/meshy:join` | [agent/claude-plugin/README.md](../agent/claude-plugin/README.md) |
| `deploy/` | meshyd Docker image, Linux sandbox test image, provider compose | [deploy/README.md](../deploy/README.md) |
| `e2e/` | End-to-end scripts: Sepolia fork (`run-local.sh`, `services-local.sh`) and live demo (`live-demo.sh`) | [e2e/README.md](../e2e/README.md) |

## Key facts (live demo, Sepolia)
- **Org:** `meshydemo.eth`, owned by admin `0x45e8747861B2E5B59b651A7d80Bc14d9B7A1E0EA`.
- **Networks:**
  - `prod.meshydemo.eth`, containing `db1`
  - `home.meshydemo.eth`, containing `pi` (with service `camera`) and `rig1`. `rig1` was created by delegated admin `0xf846…Fc98` from the dashboard.
- **Agent:** `claude-1.agents.meshydemo.eth`
- **Contracts:** `ProviderRegistry` `0x8B93557b315E825C657D48A4CB962A3fF7E4eEec`, `MeshySubscription` `0x83989608D93858c1D835216B44B44469Dc8204Df`
- **Arkiv writers (Rabdi):** `db1` daemon `0x7Dec34A86907eeF26c70A6578df7bAaE7F223384`, `pi` daemon `0x6E36ec35e3bbec78aC17d0e5bE66FEB829Cf5B7F`
- **Deadline:** ETHGlobal Mumbai, Sat 7 Nov 2026 09:00 IST. Partner picks: **ENS, World, Arkiv**.

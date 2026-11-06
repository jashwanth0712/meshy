# e2e/: end-to-end scripts

All scripts build the binaries themselves. Fork scripts use **real ENSv2 contracts** through an anvil fork of Sepolia; fork from Alchemy for stability (`FORK_RPC`).

| Script | What it proves | Typical run |
|---|---|---|
| `run-local.sh` | Org → network → device → meshyd; agent join; no-grant refusal, allowed read, sibling/`..`/ro-write blocked, no network; N live revokes with latency (p50/p95) | `FORK_RPC=… SSHPORT=2242 BLOCK_TIME=12 RUNS=5 ./run-local.sh` |
| `run-local.sh` + live Arkiv | Same, but meshyd writes audit to real Rabdi with a funded key | `ARKIV_RPC=…/<key> MESHYD_STATE=~/.meshy/meshyd-demo ./run-local.sh` |
| `services-local.sh` | Device kind/meta, camera service via `meshy curl`, service grant gives no shell, a **live TCP stream cut by an on-chain revoke**, per-service revocation, EAC delegated admin (grant + `meshy.policy` + device records allowed, `meshy.orgkey` refused), agent rotates `meshy.ssh` but can't forge `meshy.worldid` | `FORK_RPC=… ./services-local.sh` |
| `mesh-live.sh` | **Over the live provider mesh** (AWS NetBird + meshy-sync): grant → ENS becomes a NetBird policy → `meshy ssh` over WireGuard → on-chain revoke kills the stream and removes the policy → reconnect refused | see [deploy/provider/README.md](../deploy/provider/README.md) |
| `live-demo.sh` | The video script on **real Sepolia** against `meshydemo.eth` (grant → read → blocks → live revoke → reconnect refused) | see [docs/RUNBOOK.md](../docs/RUNBOOK.md) §7 |

**Latest results:**
- `run-local.sh` at 12 s blocks, 5 runs: p50 11.71 s, p95 12.19 s.
- `services-local.sh`: 21/21.
- Live revokes on Sepolia: 9.46 / 15.75 / 6.82 s from the CLI, and 1.6 s after the block was mined from the dashboard (meshyd reaction 120–216 ms).

- Over the mesh (`mesh-live.sh` steps, first run): session killed 9.03 s after the tx was sent (0.75 s after mining), and the NetBird policy was removed at about 27 s.

**Not implemented from the test plan:** toxiproxy RPC chaos, provider kill, clock skew, load tests.

# Runbook

How to build, run, demo and debug Meshy. Paths that start with `~/.meshy` live on the dev Mac and are **not in git**.

## 0. Prerequisites
- Go 1.27+ (Homebrew), Node 22 with pnpm 10, Foundry (`forge`, `cast`, `anvil`), Docker (for the Linux sandbox tests)
- `~/.meshy/secrets.env` (mode 600), with `ALCHEMY_KEY=…` and `ARKIV_ACCESS_KEY=…`. Never commit it; source it with `. ~/.meshy/secrets.env`.
- Sepolia RPCs:
  - Alchemy `https://eth-sepolia.g.alchemy.com/v2/$ALCHEMY_KEY` (wss for meshyd)
  - the public node `https://ethereum-sepolia-rpc.publicnode.com`, for `eth_getLogs` (see LEARNINGS #32)

## 1. Build
```bash
cd meshy && go build -o ~/.local/bin/ ./cmd/meshyd ./cmd/meshy ./cmd/meshy-admin ./cmd/meshy-sync
```
```bash
cd meshnet && go build -o ~/.local/bin/meshy-mesh .
```
```bash
cd web && pnpm install && pnpm dev
```
```bash
cd menubar && ./build-app.sh && open dist/Meshy.app
```
```bash
cd contracts && forge install foundry-rs/forge-std --no-git && forge test
```

## 2. Keys and state (dev Mac)
| Path | What |
|---|---|
| `~/.meshy/admin.key` | `meshydemo.eth` owner key (`0x45e8…E0EA`), used by `meshy-admin` via `MESHY_ADMIN_KEY` |
| `~/.meshy/admin/meshydemo.eth.json` | `meshy-admin` cache: registries, resolver, **org X25519 private key** (`orgKeyPriv`; import it in dashboard Settings to decrypt audit details) |
| `~/.meshy/attestor.key` | Dev World ID attestor and provider signer (`0x6Aaf…0EC4`), trusted in `meshy.attestor` |
| `~/.meshy/meshyd-demo/` | `db1.prod` daemon keys (Arkiv writer `0x7Dec…3384`, funded) |
| `~/.meshy/meshyd-pi/` | `pi.home` daemon keys (Arkiv writer `0x6E36…5B7F`, funded) |
| `~/.meshy/meshyd-rig1/` | `rig1.home` daemon keys (unfunded on Arkiv, so it runs with `--arkiv ""`) |
| `~/.meshy/agent-claude-1/` | `claude-1` agent identity (SSH key + eth key); use with `MESHY_HOME=~/.meshy/agent-claude-1` |

## 3. What is running
| Process | Port / socket | Start command |
|---|---|---|
| Dashboard | http://localhost:3000 | `.claude/launch.json` `meshy-web` (or `pnpm --dir meshy/web dev`) |
| meshy-sync (provider API) | :8787 | `meshy-sync --sepolia https://ethereum-sepolia-rpc.publicnode.com --dev-attest --cors http://localhost:3000` |
| meshyd `db1.prod.meshydemo.eth` | 127.0.0.1:2222, `/tmp/meshyd-live.sock` | see §4 |
| meshyd `pi.home.meshydemo.eth` | 127.0.0.1:2252, `/tmp/meshyd-pi.sock` | see §4 |
| meshyd `rig1.home.meshydemo.eth` | 127.0.0.1:2262, `/tmp/meshyd-rig1.sock` | see §4 (`--arkiv ""`) |
| Camera stand-in (service `camera.pi…`) | 127.0.0.1:8001 | `cd ~/meshy-demo/camera && python3 -m http.server 8001 --bind 127.0.0.1` |

Live view of a daemon: `meshyd status -w --control /tmp/meshyd-live.sock`

## 4. Start a resource daemon (live Sepolia + live Arkiv)
```bash
. ~/.meshy/secrets.env
meshyd run --name db1.prod.meshydemo.eth --roots ~/meshy-demo/srv --state ~/.meshy/meshyd-demo \
  --sepolia "wss://eth-sepolia.g.alchemy.com/v2/$ALCHEMY_KEY" --listen 127.0.0.1:2222 \
  --arkiv "https://rpc.rabdi.db-chain.testnet.arkiv.network/$ARKIV_ACCESS_KEY" --control /tmp/meshyd-live.sock
```
`meshyd doctor --name <ens> --state <dir> --sepolia <https rpc>` checks keys, the sandbox, the RPC, that the ENS name is live, and the org key.

## 5. Add things (CLI path; the dashboard does the same with a wallet)
```bash
. ~/.meshy/secrets.env; export MESHY_SEPOLIA="https://eth-sepolia.g.alchemy.com/v2/$ALCHEMY_KEY" MESHY_ADMIN_KEY=$(cat ~/.meshy/admin.key)
meshy-admin network add --org meshydemo.eth --name lab
meshyd init --name rig2.lab.meshydemo.eth --roots ~/data --state ~/.meshy/meshyd-rig2 --endpoint 127.0.0.1:2272   # prints the join code
meshy-admin resource add --org meshydemo.eth --network lab --join meshy-join:… --kind gpu-server --meta '{"gpus":"2x RTX 4090"}'
meshy-admin service add --device rig2.lab.meshydemo.eth --name gpu0 --target 127.0.0.1:8000 --proto http --desc "inference API"
meshy-admin grant --resource rig2.lab.meshydemo.eth --agent claude-1 --path ~/data/models:ro --for 1h   # folders
meshy-admin grant --resource gpu0.rig2.lab.meshydemo.eth --agent claude-1 --for 1h                    # one port
meshy-admin revoke --resource gpu0.rig2.lab.meshydemo.eth --agent claude-1
meshy-admin admin add --org meshydemo.eth --network lab --addr 0x… --devices                          # delegate (EAC)
```
**Dashboard path:** **Directory → Add device / Add service**, **Access → Give access / Revoke**, **Settings → Delegated admins**. Any connected wallet can try; the chain enforces roles.

## 6. Agent side
```bash
export MESHY_HOME=~/.meshy/agent-claude-1 MESHY_SEPOLIA="https://eth-sepolia.g.alchemy.com/v2/$ALCHEMY_KEY"
meshy whoami
meshy grants rig1.home.meshydemo.eth
meshy ssh rig1.home.meshydemo.eth -- ls ~/meshy-demo/rig1/models
meshy curl camera.pi.home.meshydemo.eth /snapshot.json
meshy connect camera.pi.home.meshydemo.eth --local 127.0.0.1:18001
```
**A new agent:** run `meshy join 'http://localhost:3000/approve?org=meshydemo.eth&agent=<label>'`, then open the printed `MESHY_APPROVE_URL` and approve it. Or approve from the CLI with `meshy-admin agent add --approve '<url>' --dev-attest-key $(cat ~/.meshy/attestor.key)`.

## 7. Tests
```bash
cd meshy && go vet ./... && go test ./...
```
```bash
cd web && npx next typegen && npx tsc --noEmit
```
```bash
cd contracts && forge test
```
Linux sandbox suite (from the repo root):
```bash
docker build -f deploy/Dockerfile.test -t meshy-test deploy
docker run --rm --device /dev/fuse --cap-add SYS_ADMIN --security-opt seccomp=unconfined \
  --security-opt apparmor=unconfined --security-opt systempaths=unconfined -e GOFLAGS=-mod=mod \
  -v "$PWD/meshy":/src -v "$(go env GOMODCACHE)":/go/pkg/mod -w /src meshy-test go test ./...
```
End-to-end on a Sepolia fork (real ENSv2 contracts); prefer forking from Alchemy:
```bash
. ~/.meshy/secrets.env; FORK_RPC="https://eth-sepolia.g.alchemy.com/v2/$ALCHEMY_KEY" SSHPORT=2242 BLOCK_TIME=12 RUNS=5 e2e/run-local.sh
```
```bash
. ~/.meshy/secrets.env; FORK_RPC="https://eth-sepolia.g.alchemy.com/v2/$ALCHEMY_KEY" e2e/services-local.sh
```
Live demo on real Sepolia (for the video):
```bash
. ~/.meshy/secrets.env; MESHY_SEPOLIA="https://eth-sepolia.g.alchemy.com/v2/$ALCHEMY_KEY" MESHY_ADMIN_KEY=$(cat ~/.meshy/admin.key) MESHY_HOME=~/.meshy/agent-claude-1 RUNS=1 e2e/live-demo.sh
```

## 8. Troubleshooting
| Symptom | Fix |
|---|---|
| Dashboard shows "Could not load org: … 10 block range" | `eth_getLogs` hit Alchemy: check that `/api/rpc` routes logs to `LOGS_RPC` |
| `MESHY_UNAVAILABLE chain view is stale` | meshyd lost its websocket. Check the Alchemy key and network; it fails closed by design |
| `MESHY_NOT_ADMITTED` from `meshy ssh` | Agent identity not approved, key mismatch, or no World ID attestation: run `meshy whoami` |
| anvil exits / "block not found" | Fork from `head-8`, use Alchemy as `FORK_RPC` |
| `bind: address already in use :2222` | The live db1 meshyd is running; pass `SSHPORT=2242` to e2e scripts |
| `control socket: bind: invalid argument` | The socket path is longer than 104 bytes; use `/tmp/x.sock` |
| `tsc` errors in `.next/types/validator.ts` | Run `npx next typegen` after route changes |
| Arkiv `NonCanonicalOperationData` | The create op must be one ABI tuple (LEARNINGS #11) |

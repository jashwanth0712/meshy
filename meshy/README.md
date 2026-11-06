# meshy/: Go components (Lane C)

| Package / command | What it does |
|---|---|
| `cmd/meshy-sync` + `provider/` | Provider API: World ID device flow + attestations, `/v1/map` (org tree from ENS), `/v1/admit`, and a NetBird reconciler (`provider/netbird`) |
| `cmd/meshyd` | Resource daemon. Runs an SSH server that admits agents from ENSv2 grants, sandboxes every session to the granted folders, audits each file op and block to Arkiv, and kills live sessions when a grant is revoked or expires. |
| `cmd/meshy` | Agent CLI: `join` (keygen, approval link, wait for ENS), `whoami`, `grants`, `ssh` (host key pinned from ENS), `connect` and `curl` (tunnel to, or one HTTP request through, a **service** like `camera.pi.home.acme.eth`). |
| `cmd/meshy-admin` | Org admin CLI. It creates the org, networks, devices (with kind and meta), **services**, agents (each with its own resolver) and grants, revokes, and **delegates admins** (`admin add [--devices]`). The dashboard does the same with a wallet. |
| `enskit` | ENSv2 reads (via `resolve()`), admin writes (registries, resolvers, EAC `GrantRootRoles` / `GrantTextKey` / `GrantAddrSetter`, `ClampTip`), child enumeration from logs, and the per-block grant watcher that fails closed. |
| `policy` | Grant policy, plus an X25519 envelope bound to the grant name and token id. |
| `sandbox` | Linux: bubblewrap + GateFS (policy-checking FUSE). macOS: Seatbelt + BlockTap + eslogger. |
| `arkivkit` | Raw-transaction Arkiv writer and batching audit sink. Payloads are sealed to the org key. |
| `worldid` | Human-backed agent attestation, signed by the provider after World ID Agent Pass. |
| `audit` | Event types and sinks. |

The interface contracts are in [../docs/INTERFACES.md](../docs/INTERFACES.md).

## Build
```bash
cd meshy
go build ./cmd/meshyd ./cmd/meshy ./cmd/meshy-admin
```

## Test
```bash
go test ./...                                     # unit + macOS Seatbelt tests
../e2e/run-local.sh                               # full flow on an anvil Sepolia fork (12s blocks)
BLOCK_TIME=2 RUNS=3 ../e2e/run-local.sh           # faster
```
Linux sandbox suite (bwrap + FUSE), from the repo root:
```bash
docker build -f deploy/Dockerfile.test -t meshy-test deploy
docker run --rm --device /dev/fuse --cap-add SYS_ADMIN --security-opt seccomp=unconfined \
  --security-opt apparmor=unconfined --security-opt systempaths=unconfined \
  -e GOFLAGS=-mod=mod -v "$PWD/meshy":/src -v "$(go env GOMODCACHE)":/go/pkg/mod -w /src \
  meshy-test go test ./...
```

## Run a resource
```bash
meshyd init --name db1.prod.acme.eth --roots /srv                # prints ENS records + a join code
meshy-admin resource add --org acme.eth --network prod --join meshy-join:...   # or use the dashboard
meshyd run --name db1.prod.acme.eth --roots /srv --sepolia wss://<sepolia-ws> --listen 127.0.0.1:2222
meshy-mesh serve --setup-key KEY --management https://provider --forward 22=127.0.0.1:2222   # mesh-only (../meshnet)
meshyd status -w                                                 # live view (opens the dashboard on a TTY; -plain for text)
meshyd top [--demo]                                              # mission-control TUI (JSON when piped); see docs/cli-tui/PLAN.md
```
Under Docker on Linux, add `--device /dev/fuse --cap-add SYS_ADMIN --security-opt seccomp=unconfined --security-opt apparmor=unconfined --security-opt systempaths=unconfined`.

## Agent
```bash
meshy join 'https://<dashboard>/approve?org=acme.eth&agent=claude-1'
meshy grants db1.prod.acme.eth
meshy ssh db1.prod.acme.eth -- cat /srv/reports/q3.csv
```

## Deviation from the plan
The plan put this code inside a vendored NetBird fork. Instead the mesh lives in a separate module, [`../meshnet`](../meshnet) (`meshy-mesh`), which wraps NetBird's `client/embed` (BSD-3) in userspace.

It has to be separate because NetBird v0.82 and go-ethereum need incompatible `pion/stun/v3` versions, so they can't link into one binary. On resources, `meshy-mesh serve` forwards the mesh port `:22` to meshyd on localhost. On agents, `meshy-mesh proxy` is the SSH ProxyCommand, so each agent is its own mesh peer. Authorization doesn't depend on source IP: meshyd authorizes the SSH key against ENS.

## Authorization in meshyd (three steps)
1. **Login:** the agent's key, address and World ID attestation are checked against ENS.
2. **Shell or exec:** requires a device grant plus its sealed folder policy.
3. **Port-forward:** requires a service grant; the target comes from `meshy.service`.

Details are in [docs/INTERFACES.md §8](../docs/INTERFACES.md). Code: `meshyd/auth.go`, `meshyd/forward.go`.

## Where to look next
- [docs/STATUS.md](../docs/STATUS.md): what is done and what is pending
- [docs/LEARNINGS.md](../docs/LEARNINGS.md): ENSv2, Arkiv, NetBird and Docker gotchas
- [docs/RUNBOOK.md](../docs/RUNBOOK.md): how to run everything

# meshy/: Go components (Lane C)

| Package / command | What it does |
|---|---|
| `cmd/meshyd` | Resource daemon. Runs an SSH server that admits agents from ENSv2 grants, sandboxes every session to the granted folders, audits each file op and block to Arkiv, and kills live sessions when a grant is revoked or expires. |
| `cmd/meshy` | Agent CLI: `join` (keygen, approval link, wait for ENS), `whoami`, `grants`, and `ssh` (host key pinned from ENS). |
| `cmd/meshy-admin` | Org admin CLI. It creates the org, networks, resources, agents and grants in ENSv2, and revokes grants. The dashboard does the same with a wallet. |
| `enskit` | ENSv2 reads and writes, and a per-block grant watcher that fails closed. |
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
go build -tags netbird -o meshyd ./cmd/meshyd   # with the mesh listener (NetBird client/embed)
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
meshyd run --name db1.prod.acme.eth --roots /srv --sepolia wss://<sepolia-ws>  \
  [--netbird-setup-key KEY --netbird-management https://provider]   # mesh-only listener
meshyd status -w                                                 # live view
```
Under Docker on Linux, add `--device /dev/fuse --cap-add SYS_ADMIN --security-opt seccomp=unconfined --security-opt apparmor=unconfined --security-opt systempaths=unconfined`.

## Agent
```bash
meshy join 'https://<dashboard>/approve?org=acme.eth&agent=claude-1'
meshy grants db1.prod.acme.eth
meshy ssh db1.prod.acme.eth -- cat /srv/reports/q3.csv
```

## Deviation from the plan
The plan put this code inside a vendored NetBird fork. Instead it is a standalone module that imports `github.com/netbirdio/netbird/client/embed` (BSD-3) behind the `netbird` build tag, and copies NetBird's two `replace` directives (circl, easyjson). It is faster to build and still uses NetBird as the mesh.

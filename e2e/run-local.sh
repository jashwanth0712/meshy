#!/usr/bin/env bash
# End-to-end Meshy run on an anvil fork of Sepolia (real ENSv2 contracts).
#
#   e2e/run-local.sh                 # 12s blocks (Sepolia cadence), 5 revoke runs
#   BLOCK_TIME=2 RUNS=3 e2e/run-local.sh
#   ARKIV_RPC=https://rpc.rabdi.db-chain.testnet.arkiv.network/<access-key> \
#   MESHYD_STATE=~/.meshy/meshyd-demo e2e/run-local.sh   # live Arkiv writes (funded key)
#
# Flow: org -> network -> resource (meshyd init join code) -> meshyd run ->
# agent join + approve (dev World ID attestation) -> grant -> allowed read ->
# blocked escapes -> live session -> revoke -> measure kill latency.
set -euo pipefail

BLOCK_TIME=${BLOCK_TIME:-12}
RUNS=${RUNS:-5}
FORK_RPC=${FORK_RPC:-https://ethereum-sepolia-rpc.publicnode.com}
PORT=${PORT:-8545}
SSHPORT=${SSHPORT:-2222}
ARKIV_RPC=${ARKIV_RPC:-}
ROOT=$(cd "$(dirname "$0")/.." && pwd)
WORK=${WORK:-$(mktemp -d /tmp/meshy-e2e.XXXX)}
BIN=$WORK/bin
RPC=http://127.0.0.1:$PORT
SOCK=/tmp/meshyd-e2e-$$.sock
MESHYD_STATE=${MESHYD_STATE:-$WORK/meshyd}

say() { printf '\n\033[1;36m## %s\033[0m\n' "$*"; }
ok() { printf '  \033[32m✓\033[0m %s\n' "$*"; }
fail() { printf '  \033[31m✗ %s\033[0m\n' "$*"; FAILED=1; }
FAILED=0
PIDS=()
cleanup() { for p in "${PIDS[@]}"; do kill "$p" 2>/dev/null || true; done; rm -f "$SOCK"; }
trap cleanup EXIT

say "build"
mkdir -p "$BIN"
(cd "$ROOT/meshy" && go build -o "$BIN/meshyd" ./cmd/meshyd && go build -o "$BIN/meshy" ./cmd/meshy && go build -o "$BIN/meshy-admin" ./cmd/meshy-admin)
ok "binaries in $BIN"

say "anvil fork of Sepolia (block time ${BLOCK_TIME}s)"
HEAD=$(cast block-number --rpc-url "$FORK_RPC")
anvil --fork-url "$FORK_RPC" --fork-block-number $((HEAD - 8)) --block-time "$BLOCK_TIME" --port "$PORT" --silent --retries 10 --timeout 60000 &
PIDS+=($!)
for _ in $(seq 1 60); do cast block-number --rpc-url "$RPC" >/dev/null 2>&1 && break; sleep 1; done
ok "forked at $((HEAD - 8))"

# Fresh keys: well-known anvil accounts carry EIP-7702 code on Sepolia, which
# breaks ERC1155 safe mints of ENSv2 names.
newkey() { cast wallet new --json | python3 -c 'import sys,json; w=json.load(sys.stdin)[0]; print(w["private_key"], w["address"])'; }
read -r ADMIN_KEY ADMIN_ADDR < <(newkey)
read -r ATT_KEY ATT_ADDR < <(newkey)
cast rpc anvil_setBalance "$ADMIN_ADDR" 0x56BC75E2D63100000 --rpc-url "$RPC" >/dev/null
export MESHY_ADMIN_HOME=$WORK/admin MESHY_ADMIN_KEY=$ADMIN_KEY MESHY_SEPOLIA=$RPC MESHY_ANVIL=1 MESHY_HOME=$WORK/agent

LABEL=meshy$(date +%s | tail -c 7)
ORG=$LABEL.eth
RES=db1.prod.$ORG
mkdir -p "$WORK/srv/reports" "$WORK/srv/secrets"
echo "revenue,42" >"$WORK/srv/reports/q3.csv"
echo "TOPSECRET" >"$WORK/srv/secrets/key"
SRV=$(cd "$WORK/srv" && pwd -P)

say "ENSv2 namespace for $ORG"
"$BIN/meshy-admin" org init --label "$LABEL" --attestor "$ATT_ADDR"
"$BIN/meshy-admin" network add --org "$ORG" --name prod
JOIN=$("$BIN/meshyd" init --name "$RES" --roots "$SRV" --state "$MESHYD_STATE" --endpoint 127.0.0.1:$SSHPORT | awk '/^  meshy-join:/{print $1}')
"$BIN/meshy-admin" resource add --org "$ORG" --network prod --join "$JOIN"

say "meshyd"
"$BIN/meshyd" run --name "$RES" --roots "$SRV" --state "$MESHYD_STATE" --sepolia "ws://127.0.0.1:$PORT" \
  --listen 127.0.0.1:$SSHPORT --arkiv "$ARKIV_RPC" --control "$SOCK" >"$WORK/meshyd.log" 2>&1 &
PIDS+=($!)
for _ in $(seq 1 30); do grep -q "meshyd ready" "$WORK/meshyd.log" && break; sleep 1; done
grep -q "meshyd ready" "$WORK/meshyd.log" && ok "ready ($(grep -o 'sandbox=[^ ]*' "$WORK/meshyd.log" | head -1))" || { fail "meshyd did not start"; cat "$WORK/meshyd.log"; exit 1; }

say "agent join"
"$BIN/meshy" join --no-open --timeout 5m "https://meshy.local/approve?org=$ORG&agent=claude-1" >"$WORK/join.log" 2>&1 &
JP=$!
for _ in $(seq 1 40); do grep -q MESHY_APPROVE_URL "$WORK/join.log" && break; sleep 0.5; done
"$BIN/meshy-admin" agent add --org "$ORG" --approve "$(awk '/MESHY_APPROVE_URL/{print $2}' "$WORK/join.log")" --dev-attest-key "$ATT_KEY"
{ wait $JP || true; } && grep -q MESHY_JOINED "$WORK/join.log" && ok "joined" || fail "join"

expect() { # name, want-exit, want-substring, command...
  local name=$1 want=$2 sub=$3; shift 3
  local out code=0
  out=$("$@" 2>&1) || code=$?
  if [[ $code == "$want" && $out == *"$sub"* ]]; then ok "$name"; else fail "$name (exit $code): $out"; fi
}

say "access checks"
expect "no grant -> MESHY_NO_GRANT" 1 MESHY_NO_GRANT "$BIN/meshy" ssh "$RES" -- cat "$SRV/reports/q3.csv"
"$BIN/meshy-admin" grant --resource "$RES" --agent claude-1 --path "$SRV/reports:ro" --for 1h >/dev/null
expect "allowed read" 0 "revenue,42" "$BIN/meshy" ssh "$RES" -- cat "$SRV/reports/q3.csv"
expect "sibling blocked" 1 MESHY_BLOCKED "$BIN/meshy" ssh "$RES" -- cat "$SRV/secrets/key"
expect "dotdot blocked" 1 MESHY_BLOCKED "$BIN/meshy" ssh "$RES" -- "cat $SRV/reports/../secrets/key"
expect "write in ro grant blocked" 1 MESHY_BLOCKED "$BIN/meshy" ssh "$RES" -- "echo x > $SRV/reports/x.txt"
expect "no network" 6 "" "$BIN/meshy" ssh "$RES" -- "curl -sS -m 3 https://example.com -o /dev/null"
[[ ! -e $SRV/reports/x.txt ]] && ok "ro file untouched" || fail "ro dir was written"

say "revocation latency ($RUNS runs, ${BLOCK_TIME}s blocks)"
LAT=()
for i in $(seq 1 "$RUNS"); do
  [[ $i -gt 1 ]] && "$BIN/meshy-admin" grant --resource "$RES" --agent claude-1 --path "$SRV/reports:ro" --for 1h >/dev/null
  (set +e; "$BIN/meshy" ssh "$RES" -- "echo started; sleep 300; echo SURVIVED" >"$WORK/live$i.log" 2>&1; echo "exit=$?" >>"$WORK/live$i.log") &
  LP=$!
  for _ in $(seq 1 40); do grep -q started "$WORK/live$i.log" 2>/dev/null && break; sleep 0.25; done
  T0=$(python3 -c 'import time;print(time.time())')
  "$BIN/meshy-admin" revoke --resource "$RES" --agent claude-1 >"$WORK/revoke$i.log"
  wait $LP || true
  T1=$(python3 -c 'import time;print(time.time())')
  L=$(python3 -c "print('%.2f' % ($T1-$T0))")
  if grep -q MESHY_REVOKED "$WORK/live$i.log" && ! grep -q SURVIVED "$WORK/live$i.log"; then
    ok "run $i: tx sent -> session killed in ${L}s ($(grep -o 'block=[0-9]*' "$WORK/revoke$i.log"))"
    LAT+=("$L")
  else
    fail "run $i: $(cat "$WORK/live$i.log")"
  fi
done
expect "reconnect after revoke refused" 1 MESHY_NO_GRANT "$BIN/meshy" ssh "$RES" -- id
if [[ ${#LAT[@]} -gt 0 ]]; then
  python3 - "${LAT[@]}" <<'EOF'
import sys, statistics as s
v = sorted(float(x) for x in sys.argv[1:])
p95 = v[min(len(v)-1, round(0.95*(len(v)-1)))]
print(f"  latency tx-sent -> killed: p50 {s.median(v):.2f}s  p95 {p95:.2f}s  max {v[-1]:.2f}s  (n={len(v)})")
EOF
fi

if [[ -n $ARKIV_RPC ]]; then
  say "Arkiv (live)"
  sleep 8   # let the batcher flush
  { grep -o 'arkiv batch written" tx=[^ ]* entities=[0-9]*' "$WORK/meshyd.log" || true; } | head -20 | sed 's/^/  /'
  N=$(grep -c 'arkiv batch written' "$WORK/meshyd.log" || true)
  [[ $N -gt 0 ]] && ok "$N Arkiv transactions written by $(grep -o 'writer=[^ ]*' "$WORK/meshyd.log" | head -1)" || fail "no Arkiv batches written"
  echo "  org for queries: $ORG"
fi

say "meshyd audit (last events)"
"$BIN/meshyd" status --control "$SOCK" | sed 's/\x1b\[[0-9;]*m//g' | tail -12
echo
[[ $FAILED == 0 ]] && printf '\033[1;32mE2E PASSED\033[0m  (work dir %s)\n' "$WORK" || { printf '\033[1;31mE2E FAILED\033[0m  (work dir %s)\n' "$WORK"; exit 1; }

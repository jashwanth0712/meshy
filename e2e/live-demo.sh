#!/usr/bin/env bash
# Live demo on real Sepolia ENSv2 (+ live Arkiv, via a running meshyd).
#
# Prereqs: org/network/resource/agent already set up (see docs/SETUP_CHECKLIST.md)
# and meshyd running for $RES. Env:
#   MESHY_SEPOLIA     Sepolia RPC (https)
#   MESHY_ADMIN_KEY   admin private key (owner of the org)
#   MESHY_HOME        agent identity dir (from `meshy join`)
#   BIN               dir with meshy / meshy-admin (default: PATH)
#   RES               resource (default db1.prod.meshydemo.eth)
#   DATA              data root on the resource (default ~/meshy-demo/srv, resolved)
#   RUNS              revoke runs (default 1)
set -uo pipefail
BIN=${BIN:-}
M=${BIN:+$BIN/}meshy
A=${BIN:+$BIN/}meshy-admin
RES=${RES:-db1.prod.meshydemo.eth}
DATA=${DATA:-$(cd ~/meshy-demo/srv && pwd -P)}
RUNS=${RUNS:-1}
AGENT=${AGENT:-claude-1}

step() { printf '\n\033[1;36m▶ %s\033[0m\n' "$*"; }
run() { printf '\033[2m$ %s\033[0m\n' "$*"; "$@"; echo "  (exit $?)"; }

step "1. No grant yet: the agent is refused"
run "$M" ssh "$RES" -- cat "$DATA/reports/q3.csv"

step "2. Admin grants $AGENT read-only $DATA/reports for 1h (ENSv2 subname $AGENT.$RES)"
run "$A" grant --resource "$RES" --agent "$AGENT" --path "$DATA/reports:ro" --for 1h
run "$M" grants "$RES"

step "3. Allowed: read the report"
run "$M" ssh "$RES" -- cat "$DATA/reports/q3.csv"

step "4. Blocked: secrets, ../ traversal, writing to a read-only grant"
run "$M" ssh "$RES" -- cat "$DATA/secrets/prod.env"
run "$M" ssh "$RES" -- "cat $DATA/reports/../secrets/prod.env"
run "$M" ssh "$RES" -- "echo pwned > $DATA/reports/q3.csv"

for i in $(seq 1 "$RUNS"); do
  [[ $i -gt 1 ]] && "$A" grant --resource "$RES" --agent "$AGENT" --path "$DATA/reports:ro" --for 1h >/dev/null
  step "5.$i Live session, then revoke on-chain"
  LOG=$(mktemp)
  ("$M" ssh "$RES" -- "echo 'agent: working on reports...'; sleep 600; echo SURVIVED" >"$LOG" 2>&1; echo "  agent exit $?" >>"$LOG") &
  P=$!
  for _ in $(seq 1 60); do grep -q working "$LOG" && break; sleep 0.25; done
  cat "$LOG"
  T0=$(python3 -c 'import time;print(time.time())')
  "$A" revoke --resource "$RES" --agent "$AGENT"
  wait $P
  T1=$(python3 -c 'import time;print(time.time())')
  tail -n +2 "$LOG"
  python3 -c "print('\033[1;32m  revoke tx sent -> agent session dead: %.2fs\033[0m' % ($T1-$T0))"
  rm -f "$LOG"
done

step "6. Reconnect after revoke"
run "$M" ssh "$RES" -- id

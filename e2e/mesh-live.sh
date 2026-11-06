#!/usr/bin/env bash
# Live revoke over the provider's WireGuard mesh (real Sepolia + live provider).
# Prereqs: db1's meshyd running and joined (`meshyd mesh-join ...`), the agent
# joined (`meshy mesh-join ...`), meshy / meshy-admin / meshy-mesh on PATH.
#   MESHY_SEPOLIA=... MESHY_ADMIN_KEY=... MESHY_HOME=~/.meshy/agent-claude-1 \
#   NB_PAT=... ./mesh-live.sh
set -uo pipefail
RES=${RES:-db1.prod.meshydemo.eth}; AGENT=${AGENT:-claude-1}
SRV=${SRV:-$(cd ~/meshy-demo/srv && pwd -P)}
NB=${NB:-https://16-4-10-3.sslip.io}
OUT=$(mktemp -d)
now() { python3 -c 'import time;print(time.time())'; }
policies() { curl -s -m 10 "$NB/api/policies" -H "Authorization: Token $NB_PAT" | jq -r '[.[].name]|join(",")'; }

echo "1. grant $AGENT ro $SRV/reports on $RES"
meshy-admin grant --resource "$RES" --agent "$AGENT" --path "$SRV/reports:ro" --for 1h | tail -1
for i in $(seq 1 40); do P=$(policies); [[ "$P" == *"$AGENT.$RES"* ]] && break; sleep 3; done
echo "   mesh policy: $P"

echo "2. read over the mesh"
meshy ssh "$RES" -- ls "$SRV/reports" || exit 1

echo "3. stream, then revoke on-chain"
meshy ssh "$RES" -- sh -c 'while true; do date +%T; sleep 1; done' > "$OUT/stream" 2>&1 &
for i in $(seq 1 60); do grep -q ':' "$OUT/stream" 2>/dev/null && break; sleep 1; done
T0=$(now)
meshy-admin revoke --resource "$RES" --agent "$AGENT" | tail -1
for i in $(seq 1 120); do grep -q MESHY_REVOKED "$OUT/stream" && break; sleep 0.5; done
T1=$(now); echo "   session killed $(python3 -c "print(round($T1-$T0,2))") s after the revoke tx was sent"
grep MESHY_REVOKED "$OUT/stream"
for i in $(seq 1 40); do P=$(policies); [[ "$P" != *"$AGENT.$RES"* ]] && break; sleep 2; done
T2=$(now); echo "   mesh policy removed $(python3 -c "print(round($T2-$T0,2))") s after the revoke tx was sent"

echo "4. reconnect must fail (no policy -> peer invisible)"
if meshy ssh "$RES" -- true 2>"$OUT/err"; then echo "FAIL: still reachable"; exit 1; fi
tail -1 "$OUT/err"

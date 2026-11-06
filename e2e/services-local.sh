#!/usr/bin/env bash
# ENS devices + services + EAC delegation, on an anvil Sepolia fork.
#   pi.home.<org>            device (kind/meta records)
#   camera.pi.home.<org>     service -> 127.0.0.1:<port>  (meshy.service record)
#   claude-1.camera.pi...    service grant (port-level), revocable
# Plus: delegated network admin (EAC root roles + setter-scoped meshy.policy),
# and the agent's own PermissionedResolver where it may edit only meshy.ssh.
set -uo pipefail
FORK_RPC=${FORK_RPC:-https://ethereum-sepolia-rpc.publicnode.com}
PORT=${PORT:-8546}
RPC=http://127.0.0.1:$PORT
WORK=${WORK:-$(mktemp -d /tmp/meshy-svc.XXXX)}
ROOT=$(cd "$(dirname "$0")/.." && pwd)
BIN=$WORK/bin
SOCK=/tmp/meshyd-svc-$$.sock
FAILED=0
PIDS=()
say() { printf '\n\033[1;36m## %s\033[0m\n' "$*"; }
ok() { printf '  \033[32m✓\033[0m %s\n' "$*"; }
fail() { printf '  \033[31m✗ %s\033[0m\n' "$*"; FAILED=1; }
cleanup() { for p in "${PIDS[@]}"; do kill "$p" 2>/dev/null; done; rm -f "$SOCK"; }
trap cleanup EXIT

mkdir -p "$BIN"
(cd "$ROOT/meshy" && go build -o "$BIN/meshyd" ./cmd/meshyd && go build -o "$BIN/meshy" ./cmd/meshy && go build -o "$BIN/meshy-admin" ./cmd/meshy-admin) || exit 1

say "fork"
HEAD=$(cast block-number --rpc-url "$FORK_RPC")
anvil --fork-url "$FORK_RPC" --fork-block-number $((HEAD - 8)) --block-time 2 --port "$PORT" --silent --retries 10 --timeout 60000 &
PIDS+=($!)
for _ in $(seq 1 60); do cast block-number --rpc-url "$RPC" >/dev/null 2>&1 && break; sleep 1; done
newkey() { cast wallet new --json | python3 -c 'import sys,json; w=json.load(sys.stdin)[0]; print(w["private_key"], w["address"])'; }
read -r ADMIN_KEY ADMIN_ADDR < <(newkey)
read -r ATT_KEY ATT_ADDR < <(newkey)
read -r DEL_KEY DEL_ADDR < <(newkey)
for a in $ADMIN_ADDR $DEL_ADDR; do cast rpc anvil_setBalance "$a" 0x56BC75E2D63100000 --rpc-url "$RPC" >/dev/null; done
export MESHY_ADMIN_HOME=$WORK/admin MESHY_ADMIN_KEY=$ADMIN_KEY MESHY_SEPOLIA=$RPC MESHY_ANVIL=1 MESHY_HOME=$WORK/agent
LABEL=meshysvc$(date +%s | tail -c 6); ORG=$LABEL.eth; DEV=pi.home.$ORG; SVC=camera.$DEV
A="$BIN/meshy-admin"

say "namespace"
$A org init --label "$LABEL" --attestor "$ATT_ADDR" >/dev/null && ok "org $ORG"
$A network add --org "$ORG" --name home >/dev/null && ok "network home.$ORG"
mkdir -p "$WORK/srv"; SRV=$(cd "$WORK/srv" && pwd -P)
JOIN=$("$BIN/meshyd" init --name "$DEV" --roots "$SRV" --state "$WORK/meshyd" --endpoint 127.0.0.1:2232 | awk '/^  meshy-join:/{print $1}')
$A resource add --org "$ORG" --network home --join "$JOIN" --kind raspberry-pi --meta '{"model":"Raspberry Pi 5","location":"office"}' >/dev/null && ok "device $DEV (kind + meta records)"

say "camera service"
mkdir -p "$WORK/cam" && echo '{"camera":"front-door","fps":30,"frame":"<jpeg bytes>"}' >"$WORK/cam/snapshot.json"
CAMPORT=$(python3 -c 'import socket;s=socket.socket();s.bind(("127.0.0.1",0));print(s.getsockname()[1])')
(cd "$WORK/cam" && python3 -m http.server "$CAMPORT" --bind 127.0.0.1 >/dev/null 2>&1) &
PIDS+=($!)
$A service add --device "$DEV" --name camera --target "127.0.0.1:$CAMPORT" --proto http --desc "front door camera" >/dev/null && ok "service $SVC -> 127.0.0.1:$CAMPORT"

"$BIN/meshyd" run --name "$DEV" --roots "$SRV" --state "$WORK/meshyd" --sepolia "ws://127.0.0.1:$PORT" --listen 127.0.0.1:2232 --arkiv "" --control "$SOCK" >"$WORK/meshyd.log" 2>&1 &
PIDS+=($!)
for _ in $(seq 1 30); do grep -q "meshyd ready" "$WORK/meshyd.log" && break; sleep 1; done

say "agent with its own resolver"
"$BIN/meshy" join --no-open --timeout 5m "https://meshy.local/approve?org=$ORG&agent=claude-1" >"$WORK/join.log" 2>&1 &
JP=$!
for _ in $(seq 1 40); do grep -q MESHY_APPROVE_URL "$WORK/join.log" && break; sleep 0.5; done
$A agent add --org "$ORG" --approve "$(awk '/MESHY_APPROVE_URL/{print $2}' "$WORK/join.log")" --dev-attest-key "$ATT_KEY" | sed 's/^/  /'
wait $JP; grep -q MESHY_JOINED "$WORK/join.log" && ok "joined (records live on the agent's own resolver)" || fail "join"

say "service access"
out=$("$BIN/meshy" curl "$SVC" /snapshot.json 2>&1); [[ $out == *MESHY_NO_GRANT* ]] && ok "no grant -> MESHY_NO_GRANT" || fail "expected refusal: $out"
$A grant --resource "$SVC" --agent claude-1 --for 1h | sed 's/^/  /'
out=$("$BIN/meshy" curl "$SVC" /snapshot.json 2>&1); [[ $out == *front-door* ]] && ok "meshy curl $SVC/snapshot.json -> camera JSON" || fail "curl: $out"
out=$("$BIN/meshy" ssh "$DEV" -- id 2>&1); [[ $out == *MESHY_NO_GRANT* ]] && ok "service grant does NOT give shell on the device" || fail "shell should be refused: $out"

say "revoke cuts a live tunnel (streaming feed service)"
FEEDPORT=$(python3 -c 'import socket;s=socket.socket();s.bind(("127.0.0.1",0));print(s.getsockname()[1])')
python3 - "$FEEDPORT" >/dev/null 2>&1 <<'PY' &
import socket,sys,threading,time
srv=socket.socket(); srv.setsockopt(socket.SOL_SOCKET,socket.SO_REUSEADDR,1); srv.bind(("127.0.0.1",int(sys.argv[1]))); srv.listen()
def serve(c):
    n=0
    try:
        while True:
            n+=1; c.sendall(b"frame %d\n" % n); time.sleep(0.2)
    except OSError: pass
while True:
    c,_=srv.accept(); threading.Thread(target=serve,args=(c,),daemon=True).start()
PY
PIDS+=($!)
FEED=feed.$DEV
$A service add --device "$DEV" --name feed --target "127.0.0.1:$FEEDPORT" --proto tcp --desc "camera frame stream" >/dev/null && ok "service $FEED -> tcp 127.0.0.1:$FEEDPORT"
$A grant --resource "$FEED" --agent claude-1 --for 1h >/dev/null && ok "granted claude-1 on $FEED"
"$BIN/meshy" connect "$FEED" --local 127.0.0.1:0 >"$WORK/connect.log" 2>&1 &
CP=$!
for _ in $(seq 1 40); do grep -q MESHY_CONNECTED "$WORK/connect.log" && break; sleep 0.25; done
LADDR=$(grep -o 'local=[^ ]*' "$WORK/connect.log" | cut -d= -f2)
python3 - "$LADDR" >"$WORK/stream.log" 2>&1 <<'PY' &
import socket,sys,time
h,p=sys.argv[1].split(":"); s=socket.create_connection((h,int(p))); n=0; t=time.time()
while True:
    d=s.recv(4096)
    if not d: break
    n+=d.count(b"\n")
print("STREAM_CLOSED frames=%d after %.1fs" % (n,time.time()-t))
PY
SP=$!
sleep 3
F=$(grep -c . "$WORK/stream.log" 2>/dev/null)
$A revoke --resource "$FEED" --agent claude-1 | sed 's/^/  /'
for _ in $(seq 1 40); do grep -q STREAM_CLOSED "$WORK/stream.log" && break; sleep 0.5; done
if grep -q STREAM_CLOSED "$WORK/stream.log"; then
  FR=$(grep -o 'frames=[0-9]*' "$WORK/stream.log" | cut -d= -f2)
  [[ ${FR:-0} -gt 5 ]] && ok "live stream ($(cat "$WORK/stream.log")) cut by the on-chain revoke" || fail "stream closed too early: $(cat "$WORK/stream.log")"
else
  fail "stream survived revoke"
fi
grep -q 'forward revoked' "$WORK/meshyd.log" && ok "meshyd: $(grep 'forward revoked' "$WORK/meshyd.log" | grep -o 'block=[0-9]*' | head -1) — forward revoked" || fail "no forward revoke in meshyd log"
kill $CP 2>/dev/null
"$BIN/meshy" connect "$FEED" --local 127.0.0.1:0 >"$WORK/connect2.log" 2>&1 &
C2=$!; sleep 6; kill $C2 2>/dev/null
grep -q MESHY_NO_GRANT "$WORK/connect2.log" && ok "reconnect to $FEED after revoke -> MESHY_NO_GRANT" || fail "after revoke: $(cat "$WORK/connect2.log")"
out=$("$BIN/meshy" curl "$SVC" /snapshot.json 2>&1); [[ $out == *front-door* ]] && ok "camera grant (not revoked) still works — revocation is per service" || fail "camera: $out"

say "EAC: delegated network admin"
$A admin add --org "$ORG" --network home --addr "$DEL_ADDR" --devices | sed 's/^/  /'
S=$(cat "$WORK/admin/$ORG.json")
SVCREG=$(python3 -c "import json,sys;print(json.loads(sys.argv[1])['resources']['$DEV'])" "$S")
RES=$(python3 -c "import json,sys;print(json.loads(sys.argv[1])['resolver'])" "$S")
AG=$(grep -o '"addr": "[^"]*"' "$WORK/agent/agent.json" | cut -d'"' -f4)
EXP=$(( $(date +%s) + 3600 ))
cast send "$SVCREG" "register(string,address,address,address,uint256,uint64)" claude-1 "$AG" 0x0000000000000000000000000000000000000000 "$RES" 0 $EXP --private-key "$DEL_KEY" --rpc-url "$RPC" >/dev/null 2>&1 && ok "delegated admin can grant claude-1 on $DEV" || fail "delegated admin could not register a grant"
DNS=$(python3 -c "import sys;n=sys.argv[1];print('0x'+''.join('%02x'%len(l)+l.encode().hex() for l in n.split('.'))+'00')" "claude-1.$DEV")
cast send "$RES" "setText(bytes,string,string)" "$DNS" meshy.policy test --private-key "$DEL_KEY" --rpc-url "$RPC" >/dev/null 2>&1 && ok "delegated admin can write meshy.policy" || fail "delegated admin could not write meshy.policy"
NDNS=$(python3 -c "import sys;n=sys.argv[1];print('0x'+''.join('%02x'%len(l)+l.encode().hex() for l in n.split('.'))+'00')" "rig1.home.$ORG")
cast send "$RES" "setAddress(bytes,uint256,bytes)" "$NDNS" 60 "$DEL_ADDR" --private-key "$DEL_KEY" --rpc-url "$RPC" >/dev/null 2>&1 && ok "delegated admin (--devices) can write a device addr record" || fail "delegated admin could not set addr"
cast send "$RES" "setText(bytes,string,string)" "$NDNS" meshy.kind gpu-server --private-key "$DEL_KEY" --rpc-url "$RPC" >/dev/null 2>&1 && ok "delegated admin (--devices) can write meshy.kind" || fail "delegated admin could not set meshy.kind"
ODNS=$(python3 -c "import sys;n=sys.argv[1];print('0x'+''.join('%02x'%len(l)+l.encode().hex() for l in n.split('.'))+'00')" "$ORG")
if cast send "$RES" "setText(bytes,string,string)" "$ODNS" meshy.orgkey attacker --private-key "$DEL_KEY" --rpc-url "$RPC" >/dev/null 2>&1; then fail "delegated admin could overwrite meshy.orgkey!"; else ok "delegated admin CANNOT touch meshy.orgkey (EAC setter scope)"; fi

say "agent owns its key record"
AGKEY=$(cat "$WORK/agent/eth.key")
cast rpc anvil_setBalance "$AG" 0x56BC75E2D63100000 --rpc-url "$RPC" >/dev/null
AGRES=$(cast call "$(python3 -c "import json,sys;print(json.loads(sys.argv[1])['agentsRegistry'])" "$S")" "getResolver(string)(address)" claude-1 --rpc-url "$RPC")
ADNS=$(python3 -c "import sys;n=sys.argv[1];print('0x'+''.join('%02x'%len(l)+l.encode().hex() for l in n.split('.'))+'00')" "claude-1.agents.$ORG")
cast send "$AGRES" "setText(bytes,string,string)" "$ADNS" meshy.ssh "$(grep -o '"ssh": "[^"]*"' "$WORK/agent/agent.json" | cut -d'"' -f4)" --private-key "0x$AGKEY" --rpc-url "$RPC" >/dev/null 2>&1 && ok "agent can rotate its own meshy.ssh" || fail "agent could not set meshy.ssh"
if cast send "$AGRES" "setText(bytes,string,string)" "$ADNS" meshy.worldid forged --private-key "0x$AGKEY" --rpc-url "$RPC" >/dev/null 2>&1; then fail "agent could forge meshy.worldid!"; else ok "agent CANNOT forge its World ID attestation"; fi

echo
[[ $FAILED == 0 ]] && printf '\033[1;32mSERVICES E2E PASSED\033[0m (%s)\n' "$WORK" || { printf '\033[1;31mSERVICES E2E FAILED\033[0m (%s)\n' "$WORK"; exit 1; }

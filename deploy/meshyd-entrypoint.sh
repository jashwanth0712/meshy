#!/bin/sh
# First boot: create keys and print the ENS records / join code. Then run.
set -e
: "${MESHY_NAME:?set MESHY_NAME to the ENS name of this resource, e.g. db1.prod.acme.eth}"
: "${MESHY_SEPOLIA:?set MESHY_SEPOLIA to a Sepolia RPC (wss:// recommended)}"

if [ ! -f "$MESHY_STATE/policy.key" ]; then
  echo "== meshyd first boot: generating keys for $MESHY_NAME =="
  meshyd init --name "$MESHY_NAME" --roots "${MESHY_ROOTS:-/srv}" --state "$MESHY_STATE" ${MESHY_ENDPOINT:+--endpoint "$MESHY_ENDPOINT"}
  echo "== paste the join code above into the Meshy dashboard (Resources → Add resource) =="
else
  echo "== meshyd: keys present in $MESHY_STATE =="
fi

meshyd doctor --name "$MESHY_NAME" --roots "${MESHY_ROOTS:-/srv}" --state "$MESHY_STATE" --sepolia "$MESHY_SEPOLIA" || true

exec meshyd run --name "$MESHY_NAME" --roots "${MESHY_ROOTS:-/srv}" --state "$MESHY_STATE" \
  --sepolia "$MESHY_SEPOLIA" --listen "${MESHY_LISTEN:-:2222}" --control "${MESHY_CONTROL:-/var/run/meshyd.sock}" "$@"

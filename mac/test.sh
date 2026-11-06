#!/usr/bin/env bash
# Headless checks for the menu-bar menu (docs/mac/PLAN.md §7). Uses an isolated defaults suite.
#   mac/test.sh            fixture + sign-in checks
#   mac/test.sh --live     also dumps the menu from live meshy-sync data
set -uo pipefail
cd "$(dirname "$0")"
BIN=build/Meshy.app/Contents/MacOS/Meshy
[ -x "$BIN" ] || ./build.sh >/dev/null
export MESHY_TEST=1
defaults delete eth.meshydemo.mac.tests >/dev/null 2>&1 || true
pass=0; fail=0
check() { if grep -qF -- "$2" <<<"$OUT"; then echo "  ✓ $1"; pass=$((pass+1)); else echo "  ✗ $1  (missing: $2)"; fail=$((fail+1)); fi; }

echo "fixture menu"
OUT=$("$BIN" --dump-menu --fixture Tests/map.json 2>/dev/null)
check "header shows the org"                "meshydemo.eth | Not signed in"
check "active org is checked"               "✓ meshydemo.eth"
check "networks list devices"               "db1  ›"
check "service under its device"            "camera  — http"
check "ssh command for a device"            "⟶ meshy ssh pi.home.meshydemo.eth"
check "connect command for a service"       "⟶ meshy connect camera.pi.home.meshydemo.eth"
check "admin console deep link"             "⟶ /org/meshydemo.eth/directory?n=rig1.home.meshydemo.eth"
check "live grant with countdown"           "claude-1 → db1.prod · "
check "revoke deep link"                    "⟶ /org/meshydemo.eth?revoke=claude-1.db1.prod.meshydemo.eth"
check "ended grants counted"                "1 ended"
check "search field present"                "[search field]"
check "quit item"                           "Quit Meshy"

echo "sign-in"
OUT=$("$BIN" --dump-menu --fixture Tests/map.json --callback "meshy://signin?state=deadbeef&address=0x45e8747861B2E5B59b651A7d80Bc14d9B7A1E0EA" 2>/dev/null)
check "unknown state is rejected"           "signin: rejected"
URL=$("$BIN" --dump-menu --fixture Tests/map.json --begin-signin 2>/dev/null | sed -n 's/^signin-url: //p')
STATE=$(sed -n 's/.*state=\([0-9a-f]*\).*/\1/p' <<<"$URL")
OUT=$("$BIN" --dump-menu --fixture Tests/map.json --callback "meshy://signin?state=$STATE&address=0x45e8747861B2E5B59b651A7d80Bc14d9B7A1E0EA&sig=0x01" 2>/dev/null)
check "issued state is accepted"            "signin: accepted"
check "identity shows with role"            "0x45e8…E0EA · admin"
OUT=$("$BIN" --dump-menu --fixture Tests/map.json --callback "meshy://signin?state=$STATE&address=0x45e8747861B2E5B59b651A7d80Bc14d9B7A1E0EA" 2>/dev/null)
check "a state can't be replayed"           "signin: rejected"

if [ "${1:-}" = "--live" ]; then
  echo "live menu (meshy-sync)"
  OUT=$("$BIN" --dump-menu 2>/dev/null)
  check "live data loaded"                  "Sepolia #"
  check "live networks"                     "pi  — "
fi
defaults delete eth.meshydemo.mac.tests >/dev/null 2>&1 || true
echo "$pass passed, $fail failed"
[ "$fail" -eq 0 ]

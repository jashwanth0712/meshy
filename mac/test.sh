#!/usr/bin/env bash
# Headless checks for Meshy for Mac (docs/mac/PLAN.md §7). Isolated defaults, Keychain item and
# ~/.meshy, so a run never touches your real sign-in, orgs or agents.
#   mac/test.sh            fresh install, Sign-In with Ethereum, persistence, fixture menu
#   mac/test.sh --live     also reads meshydemo.eth from the live provider (api.meshy.live)
set -uo pipefail
cd "$(dirname "$0")"
BIN=build/Meshy.app/Contents/MacOS/Meshy
[ -x "$BIN" ] || ./build.sh >/dev/null
export MESHY_TEST=1 MESHY_URL=https://meshy.live MESHY_TEST_KEYCHAIN_ACCOUNT="run-$$"
export MESHY_HOME=$(mktemp -d) # no agents on this "Mac"
defaults delete live.meshy.mac.tests >/dev/null 2>&1 || true
pass=0; fail=0
check()  { if grep -qF -- "$2" <<<"$OUT"; then echo "  ✓ $1"; pass=$((pass+1)); else echo "  ✗ $1  (missing: $2)"; fail=$((fail+1)); fi; }
absent() { if grep -qF -- "$2" <<<"$OUT"; then echo "  ✗ $1  (found: $2)"; fail=$((fail+1)); else echo "  ✓ $1"; pass=$((pass+1)); fi; }
M() { "$BIN" "$@" 2>/dev/null; }

echo "fresh install"
OUT=$(M --dump-menu)
check  "signed out"                          "Meshy | Not signed in"
check  "sign-in offered"                     "Sign In with Ethereum…"
check  "join offered"                        "Join with an Invite Link…"
absent "no demo org"                         "meshydemo"
absent "no demo agent sessions"              "claude-1"
absent "no demo devices"                     "db1"
check  "tools are bundled"                   "Install Command-Line Tools"
[ -x build/Meshy.app/Contents/Resources/bin/meshy ] && [ -x build/Meshy.app/Contents/Resources/bin/meshyd ] && [ -x build/Meshy.app/Contents/Resources/bin/meshy-mesh ] \
  && { echo "  ✓ meshy, meshyd, meshy-mesh in Resources/bin"; pass=$((pass+1)); } || { echo "  ✗ bundled binaries missing"; fail=$((fail+1)); }

echo "sign-in with ethereum"
URL=$(M --begin-signin | sed -n 's/^signin-url: //p')
OUT=$URL; check "opens /connect with a nonce"  "https://meshy.live/connect?nonce="
check  "callback is meshy://auth"            "callback=meshy://auth"
NONCE=$(sed -n 's/.*nonce=\([0-9a-f]*\).*/\1/p' <<<"$URL")
OUT=$(M --callback "$(node Tests/siwe.mjs deadbeefdeadbeef 2>/dev/null)")
check  "unknown nonce is rejected"           "signin: rejected"
OUT=$(M --callback "$(node Tests/siwe.mjs "$NONCE" evil.example 2>/dev/null)")
check  "other domain is rejected"            "signin: rejected"
GOOD=$(node Tests/siwe.mjs "$NONCE" 2>/dev/null); ADDR=$(node Tests/siwe.mjs x 2>&1 >/dev/null)
OUT=$(M --callback "${GOOD%??}00")
check  "bad signature is rejected"           "signin: rejected"
OUT=$(M --callback "$GOOD")
check  "valid signature is accepted"         "signin: accepted $ADDR"
OUT=$(M --callback "$GOOD")
check  "a nonce can't be replayed"           "signin: rejected"

echo "after relaunch"
OUT=$(M --dump-menu)
check  "still signed in (Keychain)"          "Meshy | ${ADDR:0:6}…${ADDR: -4}"
check  "asks for an org"                     "Add Organization…"
absent "still no demo data"                  "meshydemo"

echo "org added (fixture)"
OUT=$(M --add-org meshydemo.eth --fixture Tests/map.json --dump-menu)
check  "header shows the org"                "meshydemo.eth | ${ADDR:0:6}"
check  "org is checked"                      "✓ meshydemo.eth"
check  "networks list devices"               "db1  ›"
check  "request access on a device"          "Request Access…  ⟶ db1.prod.meshydemo.eth"
check  "ssh command for a device"            "⟶ meshy ssh pi.home.meshydemo.eth"
check  "connect command for a service"       "⟶ meshy connect camera.pi.home.meshydemo.eth"
check  "your access section"                 "Yours"
check  "others' live grants listed apart"    "Everyone in meshydemo.eth"
check  "share this mac offered"              "Share this Mac…"
check  "admin console uses the picked org"   "Admin Console  ⟶ /org/meshydemo.eth"
absent "member can't revoke"                 "?revoke="

if [ "${1:-}" = "--live" ]; then
  echo "live provider (api.meshy.live)"
  OUT=$(M --dump-menu)
  check "live data loaded"                   "Sepolia #"
  check "live networks"                      "pi  — "
fi

echo "sign out"
OUT=$(M --sign-out --dump-menu)
check  "signed out again"                    "Meshy | Not signed in"
absent "org hidden when signed out"          "meshydemo"
defaults delete live.meshy.mac.tests >/dev/null 2>&1 || true
rm -rf "$MESHY_HOME" "$HOME/Library/Application Support/Meshy-tests"
echo "$pass passed, $fail failed"
[ "$fail" -eq 0 ]

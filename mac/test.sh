#!/usr/bin/env bash
# Headless checks for Meshy for Mac (docs/mac/PLAN.md §7). Isolated defaults, Keychain item and
# ~/.meshy, so a run never touches your real sign-in, orgs or agents.
#   mac/test.sh            fresh install, Sign-In with Ethereum, persistence, member and owner menus,
#                          every menu item clicked (dialogs answered by MESHY_TEST_INPUT), flows
#   mac/test.sh --live     also reads meshydemo.eth from the live provider (api.meshy.live)
set -uo pipefail
cd "$(dirname "$0")"
BIN=build/Meshy.app/Contents/MacOS/Meshy
[ -x "$BIN" ] || ./build.sh >/dev/null
export MESHY_TEST=1 MESHY_URL=https://meshy.live MESHY_TEST_KEYCHAIN_ACCOUNT="run-$$"
export MESHY_HOME=$(mktemp -d) MESHY_TOOLS_DIR=$(mktemp -d) # no agents on this "Mac"; tools never touch ~/.local/bin
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
OUT=$(M --callback "${GOOD%????}00${GOOD: -2}") # change a byte of s (not v: v=00 can still recover the signer)
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

echo "member menu (fixture)"
OUT=$(M --add-org meshydemo.eth --role member --fixture Tests/map.json --dump-menu)
check  "header shows the org and role"       "meshydemo.eth | ${ADDR:0:6}…${ADDR: -4} · member"
check  "org is checked"                      "✓ meshydemo.eth"
check  "request access lists devices"        "db1…  — /srv  ⟶ db1.prod.meshydemo.eth"
check  "request access lists services"       "camera…  — http port"
check  "my access"                           "My Access  ›"
check  "share this mac offered"              "Share this Mac…"
absent "members don't manage: no revoke"     "?revoke="
absent "members don't manage: no give"       "?give="
absent "no admin console for members"        "Admin Console"
absent "members don't see others' folders"   "you oversee"
absent "no device addresses for members"     "Copy Device Address"

echo "owner menu (fixture)"
OUT=$(M --role owner --add-org meshydemo.eth --role owner --fixture Tests/map.json --dump-menu)
check  "networks list devices"               "db1  ›"
check  "give access on a device"             "Give Access…  ⟶ /org/meshydemo.eth?give=&target=db1.prod.meshydemo.eth"
check  "ssh command for a device"            "⟶ meshy ssh pi.home.meshydemo.eth"
check  "connect command for a service"       "⟶ meshy connect camera.pi.home.meshydemo.eth"
check  "revoke a live grant"                 "Revoke…  ⟶ /org/meshydemo.eth?revoke=claude-1.db1.prod.meshydemo.eth"
check  "admin console for the picked org"    "Admin Console  ⟶ /org/meshydemo.eth"
check  "owners see every shared folder"      "srv · you oversee"
check  "browse opens in Finder (no request)" "Open in Finder  ⟶ db1.prod.meshydemo.eth"

echo "every menu item, clicked"
for role in owner member; do
  M --add-org meshydemo.eth --role $role --fixture Tests/map.json >/dev/null
  OUT=$(M --fixture Tests/map.json --click-all); code=$?
  n=$(sed -n 's/^clicked \([0-9]*\) of \([0-9]*\)$/\1 \2/p' <<<"$OUT")
  [ "$code" -eq 0 ] && [ -n "$n" ] && [ "${n% *}" = "${n#* }" ] && { echo "  ✓ $role: all ${n% *} items clicked, no crash"; pass=$((pass+1)); } || { echo "  ✗ $role: click-all failed (exit $code, $n)"; fail=$((fail+1)); }
  bad=$(grep '^OPEN: ' <<<"$OUT" | grep -vE '^OPEN: (https://meshy.live/(org/meshydemo.eth|connect)|https://github.com/jashwanth0712/meshy|file://)' || true)
  [ -z "$bad" ] && { echo "  ✓ $role: every link is meshy.live for the picked org"; pass=$((pass+1)); } || { echo "  ✗ $role: unexpected links: $bad"; fail=$((fail+1)); }
done

echo "flows"
OUT=$(MESHY_TEST_INPUT='[["claude-1","0","1","2"],0]' M --fixture Tests/map.json --click "Request Access › rig1…")
check  "request access makes a prefilled link" "COPY: https://meshy.live/org/meshydemo.eth?give=claude-1&target=rig1.home.meshydemo.eth&dur=14400&path=%2Fsrv&mode=rw"
OUT=$(M --fixture Tests/map.json --dump-menu)
check  "request shows as pending"            "claude-1 → rig1.home  — requested"
mkdir -p "$MESHY_HOME/shared"
OUT=$(MESHY_TEST_INPUT="[[\"$MESHY_HOME/shared\"],[\"test-mac\",\"0\"]]" MESHY_STATE="$MESHY_HOME/meshyd" M --fixture Tests/map.json --click "This Mac › Share this Mac…")
check  "share runs meshyd init, link for admin" "COPY: https://meshy.live/org/meshydemo.eth/directory?add=meshy-join:"
OUT=$(M --fixture Tests/map.json --dump-menu)
check  "shared device waits for ENS"         "waiting for an admin to add it"
check  "shared folders listed by name"       "shared  — "
check  "add more folders"                    "Add Folders…"
# a device added from the dashboard whose keys are on this Mac is adopted, not made again
KEYADDR=$(build/Meshy.app/Contents/Resources/bin/meshyd whoami --state "$MESHY_HOME/meshyd" | sed -n 's/.*"addr":"\(0x[0-9a-fA-F]*\)".*/\1/p')
ADOPT=$(mktemp); node -e 'const m=require("./Tests/map.json"); m.networks[0].resources[0].addr=process.argv[1]; console.log(JSON.stringify(m))' "$KEYADDR" > "$ADOPT"
DB1=$(node -e 'console.log(require("./Tests/map.json").networks[0].resources[0].fullName)')
OUT=$(MESHY_TEST_INPUT='[true]' MESHY_STATE="$MESHY_HOME/meshyd" M --fixture "$ADOPT" --click "This Mac › Advanced › Forget This Device…")
check  "a dashboard-added device is adopted" "ADOPTED: $DB1"
OUT=$(MESHY_TEST_INPUT='[true]' MESHY_STATE="$MESHY_HOME/meshyd" M --fixture "$ADOPT" --click "This Mac › Advanced › Forget This Device…")
absent "forget stays forgotten"              "ADOPTED:"
OUT=$(MESHY_STATE="$MESHY_HOME/meshyd" M --fixture "$ADOPT" --click "This Mac › Share this Mac…")
check  "share adopts it again, no new device" "This Mac is already"
# a grant you hold (owned by your wallet): folders open in Finder, no terminal
MINE=$(mktemp); node -e 'const m=require("./Tests/map.json"); m.grants.push({name:"you",fullName:"you.db1.prod.meshydemo.eth",owner:process.argv[1],expiry:4102444800,status:"live",kind:"device",agent:"you.people.meshydemo.eth",target:"db1.prod.meshydemo.eth",device:"db1.prod.meshydemo.eth"}); console.log(JSON.stringify(m))' "$ADDR" > "$MINE"
OUT=$(M --fixture "$MINE" --dump-menu)
check  "your folders are in the menu"         "Folders You Can Open"
check  "open your folders in Finder"         "Open in Finder"
check  "link this Mac offered"               "Link This Mac to meshydemo.eth…"
absent "stop sharing never auto-restarts"    "test-mac.prod · starting…"
OUT=$(MESHY_TEST_INPUT='["https://meshy.live/approve?org=meshydemo.eth&agent=mac-test-bot"]' M --fixture Tests/map.json --click "This Mac › Advanced › Join with an Invite Link…" --wait 12)
check  "join runs meshy join (times out unapproved)" "MESHY_JOIN_TIMEOUT mac-test-bot.agents.meshydemo.eth"
OUT=$(MESHY_TEST_INPUT='["not an org"]' M --click "Organizations › Add Organization…")
check  "bad org name is refused"             "isn't an ENS name"
OUT=$(MESHY_TOOLS_DIR="$MESHY_HOME/bin" M --click "This Mac › Advanced › Install Command-Line Tools")
check  "tools install as symlinks"           "Installed meshy, meshyd, meshy-mesh"
[ "$(readlink "$MESHY_HOME/bin/meshyd")" = "$PWD/build/Meshy.app/Contents/Resources/bin/meshyd" ] && { echo "  ✓ symlink points into Meshy.app"; pass=$((pass+1)); } || { echo "  ✗ symlink wrong"; fail=$((fail+1)); }

if [ "${1:-}" = "--live" ]; then
  echo "live (api.meshy.live, meshy.live/api/role)"
  OUT=$(MESHY_TEST_INPUT='["meshydemo.eth"]' M --click "Organizations › Add Organization…" --wait 20)
  check "role read from ENS"                 "ADDED: meshydemo.eth role=member"
  OUT=$(MESHY_TEST_INPUT='["nosuchorg-zz9.eth"]' M --click "Organizations › Add Organization…" --wait 20)
  check "unknown org is refused"             "nosuchorg-zz9.eth isn't a Meshy org"
  OUT=$(M --dump-menu)
  check "live data loaded"                   "Sepolia #"
  check "live devices to request"            "pi…"
fi

echo "sign out"
OUT=$(MESHY_TEST_INPUT='[true]' M --click "More › Sign Out…" --dump-menu)
check  "signed out again"                    "Meshy | Not signed in"
absent "org hidden when signed out"          "meshydemo"
defaults delete live.meshy.mac.tests >/dev/null 2>&1 || true
rm -rf "$MESHY_HOME" "$MESHY_TOOLS_DIR" "$HOME/Library/Application Support/Meshy-tests"
echo "$pass passed, $fail failed"
[ "$fail" -eq 0 ]

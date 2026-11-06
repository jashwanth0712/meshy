#!/usr/bin/env bash
# Build Meshy.app (universal arm64 + x86_64), signed, plus a zip and a dmg.
# Bundles the meshy, meshyd and meshy-mesh binaries (universal) in Contents/Resources/bin,
# so Share this Mac, Join and Sign-In with Ethereum work without installing anything else.
#   mac/build.sh            -> mac/build/Meshy.app, Meshy-mac.zip, Meshy.dmg
#   MESHY_URL=http://localhost:3000 open mac/build/Meshy.app   (point at a dev server)
#   VERSION=v0.3.0 mac/build.sh   (stamps the Go tools; default: dev)
#
# Signing: ad-hoc by default (works on the building Mac only). For a release that opens on any
# Mac with no Gatekeeper warning, set both:
#   MESHY_SIGN_IDENTITY="Developer ID Application: Name (TEAMID)"   (security find-identity -v -p codesigning)
#   MESHY_NOTARY_PROFILE=meshy-notary   (xcrun notarytool store-credentials meshy-notary ...)
# Then bin/* and the app get the hardened runtime + a secure timestamp; the app is notarized and
# stapled; the zip and DMG are built from the stapled app; the DMG is signed, notarized, stapled.
set -euo pipefail
cd "$(dirname "$0")"
OUT=build
APP="$OUT/Meshy.app"
rm -rf "$APP" "$OUT/obj"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources" "$OUT/obj"

echo "› compiling"
for arch in arm64 x86_64; do
  swiftc -O -target "$arch-apple-macos13.0" -framework AppKit \
    Sources/*.swift -o "$OUT/obj/Meshy-$arch"
done
lipo -create "$OUT/obj/Meshy-arm64" "$OUT/obj/Meshy-x86_64" -output "$APP/Contents/MacOS/Meshy"
cp Info.plist "$APP/Contents/Info.plist"

echo "› go tools (meshy, meshyd, meshy-mesh)"
BIN="$APP/Contents/Resources/bin"; mkdir -p "$BIN"; HERE=$(pwd)
LDFLAGS="-s -w -X main.version=${VERSION:-dev}"
gobuild() { # tool moddir pkg
  for arch in arm64 amd64; do
    (cd "../$2" && GOOS=darwin GOARCH=$arch CGO_ENABLED=0 go build -trimpath -ldflags "$LDFLAGS" -o "$HERE/$OUT/obj/$1-$arch" "$3")
  done
  lipo -create "$OUT/obj/$1-arm64" "$OUT/obj/$1-amd64" -output "$BIN/$1"
}
gobuild meshy meshy ./cmd/meshy
gobuild meshyd meshy ./cmd/meshyd
gobuild meshy-mesh meshnet .

echo "› icon"
# macOS icons sit on an 824 px tile inside a 1024 canvas; the brand tile is full-bleed, so pad it.
ICONSET="$OUT/obj/AppIcon.iconset"; mkdir -p "$ICONSET"
SRC=../brand/png/meshy-icon-1024.png
sips -z 824 824 "$SRC" --out "$OUT/obj/tile.png" >/dev/null
sips --padToHeightWidth 1024 1024 "$OUT/obj/tile.png" --out "$OUT/obj/icon-1024.png" >/dev/null
for s in 16 32 128 256 512; do
  sips -z $s $s "$OUT/obj/icon-1024.png" --out "$ICONSET/icon_${s}x${s}.png" >/dev/null
  sips -z $((s*2)) $((s*2)) "$OUT/obj/icon-1024.png" --out "$ICONSET/icon_${s}x${s}@2x.png" >/dev/null
done
iconutil -c icns "$ICONSET" -o "$APP/Contents/Resources/AppIcon.icns"

# menu-bar icon: the mark as a template image (alpha only, tinted by macOS)
sips -z 18 18 ../brand/png/meshy-mark-1024.png --out "$APP/Contents/Resources/StatusIconTemplate.png" >/dev/null
sips -z 36 36 ../brand/png/meshy-mark-1024.png --out "$APP/Contents/Resources/StatusIconTemplate@2x.png" >/dev/null

ENT=Meshy.entitlements
SIGN_ID=${MESHY_SIGN_IDENTITY:-}
NOTARY=${MESHY_NOTARY_PROFILE:-}
if [[ -n "$SIGN_ID" && -z "$NOTARY" ]] || [[ -z "$SIGN_ID" && -n "$NOTARY" ]]; then
  echo "✗ set both MESHY_SIGN_IDENTITY and MESHY_NOTARY_PROFILE, or neither" >&2; exit 1
fi

if [[ -n "$SIGN_ID" ]]; then
  echo "› signing (Developer ID, hardened runtime)"
  # inside-out: nested executables first, then the bundle (no --deep: it skips per-file options)
  for b in "$BIN/"*; do
    codesign --force --options runtime --timestamp --entitlements "$ENT" --sign "$SIGN_ID" "$b"
  done
  codesign --force --options runtime --timestamp --entitlements "$ENT" --sign "$SIGN_ID" "$APP"
else
  echo "› signing (ad-hoc)"
  for b in "$BIN/"*; do codesign --force --sign - "$b"; done
  codesign --force --sign - "$APP"
fi
codesign --verify --deep --strict --verbose=2 "$APP"

notarize() { # file — submit, wait, print Apple's log on failure
  echo "  notarytool submit $(basename "$1") (a few minutes)"
  local log="$OUT/obj/notary.log"
  xcrun notarytool submit "$1" --keychain-profile "$NOTARY" --wait --timeout 30m | tee "$log"
  if ! grep -q "status: Accepted" "$log"; then
    local id; id=$(awk '/^ *id:/{print $2; exit}' "$log")
    [[ -n "$id" ]] && xcrun notarytool log "$id" --keychain-profile "$NOTARY" >&2
    echo "✗ notarization failed" >&2; exit 1
  fi
}

rm -f "$OUT/Meshy-mac.zip" "$OUT/Meshy.dmg"
if [[ -n "$NOTARY" ]]; then
  echo "› notarizing the app"
  ditto -c -k --keepParent "$APP" "$OUT/obj/Meshy-notarize.zip"
  notarize "$OUT/obj/Meshy-notarize.zip"
  xcrun stapler staple "$APP"
  spctl -a -vvv -t exec "$APP"
fi

echo "› packaging"
ditto -c -k --keepParent "$APP" "$OUT/Meshy-mac.zip"   # app is stapled, so the zip opens offline too
STAGE="$OUT/obj/dmg"; mkdir -p "$STAGE"; cp -R "$APP" "$STAGE/"; ln -sf /Applications "$STAGE/Applications"
hdiutil create -volname Meshy -srcfolder "$STAGE" -ov -format UDZO "$OUT/Meshy.dmg" >/dev/null
if [[ -n "$SIGN_ID" ]]; then
  codesign --force --timestamp --sign "$SIGN_ID" "$OUT/Meshy.dmg"
  echo "› notarizing the dmg"
  notarize "$OUT/Meshy.dmg"
  xcrun stapler staple "$OUT/Meshy.dmg"
  xcrun stapler validate "$OUT/Meshy.dmg"
  spctl -a -vvv -t open --context context:primary-signature "$OUT/Meshy.dmg"
fi

rm -rf "$OUT/obj"
echo "✓ $APP"
ls -lh "$OUT"

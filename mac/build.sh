#!/usr/bin/env bash
# Build Meshy.app (universal arm64 + x86_64), ad-hoc signed, plus a zip and a dmg.
#   mac/build.sh            -> mac/build/Meshy.app, Meshy-mac.zip, Meshy.dmg
#   MESHY_URL=http://localhost:3000 open mac/build/Meshy.app   (point at a dev server)
set -euo pipefail
cd "$(dirname "$0")"
OUT=build
APP="$OUT/Meshy.app"
rm -rf "$APP" "$OUT/obj"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources" "$OUT/obj"

echo "› compiling"
for arch in arm64 x86_64; do
  swiftc -O -target "$arch-apple-macos13.0" -framework AppKit -framework WebKit \
    Sources/main.swift -o "$OUT/obj/Meshy-$arch"
done
lipo -create "$OUT/obj/Meshy-arm64" "$OUT/obj/Meshy-x86_64" -output "$APP/Contents/MacOS/Meshy"
cp Info.plist "$APP/Contents/Info.plist"

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

echo "› signing (ad-hoc)"
codesign --force --deep --sign - "$APP"
codesign --verify --strict "$APP"

echo "› packaging"
rm -f "$OUT/Meshy-mac.zip" "$OUT/Meshy.dmg"
ditto -c -k --keepParent "$APP" "$OUT/Meshy-mac.zip"
STAGE="$OUT/obj/dmg"; mkdir -p "$STAGE"; cp -R "$APP" "$STAGE/"; ln -sf /Applications "$STAGE/Applications"
hdiutil create -volname Meshy -srcfolder "$STAGE" -ov -format UDZO "$OUT/Meshy.dmg" >/dev/null
rm -rf "$OUT/obj"
echo "✓ $APP"
ls -lh "$OUT"

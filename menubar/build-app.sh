#!/bin/bash
# Build Meshy.app (menu-bar only, no Dock icon) into menubar/dist/.
set -euo pipefail
cd "$(dirname "$0")"
APP=dist/Meshy.app
rm -rf "$APP" && mkdir -p "$APP/Contents/MacOS"
go build -o "$APP/Contents/MacOS/meshy-menubar" .
cat > "$APP/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>CFBundleName</key><string>Meshy</string>
  <key>CFBundleIdentifier</key><string>xyz.meshy.menubar</string>
  <key>CFBundleExecutable</key><string>meshy-menubar</string>
  <key>CFBundleVersion</key><string>0.1.0</string>
  <key>CFBundleShortVersionString</key><string>0.1.0</string>
  <key>LSUIElement</key><true/>
  <key>LSMinimumSystemVersion</key><string>12.0</string>
</dict></plist>
PLIST
codesign --force --sign - "$APP" >/dev/null 2>&1 || true
echo "built $APP — open it with: open $APP   (config via MESHY_ORG, MESHY_SYNC, MESHY_DASHBOARD, MESHY_CONTROL)"

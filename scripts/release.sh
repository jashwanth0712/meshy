#!/usr/bin/env bash
# Build every Meshy artifact into dist/<version>/ and (with --publish) create
# the GitHub release that get.meshy.live installs from.
#
#   scripts/release.sh v0.1.0              # build only
#   scripts/release.sh v0.1.0 --publish    # build + gh release create (public!)
#
# Asset names are stable, so install.sh can use releases/latest/download/<name>:
#   <tool>_<os>_<arch>.tar.gz   (tool ∈ meshy meshyd meshy-mesh meshy-admin meshy-sync)
#   Meshy-mac.zip, Meshy.dmg, checksums.txt
#
# The Mac app is notarized when MESHY_SIGN_IDENTITY and MESHY_NOTARY_PROFILE are set
# (see mac/build.sh); --publish refuses to ship it ad-hoc signed.
set -euo pipefail
VERSION=${1:?usage: release.sh <version> [--publish]}
PUBLISH=${2:-}
if [[ "$PUBLISH" == --publish && "$(uname)" == Darwin && -z "${MESHY_NOTARY_PROFILE:-}" ]]; then
  echo "✗ --publish needs a notarized Mac app: set MESHY_SIGN_IDENTITY and MESHY_NOTARY_PROFILE" >&2; exit 1
fi
ROOT=$(cd "$(dirname "$0")/.." && pwd)
OUT="$ROOT/dist/$VERSION"
rm -rf "$OUT" && mkdir -p "$OUT/obj"
LDFLAGS="-s -w -X main.version=$VERSION"

build() { # tool moddir pkg os arch
  local tool=$1 dir=$2 pkg=$3 os=$4 arch=$5 stage="$OUT/obj/${1}_${4}_${5}"
  mkdir -p "$stage"
  (cd "$ROOT/$dir" && GOOS=$os GOARCH=$arch CGO_ENABLED=0 go build -trimpath -ldflags "$LDFLAGS" -o "$stage/$tool" "$pkg")
  cp "$ROOT/LICENSE" "$stage/" 2>/dev/null || true
  tar -C "$stage" -czf "$OUT/${tool}_${os}_${arch}.tar.gz" .
  echo "  ✓ ${tool}_${os}_${arch}"
}

echo "› Go binaries ($VERSION)"
for p in darwin/arm64 darwin/amd64 linux/amd64 linux/arm64; do
  os=${p%/*}; arch=${p#*/}
  build meshy       meshy   ./cmd/meshy       $os $arch
  build meshyd      meshy   ./cmd/meshyd      $os $arch
  build meshy-admin meshy   ./cmd/meshy-admin $os $arch
  build meshy-mesh  meshnet .                 $os $arch
done
for arch in amd64 arm64; do build meshy-sync meshy ./cmd/meshy-sync linux $arch; done

if [[ -z "${SKIP_MAC:-}" && "$(uname)" == Darwin ]] && command -v swiftc >/dev/null; then
  echo "› Mac app"
  VERSION="$VERSION" "$ROOT/mac/build.sh" | grep -E "^(›|✓|  notarytool)|status:"   # pipefail: a build failure stops the release
  cp "$ROOT/mac/build/Meshy-mac.zip" "$ROOT/mac/build/Meshy.dmg" "$OUT/"
  echo "  ✓ Meshy-mac.zip, Meshy.dmg"
fi

cp "$ROOT/web/public/install.sh" "$OUT/install.sh"
rm -rf "$OUT/obj"
(cd "$OUT" && shasum -a 256 *.tar.gz *.zip *.dmg install.sh 2>/dev/null > checksums.txt)
echo "› $(ls "$OUT" | wc -l | tr -d ' ') files in $OUT"

if [[ "$PUBLISH" == --publish ]]; then
  echo "› publishing GitHub release $VERSION"
  gh release create "$VERSION" "$OUT"/* --repo jashwanth0712/meshy --title "Meshy $VERSION" \
    --notes "Install: \`curl -fsSL https://get.meshy.live | sh\` — see https://meshy.live/download. Checksums in checksums.txt."
fi

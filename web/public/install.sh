#!/bin/sh
# Meshy installer: https://get.meshy.live
#
#   curl -fsSL https://get.meshy.live | sh                                  # agent: meshy + meshy-mesh
#   curl -fsSL https://get.meshy.live | MESHY_TOOLS="meshyd meshy-mesh" sh  # device daemon
#   curl -fsSL https://get.meshy.live | MESHY_TOOLS=all sh                  # everything
#
# Env:
#   MESHY_TOOLS    tools to install (default "meshy meshy-mesh"; "all")
#   MESHY_VERSION  release tag (default: latest)
#   MESHY_BIN      install dir (default ~/.local/bin)
#
# Downloads from GitHub Releases (github.com/jashwanth0712/meshy) and verifies
# every archive against the release's checksums.txt before installing.
set -eu

REPO="jashwanth0712/meshy"
TOOLS="${MESHY_TOOLS:-meshy meshy-mesh}"
[ "$TOOLS" = all ] && TOOLS="meshy meshyd meshy-mesh meshy-admin"
BIN="${MESHY_BIN:-$HOME/.local/bin}"
if [ -n "${MESHY_BASE:-}" ]; then
  BASE="$MESHY_BASE" # testing: a local dir served over http, or a mirror
elif [ -n "${MESHY_VERSION:-}" ]; then
  BASE="https://github.com/$REPO/releases/download/$MESHY_VERSION"
else
  BASE="https://github.com/$REPO/releases/latest/download"
fi

say() { printf '\033[1;34m›\033[0m %s\n' "$*"; }
die() { printf '\033[1;31mmeshy install:\033[0m %s\n' "$*" >&2; exit 1; }

case "$(uname -s)" in
  Darwin) OS=darwin ;;
  Linux) OS=linux ;;
  *) die "unsupported OS $(uname -s) (macOS and Linux only)" ;;
esac
case "$(uname -m)" in
  x86_64 | amd64) ARCH=amd64 ;;
  arm64 | aarch64) ARCH=arm64 ;;
  *) die "unsupported CPU $(uname -m)" ;;
esac

command -v curl >/dev/null || die "curl is required"
if command -v sha256sum >/dev/null; then SHA="sha256sum"; else SHA="shasum -a 256"; fi

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

say "Meshy for $OS/$ARCH from $BASE"
curl -fsSL "$BASE/checksums.txt" -o "$TMP/checksums.txt" || die "no release found at $BASE"

mkdir -p "$BIN"
for tool in $TOOLS; do
  asset="${tool}_${OS}_${ARCH}.tar.gz"
  want=$(grep " $asset\$" "$TMP/checksums.txt" | awk '{print $1}')
  [ -n "$want" ] || die "$asset is not in this release"
  curl -fsSL "$BASE/$asset" -o "$TMP/$asset" || die "download failed: $asset"
  got=$($SHA "$TMP/$asset" | awk '{print $1}')
  [ "$got" = "$want" ] || die "checksum mismatch for $asset (expected $want, got $got)"
  mkdir -p "$TMP/$tool" && tar -xzf "$TMP/$asset" -C "$TMP/$tool"
  install -m 0755 "$TMP/$tool/$tool" "$BIN/$tool"
  [ "$OS" = darwin ] && xattr -d com.apple.quarantine "$BIN/$tool" 2>/dev/null || true
  say "installed $BIN/$tool (sha256 ok)"
done

case ":$PATH:" in
  *":$BIN:"*) ;;
  *) say "add $BIN to your PATH:  export PATH=\"$BIN:\$PATH\"" ;;
esac

cat <<EOF

Next:
  agent   meshy join <invite-link>          # from the dashboard: Directory → Invite agent
          meshy mesh-join --provider https://sync.16-4-10-3.sslip.io
  device  meshyd init --name <device>.<network>.<org>.eth --roots /srv
  docs    https://meshy.live/download  ·  https://github.com/$REPO
EOF

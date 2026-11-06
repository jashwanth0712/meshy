#!/usr/bin/env bash
# Bring up a Meshy provider on a fresh Ubuntu host with Docker:
#   NetBird (combined server + embedded IdP + Traefik/Let's Encrypt) + meshy-sync.
# Usage: NB_DOMAIN=16-4-10-3.sslip.io ./bootstrap.sh
# Needs: ports 80/443 tcp and 3478 udp open; ./state/attestor.key (optional:
# generated on first start; register its address in ProviderRegistry).
set -euo pipefail
: "${NB_DOMAIN:?set NB_DOMAIN}"
HERE=$(cd "$(dirname "$0")" && pwd)
NBDIR=${NBDIR:-$HOME/netbird}
mkdir -p "$NBDIR" "$HERE/state" && chmod 700 "$HERE/state"

# 1. NetBird's own installer, non-interactive (LEARNINGS: getting-started.sh).
if [ ! -f "$NBDIR/docker-compose.yml" ]; then
  curl -fsSL https://github.com/netbirdio/netbird/releases/latest/download/getting-started.sh -o "$NBDIR/getting-started.sh"
  (cd "$NBDIR" && sudo NETBIRD_NON_INTERACTIVE=true NETBIRD_DOMAIN="$NB_DOMAIN" \
     NETBIRD_LETSENCRYPT_EMAIL="${LETSENCRYPT_EMAIL:-meshy@$NB_DOMAIN}" bash getting-started.sh)
fi

# 2. Allow /api/setup to mint an API token. This must be set BEFORE the first
#    /api/setup call: setup can only run once per instance.
sudo chown "$USER" "$NBDIR/docker-compose.yml"
if ! grep -q NB_SETUP_PAT_ENABLED "$NBDIR/docker-compose.yml"; then
  python3 - "$NBDIR/docker-compose.yml" <<'PY'
import sys; p=sys.argv[1]; s=open(p).read()
s=s.replace("    container_name: netbird-server\n    restart: unless-stopped\n",
            "    container_name: netbird-server\n    restart: unless-stopped\n    environment:\n      NB_SETUP_PAT_ENABLED: \"true\"\n",1)
open(p,"w").write(s)
PY
  (cd "$NBDIR" && sudo docker compose up -d netbird-server)
fi

# 3. Owner + token (once). The password is only needed for the NetBird web UI.
touch "$HERE/.env"; chmod 600 "$HERE/.env"
if ! grep -q '^NETBIRD_TOKEN=.\+' "$HERE/.env"; then
  PW=$(openssl rand -base64 18 | tr -d '/+=')
  for i in $(seq 1 30); do
    TOKEN=$(curl -s -X POST "https://$NB_DOMAIN/api/setup" -H 'Content-Type: application/json' \
      -d "{\"email\":\"admin@$NB_DOMAIN\",\"password\":\"$PW\",\"name\":\"meshy-provider\",\"create_pat\":true,\"pat_expire_in\":365}" \
      | python3 -c 'import sys,json; print(json.load(sys.stdin).get("personal_access_token",""))' 2>/dev/null || true)
    [ -n "$TOKEN" ] && break; sleep 3
  done
  [ -n "${TOKEN:-}" ] || { echo "NetBird /api/setup did not return a token (already set up?)" >&2; exit 1; }
  [ -f "$HERE/.env" ] && grep -v '^NETBIRD_TOKEN=' "$HERE/.env" > "$HERE/.env.tmp" || true
  { cat "$HERE/.env.tmp" 2>/dev/null; echo "NETBIRD_TOKEN=$TOKEN"; echo "NB_ADMIN_PASSWORD=$PW"; } > "$HERE/.env"; rm -f "$HERE/.env.tmp"
  grep -q '^NB_DOMAIN=' "$HERE/.env" || echo "NB_DOMAIN=$NB_DOMAIN" >> "$HERE/.env"
fi

# 4. meshy-sync (fill MESHY_* in .env from .env.example first).
(cd "$HERE" && sudo docker compose --env-file .env up -d --build)
echo "provider up: https://$NB_DOMAIN (mesh)  https://sync.$NB_DOMAIN/v1/health (meshy-sync)"

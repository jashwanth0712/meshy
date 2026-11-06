# meshy.live: DNS, hosting and software distribution

`meshy.live` was registered at Hostinger on 6 Nov 2026. This plan maps every public Meshy surface onto it, and says how each one is served and verified.

## 1. Who answers DNS
**Decision: delegate the zone to Vercel DNS** (`ns1.vercel-dns.com`, `ns2.vercel-dns.com`). It's a single change in Hostinger: *Domains → meshy.live → DNS / Nameservers → Change nameservers*.

Why not keep Hostinger's DNS:
- Every record below can then be scripted (`vercel dns add …`), reviewed in this file and recreated by any agent. With Hostinger, each change is a manual edit in hPanel.
- Vercel issues and renews TLS for the apex and its subdomains automatically, and records for the AWS provider sit in the same zone.
- Fallback, if we must stay on Hostinger DNS: add the same records in hPanel. That's `A @ 76.76.21.21`, `A www/get 76.76.21.21`, plus the `api`/`mesh` A records.

Registrar-side settings to check in Hostinger: auto-renew **on**, domain lock **on**, WHOIS privacy **on**. Leave DNSSEC off until the move has propagated, then enable it at Vercel if needed.

## 2. Hostnames
| Host | Record | Points to | Serves | TLS |
|---|---|---|---|---|
| `meshy.live` | A `76.76.21.21` (Vercel) | Vercel project `meshy` (`web/`) | Dashboard: `/org/<org>`, `/approve`, `/download` | Vercel |
| `www.meshy.live` | A `76.76.21.21` | Vercel | 308 redirect to `meshy.live` | Vercel |
| `get.meshy.live` | A `76.76.21.21` | Vercel (same project) | `curl -fsSL https://get.meshy.live \| sh`: `install.sh` served at `/` on this host | Vercel |
| `api.meshy.live` | A `16.4.10.3` | AWS EC2 (elastic IP), Traefik → meshy-sync :8787 | Provider API (`/v1/*`), replacing `sync.16-4-10-3.sslip.io` | Let's Encrypt (Traefik) |
| `mesh.meshy.live` | A `16.4.10.3` | AWS EC2, Traefik → NetBird | NetBird management, signal and relay (phase 2; see §4) | Let's Encrypt (Traefik) |
| `_meshy.meshy.live` | TXT | `v=meshy1 org=meshydemo.eth provider=https://api.meshy.live` | Discovery: `meshy join meshy.live` finds the org and provider from DNS (phase 3) | n/a |

Not used: MX/email (add Hostinger or another email provider's records later if needed) and CAA (the defaults let both Vercel and Let's Encrypt issue).

## 3. Software distribution
Everything is downloaded from **GitHub Releases** on the public repo. DNS only gives it short, stable URLs.

| Artifact | Built from | Platforms | URL |
|---|---|---|---|
| `meshy` (agent CLI) | `meshy/cmd/meshy` | darwin arm64/amd64, linux amd64/arm64 | `github.com/jashwanth0712/meshy/releases/latest/download/meshy_<os>_<arch>.tar.gz` |
| `meshyd` (device daemon) | `meshy/cmd/meshyd` | same | same pattern |
| `meshy-mesh` (WireGuard peer) | `meshnet/` | same | same pattern |
| `meshy-admin` (org admin CLI) | `meshy/cmd/meshy-admin` | same | same pattern |
| `meshy-sync` (provider) | `meshy/cmd/meshy-sync` | linux amd64/arm64 | same pattern |
| `Meshy.app` (Mac app, loads `meshy.live`) | `mac/` | macOS 13+, universal | `Meshy-mac.zip` |
| Claude Code plugin | `agent/claude-plugin/` | n/a | installed from the repo (`/plugin install`) |
| `meshyd` container | `deploy/meshyd.Dockerfile` | linux amd64 | `ghcr.io/jashwanth0712/meshyd` (phase 3) |

- **`scripts/release.sh <version>`:**
  - cross-compiles everything with `CGO_ENABLED=0 -trimpath -ldflags "-s -w -X main.version=…"`
  - packs one `tar.gz` per tool per platform, plus `checksums.txt` (SHA-256)
  - builds and zips the Mac app
  - runs `gh release create` with all assets
- **`get.meshy.live` → `install.sh`:**
  - detects the OS and architecture, and downloads the requested tools (default: `meshy meshy-mesh`; `MESHY_TOOLS="meshyd meshy-mesh"` for a device) from the latest release
  - **verifies the SHA-256 against `checksums.txt`**
  - installs to `~/.local/bin` (or `$MESHY_BIN`) and prints next steps
  - is served by the dashboard: `web/public/install.sh`, rewritten to `/` for the `get.` host only
- **`meshy.live/download`** has one row per artifact, with copy-paste install commands, the release version and the checksums link.

Signing (later): Apple Developer ID and notarisation for `Meshy.app` (the user has an Apple developer account), plus cosign or minisign for the tarballs.

## 4. Provider on `api.` and `mesh.`
- **`api.meshy.live`** (low risk, done first):
  - add the Host to the meshy-sync Traefik router: `Host(sync.16-4-10-3.sslip.io) || Host(api.meshy.live)`. Traefik fetches the certificate itself.
  - point the dashboard (`NEXT_PUBLIC_MESHY_SYNC`), the docs and the CLI examples at `https://api.meshy.live`
  - update provider #1's on-chain endpoint (`ProviderRegistry.update`)
  - meshy-sync `--cors` gets `https://meshy.live`
- **`mesh.meshy.live`** (phase 2, after the deadline unless time allows):
  - Change NetBird's domain in `~/netbird/config.yaml`, `dashboard.env` and the Traefik rules, keeping **both** hosts in the Traefik rules so already-joined peers, which stored `https://16-4-10-3.sslip.io`, keep working.
  - Then set meshy-sync `--netbird-management-url=https://mesh.meshy.live`, so new admissions get the new URL.
  - Old peers can stay as they are or rejoin.
- The sslip.io names keep working throughout. They depend only on the elastic IP.

## 5. Order of work
1. Hostinger: switch the nameservers to Vercel (the human signs in; the agent clicks).
2. `vercel dns add` for `api`, `mesh` and `_meshy`. The apex, `www` and `get` are already attached to the Vercel project.
3. Wait for delegation (usually minutes, sometimes hours), then check: `dig NS meshy.live`, `curl -I https://meshy.live`, `curl https://api.meshy.live/v1/health`.
4. Traefik Host rule for `api.meshy.live`; meshy-sync CORS; dashboard env `NEXT_PUBLIC_MESHY_SYNC=https://api.meshy.live`; production redeploy.
5. `www` → apex redirect; the `get.` rewrite to `install.sh`; the `/download` page.
6. `scripts/release.sh v0.1.0`, which publishes the GitHub release; then test `curl -fsSL https://get.meshy.live | sh` on macOS and Linux.
7. The Mac app's default URL becomes `https://meshy.live`; then the docs (README, RUNBOOK, deploy READMEs) and the on-chain provider endpoint.
8. Phase 2/3: `mesh.` for NetBird, `_meshy` TXT discovery in `meshy join`, the ghcr image, signing.

## 6. Verification checklist
- [ ] `dig +short NS meshy.live` → the Vercel nameservers
- [ ] `https://meshy.live/org/meshydemo.eth` loads live data; `www` redirects
- [ ] `https://api.meshy.live/v1/health` returns 200 with a valid certificate; CORS allows `https://meshy.live`
- [ ] the Provider and Approve pages on `meshy.live` reach the API
- [ ] `curl -fsSL https://get.meshy.live | sh` installs and checksums match (macOS arm64 + Linux amd64)
- [ ] `/download` links resolve to release assets
- [ ] the Mac app opens `meshy.live`
- [ ] `ProviderRegistry.getProvider(1).endpoint == https://api.meshy.live`

## 7. Status (6 Nov, evening)
- Hostinger: `meshy.live` is **in review** ("contains some sensitive keywords", up to 24 h). No DNS can be set until it is Active, so the registry has no nameservers for it yet.
- Vercel: `meshy.live`, `www.meshy.live` and `get.meshy.live` are attached to project `meshy`. They start serving as soon as DNS points at Vercel.
- Built and tested locally:
  - `scripts/release.sh v0.1.0` produced 22 artifacts
  - `web/public/install.sh` installed and verified checksums on macOS arm64 and in a Linux arm64 container, and aborted on a tampered checksum
  - the `get.` host rewrite, the `www` redirect and `/download` all work on the dev server
- Not yet done:
  - the GitHub release (public, so it waits for a go-ahead)
  - the production redeploy with `/download`
  - the DNS records, then `api.`/`mesh.` on the VPS (blocked on the review)

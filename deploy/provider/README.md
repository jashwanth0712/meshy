# Meshy Provider: meshy-sync + NetBird

This directory contains the Docker Compose stack that runs a Meshy provider node.

## What runs here

| Container | Purpose |
|---|---|
| `netbird-combined` | NetBird management API, signal server, TURN relay (coturn) and dashboard in one image |
| `meshy-sync` | Provider service: World ID device flows, ENS org-tree reads, mesh admission |

## Prerequisites

- VPS with a public IP and DNS (`NB_DOMAIN` → IP)
- Port 443/80/3478(udp) open inbound
- Alchemy (or other Sepolia node) API key
- World ID OIDC confidential client credentials (sandbox or production)

## Quickstart

```bash
cp .env.example .env     # fill in NB_DOMAIN, MESHY_SEPOLIA, WORLD_CLIENT_ID, etc.
docker compose up -d
```

### Bootstrap NetBird and get a PAT

On first run NetBird has no admin user yet. Use the setup endpoint:

```bash
PAT=$(docker compose exec netbird-combined \
  wget -qO- --post-data='{}' --header='Content-Type: application/json' \
  http://localhost:8080/api/setup | jq -r '.access.token')
echo "NB_PAT=$PAT" >> .env
docker compose restart meshy-sync
```

### Register the provider on-chain

The provider's signer address is printed at startup:

```
INFO meshy-sync starting attestor=0xABCD...
```

Register this address as the `meshy.attestor` on your org, e.g.:

```bash
meshy-admin org init --label myorg --attestor 0xABCD...
```

Or through the web dashboard under **Org Settings → Provider**.
See `contracts/README.md` for details on the staking contract.

## Environment variables

| Variable | Required | Default | Description |
|---|---|---|---|
| `NB_DOMAIN` | yes | — | Public hostname for NetBird (e.g. `mesh.example.com`) |
| `LETSENCRYPT_EMAIL` | yes | — | ACME e-mail for TLS certificate |
| `NB_OIDC_DISCOVERY` | yes | — | OIDC discovery URL for NetBird dashboard auth |
| `NB_AUTH_CLIENT_ID` | yes | — | OIDC client ID for NetBird |
| `NB_AUTH_AUDIENCE` | yes | — | OIDC audience for NetBird |
| `NB_PAT` | yes (after bootstrap) | — | NetBird management PAT |
| `MESHY_SEPOLIA` | yes | — | Sepolia JSON-RPC URL (`https://eth-sepolia.g.alchemy.com/v2/<key>`) |
| `WORLD_CLIENT_ID` | yes* | — | World ID OIDC client ID |
| `WORLD_CLIENT_SECRET` | yes* | — | World ID OIDC client secret |
| `MESHY_WORLD_ISSUER` | no | `https://sandbox.auth.world.org` | World ID issuer |
| `MESHY_CORS` | no | `http://localhost:3000` | Comma-separated CORS origins |
| `MESHY_DEV_ATTEST` | no | `false` | Enable `/v1/agent/attest-dev` for demos |

*Required for the World ID device flow. Use `MESHY_DEV_ATTEST=true` while credentials are pending.

## Running locally (development)

```bash
meshy-sync \
  --sepolia "https://eth-sepolia.g.alchemy.com/v2/$ALCHEMY_KEY" \
  --dev-attest \
  --cors "http://localhost:3000" \
  --listen :8787
```

The signer key is auto-generated on first run at `~/.meshy/attestor.key`.

## What meshy-sync does (not what NetBird does)

Meshy-sync is new code for this hackathon:

- Reads the ENS org tree (`GET /v1/map`) to discover networks, resources, agents and grants
- Runs a World ID OIDC device flow (confidential client) and signs the resulting attestation
- Verifies EIP-191 admission signatures for WireGuard peer registration
- Reconciles the ENS grant state into NetBird groups, policies and DNS records every ~30s

NetBird (the network tunnel) is used as-is from https://github.com/netbirdio/netbird.

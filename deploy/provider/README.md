# Meshy provider: NetBird + meshy-sync

A provider runs the WireGuard mesh for the orgs that pay it. **ENS decides who's on it.** meshy-sync reads each org's ENSv2 tree and enforces it on NetBird:

| ENS (source of truth) | NetBird (enforcement) |
|---|---|
| device `db1.prod.acme.eth` / agent `claude-1.agents.acme.eth` | peer named `db1-prod-acme-eth`, in its own group `meshy:res:…` / `meshy:agent:…` |
| live grant `claude-1.db1.prod.acme.eth` (or a service grant under `camera.pi…`) | policy `meshy:grant:<grant>`: agent group → device group, tcp/22 only |
| grant revoked or expired | policy deleted on the next pass (~10–30 s); meshyd has already killed the session (~1 s after the block) |
| device or agent name gone | peer evicted (after 2 consecutive passes, so a flaky RPC read never evicts) |
| NetBird's allow-all `Default` policy | deleted: the mesh is deny-by-default |

**Live instance:**
- Mesh: `https://16-4-10-3.sslip.io` (AWS EC2 t3.medium, ap-south-1).
- meshy-sync: `https://sync.16-4-10-3.sslip.io/v1/health`.
- It is provider **#1** in `ProviderRegistry`, and its signer is the attestor key.

## Bring one up
On a fresh Ubuntu host with Docker, open ports 80/443 tcp and 3478 udp, then:
```bash
git clone https://github.com/jashwanth0712/meshy && cd meshy/deploy/provider
cp .env.example .env                                  # set MESHY_ORGS, MESHY_PROVIDER_ID, …
mkdir -p state && cp /path/to/attestor.key state/     # optional: your ProviderRegistry signer
NB_DOMAIN=<ip-with-dashes>.sslip.io ./bootstrap.sh
```
`bootstrap.sh` does four things:
1. Runs NetBird's official `getting-started.sh` non-interactively. That gives the combined server, the embedded IdP, the dashboard, and Traefik with Let's Encrypt.
2. Sets `NB_SETUP_PAT_ENABLED=true`.
3. Calls `/api/setup` with `create_pat` to create the owner and an API token. Both land in `.env` (chmod 600).
4. Starts meshy-sync on NetBird's docker network, published at `https://sync.<domain>`.

Then register the provider on-chain: `ProviderRegistry.register(endpoint, signer, price, metadata)`, where `signer` is the attestor address that meshy-sync prints at startup. Update it later with `update(…)`.

**Faster redeploys** (module downloads in Docker are slow): cross-compile and copy the binary.
```bash
cd meshy && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o /tmp/meshy-sync ./cmd/meshy-sync
scp /tmp/meshy-sync host:meshy-sync/ && ssh host 'cd meshy-sync && sudo docker compose up -d --build'
```
The live box uses this setup, with a 4-line Dockerfile that only copies the binary.

## Joining the mesh
```bash
# a device (next to its meshyd): admit with the daemon key, then forward mesh :22 to meshyd
meshyd mesh-join --name db1.prod.acme.eth --state ~/.meshy/meshyd-demo \
  --provider https://sync.16-4-10-3.sslip.io --forward 22=127.0.0.1:2222

# an agent: becomes its own peer; meshy ssh/connect/curl then go over the mesh by ENS name
MESHY_HOME=~/.meshy/agent-claude-1 meshy mesh-join --provider https://sync.16-4-10-3.sslip.io
MESHY_HOME=~/.meshy/agent-claude-1 meshy ssh db1.prod.acme.eth -- ls /srv/reports
```
Both need `meshy-mesh` (built from `meshnet/`) on PATH.

## API (meshy-sync)
| Endpoint | What |
|---|---|
| `GET /v1/health` | attestor, World mode, and `mesh.lastSync` (groups, policies, peers matched/evicted, last error) |
| `GET /v1/map?org=` | ENS tree: networks → devices (kind, meta, **services**) → grants (`kind`, `agent`, `target`, `device`, status) |
| `POST /v1/admit` | `{name, wgPubKey, sig}`. `sig` is EIP-191 over `meshy-admit:<name>:<wgPubKey>`, by the name's **addr record** (daemon or agent key) or its owner. The checks, in order: the name is live, `meshy.wg` matches if set, **seats** (`MeshySubscription.seatsOf`: no subscription, the wrong provider, or used > paid all return **402**). Returns a one-off setup key into the name's group, plus an **EIP-712 `Attestation`** |
| `GET /v1/attestations?name=` | every admission this provider signed: evidence for `ProviderRegistry.slash` |
| `GET /v1/seats?org=` | paid seats vs used (devices + agents) |
| `POST /v1/agent/device-flow/*`, `/v1/agent/attest-dev` | World ID Agent Pass and the dev fallback |

### Slashing evidence
Each admission is signed as `Attestation(providerId, dnsName, resource, issuedAt, validUntil, peerKey)` under the `ProviderRegistry` EIP-712 domain. If the provider ever admits a name that was already dead (expired, unregistered, or re-registered with a different `resource`), anyone can submit that attestation to `slash()` and receive half the stake. Verified against the deployed contract with `eth_call`:
- An honest admission (db1) → `PR: no fraud detected` (the signature is accepted).
- A tampered one → `PR: bad signature`.
- An admission of the revoked grant `claude-1.db1.prod.meshydemo.eth` → `Slashed(1, 0.01 ETH, "expired at issuance")`.

## Notes
- NetBird returns a policy rule's `sources`/`destinations` as group objects but takes IDs on create (see `GroupRefs`).
- The embedded IdP's admin password (`NB_ADMIN_PASSWORD` in `.env`) is only needed for NetBird's own web dashboard. Meshy never uses it.
- `meshy-sync.Dockerfile` builds from source if you prefer it over copying the binary.

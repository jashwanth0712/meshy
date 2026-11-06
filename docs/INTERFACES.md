# Interface contracts

This document is what lets the three lanes interoperate. Change it only by PR, and tag all three of us.

- **Status:** Lane C (meshyd, the `meshy` CLI, the plugin) implements everything below. It is tested on an anvil fork of Sepolia against the real ENSv2 contracts.
- **Reference implementations:** `meshy/enskit`, `meshy/policy`, `meshy/worldid`, `meshy/arkivkit`, `meshy/cmd/meshy-admin`.

## 0. ENSv2 facts that bit us (read this first)
- **Record reads go only through ENSIP-10 `resolve(bytes dnsName, bytes call)`.** `PermissionedResolver` has no direct `text()` or `addr()` getters, so a direct call reverts with empty data. viem's `getEnsText`/`getEnsAddress` work, because they go through the Universal Resolver. For direct resolver calls, use `resolve(dnsEncode(name), encodeFunctionData(text, [node, key]))`.
- **Don't use anvil's or hardhat's default accounts as name owners on a Sepolia fork.** They carry EIP-7702 code on Sepolia, so the ERC-1155 mint reverts with `ERC1155InvalidReceiver`. Use fresh keys.
- **Revocation is `unregister(labelhash)`.** Our daemons detect it on the next block with `UniversalHelper.findExactOwner(dnsName)`, which returns `0x0` when the name or any ancestor has expired or been unregistered.
- **Resolver records survive `unregister`.** Every policy is therefore bound to the grant's `tokenId` (§2). Re-registering a label gives it a new tokenId and invalidates the old policy.

## 1. Names and records
Each name below has its own UserRegistry, the subregistry of its parent. All records live on the org's PermissionedResolver.

| Name | Owner | Records |
|---|---|---|
| `acme.eth` (org) | admin wallet | `meshy.orgkey` (X25519 pubkey, base64 raw-std, no padding), `meshy.attestor` (comma-separated EVM addresses of trusted World ID attestors, i.e. provider signers), `meshy.provider` (provider id) |
| `prod.acme.eth` (network) | admin | (none required) |
| `db1.prod.acme.eth` (resource) | admin | `addr` (daemon EVM address), `meshy.enckey` (daemon X25519 pubkey), `meshy.ssh-host` (SSH host key, authorized_keys line), `meshy.roots` (JSON array of root paths), `meshy.endpoint` (optional `host:port`; omit when using mesh DNS) |
| `agents.acme.eth` | admin | (none) |
| `camera.pi.home.acme.eth` (**service** on a device) | org/admin | `meshy.service` = `{"proto":"http","target":"127.0.0.1:8001","desc":"…"}`. It has its own subregistry for grants. |
| `claude-1.agents.acme.eth` (agent identity) | the human who approved it (the ENS owner, so there is **no** `meshy.owner` record) | **On the agent's own PermissionedResolver**: `addr` (agent EVM address), `meshy.ssh` (agent SSH pubkey; the agent holds a setter role for this key only), `meshy.worldid` (attestation, §4), `agent-context` (ENSIP-26) |
| `claude-1.db1.prod.acme.eth` (**device grant**) | **agent address** (must equal the identity's `addr`) | `meshy.policy` (§2). Expiry is the grant's end; the default is 1 h, and the CLI caps agents at 4 h. Register it with `roleBitmap = 0`, so the agent can't renew, re-point or transfer it. |
| `claude-1.camera.pi.home.acme.eth` (**service grant**) | agent address | none. It's port-level: meshyd forwards to the service's `meshy.service` target while this name is live. |

Devices may also carry enrichment records: `meshy.kind` (`server`, `gpu-server`, `raspberry-pi`, …) and `meshy.meta` (JSON, for example `{"model":"Raspberry Pi 5","gpus":"2x RTX 4090","location":"…"}`).

**How a device's children are classified:** a device's subregistry holds both agent grants and services. A child is a **service** if it has its own subregistry **and** a `meshy.service` record; otherwise it is a grant.

**Delegated admins (Enhanced Access Control):**
- **Registry roles:** `grantRootRoles(REGISTRAR|UNREGISTER|RENEW = 0x11001, admin)` on the network registry and on each device and service registry in it.
- **Resolver roles:** `grantSetterRoles(setText(_, "meshy.policy", _), admin)` on the org resolver.
- **With `--devices`, additionally:** setter roles for `meshy.enckey`, `meshy.ssh-host`, `meshy.roots`, `meshy.endpoint`, `meshy.kind`, `meshy.meta`, `meshy.service`, plus `setAddress(_, 60, _)`, and the registry roles on `agents.<org>`.
- **Admins can never write:** `meshy.orgkey`, `meshy.attestor`, or another network.

**Labels:** `a-z`, `0-9` and `-`, up to 63 characters. The SSH username equals the agent label, and meshyd derives the grant name as `<user>.<resource>`.

## 2. `meshy.policy` (encrypted grant policy)
**Plaintext JSON:**
```json
{"v":1,"grant":"claude-1.db1.prod.acme.eth","grantTokenId":"<decimal tokenId after register>",
 "paths":[{"p":"/srv/reports","m":"ro"},{"p":"/srv/reports/drafts","m":"rw"}],"cmds":[]}
```
- `paths` must be absolute. `/` is rejected. The longest prefix wins, matched on component boundaries.
- `cmds` is optional. It is an allowlist of command basenames, and when it is set no shell is used.

**Envelope** (`meshy/policy/envelope.go`). The record value is JSON:
```
{"v":1,"alg":"x25519-hkdf-sha256-xchacha20poly1305","n":<b64 nonce>,"c":<b64 ciphertext>,
 "r":[{"kid":<hex sha256(pub)[:8]>,"epk":<b64>,"n":<b64>,"wk":<b64 wrapped CEK>}...]}
```
- The content key (CEK) is 32 random bytes. The content is encrypted with XChaCha20-Poly1305, using `aad = grant name`.
- **Per recipient:** `kek = HKDF-SHA256(X25519(eph, rcpt), salt=nil, info="meshy-policy-v1"||epk||rcptPub)`, then `wk = XChaCha20-Poly1305(kek).Seal(cek, aad=grant name)`.
- The recipients are the resource's `meshy.enckey` and the org's `meshy.orgkey`.
- base64 here is RawStd (no padding).
- **Grant flow (dashboard):** call `register` for the grant, read `getState(labelhash).tokenId`, seal the policy, then `setText(grant, "meshy.policy", …)`.

## 3. Agent join (approve URL), the contract between the CLI and the dashboard
1. The admin invites the agent. That reserves `claude-1.agents.acme.eth` (optional) and sends this link:
   `https://<dashboard>/approve?org=acme.eth&agent=claude-1`
2. `meshy join <link>` appends `addr=<agent address>` and `ssh=<authorized_keys line>`, then opens the link. The CLI prints `MESHY_APPROVE_URL <url>`.
3. **On the dashboard `/approve` page:**
   1. The human signs in.
   2. The human completes World ID Agent Pass. The provider (§4) does this and returns the `meshy.worldid` attestation.
   3. The page registers `claude-1` in the agents registry, owned by the human, with the org resolver.
   4. The page sets `addr`, `meshy.ssh`, `meshy.owner`, `meshy.worldid` and `agent-context` in a single `multicall`.
4. The CLI polls ENS until `meshy.ssh` equals its key and `meshy.worldid` is non-empty, then prints `MESHY_JOINED`.

`meshy-admin agent add --approve '<url>' --dev-attest-key …` does step 3 from the command line. This is for dev use only.

## 4. `meshy.worldid` (human-backed attestation; the provider signs it)
The record value is `base64url(JSON)` (no padding) of:
```json
{"v":1,"agent":"claude-1.agents.acme.eth","addr":"0xAgent…","human":"0x<keccak(iss|sub|org)>",
 "acr":"https://world.org/oidc/acr/orb","authTime":1730851200,"iat":…,"exp":…,
 "attestor":"0xProviderSigner…","sig":"0x<65-byte EIP-191 sig over the same JSON with sig=\"\">"}
```
- **Provider (Lane B):** it is the confidential OIDC client of `sandbox.auth.world.org` and runs the device flow. It validates the ID token server-side, checking `iss`, `aud`, `exp`, `auth_time` (which must be after the request) and `acr`/`amr`. Then it calls `worldid.Sign(att, providerKey)`.
- The provider's signer address must appear in the org's `meshy.attestor`.
- **meshyd** verifies the signature, the agent and address match, `exp`, and that the attestor is trusted (`worldid.Verifier`). Without a valid attestation the agent gets `MESHY_NOT_HUMAN_BACKED`.

## 5. Arkiv entities (meshyd writes them; the dashboard reads them)
- **Network:** Rabdi (chain 7794095). Writes go to `0x44…44` `execute((uint8,bytes)[])`; the create op data is a single ABI tuple.
- **Common indexed attributes:** `arkiv.schema` (str), `org` (str), `resource` (str), `ts` (u64, ms), `agent` (str), `grant` (str), `session` (str).
- **Payload:** `payload` (bytes) is a `policy.SealBytes` envelope sealed to `meshy.orgkey`, with `aad` set to the schema name. Without an org key the payload is plaintext and a plain `path` attribute is added (dev only).

| `arkiv.schema` | One per | Extra indexed attributes | Payload |
|---|---|---|---|
| `meshy.session` | session start, end or revoke | `kind` (`session.start`/`session.end`/`session.revoke`); revoke also has `block` (u64), `latencyMs` (u64) and `reason` (str) | full audit event JSON |
| `meshy.blocked` | blocked attempt or denied login | `kind` (`blocked`/`denied`), `op`, `reason` | event JSON, including `path` |
| `meshy.audit-chunk` | flush per session | `tsEnd` (u64), `nOps` (u64) | gzip(JSONL of `op` events: open, read, write, create, unlink, rename, readdir, with bytes) |

- **Lifetime:** 30 days (1,296,000 blocks). Entities are read-only (flag bit 0).
- **Example query:** `arkiv.schema = str('meshy.blocked') AND org = str('acme.eth') ORDER BY ts:u64 DESC`

## 6. Agent-facing strings (stable; the plugin parses them)
- `MESHY_BLOCKED path=<p> op=<op> reason="outside grant|read-only grant" allowed=[<p>:<m>,…] grant=<name> expires=<HH:MM> ask=admins-of-<org>`
- `MESHY_REVOKED grant=<name> block=<n> reason="…"; session terminated` (exit 137)
- `MESHY_EXPIRED grant=<name> at <time>; session terminated` (exit 137)
- `MESHY_NO_GRANT resource=<name> …`, `MESHY_NOT_HUMAN_BACKED: …`, `MESHY_UNREACHABLE …`, `MESHY_UNAVAILABLE chain view is stale`

## 7. meshy-sync HTTP API (`/v1`, implemented in `meshy/provider/server.go`)
- `GET /v1/health` → `{ok, attestor, world: "sandbox"|"production"|"disabled", devAttest, version}`
- `POST /v1/agent/device-flow/start` takes `{org, agent, addr}` and returns `{pollId, userCode, verificationUri, verificationUriComplete, expiresIn, interval}`. This is the World IdP OIDC device flow.
- `POST /v1/agent/device-flow/poll` takes `{pollId}` and returns `{status: "pending"|"denied"|"expired"|"ok", attestation?}`. The ID token is validated server-side (RS256 JWKS, iss, aud, exp, auth_time, acr).
- `POST /v1/agent/attest-dev` takes `{org, agent, addr}` and returns `{attestation, dev: true}`. It only exists with `--dev-attest`.
- `GET /v1/map?org=` returns the org tree from ENS logs. Grants are a flat list (`fullName = <agent>.<target>`); services are not yet classified (STATUS pending #7).
- `POST /v1/admit` takes `{name, wgPubKey, sig}` (EIP-191 by the name's owner over `meshy-admit:<name>:<wgPubKey>`) and returns `{setupKey, managementUrl}`, or 503 without NetBird.

## 8. meshyd authorization (three steps)
1. **Login (`Authenticate`):** the SSH username is the agent label. The key must equal `meshy.ssh` on `<label>.agents.<org>`, the agent must have an `addr`, and it needs a valid `meshy.worldid` attestor signature. No grant is required.
2. **Shell or exec (`AuthorizeSession`):** a live grant `<label>.<resource>`, owned by the agent's `addr`, and a `meshy.policy` that decrypts with the daemon key and matches the token ID. Otherwise the agent gets `MESHY_NO_GRANT …` on stderr and exit 1.
3. **Port-forward (`AuthorizeService`, `direct-tcpip`):** the requested host is the **service ENS name** and must be a child of this resource. It needs a live grant `<label>.<service>`, and meshyd dials the `meshy.service` target (the client's port is ignored). The forward is watched every block and cut on revoke or expiry.

# Interface contracts (freeze at H+1)

All three lanes build against this file. Change it only by PR, and tag all three of us.

## 1. ENS namespace and record keys
See the namespace tree in [PLAN.md](PLAN.md#ensv2-namespace).

| Name level | Record | Value | Written by |
|---|---|---|---|
| org `acme.eth` | `meshy.provider` | provider id (ProviderRegistry) | owner |
| org | `meshy.orgkey` | X25519 pubkey, base64 | owner |
| resource `db1.prod.acme.eth` | addr(60) | daemon EVM address | admin |
| resource | `meshy.wg` | WireGuard pubkey, base64 | admin |
| resource | `meshy.ssh-host` | SSH host key, authorized_keys format | admin |
| resource | `meshy.roots` | JSON array of advertised root paths | admin |
| grant `<agent>.db1.prod.acme.eth` | `meshy.policy` | ECIES envelope, see §2 | admin |
| device `mbp.alice.people.acme.eth` | `meshy.wg` | WireGuard pubkey | member (own resolver) |
| agent `claude-1.agents.acme.eth` | addr(60), `meshy.wg`, `meshy.ssh`, `meshy.owner`, `meshy.worldid`, `agent-context` | | member (own resolver) |

## 2. Policy envelope (`meshy.policy`)
TODO (Lane C): JSON plaintext `{v:1, grantTokenId, paths:[{p,m:"ro"|"rw"}], cmds?:[]}`, encrypted with ECIES to the daemon key and the org key.

## 3. meshy-sync HTTP API (`/v1`)
TODO (Lane B): `POST /admit`, `POST /agent/device-flow/start`, `POST /agent/device-flow/poll`, `POST /agent/attest`, `GET /map`, `GET /health`, each with request and response JSON.

## 4. Arkiv entities
TODO (Lane C): `meshy.session`, `meshy.blocked`, `meshy.audit-chunk`, with attributes, types and expiry.

## 5. Agent-facing error strings
- `MESHY_BLOCKED path=<p> allowed=[<p>:<m>,...] grant=<name> expires=<hh:mm> ask=<owner>`
- `MESHY_REVOKED grant=<name> block=<n>; session terminated`
- `MESHY_EXPIRED grant=<name>`
- `MESHY_NOT_HUMAN_BACKED: complete World Agent Pass via /meshy:join`
- `MESHY_NO_GRANT resource=<name>`

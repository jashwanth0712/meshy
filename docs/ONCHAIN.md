# What Meshy puts on-chain, and why

ENS is public, and that's fine: **who can reach what is meant to be visible and verifiable.** Anyone can open the dashboard without a wallet and see an org's networks, devices, services, agents and grants. We keep exactly one thing private, the **folder paths inside a grant**, which are sealed to the device and the org. Every record below is read by something. If nothing read it, we removed it.

## Names (structure is data)
The hierarchy itself carries the relationships, so we never duplicate them in records.

| Name | Meaning | Who owns it |
|---|---|---|
| `acme.eth` | org | org owner |
| `home.acme.eth` | network | org owner (delegated admins hold EAC roles on its registry) |
| `pi.home.acme.eth` | device | org/admin |
| `camera.pi.home.acme.eth` | service on that device | org/admin |
| `claude-1.agents.acme.eth` | agent identity | the human who approved it (so `meshy.owner` would be redundant) |
| `claude-1.pi.home.acme.eth` | **grant**: folders on the device | the agent's address |
| `claude-1.camera.pi.home.acme.eth` | **grant**: one port (the service) | the agent's address |

Expiry, revocation (`unregister`) and parent/child liveness are native ENSv2 state.

## Records

| On | Record | Read by | Why it must exist |
|---|---|---|---|
| org | `meshy.orgkey` | dashboard (grant sealing), meshyd (Arkiv sealing) | public half of the org's X25519 key; folder paths and audit details are encrypted to it |
| org | `meshy.attestor` | meshyd | which provider signers may vouch for agents (World ID) |
| device | `addr` | dashboard, Arkiv `$owner` filter | the daemon's identity; only entities written by this address count as audit |
| device | `meshy.enckey` | dashboard | the daemon's X25519 key; grant policies are sealed to it |
| device | `meshy.ssh-host` | `meshy` CLI | SSH host key pinned in ENS, so there's no trust-on-first-use |
| device | `meshy.roots` | dashboard | which folders can be offered in the grant picker |
| device | `meshy.endpoint` | `meshy` CLI | how to reach the daemon until mesh DNS is on |
| device | `meshy.kind`, `meshy.meta` | dashboard, map | **enrichment** (for example `raspberry-pi`, `{"model":"Raspberry Pi 5"}`), so a clean name replaces an IP |
| service | `meshy.service` `{proto,target,desc}` | meshyd (forward target), dashboard | the only place the port lives; meshyd forwards to this, never to what a client asks |
| agent | `addr` | meshyd | grant owner must equal this |
| agent | `meshy.ssh` | meshyd | the key the agent logs in with (on the agent's own resolver, so it can rotate it) |
| agent | `meshy.worldid` | meshyd | human-backed attestation; without it the agent is refused |
| agent | `agent-context` | humans, other agents | ENSIP-26 agent description |
| grant | `meshy.policy` | meshyd | **sealed** folder paths (ro/rw), bound to the grant's token ID |

**Removed because they duplicated ENS:** `meshy.owner` on agents (the ENS owner is the approver), and `meshy.kind`, `url` and `description` on services (`meshy.service` already says it), plus the org `description`. Names created before this change may still carry these keys; nothing reads them.

## Off-chain or elsewhere
- **Arkiv:** the audit trail (sessions, blocks, revocations, file-op chunks). Indexed fields are metadata only; paths and commands are sealed to `meshy.orgkey`.
- **Sepolia contracts:** provider stake and slashing (`ProviderRegistry`) and seat billing (`MeshySubscription`).
- **Never on-chain:** file contents, private keys, World ID tokens.

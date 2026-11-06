# Agent security: only approved agents touch the network

Status: **plan only** (6 Nov 2026). No code has been changed for this yet.

**The goal:** a Meshy network must be reachable only by agents a human explicitly approved, and only from the agent instance they approved. A random subagent, another Claude Code or Codex session, a script or a malicious MCP server on the same Mac must not be able to act as `claude-1`. An approved agent should still be able to run commands, read insights and get details without a prompt on every call.

**In one paragraph:** the trust boundary today is "can read `~/.meshy/agent-claude-1/`", which is every process running as this macOS user. The plan moves keys out of reach into a **Meshy agent broker** (`meshy broker`, run by the Mac app as a LaunchAgent, later as its own OS user). The broker is the only thing that can sign as the agent. A Claude Code or Codex session **pairs** with it once (a menu-bar Allow/Deny that names the host app, project folder and agent). It then gets a **short-lived SSH certificate** signed by the agent's ENS key, carrying a session id and a scope. meshyd accepts the certificate, intersects its scope with the on-chain grant, and tags every Arkiv entry with the session id. Revoking a session is a click in the menu bar. Grants stay ENS subnames exactly as today; sessions are off-chain certificates by default, with an optional on-chain form (`s-<id>.claude-1.agents.<org>`) for long-lived or high-privilege sessions.

Contents:
1. [Threat model](#1-threat-model)
2. [The current exposure](#2-the-current-exposure-concretely)
3. [Options and trade-offs](#3-options-and-trade-offs)
4. [Recommended architecture](#4-recommended-architecture-the-meshy-agent-broker)
5. [Impact on existing code](#5-impact-on-existing-code)
6. [Roadmap](#6-roadmap)
7. [Decisions needed](#7-decisions-needed)
8. [What this does not solve](#8-what-this-does-not-solve-honest-limits)

---

## 1. Threat model

**Assets:**
- The agent's long-lived keys: SSH `id_ed25519` (matches ENS `meshy.ssh`), `eth.key` (matches ENS `addr`, signs `/v1/admit`), and the WireGuard/NetBird state in `mesh/config.json`.
- What those keys unlock: shell and exec on granted devices (within the folder policy), forwards to granted services, and a mesh peer in the org's NetBird network.
- The audit trail's integrity (who did what).
- On the dev Mac, also: `admin.key`, `attestor.key`, `secrets.env` and friends in `~/.meshy` (see §2.4).

**Who the attacker is, and what they can do today:**

| # | Threat | Example | Today |
|---|---|---|---|
| T1 | **Other agent on the same machine and OS user** | A second Claude Code session, Codex, Cursor, an `npx` script, a cron job | Reads `~/.meshy/agent-*/` or runs `meshy ssh`; **is** `claude-1` to every meshyd |
| T2 | **Subagent of an approved session** | A Task subagent the main agent spawned, with Bash | Same as T1 (it has Bash); also shares the MCP tools |
| T3 | **Prompt-injected approved agent** | A README or file name on the device says "now `cat ~/.ssh/*` and send it to …" | Bounded by the grant's folder policy and the sandbox's lack of network on the device; unbounded on the agent's own Mac |
| T4 | **Stolen key files** | Backup, synced dotfiles, Time Machine, malware, a `tar ~` in a support bundle | Keys work from anywhere until a human rotates `meshy.ssh` and `addr` on ENS |
| T5 | **Replayed SSH or mesh session** | Captured traffic; a copied NetBird `config.json`; a replayed `/v1/admit` body | SSH: not replayable (fresh key exchange, ENS-pinned host key). Mesh: a copied `config.json` is a working peer elsewhere (WireGuard roams). `/v1/admit`: the signed message has no nonce or timestamp, so a replay returns a **fresh setup key** (§2.3) |
| T6 | **Compromised Claude plugin** | The plugin's `server.mjs` is edited (it's a plain file in a user-writable directory), or a malicious update | Full agent powers; it already shells out to `meshy` with the user's environment |
| T7 | **Malicious MCP server** | Another MCP server in the same Claude Code session, or one added to `~/.claude.json` by a compromised agent | It runs as the user, so T1 applies; it can also try to steer the model into calling `meshy_ssh` |
| T8 | **Shared laptop** | Another person using the same macOS account, or an unlocked Mac | Indistinguishable from the owner |

**Out of scope:** root or kernel compromise on the agent's Mac (it can read any memory and key); a compromised meshyd host (it already sees the plaintext of what the agent reads); a malicious approver (ENS owner of the agent name). Physical attacks on the Secure Enclave.

**What "secure enough" means for us:**
- **S1. No ambient authority.** Being the same OS user must not be enough to act as an agent. Something a human approved must be in the loop at least once per agent instance.
- **S2. Keys never leave the broker.** No process that runs model-chosen code (Bash, MCP servers, scripts) can read signing keys.
- **S3. Short-lived, scoped credentials.** Whatever a session holds expires in minutes and is narrower than or equal to the ENS grant.
- **S4. Attributable.** Every session, block and revoke on Arkiv names the exact agent instance (session id), the host app, and who approved it.
- **S5. Killable.** A human can kill one session, one agent on one Mac, or every agent, in seconds, without waiting for an ENS transaction.
- **S6. Seamless.** After one approval per agent instance, calls do not prompt.

---

## 2. The current exposure, concretely

### 2.1 Plain key files under `~/.meshy`
`meshy/cmd/meshy/main.go` (`createIdentity`, `loadIdentity`) writes and reads:

```
~/.meshy/agent-claude-1/        (MESHY_HOME; drwx------)
  agent.json                    identity + mesh URL         -rw-------
  id_ed25519                    SSH key = ENS meshy.ssh     -rw-------
  eth.key                       hex secp256k1 = ENS addr    -rw-------
  mesh/config.json              NetBird state incl. the WireGuard private key
```

`0700` / `0600` only stops **other** OS users. Every process of this user, including every Bash tool call from every agent, can `cat` these. Nothing is encrypted, nothing is in the Keychain, nothing asks.

### 2.2 Any process can call `meshy ssh`
- The CLI authenticates *nobody*: whoever runs it gets the keys in `MESHY_HOME`.
- The plugin's MCP server (`agent/claude-plugin/mcp/server.mjs`) is a convenience wrapper, not a boundary. It just spawns `meshy ssh <resource> -- <command>`. An agent with Bash, or a subagent, can run the CLI directly and skip MCP entirely.
- meshyd (`meshy/meshyd/auth.go`, `Authenticate`) checks the key against ENS `meshy.ssh`, the `addr` record and the `meshy.worldid` attestation. All three describe the **agent identity**, not the **instance**. Two processes holding the same key are the same agent to meshyd and on Arkiv (`agent=claude-1`).
- The World ID attestation proves a human approved the identity once. It is a public ENS record, so it proves nothing about the process presenting the key.

### 2.3 Mesh state is reusable
- `mesh/config.json` holds the WireGuard private key and NetBird peer state. Copied to another machine, `meshy-mesh serve --state <copy>` is the same peer (WireGuard roams to the new endpoint). It stays in the mesh until the agent's ENS name is gone (reconciler eviction).
- `/v1/admit` (`meshy/provider/admit.go`) signs `meshy-admit:<name>:<wgPubKey>` with **no timestamp, nonce or audience**. Anyone who sees one admission body (logs, proxies, the `attestations.jsonl` evidence) can replay it and receive a **new one-off setup key** into the agent's NetBird group. Unless `meshy.wg` is set on ENS (it usually isn't), the setup key is not bound to that WireGuard key, so the replayer can enrol their own peer.
- **Mitigating factor:** the mesh is only reachability. NetBird policies open only `tcp/22` toward granted devices, and meshyd still demands the SSH key. A mesh-only attacker can knock on the door but not log in. The real exposure is the SSH key (§2.1).

### 2.4 The dev Mac is worse than the design
`~/.meshy` on the dev Mac also holds `admin.key` (the org admin), `attestor.key` (the World ID attestor signer that `meshy.attestor` trusts), `secrets.env`, `aws.env`, `netbird.env` and `meshy-provider.pem`, next to the agent's keys. Any agent on this Mac can self-attest a new "human-backed" agent and grant itself anything. This is a demo-setup artifact, but the same user also runs Claude Code here. **Quick win Q0:** move these out of `~/.meshy` (and out of reach of agent sessions) before the demo.

### 2.5 Subagents are invisible
A Claude Code subagent runs inside the same process as its parent and shares its MCP connections. No OS mechanism can tell them apart, and neither can Meshy. See §8 for what we can and can't do.

---

## 3. Options and trade-offs

Each option names what threat it addresses and what it costs in seamlessness and effort (S = hours, M = a day, L = several days).

### 3.1 Key custody

| Option | How | Stops | Doesn't stop | Seamless? | Effort |
|---|---|---|---|---|---|
| **A. macOS Keychain items with an app ACL** | Store `id_ed25519`, `eth.key` and the WG key as generic-password items. Legacy ACL: only `Meshy.app` / `meshy broker` is a trusted app; anything else gets a "allow access?" dialog. Data-protection keychain + `keychain-access-groups` is stronger but needs a Developer ID–signed, provisioned app | `cat` by other processes (T1, T2, T4 for file backups) | A user clicking "Always Allow"; a process that runs the trusted binary itself (§3.2) | Yes, after setup | S–M. Note: today the app is **ad-hoc signed** (`mac/build.sh`, `--sign -`), so the ACL is a cdhash and every rebuild re-prompts. Developer ID fixes that |
| **B. Secure Enclave keys** | Non-exportable P-256 key, optionally Touch ID per use (like Secretive). SSH supports `ecdsa-sha2-nistp256` | Key theft entirely (T4); copying to another Mac | **secp256k1 isn't supported**, so the eth key can't live there. ed25519 neither, so `meshy.ssh` must switch to P-256 | Touch ID per use is not seamless; "unlocked while logged in" is | M. Wrap `eth.key` with an SE key (encrypted at rest, decrypted only in broker memory), or move admission signatures to P-256 (meshy-sync verifies off-chain anyway; Sepolia has the P256VERIFY precompile since Fusaka) |
| **C. ssh-agent with confirm-on-use** | `ssh-add -c`: the agent asks (via `SSH_ASKPASS`) before each signature | Silent use by others | No context (*which* process?), prompts on every connection, and the socket is reachable by any same-user process | No | S |
| **D. Hardware key or passkey** | YubiKey `sk-ssh-ed25519@openssh.com` with touch; passkey for SIWE | Theft, remote use | Needs a touch per login; agents run unattended | No, for agents. Good for **approvers** and the kill switch | S–M |
| **E. Separate OS user owns the keys** | The broker runs as `_meshy` (LaunchDaemon) and keys live in `/var/db/meshy` (0700 `_meshy`). Clients talk over a Unix socket | All file reads by the user's processes, cleanly, with no prompts | Clients still need to be authenticated at the socket (§3.2) | Yes | M: needs a pkg installer (admin password once) |

**Takeaway:** custody alone doesn't solve "which process may use the key" (S1). It removes the `cat` path. Something has to stand between the key and the callers: a broker.

### 3.2 Binding an identity to one agent instance

**a) Per-session ephemeral keys or certificates.**
- The agent's long-lived key (`meshy.ssh`) acts as an **SSH certificate authority** for its own sessions. The broker generates a fresh ed25519 key per session and signs an OpenSSH certificate:
  - principal: `claude-1`
  - KeyId: `s-7f3a9c` (session id)
  - valid for 15–60 min
  - `source-address`: the mesh IP, once the broker owns the mesh peer
  - extension `scope@meshy.live`: the capability (§3.3)
- meshyd checks the cert's signature key against `meshy.ssh`. No ENS write per session, and it's instant.
- *Alternative:* sessions as ENS subnames `s-7f3a9c.claude-1.agents.<org>` with their own `meshy.ssh` and expiry. That's on-chain, revocable by the existing block watcher, and a strong ENS story. But it costs a transaction (12 s+ and gas) per session, it's public metadata, and it needs a subregistry under every agent.
- *Recommendation:* certificates by default. ENS session names are an opt-in "pinned session" for long-running or CI agents, and for the demo of the ENS angle.

**b) Process attestation (who is calling the broker).**
- On macOS, the broker can get the peer of its Unix socket (`LOCAL_PEERPID`, and an audit token via `LOCAL_PEERTOKEN`). From that it can read the caller's code signature (`SecCodeCopyGuestWithAttributes`) and walk the **parent chain**: `claude` (signed by Anthropic) → `meshy mcp`; or Codex (signed by OpenAI) → `meshy mcp`.
- **The limit:** code signing proves *which binary* is running, not *who is driving it*. Bash can run the signed `meshy` binary itself.
- So attestation is used for:
  - showing a truthful approval prompt ("Claude Code, in `~/proj`, PID 4123")
  - rejecting obvious misuse: a parent that is a shell (`zsh`, `bash`) rather than a known agent host, or a process reparented to `launchd`
- It is never sufficient alone.

**c) A local broker daemon.**
- It owns the keys (Keychain, SE or a separate OS user) and answers only **paired** clients over a Unix socket.
- Pairing creates a per-instance secret held **only in the client process's memory**: not on disk, and not in the environment (`ps -E` shows same-user environments on macOS).
- Every later request is authenticated with that secret plus the same peer PID, so a different process can't reuse it.
- This is the piece that delivers S1, S2 and S5. *Recommended.*

### 3.3 Scoped capability tokens per task

| Format | Pros | Cons |
|---|---|---|
| **SSH certificate extension** (JSON in `scope@meshy.live`) | meshyd already speaks SSH (`golang.org/x/crypto/ssh` parses certs); no new dependency; the scope travels with the login | No offline attenuation by the holder |
| **Biscuit** (`biscuit-go`) | Datalog checks; **offline attenuation**: an MCP session could mint a narrower token for a subagent without asking the broker; public-key verifiable | New dependency, and the policy language is new for the team |
| **Macaroons** | Simple caveats, attenuable | Symmetric (HMAC), so meshyd would have to share a root secret with the broker. That doesn't fit (meshyd is remote and trusts only ENS) |
| **EIP-712 typed data** | Verifiable on-chain; fits ProviderRegistry-style evidence | No attenuation, verbose; nothing on-chain needs it per session |

**What the scope contains:** this is always **intersected** with the grant's sealed `meshy.policy`. It can only narrow.

```json
{"v":1,"sid":"s-7f3a9c","agent":"claude-1.agents.meshydemo.eth",
 "host":{"app":"claude-code","team":"<host Team ID>","cwd_h":"h:91c2…"},
 "res":["db1.prod.meshydemo.eth"],            // subset of live grants
 "paths":[{"p":"/srv/reports","m":"ro"}],      // ⊆ policy paths
 "cmds":["ls","cat","rg","du"],                // ⊆ policy cmds (or any, if the policy has none)
 "max_sessions":4, "rate":"60/m", "exp":1762430000}
```

**Recommendation:** use the SSH-cert extension now. Keep Biscuit in mind for subagent attenuation later (§8).

### 3.4 Human in the loop
- **Pairing prompts in the Mac app.** A new client asks to act as an agent, and a notification or menu-bar sheet appears:

  > **Claude Code** wants to use **claude-1.agents.meshydemo.eth**
  > Project `~/code/reports-bot` · PID 4123 · signed by Anthropic
  > Scope: db1 (read-only) · for 8 h
  > [Deny] [Allow once] [Allow for this project]

  - "Allow for this project" remembers (host Team ID, project dir, agent) for N hours, so a Claude Code restart re-pairs silently with a notification instead of a prompt.
  - Optional Touch ID for `rw` scopes or `prod` networks.
- **World ID per agent** (already in place) proves a human stands behind the *identity*. Requiring World ID per *session* is too much friction. Use it as **step-up** instead: re-verify with World ID when a session asks for a scope the agent has never used (first `rw`, first new network). This also makes a nicer World prize story.

### 3.5 Separate OS users or sandboxing for agents
- **One macOS user per agent host** (run Claude Code as `agent-bob`). This is the strongest isolation of T1 and T8, but it's clumsy: separate logins, file sharing and GUI.
- **Sandbox the agent host's tools.**
  - Claude Code's Bash sandbox and permission rules can **deny reads of `~/.meshy`** and the broker socket path.
  - Codex's sandbox modes mainly restrict writes and network, so check what read restrictions it supports before relying on them.
  - This is cheap defense in depth, but it relies on the host being configured right, and it's per host.
- **The broker as its own OS user** (option E) gets most of the OS-user benefit with none of the clumsiness. *Recommended later.*

### 3.6 Per-session `meshy.policy` constraints
The policy already supports `cmds` (an allow-list of basenames, with no shell when it is set; see `meshyd/server.go`). Use it in two places:
- **Grants:** offer presets in the dashboard ("read-only explore": `ls cat head tail rg find du stat`; "logs"; "full shell").
- **Session scope:** the session's cert can narrow `cmds` and `paths` further. Effective = grant ∩ session.

### 3.7 Rate limits and anomaly kill switches
- **Broker side** (cheap, local):
  - per-session limits on calls per minute, concurrent sessions and bytes out
  - **auto-suspend** on a burst of `MESHY_BLOCKED` (for example 5 in 60 s): policy probing is a known signal (insights PLAN §6)
  - suspend after N denied pairing attempts from the same host
- **meshyd side:** a per-agent cap on concurrent sessions, and a per-cert-session cap. Optionally, reject a cert whose KeyId is in a local deny-list, pushed via the control socket for local devices.
- **The panic button:** "Stop all agents" in the menu bar.
  1. The broker drops every session secret and stops signing.
  2. It kills local meshyd sessions via the control socket.
  3. *Optionally*, it sends one ENS transaction rotating `meshy.ssh` for the agent. meshyd then needs an **identity watch**: re-check `meshy.ssh` for agents with live sessions on every block, like the grant watcher does. That kills them everywhere in one block.

### 3.8 Attribution on Arkiv
- Add an indexed `instance` attribute (the session id `s-7f3a9c`, random, so no metadata leaks) to `meshy.session`, `meshy.blocked` and `meshy.audit-chunk`.
- The sealed payload adds: host app and Team ID, project-dir hash, the approver (the address that approved the pairing, i.e. the Mac app's SIWE session), and the scope hash.
- The dashboard Activity and insights views can then group by instance. Example: "claude-1 / Claude Code in reports-bot / approved 10:02 by 0x45e8…" versus "claude-1 / Codex in infra".

---

## 4. Recommended architecture: the Meshy agent broker

### 4.1 Components

```
 ┌───────────────── agent's Mac ───────────────────────────────────────────┐
 │                                                                         │
 │  Claude Code ──stdio──▶ meshy mcp ─┐   Codex ──stdio──▶ meshy mcp ─┐    │
 │   (Anthropic-signed)    (signed)   │    (OpenAI-signed)            │    │
 │                                    ▼                               ▼    │
 │              ~/Library/Application Support/Meshy/broker.sock (0600)     │
 │                                    │                                    │
 │  ┌─────────────────────────────────▼──────────────────────────────┐     │
 │  │ meshy broker  (LaunchAgent from Meshy.app; later _meshy user)  │     │
 │  │  • keys: Keychain (ACL: broker only) / SE-wrapped eth key      │     │
 │  │  • pairing table: sid → {peer pid, host, cwd, scope, exp}      │     │
 │  │  • mints SSH certs (KeyId = sid) from the agent key            │     │
 │  │  • owns ONE long-running mesh peer (meshy-mesh embed)          │     │
 │  │  • runs exec/curl/connect itself; clients never see keys       │     │
 │  │  • rate limits, auto-suspend, audit of local calls             │     │
 │  └───────────────┬──────────────────────────▲─────────────────────┘     │
 │                  │ XPC / socket             │ Approve / Revoke          │
 │            Meshy.app menu bar ──────────────┘ (SIWE session = approver) │
 └──────────────────┼──────────────────────────────────────────────────────┘
                    │ WireGuard (NetBird) → tcp/22
                    ▼
            meshyd on db1  ── checks cert: signed by ENS meshy.ssh, valid,
                              principal, scope ∩ grant policy; audits sid
                              to Arkiv; watcher kills on revoke/expiry.
```

**Key properties:**
- **`meshy mcp`** replaces `server.mjs`. It's the same tools, written in Go and shipped signed inside `Meshy.app` (and as a standalone binary). It holds only a pairing secret, in memory. The plugin's `.mcp.json` points at it. The old JS server can stay as a thin launcher that `exec`s it.
- **The `meshy` CLI becomes a broker client.** `meshy ssh` from a terminal pairs like any other client. For humans in a terminal the prompt says "Terminal (zsh) in ~/proj". The CLI can still read files directly behind `--insecure-file-keys` for dev and CI.
- **The broker does the network I/O.** That makes "no keys in client processes" true, and it fixes STATUS P1's 8–25 s per-connection mesh start, because there's one warm peer.

### 4.2 Flow: pairing (once per agent instance)

```
meshy mcp (started by Claude Code)            broker                       Meshy.app (human)
  │ connect broker.sock                         │                                 │
  │ HELLO {agent?:"claude-1", want:{res,mode}}  │                                 │
  │────────────────────────────────────────────▶│ peer pid → audit token          │
  │                                             │ code sig of peer + parent chain │
  │                                             │ reject if parent is a shell /   │
  │                                             │ launchd / unsigned (policy)     │
  │                                             │ remembered (team,cwd,agent)?    │
  │                                             │──── no ───▶ "Claude Code wants  │
  │                                             │            claude-1 …" [Allow]  │
  │                                             │◀──────────── Allow 8h ──────────│
  │                                             │ (step-up World ID / Touch ID    │
  │                                             │  if scope is new or rw/prod)    │
  │◀──── PAIRED {sid:"s-7f3a9c", secret, exp} ──│ record sid → pid, scope, exp    │
  │ keeps secret in memory only                 │ notify "claude-1 paired (s-7f3a9c)"
```

- **Re-pairing** happens when the process restarts (new PID). If the (host Team ID, cwd, agent) triple is remembered and unexpired, it's silent with a notification.
- **A shell-spawned `meshy mcp`** (Bash running it) has a shell as its parent, so the default policy denies it, or always prompts and never auto-allows.

### 4.3 Flow: run a command (seamless)

```
Claude Code → meshy mcp: tools/call meshy_exec {resource:"db1.prod…", command:"rg -n q3 /srv/reports"}
meshy mcp → broker:      EXEC {sid, mac(secret, req)}           (same peer pid as at pairing)
broker:                  check sid live, pid matches, rate ok, resource ∈ scope
                         ensure cert for (sid, db1) — mint if none or < 2 min left:
                           ephemeral ed25519, cert{principal claude-1, KeyId s-7f3a9c,
                           valid 30m, ext scope@meshy.live}, signed by agent key (Keychain)
                         dial db1 over the warm mesh peer, SSH with the cert
meshyd(db1):             Authenticate: cert.SignatureKey == ENS meshy.ssh(claude-1),
                           principal == user, now ∈ validity, worldid ok
                         AuthorizeSession: live grant + policy (unchanged)
                         effective = policy ∩ cert scope; cmd ∈ cmds, paths ∩
                         sandbox; audit {agent, instance:s-7f3a9c, grant}
broker → meshy mcp:      {exit_code, stdout, stderr, meshy codes}  (same output contract as today)
```

The `MESHY_*` strings and the MCP output format are unchanged, so the skill and the agent's behaviour don't change.

**Tools on the same path:**
- `meshy_exec` (alias `meshy_ssh`)
- `meshy_grants`
- `meshy_whoami`
- `meshy_details`: device kind, meta, roots and services from `/v1/map` plus ENS
- `meshy_insights`: the agent's *own* sessions, blocks and revokes. These are served by meshyd and the broker, scoped to the agent, and quoted as data (insights PLAN §6, risk 1)

Read-only tools (`whoami`, `grants`, `details`) still require pairing, but need no device grant.

### 4.4 Flow: revoke a session

| What | Who | How fast | Mechanism |
|---|---|---|---|
| **One session** | Menu bar → Agents → claude-1 → `s-7f3a9c` (Claude Code · reports-bot) → **Revoke** | Instant locally; ≤ cert TTL remotely, or instant with the identity watch below | The broker forgets the sid and refuses to mint. It closes its live SSH connections for that sid (the broker holds them, so it can). Remote meshyds see no new logins from it |
| **All sessions of an agent on this Mac** | Menu bar → **Stop claude-1 here** | Instant | Same, for every sid of the agent, plus the broker tears down the mesh peer |
| **Everything, everywhere** | Menu bar → **Panic: rotate claude-1** | One Sepolia block | One ENS tx (the approver's wallet) sets a new `meshy.ssh` (broker-generated). meshyd's **identity watch** kills every live session whose cert was signed by the old key. Combine with grant revoke if needed |
| **The grant itself** | Dashboard (today) | One block (verified: 0.75–1.6 s after mining) | Unchanged |

All of them write `meshy.session` `kind=session.revoke` with `instance=sid` and `reason=broker|rotate|grant`.

### 4.5 How it maps onto ENS

| Concept | Representation | On-chain? |
|---|---|---|
| Agent identity | `claude-1.agents.<org>`: `addr`, `meshy.ssh` (**becomes the session CA key**), `meshy.worldid` | yes (unchanged) |
| Where the agent may run (new, optional) | `meshy.hosts` on the agent: a list of broker host keys (one per Mac) allowed to mint for it. meshyd checks that the cert carries a broker signature or extension from one of them | yes; one record, set at approval |
| Grant | `claude-1.db1.prod.<org>` + `meshy.policy` | yes (unchanged) |
| Session (default) | SSH certificate, KeyId `s-<id>`, TTL 15–60 min | no; Arkiv `instance` attribute |
| Pinned session (opt-in) | `s-<id>.claude-1.agents.<org>`: `meshy.ssh` = that session's key, ENS expiry = the session's end, `meshy.scope` = the scope | yes; revocable by the existing watcher |

**About `meshy.ssh` as a CA key:**
- Keep the record value an `authorized_keys` line. meshyd treats a **plain key** as before (backward compatible: an old CLI with file keys still works until the org flips a switch). It treats a **certificate** signed by that key as a session.
- Add an org-level switch `meshy.require-session=1` on the org name. Once set, meshyd refuses bare keys, so file-key clients stop working: the "no ambient authority" end state.

### 4.6 What each threat looks like afterwards

| Threat | After the broker |
|---|---|
| T1 other agent, same user | No keys to read (Keychain/SE/`_meshy`). Calling the broker means pairing, which needs a human click, and the prompt names the real caller |
| T2 subagent | Shares the parent's paired MCP session: same scope and audit sid, no more power. A subagent's Bash running `meshy` is a new, shell-parented client, so it is denied. See §8 |
| T3 prompt-injected approved agent | Bounded by grant ∩ session scope. `cmds` presets make "read-only explore" the default. Burst of blocks → auto-suspend |
| T4 stolen files | Nothing useful on disk. SE keys can't be exported; Keychain items need the broker's code identity |
| T5 replay | SSH as before. Admission signs `meshy-admit:v2:<name>:<wg>:<providerId>:<ts>:<nonce>`, valid 5 min, nonce cached; meshy-sync binds the setup key to the WG key and the reconciler pins the peer key. A copied mesh state is useless when the WG key lives in the broker |
| T6 compromised plugin | The plugin is just a pointer to a signed binary. A modified `server.mjs` is a new client, and the prompt says "unsigned node script" |
| T7 malicious MCP server | It can't read keys and can't pair silently. It can still try to steer the model into calling `meshy_exec`, which is bounded like T3 |
| T8 shared laptop | Pairing prompts and step-up (Touch ID) need the owner. The panic button is there |

---

## 5. Impact on existing code

| Component | Change | Size |
|---|---|---|
| **meshyd auth** (`meshy/meshyd/auth.go`, `server.go`) | `Authenticate`: accept `*ssh.Certificate`: check `SignatureKey` == `meshy.ssh`, `CertType == UserCert`, principal == user, validity window (with 60 s skew), parse `scope@meshy.live`. Put `Instance` and `Scope` on `Identity`. `AuthorizeSession`: effective policy = policy ∩ scope (paths, cmds); `AuthorizeService`: service ∈ scope. Kill live sessions at cert `ValidBefore` (add to the expiry timer). Optional `meshy.require-session`. **Identity watch**: re-check `meshy.ssh` per block for agents with live sessions. Tests in `server_test.go` (cert ok, expired, wrong CA, scope narrowing, scope widening refused) | M |
| **audit / arkivkit** | `Instance` on `audit.Event`, indexed `instance` attribute on all three schemas; payload adds host/approver | S |
| **policy** (`meshy/policy`) | `Intersect(policy, scope)`: longest-prefix-aware path intersection; `cmds` intersection | S |
| **meshy CLI** (`meshy/cmd/meshy`) | New `meshy broker` (socket server, pairing table, cert minting, exec/curl/connect, mesh peer) and `meshy mcp` (stdio MCP to broker). Existing commands become broker clients; `--insecure-file-keys` keeps the old path. `join` generates keys **inside the broker** (Keychain) and only prints public parts | L (broker), M (mcp) |
| **meshnet / meshadmit** | The broker keeps one long-running `meshy-mesh` peer and dials through it (local SOCKS or in-process). WG key from the broker, not a file (or a broker-only dir under `_meshy`). Admission message v2 with ts, nonce and provider id | M |
| **meshy-sync admission** (`meshy/provider/admit.go`, `netbird/reconciler.go`) | Accept v2 messages (5-min window, nonce cache); keep v1 behind a flag until clients upgrade. Bind the setup key to the presented WG key (reject or evict a peer in the group whose key differs from the admitted one). Optional `meshy.hosts` check | S–M |
| **Claude plugin** (`agent/claude-plugin/`) | `.mcp.json` runs `meshy mcp` (from `Meshy.app` or `$PATH`). `server.mjs` becomes a 10-line launcher or is deleted. New tools `meshy_details`, `meshy_insights`. Skill text: "always use the MCP tools; never call the `meshy` CLI from Bash". `/meshy:status` shows the paired sid | S |
| **Mac app** (`mac/Sources`) | Runs `meshy broker` as a LaunchAgent (like `live.meshy.meshyd` in `Device.swift`). New "Agents on this Mac" submenu: sessions per agent (host, project, age, scope), Revoke, Stop agent here, Panic rotate. Pairing sheet and notifications. Uses the SIWE session (`Session.swift`) as the approver identity. **Developer ID signing** (today it's ad-hoc) for stable Keychain ACLs and code-sig checks by Team ID | M–L |
| **Dashboard** (`web/`) | Activity and insights: group by `instance`. Agent page: "where it runs" (`meshy.hosts`), command presets on Give access | S–M |
| **Docs** | INTERFACES §3 (join keys in the broker), §7 (admit v2), §8 (cert step), new §: broker socket protocol; RUNBOOK; STATUS | S |

**Broker socket protocol** (sketch; JSON lines over a `SOCK_STREAM` Unix socket; each request carries `sid` and `mac = HMAC(secret, seq || body)`):
- `hello`
- `pair`
- `whoami`
- `grants`
- `details`
- `exec`, which streams `stdout`/`stderr`/`exit`
- `curl`
- `connect`, which returns a local port bound to the peer's UID, or does fd passing
- `insights`
- `list-sessions`, `revoke`, `suspend`: menu-bar only, checked by the caller's code signature == Meshy.app

---

## 6. Roadmap

The deadline is **Sat 7 Nov 09:00 IST**, about 15 hours from writing. Everything in "before judging" is chosen to be demoable and to stay off the critical path of the existing demo (`e2e/live-demo.sh`, DEMO-3-MACS).

### Before judging (quick wins, in priority order)

| # | What | Why it demos | Effort |
|---|---|---|---|
| **Q0** | Move `admin.key`, `attestor.key`, `secrets.env`, `aws.env`, `netbird.env` and `meshy-provider.pem` out of `~/.meshy` on the demo Macs (to a separate macOS user or an encrypted volume that is locked during the demo). | It removes the worst exposure; there's no story to tell otherwise | 15 min, no code |
| **Q1** | **meshyd accepts SSH certificates** signed by `meshy.ssh` (§5 row 1, without the scope intersection or identity watch). Plain keys still work. KeyId goes into audit and Arkiv as `instance` | "Every action is attributable to the exact agent instance" shows on Arkiv and in the Activity page | 3–4 h incl. tests |
| **Q2** | **Minimal broker in `meshy`**: `meshy broker` holds the file keys (unchanged storage), listens on a 0600 socket, and mints a 30-min cert per paired client. `meshy mcp` (Go) pairs and calls `exec`. Pairing approval is a **macOS notification with Allow/Deny via `osascript`/`terminal-notifier`**, or a simple dialog in the Mac app. No Keychain yet | The demo: a second Claude Code or a script tries `meshy_exec` → a prompt appears → Deny → `MESHY_NOT_PAIRED`. The approved session works with no further prompts | 5–6 h |
| **Q3** | **Plugin points at `meshy mcp`**; skill says "use MCP tools only". | The real Claude Code session demo (STATUS P2 #8, never run live) | 1 h |
| **Q4** | **Admission v2** (timestamp + nonce + provider id) and **setup-key ↔ WG-key binding** in meshy-sync | Closes a real replay hole cheaply, with a clear one-line story | 1–2 h |
| **Q5** | **Command presets** on Give access ("read-only explore") using the existing `cmds` | It already works in meshyd; this is UI only | 1 h |

*If time is short:* do Q0, then Q1 with a `meshy ssh --cert` path (the CLI signs its own short cert from the file key). That alone gives per-instance attribution on Arkiv, which is the most visible part. The pairing story (Q2) is the most compelling demo but the riskiest to finish.

### After judging

| Phase | What | Effort |
|---|---|---|
| **P1: custody** | Keys in the Keychain with an ACL to the broker. Developer ID signing and notarization for `Meshy.app`, so the ACL and Team ID checks are stable. `join` generates keys inside the broker. Remove file keys (keep `--insecure-file-keys` for CI) | 1–2 d (plus Apple account) |
| **P2: broker v1** | Full menu-bar UI (Agents submenu, per-session revoke, Stop agent here), remembered approvals per (Team ID, cwd, agent), peer code-sig and parent-chain checks, rate limits and auto-suspend, `meshy_details` and `meshy_insights` | 3–4 d |
| **P3: scopes** | `scope@meshy.live` intersection in meshyd; scope chooser in the pairing sheet; World ID step-up for new rw or prod scopes | 1–2 d |
| **P4: the warm mesh peer in the broker** | One long-running embed client; dial through it (also fixes 8–25 s connects); WG key from the broker | 1–2 d |
| **P5: kill everywhere** | Identity watch in meshyd; Panic rotate in the menu bar (one ENS tx); `meshy.require-session` org switch | 1 d |
| **P6: hardening** | Broker as the `_meshy` user via a pkg (LaunchDaemon); SE-held P-256 SSH key + SE-wrapped eth key; `meshy.hosts`; optional pinned ENS session names `s-<id>.claude-1.agents.<org>`; Biscuit attenuation for subagents; Linux broker (systemd user + `SO_PEERCRED`) | 1–2 w |

---

## 7. Decisions needed

| # | Decision | Options | Recommendation |
|---|---|---|---|
| **D1** | What to build before judging | (a) Q0 + Q1 only, (b) Q0–Q3, (c) nothing; pitch the plan | **(b)** if one person can take it from ~22:00 IST; else (a) |
| **D2** | Session representation | (a) SSH certs (off-chain), (b) ENS subnames per session, (c) certs + opt-in pinned ENS sessions | **(c)**; certs for every session, ENS for long-lived or CI agents and the ENS prize story |
| **D3** | What a pairing approval covers | (a) one process, (b) (host app, project dir, agent) for N hours, (c) per agent forever | **(b)** with N = 8 h; (a) for rw or prod |
| **D4** | Where keys live (post-hackathon) | Keychain ACL, Secure Enclave (needs P-256 SSH and a wrapped eth key), broker as a separate OS user | **Keychain now, separate OS user next, SE later** |
| **D5** | Apple Developer ID ($99/yr) for signing and notarizing `Meshy.app` | yes / no | **Yes**: stable Keychain ACLs and Team-ID caller checks depend on it |
| **D6** | Shell-parented clients (`meshy` from Terminal or a Bash tool) | (a) deny, (b) always prompt and never remember, (c) allow for humans | **(b)**: humans in Terminal can still use it; agents' Bash can't do it silently |
| **D7** | When to flip `meshy.require-session` (refuse bare keys) on `meshydemo.eth` | after the demo / never for the demo org | **After the demo**, once the plugin and CLI use the broker |
| **D8** | World ID step-up per new scope | yes / no / only rw on prod | **Only rw, or a network the agent never used** |
| **D9** | Public metadata | an `instance` attribute on Arkiv (random id) and `meshy.hosts` on ENS (broker public keys) | Accept both; neither identifies a person |

---

## 8. What this does not solve (honest limits)

- **Subagents inside an approved session.** A Claude Code subagent shares its parent's process and its MCP connection, so Meshy sees it as the parent session. Meshy can guarantee it gets **no more** than the parent's scope, that it can't read keys, and that its Bash can't pair silently. It cannot tell the subagent apart. Options:
  - configure subagents in Claude Code with tool allow-lists (`tools:` in `.claude/agents/*.md`, excluding `mcp__meshy__*`)
  - later, Biscuit attenuation: the MCP server mints a narrower token when told it's serving a subagent. That needs host cooperation, which MCP doesn't offer today
- **Model behaviour within scope.** An approved, prompt-injected agent can still misuse what it legitimately has. The answer is a narrow scope (presets, `cmds`), short TTLs, auto-suspend and the audit trail, not identity.
- **Root on the agent's Mac** reads everything, including broker memory. SE keys limit that to "use while compromised", not "steal".
- **The Keychain "Always Allow" button.** Until the app is Developer ID–signed and uses the data-protection keychain, a user can grant another app access. The separate-OS-user broker removes that path.
- **A human approving the wrong prompt.** The prompt must be truthful (code signature, parent chain, project dir) and rare (remembered approvals), or people click through it.

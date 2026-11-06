# Audit: plan vs. built, and prize depth

Written on Fri 6 Nov. This is an honest check of [PLAN.md](PLAN.md) against what exists in the repo, and of each prize track against the official ETHGlobal Mumbai requirements.

## 1. The plan, component by component

| Plan item | Status | Notes |
|---|---|---|
| meshyd: SSH server, ENS authz, revoke kills sessions, fail-closed | ✅ done, tested | Live on Sepolia: revoke takes 6.8–15.8 s total, and the daemon kills the session about 150 ms after it sees the block |
| Linux sandbox (bwrap + FUSE gate, per-op audit) | ✅ done, tested in Docker | 9 escape attempts blocked |
| macOS sandbox (Seatbelt) | ✅ done, tested | |
| macOS read audit (eslogger) | ⚠️ built, **never run live** | Needs root and Full Disk Access |
| `meshy` CLI + Claude Code plugin | ✅ built | ⚠️ **The plugin has not been exercised inside a real Claude Code session** |
| Arkiv audit writer | ✅ live on Rabdi | |
| Contracts (ProviderRegistry + MeshySubscription) | ✅ deployed and verified on Sepolia | |
| Dashboard: map, grants, revoke timeline, activity, resources, agents, approve, billing, provider | ✅ built; reads live Sepolia + Arkiv | ⚠️ **No write has been tested with a real wallet yet** (grant, revoke, approve, add resource) |
| Dashboard onboarding wizard (create the `.eth` org from the UI) | ❌ missing | Only `meshy-admin org init` does this today |
| Provider: meshy-sync (World device flow, attestations, `/v1/map`, admit, NetBird reconcile) | 🔄 in progress | |
| Provider: **NetBird mesh actually running** | ❌ not deployed (no VPS) | **The "Tailscale alternative" part has never been demonstrated.** Agents currently reach meshyd over plain TCP on localhost. |
| `meshy-mesh` (userspace WireGuard bridge) | ⚠️ builds; **never connected to a real NetBird** | |
| Provider-signed **admission attestations** (evidence for slashing) | ❌ missing | So the slashing contract has no real evidence source yet, and a slash has never been demoed |
| Seat enforcement at admission | ❌ missing (meshy-sync admit doesn't check MeshySubscription yet) | |
| People and devices namespace (`alice.people.<org>`, `mbp.alice.people.<org>`, `meshy.wg`) | ❌ **not implemented** | Only agents and resources exist |
| Mac menu-bar app | ⚠️ built (custom Go systray) | Shows status only. No wallet sign-in, no device registration, doesn't join the mesh. The plan's NetBird-UI fork was dropped. |
| Privy sign-in | ❌ dropped (browser wallet only) | No Privy app ID |
| Teams (`eng.teams.<org>`) | ❌ skipped (it was a stretch goal) | |
| Simulation suite (toxiproxy, provider kill, clock skew, load) | ❌ not done | Fail-closed is covered by unit tests only |
| Live demo URL, public repo, video | ❌ not done | All three are required for submission |

## 2. Prize depth

### ENS: Best Use of ENSv2 (main prize)
What they ask for, and what we have:
- **Built on ENSv2 Sepolia, central to the product.** ✅ ENSv2 *is* the access-control plane: grants are subnames, revoke is `unregister`, and the daemon re-checks ENS every block and kills sessions. This is the strongest part of the project.
- **Deploy your own subname registry.** ✅ One UserRegistry per org, network, resource (grants) and agents.
- **Expiring, revocable, non-transferable subnames.** ✅ Grants have a native expiry, can be unregistered, and use `roleBitmap = 0`.
- **Agents as namespaces with their own identity and permissions** (the named bonus). ✅ `claude-1.agents.<org>` carries addr, SSH key, World attestation and `agent-context`.
- **Enhanced Access Control** (role-based delegation, "edit only certain text records"). ❌ **Shallow.** We only give the owner ALL_ROLES at deployment. There are no delegated network admins (`grantRootRoles`) and no setter-scoped roles (`grantSetterRoles`). The judges named this feature explicitly.
- **Give subnames their own Permissioned Resolver so they fully own their data.** ❌ **Not done.** Everything is on one org resolver.
- **Wildcard resolution.** ❌ Not used.
- **Live demo link + open source.** ❌ Pending: needs a deployed dashboard and a public repo.

Verdict: the core usage is deep and real. **Two features the judges list by name (EAC delegation, per-subname Permissioned Resolvers) are missing.** We should add them, and they also make the product better.

### World: Agent Pass (+ IDKit)
- **Integrate the official Agent Pass.** 🔄 meshy-sync is implementing the auth.world.org device flow. **It has never run against World**, because we have no sandbox credentials yet.
- **Complete journey with a protected agent action.** ✅ This part is meaningful: meshyd refuses any agent without a valid provider-signed attestation (`MESHY_NOT_HUMAN_BACKED`), so World ID gates real infrastructure access rather than a login screen. ⚠️ Today the attestation is a **dev signature**, not a real World proof.
- **Denied, expired or cancelled path.** ⚠️ It's enforced in code. It hasn't been demoed with World.
- **Server-side validation, no client secrets.** 🔄 That's the design (confidential client in meshy-sync, ID-token validation). Not yet verified.
- **Integration debrief.** ❌ Not written.
- **IDKit.** ❌ Not done. A proportionate use exists: require a Proof-of-Human IDKit check before an *org admin* grants **rw** access, which is the high-risk action. That's optional.

Verdict: the design is deep (World gates real access), **but right now it's integration-in-progress plus a dev stand-in.** It's not credible for the prize until a real sandbox World ID flow runs end to end.

### Arkiv: Build on Arkiv
- **Writes to the current testnet.** ✅ Raw-tx JSON-RPC from meshyd: 12+ live batches.
- **A user-facing feature reads from Arkiv.** ✅ The dashboard Activity page (live feed, kill latency, live sessions), plus the menu bar.
- **README lists writer addresses + a runnable query.** ✅ In `arkiv/schema.md` and the README.
- **`/arkiv/schema.md`** (entities, queried fields, owners, expiry). ✅ Mostly, but **owners and expiry need to be stated per entity.**
- **`friction.md`, `reach.md`, `why-arkiv.md`, `how-you-use-arkiv.md`.** ❌ **Missing. Each is 25% of the score.** We have real friction to report: the `NonCanonicalOperationData` tuple encoding, docs disagreeing on `NOT`/`!=`, no Go SDK, no published quota numbers.
- **Depth issues to fix:**
  - **Spoofable reads.** The dashboard filters by an `org` attribute, but anyone can write entities with `org = 'meshydemo.eth'`. We should also filter on `$owner = <resource daemon address from ENS>`. That's a real ENS × Arkiv trust link, and it fixes a real hole.
  - **Every entity lives 30 days.** The plan wanted per-type lifetimes: `meshy.session` living only as long as the grant, with `extendEntity` as a lease on renew. The judges ask whether "expiration settings fit what the app does".
  - **Entity edit permissions.** All entities are created read-only (good). This should be stated in how-you-use-arkiv.md.
  - **Polling instead of `watchEntityEvents`.** Fine, but subscriptions would be nicer.

Verdict: real usage. **We lose points today for the 4 missing files and the spoofable-owner issue.** All of it can be fixed in a few hours.

## 3. Fix list, by priority
1. Arkiv: add the 4 judging files; filter on `$owner` (the resource daemon from ENS); per-type lifetimes and a session lease.
2. ENS: delegated network admins via EAC (`grantRootRoles` UI and CLI); a per-agent Permissioned Resolver with a setter-scoped role, so the agent owns its keys (for example, it can rotate `meshy.ssh` itself).
3. World: get sandbox credentials (user action), run the real device flow, record the success and denied paths, write the debrief.
4. Test the dashboard writes with a real wallet: grant, revoke, approve.
5. Provider: admission attestations (evidence for slashing) and the seat check, then demo a slash.
6. Deploy the dashboard (Vercel or similar), make the repo public, record the video.
7. If time and a VPS allow: run NetBird and demo `meshy-mesh`. Otherwise, state clearly that the mesh transport is not demoed.

# Anyone can create an org and invite people

**Today:** there's exactly one org, `meshydemo.eth`, created from a terminal with `meshy-admin org init` and an admin key. That's why it shows up in docs, defaults and the provider's `--orgs` flag.

**Goal:** anyone with a wallet creates one or more orgs from the dashboard or the Mac app in under 2 minutes, invites people (link, wallet/ENS, email via Resend, QR code), and nothing in the product names `meshydemo.eth` except an explicit "view the demo" link.

**Decisions made (6 Nov):**

| Decision | Choice |
|---|---|
| Org name | **Both:** a free `acme.meshy.eth` subname, instant, or bring your own `acme.eth` |
| Gas | The user's own wallet pays (onboarding links a Sepolia faucet) |
| Invites | Shareable link, by wallet or ENS name, by email (Resend), QR code in the Mac app |
| Roles | Owner, Admin, Member, Agent |

---

## 1. What an org is on chain
```
acme.meshy.eth  (or acme.eth)          OWNER = the creator's wallet. Resolver = org resolver (owner-controlled)
│  meshy.orgkey   X25519 public key (audit/policy encryption)
│  meshy.attestor provider signer(s) trusted for World ID attestations
│  meshy.provider provider id (ProviderRegistry), set when they subscribe
│  meshy.admins   (no record: admins are EAC roles on the registries; listed from events)
├─ people.acme.meshy.eth               MEMBERS (new). <name>.people… owner = member wallet
│  └─ alice.people.acme.meshy.eth      addr = wallet, meshy.ssh, avatar/display name (ENS text)
├─ agents.acme.meshy.eth               AGENTS. <agent>.agents… owned by the org; meshy.owner = the member who runs it
│  └─ claude-1.agents.acme.meshy.eth
└─ <network>.acme.meshy.eth            NETWORKS → devices → services → grants (unchanged)
```
- **Owner:** holds the org name and can do everything, including adding admins and transferring the org.
- **Admin:** has EAC roles `REGISTRAR|UNREGISTER|RENEW` on the org, `people`, `agents` and network registries, all of them or per network. This already works (`admin add`).
- **Member:** a name under `people.` with their own wallet. Members can receive grants and may own agents.
- **Agent:** a name under `agents.` with `meshy.owner` set to its member, approved by a human (World ID).

Grants become `<member-or-agent>.<device>`. `meshyd` learns to accept `people.` identities alongside `agents.` (a small change in `meshyd/auth.go`). Agent names and member names must not collide, because grants use the bare label. The rule: a label is unique across `people` and `agents`, enforced when invites are claimed.

## 2. Creating an org: one guided flow
**Contract: `MeshyOrgFactory`** (new, about 150 lines, Foundry, verified on Sepolia).
- **One transaction for a free org:** `createOrg("acme", orgKeyPub, attestor)`. It:
  1. registers `acme` under `meshy.eth`'s registry to `msg.sender`, with a 1-year expiry (renewable)
  2. deploys, through the ENSv2 VerifiableFactory, the org registry, the `people` and `agents` registries, and the org resolver, **all owned by `msg.sender`**
  3. registers `people.` and `agents.`, and writes `meshy.orgkey` and `meshy.attestor`

  Meshy owns `meshy.eth` only to issue names. Once created, the org's registries and resolver belong to the user, so Meshy can't change their access rules. The parent can only refuse to renew; the plan is 1 year now, and a long expiry or locking comes later.
- **Bring your own `acme.eth`:**
  1. In the browser: mint MockUSDC → approve → commit → a 60-second countdown → register. This logic exists in Go (`RegisterETH`) and gets ported to `lib/ens.ts`.
  2. Then `factory.initOrg(...)`, which deploys the same registries, plus one owner transaction to point the name at them. Four or five transactions, shown in a stepper.
- **Same wizard in the dashboard (`/new`) and the Mac app ("Create organization…"):**
  1. Sign in (SIWE, already built).
  2. Name it: a live availability check for `acme.meshy.eth` or `acme.eth`.
  3. **Org key:** derived from a wallet signature, nothing to back up (§4).
  4. Choose a provider from `ProviderRegistry` (default: provider #1 at `api.meshy.live`) and seats. A free trial of 3 seats needs **no USDC**: `MeshySubscription` gains `trial()` per org, once.
  5. Done → a checklist: create a network, add this Mac, invite people.
- **Provider side:** meshy-sync stops using a fixed `--orgs` flag. It **discovers orgs from `MeshySubscription` events** for its provider id (plus factory events), reconciles each one, and caches each tree.

## 3. Invitations: signed, claimable, four ways to deliver
**An invite is an EIP-712 message signed by an Owner or Admin:**
```
Invite(org, role ∈ {member, agent, admin}, label?, invitee?, network?, expiresAt, nonce)
```
- `label`: optional. If empty, the invitee picks their name when claiming.
- `invitee`: optional, binds the invite to one wallet.
- Signing is free, there's no transaction, and it works offline.

**Claiming is self-serve and trustless: contract `MeshyInvites`**, granted the REGISTRAR role on the org's `people` and `agents` registries during org creation.
- `claim(invite, sig, label, sshKey)`:
  - **Checks:**
    - the signer still has admin roles (`hasRootRoles`) or is the owner
    - the invite hasn't expired and its nonce is unused
    - the invitee matches, if the invite names one
    - the label is free in both `people` and `agents`
  - **Then:** registers `label.people.<org>` to the claimer and sets `addr` and `meshy.ssh`. For admin invites it also grants EAC roles.
  - **Gas:** the claimer pays, about 1 transaction.
- `cancel(nonce)`: an admin revokes an outstanding invite on chain.
- Removing a member is an `unregister`, the same as a revoke. Live sessions die as they do today.

**Delivery:** the same signed blob, four ways.

| Channel | How | Where it's stored |
|---|---|---|
| **Link** | `https://meshy.live/join/<base64url(invite+sig)>` | Nowhere: the link *is* the invite |
| **Wallet or ENS name** | The admin types `0x…` or `bob.eth` (resolved through ENS). The invite is published as an Arkiv entity `meshy.invite` with `to=<addr>`, `org`, `expiresAt` and the signed payload. | Arkiv (public; holds only what the link holds) |
| **Email (Resend)** | A server route `POST /api/invite/email` sends the link from `invites@meshy.live` with a branded template. **No email is stored** by Meshy, and the route is rate-limited per signing admin. | Resend logs only |
| **QR code** | The Mac app and dashboard show the link as a QR code, for in-person onboarding. Scanning opens `meshy.live/join/…` on a phone, or `meshy://join/…` on a Mac with the app. | Nowhere |

**The invitee's experience:**
1. Open the link (or see "You have 1 invite" in the dashboard or Mac app, through the Arkiv lookup by address).
2. Sign in.
3. Pick a display label (`alice`).
4. Claim: one transaction.

The Mac app then generates the SSH and mesh keys locally and offers "Share this Mac" and "Join as an agent".

**Agent invites** use the same flow with role `agent`. The claimer is the agent's human, and the agent's keys come from `meshy join` / the Mac app. World ID verification stays (dev attestation until it's configured).

## 4. The org key without key management
Today the X25519 org key is generated by `meshy-admin` and lives in one admin's cache file.

**New design:**
- `orgKey = HKDF(signature of "Meshy org key v1 for <org>")` by the **owner's** wallet: deterministic and re-derivable on any device, with nothing to back up.
- **Admins and members with audit access:** the org key is sealed to each one's own derived key (the same envelope as policies) and published as Arkiv entities `meshy.keyshare` (`to=<addr>`, `org`). Revoking someone's audit access means rotating `meshy.orgkey` (owner action), which re-seals to the remaining holders.
- This removes the "import the org key in Settings" step.

## 5. Removing every hard-coded `meshydemo.eth`
| Place | Change |
|---|---|
| `web/src/lib/config.ts` `DEFAULT_ORG` | Already not a silent default (0.5.0); keep only for the "view demo" link |
| Dashboard home | Signed out: "Create an organization" / "View demo". Signed in: **your orgs** (owner / admin / member), discovered from factory `OrgCreated` events and `MeshyInvites` claims, plus EAC role events for the wallet |
| meshy-sync `--orgs` | Replaced by discovery from subscription and factory events (§2) |
| `meshy` CLI and Mac app | `meshy join <invite-link>` takes the org from the invite; `meshy orgs` lists your orgs; the Mac app's org list comes from the same discovery |
| Docs | `meshydemo.eth` only in demo sections; examples use `acme.meshy.eth` |
| `_meshy` TXT on `meshy.live` | Stays: it describes the demo org, as documentation |

## 6. Screens
- **`/new`, the create-org wizard** (described in §2).
- **`/org/<org>/people`:** members, agents and admins in one list with a role chip, last seen (Arkiv) and live grants. There's an **Invite** button (a sheet with role, optional name or wallet, expiry, delivery: link / email / wallet / QR) and a list of pending invites (Arkiv entities and links made in this browser) with Cancel (on chain).
- **`/join/<invite>`:** the org's name and avatar, who invited you, the role, the expiry, then "Sign in to accept".
- **Mac app:**
  - **Create organization…**
  - **Invite…**: a QR sheet plus copy link plus "Email it"
  - **Invites for you** (badge)
  - a **Your orgs** submenu that switches orgs

## 7. Implementation order (each step ships on its own)
| # | Step | Where | Effort |
|---|---|---|---|
| 1 | Register `meshy.eth` on Sepolia; deploy `MeshyOrgFactory` + `MeshyInvites` with forge tests (create, claim, expired, reused nonce, revoked admin, label collision, cancel) | `contracts/` | 4–5 h |
| 2 | `/new` wizard (free subname path) + derived org key + trial seats | `web/`, `contracts/MeshySubscription` | 3 h |
| 3 | Invites: signing, `/join/<invite>`, claim, the People page, Arkiv `meshy.invite` | `web/`, `meshy/arkivkit` schema | 3–4 h |
| 4 | Resend email route (`invites@meshy.live`: SPF/DKIM records added via `vercel dns`) | `web/app/api/invite/email` | 1 h |
| 5 | `people.` identities in `meshyd` auth and grants; `meshy join <invite>`; label uniqueness | `meshy/` | 2–3 h |
| 6 | meshy-sync org discovery (no `--orgs`) | `meshy/provider` | 1–2 h |
| 7 | Mac app: Create org, Invite with QR code, Invites for you, org switcher | `mac/` | 3 h |
| 8 | Own `.eth` path (browser commit/reveal) | `web/lib/ens.ts` | 2 h |
| 9 | Key shares for admins (§4) + rotation | `web/`, `meshy/policy` | 2 h |

About 2 days in total; steps 1–4 make a demoable "create your org and invite someone" in about a day.

## 8. Needed from you
- **A Resend API key** (stored as a Vercel env var, server-side only) and approval to add Resend's DNS records (SPF/DKIM/return-path) to `meshy.live`.
- **Approval to register `meshy.eth` on Sepolia**, about 8 MockUSDC per year from the admin key (free test tokens), and to deploy the two contracts. The admin key has about 0.015 Sepolia ETH, which is enough on today's near-zero gas, but a faucet top-up would help.
- **Trial size:** 3 free seats per new org (recommended), or require payment up front?

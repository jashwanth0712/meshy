# Meshy for macOS: menu-bar menu (plan)

**Goal.** Meshy lives in the menu bar, like Twingate. One click shows:
- which org you're in and who you're signed in as
- what you can reach (networks → devices → services)
- who has access right now, with live countdowns
- quick copy of the `meshy ssh` / `meshy connect` command for any name

Everything that needs a wallet signature (grant, renew, revoke, admin) hands off to the dashboard, which the app already hosts as a window ("Admin Console") or opens in the browser.

Scope: `mac/` (Swift, AppKit, no dependencies), plus three small additions to `web/` for deep links and sign-in. Nothing in `meshy/`, `meshnet/` or `deploy/` changes.

> **0.5.0 (2026-11-06) supersedes §3–§5 where they differ.** See "0.5.0: universal app" below. The tree in §1 is the signed-in shape; the Admin Console is now the browser.

---

## 0.5.0: universal app (fixes from a fresh install on a second Mac)

| Problem on a fresh install | Fix |
|---|---|
| Connect wallet redirected again and again and didn't keep the login | **SIWE (EIP-4361).** The app opens `/connect?nonce=…&callback=meshy://auth`. The page signs `meshy.live wants you to sign in…` (nonce, chain 11155111, 10-minute expiry) and redirects **once**; a reload shows "Signed in" instead. The app verifies it with the bundled `meshy verify-siwe` (secp256k1 recovery plus nonce, domain, chain and expiry checks). Nonces are single-use and expire after 15 min. The session goes in the login Keychain, through `/usr/bin/security` so app updates don't prompt. |
| Demo org, `claude-1` sessions on `db1.prod` and an Arkiv feed shown to a new user | **No defaults.** Signed out = only Sign In / Join / More. Signed in with no org = Add Organization…. Orgs, favorites and requests are keyed by the signed-in address. Recent loads only for an org you added. `DEFAULT_ORG` is gone from `web/`; the home page asks for an org and lists the ones this browser opened, with the demo as an explicit link. |
| Requesting access meant curl and CLI installs | **Bundled tools.** `meshy`, `meshyd` and `meshy-mesh` are in `Contents/Resources/bin`. **Share this Mac…** runs `meshyd init`, opens Add device prefilled (`?add=`), then starts LaunchAgents once ENS shows the device. **Join with an Invite Link…** runs `meshy join` then `meshy mesh-join`. **Install Command-Line Tools** adds symlinks in `~/.local/bin`. |
| Access requests were hard | **Request Access…** on every device and service: agent, folder (from `meshy.roots`), read / read-write, duration. It produces `/org/<org>?give=&target=&path=&mode=&dur=`, the Give access sheet fully prefilled, to copy or email to an admin. Pending requests show under Access › Yours until the grant lands. |
| Connect wallet landed on someone else's admin panel | The WKWebView console is gone. **Admin Console** opens `/org/<your picked org>` in the default browser, and only once you've picked an org. |
| Paths assumed this machine | Folder choices come from an NSOpenPanel on the device itself, or from the target device's `meshy.roots`. Nothing in `mac/` or `web/src` names a user folder. |

**0.5.1: role-based menu and no rebuilds while open.**
- **Owners and delegated admins** get the org menu: Networks, Access (all grants, Renew and Revoke), Recent and Admin Console.
- **Members** (nodes, agents, people) get Request Access ›, My Access › and This Mac › only. They see no addresses and no give or revoke.
- **Rebuilds.** The menu is rebuilt in `menuNeedsUpdate` and after it closes, never while it's open; earlier builds rebuilt the open menu on every refresh.
- **Dialogs.** Async results (role check, join) show their dialog only after the menu closes, and the app never opens the menu by itself.
- **Main thread.** launchctl work runs off it, and Install Command-Line Tools no longer spawns a login shell.
- **Tests.** `UI.swift` routes every dialog, browser open and copy, so `--click-all` clicks every item headlessly and checks where each link goes.

**Role.** `GET /api/role?org=&address=` (web) reads `exactOwner(org)` and, for each network, `hasRootRoles(ADMIN_ROLES, address)`. It returns `owner`, `admin` (with `adminOf`) or `member`. The menu adds "agent <label>" for identities in `~/.meshy`. Only owners and admins see Give Access…, Renew… and Revoke….

**Hosts.** Dashboard `https://meshy.live`, provider `https://api.meshy.live`, Sepolia `wss://ethereum-sepolia-rpc.publicnode.com`. Stored `meshy-bay` / `meshy-next` / `sslip.io` settings from older builds are migrated.

**Tests.** `mac/test.sh --live`, 33 checks:
- a fresh install shows no demo data
- SIWE: unknown nonce, other domain, bad signature and replay are rejected, and a valid signature is accepted
- the session survives a relaunch
- the org menu works on the fixture, and the live provider loads
- signing out hides everything

`go test ./cmd/meshy -run TestVerifySIWE` covers the verifier.

---

## 1. Menu tree

```
⬡ 1                                   ← status item: Meshy mark (template image) + live-grant count when > 0
├─ [M]  meshydemo.eth            (●)   ← header view: org initial, org name, identity line, Live switch
│       0x45e8…E0EA · admin
├─ ● Sepolia #12,075,923 · updated 12s ago     (or "Offline · showing data from 4m ago")
├─ Sign In…                            ← only when signed out
├─────────
├─ Organizations                   ›
│   ✓ meshydemo.eth        3 devices · 1 agent
│     acme.eth
│     ─────
│     Add Organization…
│     Remove meshydemo.eth
├─ Networks                        ›   ← "All Resources"
│   [🔍 Search names]                  ← NSSearchField view; typing flattens to matches
│   4 resources
│   ─ home ─
│     pi           Raspberry Pi 5  ›
│     │  pi.home.meshydemo.eth           (disabled label)
│     │  raspberry-pi · front door       (disabled label)
│     │  Copy "meshy ssh pi.home.meshydemo.eth"
│     │  Copy Device Address
│     │  Open in Admin Console            → /org/<org>/directory?n=pi.home.meshydemo.eth
│     │  Give Access…                     → dashboard sheet (?give=&target=)
│     │  ★ Add to Favorites
│     │  ─ Services ─
│     │  camera  http ›                   (same submenu shape, "Copy meshy connect …", target host:port)
│     rig1         server          ›
│   ─ prod ─
│     db1                          ›
├─ Access                          ›
│   ● claude-1 → db1.prod · 42m left  ›
│   │  claude-1.db1.prod.meshydemo.eth   (disabled label)
│   │  Folders · expires 15:04           (disabled label)
│   │  Copy "meshy ssh db1.prod.meshydemo.eth"
│   │  Open in Admin Console
│   │  Renew +1h…                         → dashboard (wallet signs)
│   │  Revoke…                            → dashboard confirm dialog (?revoke=<grant>)
│   ─────
│   3 ended                               → Admin Console, Ended tab
│   Give Access…
├─ Favorites                       ›   ← only when there are favorites; same item submenus
├─ This Mac                        ›
│   Agent claude-1 (0x35e7…A62f)          ← ~/.meshy/agent*/agent.json
│   │  Copy "meshy whoami"
│   │  live grants for this agent…
├─ Recent                          ›   ← Arkiv: refusals and revocations, newest first (owner-filtered)
│   claude-1 refused on pi.home · 2h
├─────────
├─ Admin Console                  ⌘0   ← the app's dashboard window
├─ More                            ›
│   ─ Account ─
│     Signed in as 0x45e8…E0EA · admin (copies the address)
│     Log Out
│   ─ App ─
│     Settings…                    ⌘,
│     ✓ Start at Login                    ← SMAppService.mainApp
│     ✓ Notifications                      ← refusals and revocations
│   ─ Help ─
│     How Meshy Works
│     Documentation
│     Download the CLI
│   Troubleshoot                   ›
│     Refresh Now                  ⌘R
│     Run meshyd doctor                    → Terminal
│     Show meshyd Logs                     → Terminal (tail -f)
│     Copy Diagnostics
│   ─────
│   Meshy 0.4.0 · ENSv2 on Sepolia · Arkiv
└─ Quit Meshy                      ⌘Q
```

The Twingate patterns kept:
- the header with an on/off switch. In Meshy the switch turns live updates (polling and notifications) on or off. Meshy has no client tunnel to toggle, because agents connect with `meshy ssh`.
- an accounts submenu (here, organizations)
- a searchable "All Resources" list (here, networks)
- a per-resource submenu with copy, open and expiry
- a "More" submenu holding settings, support and troubleshooting

---

## 2. Data and refresh

| Source | What | How often |
|---|---|---|
| meshy-sync `GET {sync}/v1/map?org=` | org tree: networks, devices (kind, meta, endpoint, addr), services (spec), agents, grants (kind, agent, target, device, status, expiry), block | every 30 s while Live is on; on menu open if older than 15 s; on wake; on org switch |
| Dashboard `POST {dashboard}/api/arkiv` | `meshy.blocked`, and `meshy.session` with `kind = session.revoke`. **Owner-filtered** to the device `addr`s from the map (Arkiv is public; unfiltered rows can be spoofed) | every 30 s while Live is on |
| `~/.meshy/agent*/agent.json` | identities on this Mac (`agent`, `org`, `addr`) | on launch and on menu open (cheap) |
| `UserDefaults` | orgs, active org, signed-in address, favorites, sync/dashboard URLs, Live, notifications | — |
| `~/Library/Application Support/Meshy/map-<org>.json` | last good map per org | written after each successful fetch |

- **Countdowns.** They're computed from `expiry` each time the menu or a submenu is about to show (`menuNeedsUpdate`), so "42m left" is always current with no timer while the menu is closed. While the menu is open, a 1 s timer updates the titles in the Access submenu.
- **Status-item title.** It shows the count of live grants (`status == "live" && expiry > now`). It's empty when there are none.
- **Why the meshy-sync map, and not the dashboard's ENS reader?**
  - It's one request, against a provider that already indexes ENS logs.
  - The browser path takes dozens of RPC calls (20 s or more on a slow public node).
  - The dashboard stays the source for anything that writes.

## 3. Sign-in

Writes need a wallet signature, and a WebKit app can't host a wallet extension. So the menu needs to know **who you are** (to mark your role and "your" access), but never holds keys. Options:

| Option | Verdict |
|---|---|
| WalletConnect inside the app | Heavy dependency, and a QR flow for a demo app. No |
| Hold a key in the app | It would make the app a custodial wallet. No |
| **Browser sign-in with a `meshy://` callback** | **Chosen.** Uses your real wallet, no secrets in the app, and fits the existing dashboard |
| Paste an address | Kept as a fallback ("Use an Address…"). ENS is public, so read-only use needs nothing more |

Flow:
1. **Sign In…** creates a random `state` (16 bytes) and remembers it for 10 min.
2. It opens `{dashboard}/connect?state=<state>&org=<org>` in the default browser.
3. The `/connect` page (new, client-only) asks the wallet for accounts with `eth_requestAccounts`. It then asks for an EIP-191 `personal_sign` of `Sign in to Meshy for Mac\norg: <org>\nstate: <state>`, which proves the address holds the key in that browser session.
4. It redirects to `meshy://signin?state=…&address=…&sig=…&org=…`.
5. The app accepts the callback only if `state` matches the pending one and has not expired. That protects against a stray or malicious `meshy://signin` link setting your identity.
6. The app stores the address and role:
   - **admin** if it approved an agent (an agent's `owner`; the map has no org owner field)
   - **agent** if it equals an agent's addr
   - **member** otherwise
7. The signature is kept for display and audit, but not verified in-app: there's no secp256k1 in AppKit, and adding one isn't worth it. The identity is only used to personalize a read-only view. Every write is still signed and enforced on-chain, so nothing gets more power from a forged identity.

**Log out** forgets the address and signature.

## 4. Writes from the menu (handed to the dashboard)

| Menu action | Opens |
|---|---|
| Give Access… | `/org/<org>?give=<agent>&target=<name>`: the sheet, preselected |
| Revoke… | `/org/<org>?revoke=<grant full name>`: the confirm dialog for that grant |
| Renew +1h… | `/org/<org>?give=<agent>&target=<name>`: a fresh grant for the same scope. A true `renew()` in the dashboard is follow-up F1 |
| Open in Admin Console | `/org/<org>/directory?n=<name>`: the item selected |

These open in the app's **Admin Console** window, where browsing is read-only. Wallet steps then hand off to the browser through the existing "Connect wallet" prompt. **More › Settings › Open dashboards in** can be set to *Browser* so a wallet user goes straight there. The default is *Admin Console*.

`web/` changes: a `/connect` page, `?revoke=` on Access, `?n=` on Directory, and `&target=` added to the existing `?give=`.

## 5. App shape

- **Menu bar first.** `LSUIElement = YES`, so there's no Dock icon at launch. Opening the Admin Console window switches to `.regular` (Dock icon, app menu, ⌘-shortcuts). Closing it switches back to `.accessory`.
- **First launch** opens the Admin Console once, so it's obvious where Meshy went. Later launches are menu-bar only.
- **Start at Login** uses `SMAppService.mainApp` (macOS 13+). It needs the app in /Applications. If registration fails (for example on an ad-hoc signed build outside /Applications), the item shows the error and stays unchecked.
- **Notifications** use `UNUserNotificationCenter`, for new refusals and revocations only. The first load primes "seen" without notifying, and an Arkiv key is never notified twice.

## 6. Edge cases

| Case | Behaviour |
|---|---|
| Offline / meshy-sync unreachable | Cached map, "Offline · showing data from 4m ago". Items still work (copy, open). No notifications. Retries with backoff (30 s → 2 min) |
| Slow RPC (meshy-sync takes 10 s or more) | Fetches are async with a 20 s timeout. The menu opens instantly with cached data and "Updating…", then rebuilds when data lands |
| Org not found / empty | The header shows the org; "No networks yet" with "Open Admin Console" |
| Many resources (more than 40) | The search field filters across every network, and results are capped at 40 with "Show all in Admin Console". Submenus are built lazily |
| Expired or revoked grants | Hidden from Access, counted as "N ended" |
| Wrong `meshy://signin` (state mismatch or expired) | Ignored, with an alert: "Sign-in link expired. Try again" |
| Several local agent identities | All listed under This Mac. Those for the active org are marked |
| meshyd not installed | Troubleshoot items show "meshyd isn't installed" and open the Download page |
| Sleep and wake | Refresh on `NSWorkspace.didWakeNotification` |
| Dark mode and menu-bar tint | The status icon is a template image |

## 7. Tests

- **Headless menu dump (CI-able):** `Meshy --dump-menu [--org meshydemo.eth] [--fixture map.json]` builds the whole menu from live data (or a fixture) and prints the tree to stdout, then quits.
  - Asserted by `mac/test.sh`:
    - Organizations › contains `meshydemo.eth` with a checkmark.
    - Networks has `pi`, `camera`, `rig1` and `db1`.
    - Access shows a live grant with "left", or "No live access".
    - Copy items carry the right `meshy ssh` / `meshy connect` strings.
  - The fixture run uses `mac/Tests/map.json`, captured from `/v1/map?org=meshydemo.eth`, and is deterministic.
- **Sign-in callback:** `open "meshy://signin?state=bad&address=0x…"` → ignored. A flow with a valid state → identity set and role computed. Both are tested through `--dump-menu` after `defaults write`.
- **Snapshot:** `Meshy --snapshot out.png` (already exists) for the Admin Console.
- **Manual, on screen:**
  - click through every submenu
  - search for "cam"
  - toggle Live
  - Start at Login on and off (from /Applications)
  - quit and relaunch: the org, favorites and identity persist
  - airplane mode: offline state and cached data

## 8. The Go `menubar/` app

It shows grants, local identities, daemon sessions and Arkiv events in a static systray list. Ideas reused here:
- the "agents on this Mac" glob
- owner-filtered Arkiv reads
- click to copy commands
- notifications on new blocks

Not reused: the daemon status over the unix socket (`/tmp/meshyd-live.sock`), which only matters on a device and needs a socket client. It's follow-up F2.

**Decision:** `menubar/` has been retired and removed. The real Mac app is `mac/`.

## 9. Follow-ups
- **F1:** Renew (+1h) in the dashboard (`renew(anyId, newExpiry)` is already in the ABI), so "Renew +1h…" lands on a one-click confirm.
- **F2:** meshyd status over the unix socket ("This Mac serves db1 · 1 session").
- **F3:** Developer ID signing and notarization, so Start at Login and notifications work without Gatekeeper prompts on other Macs.

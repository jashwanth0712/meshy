# Demo: three MacBooks, folder-level access, one Claude agent

**Goal:**
- One laptop holds the admin wallet and a Claude Code agent (`claude-1`).
- The two other laptops are devices that share folders.
- From the dashboard you give the agent one folder on each device, show that it can't touch anything else, then revoke and watch its live session die.
- Every connection goes over the provider's WireGuard mesh on AWS.

Until `meshy.live` DNS is approved, use **`https://meshy-bay.vercel.app`** wherever this says "dashboard".

| Laptop | Role | ENS name | Runs |
|---|---|---|---|
| **Mac A** (yours) | Admin + agent | wallet `0xf846…Fc98` (delegated admin of `home` and `agents`); agent `claude-1.agents.meshydemo.eth` | Browser or Meshy.app with MetaMask, Claude Code with the Meshy plugin, `meshy` + `meshy-mesh` |
| **Mac B** | Device | `macb.home.meshydemo.eth` | `meshyd` + `meshy-mesh` |
| **Mac C** | Device | `macc.home.meshydemo.eth` | `meshyd` + `meshy-mesh` |

## What's already in place
- **ENS:** `meshydemo.eth`, with the `home` network and the agents registry. Your wallet can add devices to `home`, and grant and revoke access there.
- **Provider:** live on AWS (`https://sync.16-4-10-3.sslip.io`, NetBird at `https://16-4-10-3.sslip.io`).
  - It turns every live grant into a WireGuard policy and denies everything else.
  - **Seats:** 10 paid, 4 used. Two Macs plus one agent fit.
- **Downloads:** GitHub release **v0.2.0** has `Meshy.dmg` (Mac app 0.4.0 with the menu-bar menu) and the CLI and daemon for macOS (arm64 and Intel). The installer verifies SHA-256 checksums.
- **`claude-1`:** already exists on Mac A (`~/.meshy/agent-claude-1`), is approved, and is joined to the mesh.

## 1. Every Mac: the app (optional on B and C)
Download `Meshy.dmg` from https://github.com/jashwanth0712/meshy/releases/latest/download/Meshy.dmg (or the dashboard's `/download` page). Drag it to Applications. The first time, **right-click → Open**: the app is ad-hoc signed, not notarised. It shows orgs, networks, devices and live access in the menu bar.

## 2. Mac B and Mac C: become devices
```bash
# 1. install the daemon + mesh peer (into ~/.local/bin)
curl -fsSL https://meshy-bay.vercel.app/install.sh | MESHY_TOOLS="meshyd meshy-mesh" sh
export PATH="$HOME/.local/bin:$PATH"

# 2. folders to share (make one you will grant and one you won't)
mkdir -p ~/Meshy/projects ~/Meshy/private && echo "hello from $(hostname)" > ~/Meshy/projects/README.txt && echo "do not read" > ~/Meshy/private/secret.txt

# 3. keys + join code   (use macc on Mac C)
meshyd init --name macb.home.meshydemo.eth --roots ~/Meshy
```
Copy the printed `meshy-join:…` code to Mac A.

**On Mac A (dashboard):** Directory → `home` → **Add device** → paste the join code → sign in MetaMask. That's 3 transactions, about 1 minute on Sepolia.

**Back on Mac B** (two terminals; replace `macb` with `macc` on Mac C):
```bash
# terminal 1: the daemon (public Sepolia websocket; no API key needed)
meshyd run --name macb.home.meshydemo.eth --roots ~/Meshy \
  --sepolia wss://ethereum-sepolia-rpc.publicnode.com --listen 127.0.0.1:2222 --arkiv ""

# terminal 2: join the provider mesh (signed with the daemon key, seat-checked)
meshyd mesh-join --name macb.home.meshydemo.eth \
  --provider https://sync.16-4-10-3.sslip.io --forward 22=127.0.0.1:2222
```
Expected:
- `meshyd ready … sandbox=sandbox-exec(seatbelt)`
- `admitted macb.home.meshydemo.eth as mesh peer macb-home-meshydemo-eth`, then `forwarding mesh :22 -> 127.0.0.1:2222`

`--arkiv ""` turns off the on-chain audit for the new Macs. To include them in the Activity feed, fund each daemon's Arkiv address with tGLM from the Rabdi faucet (printed by `meshyd init` and `meshyd doctor`) and drop that flag.

## 3. Mac A: the agent
`claude-1` already exists and is on the mesh. Install the plugin into Claude Code:
```bash
git clone https://github.com/jashwanth0712/meshy && cd meshy
MESHY_HOME=~/.meshy/agent-claude-1 claude --plugin-dir agent/claude-plugin
```
To demo a **fresh** agent instead:
1. Install the CLI (`curl -fsSL https://meshy-bay.vercel.app/install.sh | sh`).
2. Dashboard → Directory → **Invite agent** (`claude-2`) → copy the invite link.
3. In Claude Code: `/meshy:join <link>`.
4. Open the approve link and sign.
5. Then run `meshy mesh-join --provider https://sync.16-4-10-3.sslip.io`.

## 4. The demo
1. **Give access:** dashboard → Access → **Give access**: agent `claude-1`, device `macb`, folder `~/Meshy/projects` (on Mac B the full path is `/Users/<user-on-B>/Meshy/projects`), read-only, 1 hour. Sign. That's 2 transactions, and the folder list is encrypted to Mac B's daemon key. About 10–30 s later the provider opens the WireGuard path. Do the same for `macc` with read-write if you want to show both modes.
2. **Use it:** ask Claude, "list and read the files in projects on macb". It runs `meshy ssh macb.home.meshydemo.eth -- cat …/projects/README.txt` over the mesh.
3. **Prevent it:**
   - Ask Claude to read `…/private/secret.txt` → `MESHY_BLOCKED path=… allowed=[…/projects:ro]`.
   - Ask it to write into `projects` → blocked (read-only).
   - Ask it for Mac C before you grant it → `MESHY_UNREACHABLE` (no live grant means no network path).
4. **Revoke live:** start something long-running (for example "tail the README every second"), then press **Revoke** on the dashboard. The revoke timeline shows the transaction mined and Mac B's daemon killing the session: in tests, about 1 s after the block. Claude receives `MESHY_REVOKED`. Reconnecting fails because the mesh policy is gone too.
5. **Show the record:** grants and revokes are ENS names with expiries (public on Sepolia), the provider's signed admissions are at `/v1/attestations`, and the Activity page has the audit (for devices with Arkiv on).

## People, not just agents
A human teammate uses the same `meshy` CLI and gets an identity the same way: invite → approve. Their name currently lives under `agents.` (for example `alice.agents.meshydemo.eth`), because `people.<org>` names aren't built yet (STATUS P1). Folder-wise grants, blocks and revokes work identically for them.

## Gotchas
- **Run in order.** `meshyd run` and `mesh-join` only succeed after the dashboard "Add device" transactions are mined; `mesh-join` refuses names that aren't live (`MESHY_NOT_ON_ENS`).
- **Slow connections:** each `meshy ssh` over the mesh starts a fresh WireGuard peer (8–25 s). Say so in the demo, or pre-warm with one call.
- **Absolute paths:** a grant path must be the absolute path on the target Mac.
- **Wallet funds:** keep MetaMask on Sepolia with some ETH. Adding a device is about 3 transactions and a grant is 2.
- **Reads aren't logged on macOS without root:** blocked attempts are still logged. Allowed reads are only audited when `meshyd` runs as root with eslogger and Full Disk Access.

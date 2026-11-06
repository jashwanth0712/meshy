# Meshy for macOS

A menu-bar app (no Dock icon). Install `Meshy.dmg`, drag Meshy to Applications, open it, and click the Meshy mark in the menu bar. Nothing else to install: `meshy`, `meshyd` and `meshy-mesh` ship inside the app (`Contents/Resources/bin`, universal).

**A fresh install shows nothing but Sign In.** There is no default org and no demo data.

1. **Sign In with Ethereum…** opens <https://meshy.live/connect> in your default browser. MetaMask signs a one-time SIWE (EIP-4361) message: nonce, `meshy.live`, chain 11155111, 10-minute expiry. The page sends you back once through `meshy://auth`. The app checks the nonce it issued and recovers the signer with the bundled `meshy verify-siwe`. The session is kept in your login Keychain until **More › Sign Out**.
2. **Add Organization…**: type your org's ENS name. The app reads your role from ENSv2 through `/api/role`: owner, delegated admin of some networks, or member. Your org list belongs to your address.
3. The menu depends on your role in the org:
   - **Owner or admin.** You get the org menu:
     - **Networks ›**, searchable: each device and service has Give Access…, the `meshy ssh` / `meshy connect` command, its address and Open in Admin Console.
     - **Access ›**: every live grant, with countdowns, Renew… and Revoke….
     - **Recent ›** and **Admin Console**.
   - **Member (a node, agent or person).** You only request:
     - **Request Access ›** lists the org's devices and services. Pick one, then choose an agent, folder, read or read-write, and a duration.
     - **My Access ›** shows your live grants (copy the command, ask for more time) and the requests still waiting for an admin.
   - **This Mac ›**:
     - **Share this Mac…**: pick folders and a network, then name the device.
     - **Join with an Invite Link…**: become an agent or person.
     - **Install Command-Line Tools**: symlinks into `~/.local/bin`.
     - Daemon and mesh status.
   - Everyone also gets **This Mac**, **Organizations** and **More**.

The menu never rebuilds while it's open, because rebuilding it while open can crash or freeze AppKit. New data shows the next time you open it, and only the countdowns tick live. Dialogs that answer slow work wait until the menu has closed.

Every write is signed in the browser by an owner or admin and enforced on-chain. The app never holds a wallet key.

## Share this Mac
1. Pick folders (Finder panel), a network and a device name.
2. The app runs `meshyd init` (keys in `~/.meshy/meshyd`). It then opens the dashboard's **Add device** sheet with the join code filled in: `/org/<org>/directory?add=meshy-join:…`.
3. An owner or admin signs 3 transactions.
4. When the device shows up on ENS, the app installs two LaunchAgents:
   - `live.meshy.meshyd`: `meshyd run` with `wss://ethereum-sepolia-rpc.publicnode.com`, `--arkiv=`, and the control socket at `~/.meshy/meshyd/control.sock`
   - `live.meshy.mesh`: `meshyd mesh-join --provider https://api.meshy.live`

   Logs are in `~/Library/Logs/Meshy`. **Stop Sharing** removes both LaunchAgents and keeps the keys.

## Request access
Pick an agent, a folder (from the device's `meshy.roots`), read or read-and-write, and a duration. You get a link to `/org/<org>?give=<agent>&target=<device>&path=…&mode=ro|rw&dur=…`, which opens the Give access sheet prefilled. Copy it or email it to an admin, or open it yourself if you are one. It shows under My Access until the grant lands.

## Build and test
```bash
mac/build.sh        # → mac/build/Meshy.app, Meshy-mac.zip, Meshy.dmg (universal, ad-hoc signed; builds the Go tools too)
mac/test.sh --live  # 51 headless checks: fresh install, SIWE accept/reject/replay, Keychain persistence,
                    # member and owner menus, every item clicked, request/share/join/tools flows, live role + provider
```
- **Requirements:** Xcode command-line tools and Go 1.24+. `test.sh` signs SIWE messages with viem from `web/node_modules`, so run `npm ci` in `web/` first.
- **Signing:** the ad-hoc signature works on the machine that built it. Elsewhere, right-click → Open the first time. A Developer ID plus notarization removes that step.
- **Settings:** More › Settings changes the provider API and dashboard URLs (defaults `https://api.meshy.live` and `https://meshy.live`). `MESHY_URL=http://localhost:3000` overrides the dashboard for development.

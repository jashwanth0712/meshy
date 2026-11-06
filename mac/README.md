# Meshy for macOS

**Menu bar first.** Meshy lives in the menu bar, like Twingate. Click the Meshy mark (the number next to it is your live grants) to see:
- your org and identity, with a Live switch
- **Organizations ›**, to switch or add ENS orgs
- **Networks ›**, searchable: devices and services. Each has copy-`meshy ssh` / `meshy connect`, its address, Open in Admin Console, Give Access… and Favorites
- **Access ›**: live grants with countdowns, plus Copy command, Renew… and Revoke…
- **This Mac ›**: agent identities in `~/.meshy`
- **Recent ›**: refusals and revocations from Arkiv, also sent as notifications
- **More ›**: account, Settings, Start at Login, Notifications, Help, Troubleshoot

Sign in from the menu: **Sign In…** opens the dashboard's `/connect` page. Your browser wallet signs a nonce, and the page hands back `meshy://signin`. No keys reach the app. Design, edge cases and tests are in [docs/mac/PLAN.md](../docs/mac/PLAN.md).

Run `mac/test.sh --live` for the headless checks (18 assertions; uses an isolated defaults suite).

The window described below is the **Admin Console** (menu › Admin Console, ⌘0). It shows a Dock icon only while it's open.

A native window around the live Meshy dashboard (AppKit + WKWebView, no dependencies). It opens <https://meshy-bay.vercel.app>, production of `main` (Vercel redeploys it on every push), for one org (default `meshydemo.eth`). Every screen reads ENSv2 on Sepolia and Arkiv live; nothing is bundled or hardcoded. The app follows macOS light and dark mode.

Develop also offers the redesign's branch build at <https://meshy-next.vercel.app>, `localhost:3000`, or a custom URL.

## Build
```bash
mac/build.sh   # → mac/build/Meshy.app, Meshy-mac.zip, Meshy.dmg (universal, ad-hoc signed)
open mac/build/Meshy.app
```
Needs Xcode command-line tools (Swift 5.9+). The binary targets macOS 13 or later. The ad-hoc signature is fine on the machine that built it. Anyone you send the zip or dmg to must right-click → Open the first time; signing it with a Developer ID and notarizing it removes that step.

## What it does
- **Same-site links stay in the app.** Etherscan, the ENS app, the Arkiv explorer and World ID open in your default browser.
- **Wallets.** Signing grants and revokes needs a wallet extension (MetaMask), which a WebKit app can't host. "Connect wallet" therefore offers to open the current page in your browser, and the app itself stays read-only.
- **Remembers state.** It keeps the last page, window size and org between launches. The org key you import in Settings stays in the app's local storage, just as in a browser.
- **Keyboard:**
  - ⌘1–⌘6: Access, Directory, Activity, Billing, Provider, Settings
  - ⇧⌘O: open another org
  - ⇧⌘B: open the page in your browser
  - ⇧⌘C: copy the link
  - ⌘R: reload
  - ⌘[ / ⌘]: back / forward
  - ⌘+ / ⌘−: zoom
- **Deep links:** `open "meshy://org/meshydemo.eth/activity"`.
- **Develop menu:** switch between production, `localhost:3000` and a custom URL. Right-click → Inspect Element opens the Web Inspector. `MESHY_URL=… open mac/build/Meshy.app` also overrides the URL.
- **QA snapshot:** `mac/build/Meshy.app/Contents/MacOS/Meshy --snapshot out.png --wait 15` renders the page to a PNG and quits.

# Meshy for macOS

A native window around the Meshy dashboard (AppKit + WKWebView, no dependencies). It opens the deployed dashboard at <https://meshy-bay.vercel.app> for one org (default `meshydemo.eth`).

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

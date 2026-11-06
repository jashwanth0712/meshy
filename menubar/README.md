# Meshy menu-bar app (macOS)

The menu-bar app shows the following at a glance:
- the org and chain status
- the agent identities on this Mac, and whether each is approved and human-backed
- every live grant with a countdown (click one to copy its `meshy ssh` command)
- live sessions on a meshyd running on this Mac
- recent blocks and revocations from Arkiv, with native notifications

```bash
./build-app.sh && open dist/Meshy.app
```

| Env | Default |
|---|---|
| `MESHY_ORG` | `meshydemo.eth` |
| `MESHY_SYNC` | `http://localhost:8787` (meshy-sync `/v1/map`) |
| `MESHY_DASHBOARD` | `http://localhost:3000` |
| `MESHY_CONTROL` | `/tmp/meshyd-live.sock` |
| `MESHY_ARKIV` | public Rabdi RPC |

When you launch it with `open`, it doesn't inherit your shell environment. To use non-default values, run `launchctl setenv MESHY_ORG yourorg.eth` first, or run the binary directly: `dist/Meshy.app/Contents/MacOS/meshy-menubar`.

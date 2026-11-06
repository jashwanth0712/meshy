# deploy/

| File | What |
|---|---|
| `meshyd.Dockerfile` + `meshyd-entrypoint.sh` | Headless resource daemon for Linux servers. First boot generates keys and prints the dashboard join code, then runs `doctor` and `meshyd run` |
| `Dockerfile.test` | Linux image (bubblewrap + fuse3) used to run the sandbox test suite |
| `provider/` | Provider stack: NetBird `combined` + meshy-sync (compose, Dockerfile, `.env.example`). See [provider/README.md](provider/README.md) |

## meshyd on a Linux server
```bash
docker build -f deploy/meshyd.Dockerfile -t meshyd .
docker run -d --name meshyd --device /dev/fuse --cap-add SYS_ADMIN \
  --security-opt seccomp=unconfined --security-opt apparmor=unconfined --security-opt systempaths=unconfined \
  -v meshy-state:/var/lib/meshy -v /srv:/srv \
  -e MESHY_NAME=db1.prod.acme.eth -e MESHY_ROOTS=/srv -e MESHY_SEPOLIA=wss://<sepolia-ws> -p 2222:2222 meshyd
docker logs meshyd   # join code -> dashboard Directory -> Add device
```
Why these flags:
- `seccomp=unconfined`, `apparmor=unconfined`: bwrap namespaces
- `/dev/fuse` + `SYS_ADMIN`: GateFS
- `systempaths=unconfined`: a private `/proc` per session. Without it, sessions get an empty `/proc`.

**Not yet verified:** the image has only been built in pieces. The full `docker build` hasn't been run end to end on this network, because module downloads in Docker were very slow (LEARNINGS #45).

# Learnings: gotchas, root causes, fixes

Each entry gives the **symptom**, the **cause** and the **fix**, plus where the fix lives. These are real things we hit while building, so read this before changing the matching area.

## ENSv2 (Sepolia)
1. **`text()` / `addr()` on PermissionedResolver revert with empty data.**
   - Cause: ENSv2's resolver exposes record reads **only** through ENSIP-10 `resolve(bytes dnsName, bytes call)`.
   - Fix: read via `resolve(dnsEncode(name), encodeFunctionData(text, [node, key]))` (`enskit/client.go` `resolveCall`, `web/src/lib/ens.ts` `text()`). viem's `getEnsText` also works, because it goes through the Universal Resolver.
2. **`register` reverts with `ERC1155InvalidReceiver(owner)` on a Sepolia fork.**
   - Cause: anvil/hardhat default accounts (for example `0xf39F…`) carry **EIP-7702 code** on Sepolia, so the ERC-1155 safe mint calls into them.
   - Fix: always use freshly generated keys as name owners (`e2e/*.sh` `newkey`). Check with `cast code <addr>`.
3. **Use the right deployment addresses.**
   - `contracts-v2` `main` has stale Sepolia addresses. Use docs.ens.domains/learn/deployments (the `post-audit-2` branch). They're in `enskit/client.go` and `admin.go`, and in `web/src/lib/config.ts`.
4. **Resolver records survive `unregister`.**
   - A re-registered label would serve the old `meshy.policy`.
   - Fix: the policy carries `grantTokenId`; meshyd rejects a mismatch (`MESHY_STALE_POLICY`).
5. **A plain expiry emits no event.**
   - Fix: the watcher re-checks every live grant on **every block** with `findExactOwner` (zero if the grant or any ancestor is dead) plus `getState.tokenId`, and keeps a local expiry timer.
6. **Resolver roles are scoped to the resolver, not to a name.**
   - Granting `ROLE_SET_TEXT` lets an account write that key on every name served by the resolver.
   - Per-key scoping uses `grantSetterRoles(setterCalldata, account)`. The setter calldata only needs the key (or coin type) argument; the name is ignored (`enskit.GrantTextKey`, `GrantAddrSetter`, `web` `textKeySetter`).
   - For per-name ownership, give the name **its own resolver** (we do this for agents).
7. **Admin roles are needed to grant roles.** `ALL_ROLES` = `0x1111…1111` (every role plus its admin). Owners deploy registries and resolvers with `ALL_ROLES` through `initialize(grants)`.
8. **Grants should be created with `roleBitmap = 0`,** so the agent (owner) can't renew, re-point or transfer its own grant.
9. **ETHRegistrar:** commit, then wait at least 60 s, then `register` (28-day minimum). MockUSDC (`0x240b…14aa`) has a public `mint`, so registration needs no faucet.
10. **The `SubregistryUpdated` topic is `0xca9c8d51…`.** One subagent mislabelled it `SubregistrySet` and "hard-coded the observed hash". It is simply `keccak("SubregistryUpdated(uint256,address,address)")`.

## Arkiv (Rabdi)
11. **`NonCanonicalOperationData(1)` (selector `0x89bc9448`).**
    - Cause: the create op data must be ABI-encoded as **one tuple** `(uint128,uint64,uint64,uint8,(bytes32,uint8,bytes)[])`, not 5 flat params.
    - The error isn't in 4byte directories; we found it by hashing the documented error list.
    - Fix: `arkivkit/encode.go` (cross-checked against `cast abi-encode` in a test).
12. **There is no Go SDK.** Raw transactions to `0x44…44` `execute((uint8,bytes)[])` work (`arkivkit/writer.go`).
13. **Entities are public, and anyone can tag `org = 'x'`.** Always filter by `$owner = addr(0x…)`. We take the trusted writers from the devices' ENS `addr` records.
14. **`$createdAt` isn't sortable.** Store your own `ts:u64`.
15. **Docs disagree on what Rabdi supports:** `NOT`/`!=` and cursors (see `arkiv/friction.md`).
16. **The faucet requires connecting a wallet.** For headless daemons, claim to a wallet and transfer (`cast send --value`).

## NetBird and the mesh
17. **NetBird v0.82 and go-ethereum can't link into one binary.** Both need `pion/stun/v3`, at incompatible versions (go-ethereum needs ≥ 3.1.7 with `transport/v4`; NetBird is written against the v3.1.0 API). That's why `meshnet/` is a separate module.
18. **NetBird `client/ssh/server`** has no public-key auth hook and no command-wrap hook, so we wrote our own gliderlabs server.
19. **Removing a NetBird peer doesn't reset open TCP connections.** The daemon must kill sessions itself.
20. **External modules importing NetBird must copy its `replace` directives** (circl → cunicu, easyjson → netbirdio).
21. **Custom DNS zones** in NetBird management (`/api/dns/zones`) could give `ssh db1.prod.acme.eth` with no custom resolver. We **don't use them**: a peer is named after its ENS name (dots → dashes), and `meshy-mesh proxy` resolves the ENS name from its peer list. A peer only appears once a policy (that is, a live grant) allows it, so "not found" means no access.

Added while deploying the provider (6 Nov):

50. **NetBird self-host (v0.82) is one script:** `getting-started.sh` with `NETBIRD_NON_INTERACTIVE=true NETBIRD_DOMAIN=<ip-dashes>.sslip.io` installs the combined server, embedded IdP (Dex), dashboard and Traefik with Let's Encrypt. sslip.io names get real certificates, so no DNS setup is needed.
51. **`/api/setup` runs once per instance**, and only returns a PAT if `NB_SETUP_PAT_ENABLED=true` was already set on `netbird-server`. We created the owner before setting the variable (the compose file was root-owned) and had to wipe the `netbird_data` volume. On the second try we didn't capture the token and had to wipe again. Set the env first, then capture the token in the same command.
52. **The NetBird API's policy rules are asymmetric:** create takes `sources: ["<groupId>"]`, but list returns `sources: [{id,name,…}]`. Decoding into `[]string` failed every reconcile pass until `GroupRefs` accepted both.
53. **`client/embed` takes exactly one credential:** a setup key, a JWT, or a private key, never two and never none. To sign the WireGuard key into `/v1/admit` before joining, seed `config.json` with `{"PrivateKey": …}` (NetBird only generates one if it's empty). Pass the setup key on the first join and the private key on later runs.
54. **The setup key's auto-groups place a new peer immediately.** The reconciler also matches peers to groups by name, so a peer re-registered by hand still lands in the right group.
55. **A Go ticker queues one tick while a pass is running.** A 10 s reconcile that took more than 10 s ran twice back to back. Wait with `time.After` after each pass instead.
56. **A provider must never evict on a single bad read.** The map builder skips entries on RPC errors, which would look like "this device is gone". The reconciler only evicts a peer after it has been missing for 2 consecutive passes.

## Sandboxing
22. **Docker masks `/proc`,** so `bwrap --proc /proc` fails inside a user namespace.
    - Fix: run with `--security-opt systempaths=unconfined`. Otherwise `Bwrap.Check()` detects it and falls back to `--tmpfs /proc`. **Never** bind the container's `/proc`: it would expose meshyd's environment.
23. **bwrap in Docker** also needs `seccomp=unconfined`, `apparmor=unconfined`, `--device /dev/fuse` and `SYS_ADMIN`.
24. **macOS `sandbox-exec` `deny default`** needs `process-info*`, `file-read*` on `/`, and `/private/etc`, or `/bin/sh` aborts (exit 134).
25. **macOS sandbox denials aren't visible to unprivileged log readers.** Detect blocks from tool stderr instead (`Operation not permitted` → BlockTap).
26. **Symlink escapes are handled by design.** GateFS decides on the absolute in-sandbox path, so a link to `/etc/shadow` re-enters the `/etc` gate and is refused.

## Go and the SSH server
27. **gliderlabs splits `ssh host 'a; b'` into words,** which breaks shell syntax. Run `sess.RawCommand()` via `sh -c` like OpenSSH does. With a command allowlist, use no shell and refuse metacharacters.
28. **To support port forwarding with ENS auth,** register a custom `direct-tcpip` `ChannelHandler`. Treat the requested host as the service ENS name, and dial only the ENS-declared target.
29. **Go `flag` stops at the first positional argument,** so `meshy connect <svc> --local …` ignored `--local`. Fix: rotate the positional to the end before `Parse`.
30. **macOS unix socket paths are capped at 104 bytes.** Use `/tmp/<name>.sock`, not deep scratch paths.
31. **Priority fees on quiet chains:** never multiply `eth_maxPriorityFeePerGas`. It's derived from your own recent transactions and compounds. Clamp it to 1–10 gwei (`enskit.ClampTip`).

## RPC providers
32. **Alchemy's free tier caps `eth_getLogs` at a 10-block range.** The dashboard proxy routes `eth_getLogs` to the public node; meshy-sync is run with `--sepolia publicnode`.
33. **The public node sometimes returns "block not found" for its own head.** anvil `--fork-url` then crashes. Fix: `--fork-block-number <head-8>`, and prefer Alchemy as the fork source.
34. **A free-tier rate limit plus 144 parallel requests** makes viem back off and retry, causing 40 s loads. Fix: `batch: { multicall: true }` (Multicall3) and fewer sequential awaits.

## Next.js 16 (read `web/AGENTS.md`)
35. **`usePathname()` or awaited `params` outside `<Suspense>`** block prerendering and produce errors. Fix: the layout passes the `params` Promise to a client component that unwraps it with `use()` inside `<Suspense>` (`components/OrgShell.tsx`, `app/approve/ApproveEntry.tsx`).
36. **Hydration mismatch** when client and server initial state differ. Start `loading` as `true` on both.
37. **Never trigger transactions from a `useState` initializer.** StrictMode runs it twice, which would send two `unregister` transactions. Use `useEffect` with a ref guard.
38. **`LayoutProps` / `PageProps` are generated globals.** Run `npx next typegen` after adding or removing routes, or `tsc` fails on stale `.next/types`.
39. **Don't use `next/font/google`** on a flaky network: it fetches at build time. We use system fonts, which also means no third-party requests.

## Testing pitfalls (bugs in our tests, not in Meshy)
40. **Python's `http.server` speaks HTTP/1.0 and closes after every response.** It can't prove a revoke cut a live tunnel. Use a TCP server that streams continuously (`e2e/services-local.sh` feed service).
41. **`set -e` in bash plus an expected exit 137** inside a background subshell aborted the latency loop. Use `set +e` in the subshell and `wait … || true`.
42. **`pgrep -f 'sleep 300'` matches its own command line.** Use `pgrep -x sleep`.
43. **A hand-typed hex constant in a test was wrong,** not the encoder. Derive test vectors from the code under test.
44. **Port clashes:** the live meshyd holds 2222, so e2e scripts take `SSHPORT`.
45. **Downloads in Docker were extremely slow on this network.** Mount the host module cache (`-v $(go env GOMODCACHE):/go/pkg/mod`).

## Process and tooling
46. **The GitHub push failed with `fatal error in commit_refs_rules`** (even for existing commits). The REST API worked meanwhile, and it cleared after about 15 minutes. Retry later rather than rewriting history.
47. **Auto mode blocks creating public repos** and making repos public. A human must do it.
48. **Subagent reports can misstate details:** flags, ports, "nothing left to do". Always verify on the branch before merging (we caught a mislabelled event name and wrong flag claims).
49. **Secrets** live only in `~/.meshy/secrets.env`, `~/.meshy/aws.env`, `~/.meshy/netbird.env` and `~/.meshy/meshy-provider.pem` (all mode 600), and on the VPS in `~/meshy-sync/sync.env`. Before each commit, `git grep` for the key values.

Added 6 Nov:

57. **The AWS access key the user pasted was 26 characters; AWS IDs are 20.** The first 20 worked. If `InvalidClientTokenId` appears with a correct-looking key, check the length.
58. **Uploads from the dev network to the VPS are slow and occasionally reset** (a 10 MB scp took minutes; one HTTPS POST was reset). `meshadmit.Admit` retries 3 times; redeploy by copying a stripped static binary (`-ldflags='-s -w'`).

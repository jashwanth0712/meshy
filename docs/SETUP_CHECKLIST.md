# Setup checklist: accounts, keys and funds

This lists what humans have to do; the code can't do it. Tick items off as you go. A ⏱ marks items with a waiting period, so start those first.

## 1. Arkiv: fund the audit writer (Lane C, needed for the Arkiv prize)
The resource daemon (meshyd) signs the Arkiv writes. Its key is in `~/.meshy/meshyd-demo/eth.key` on the demo Mac.

- **Writer address:** `0x7Dec34A86907eeF26c70A6578df7bAaE7F223384`
- **Network:** Rabdi, chain ID `7794095`, RPC `https://rpc.rabdi.db-chain.testnet.arkiv.network`, explorer `https://rabdi.explorer.arkiv.network`, gas token tGLM

Steps:
1. Add Rabdi to MetaMask (Settings → Networks → Add manually) using the values above. The currency symbol is `tGLM`.
2. Open **https://hub.arkiv.network/faucet**, connect that wallet, and claim tGLM. You get one claim per wallet per cooldown window.
3. Send most of it to `0x7Dec34A86907eeF26c70A6578df7bAaE7F223384`.
   - Each audit batch costs about 130k gas at about 1 gwei, so even 0.5 tGLM covers thousands of batches.
4. Recommended: create an access key at **https://hub.arkiv.network/access-keys**, because the public RPC is heavily rate-limited. meshyd takes it as a URL path suffix: `--arkiv https://rpc.rabdi.db-chain.testnet.arkiv.network/<ACCESS_KEY>`.
5. Tell Claude when you're done. It will run a live write, then put the writer address, the tx hash and a runnable query into `arkiv/schema.md`.

Check the balance: `cast balance 0x7Dec34A86907eeF26c70A6578df7bAaE7F223384 --ether --rpc-url https://rpc.rabdi.db-chain.testnet.arkiv.network`

## 2. World: Agent Pass (Lane B; ⏱ about 1 h activation)
- [ ] Install the **sandbox** World ID app (TestFlight link on the ETHGlobal World prize page). Choose **Manual verify** (selfie or upload). Activation takes about 1 hour.
- [ ] Create a client at **https://sandbox.auth.world.org/portal** (sign in with Google). Note the `client_id` and `client_secret`; the secret is shown only once. Register an HTTPS redirect URI even though we use the device flow.
- [ ] Give the secret to the provider (meshy-sync) only. It must never reach the dashboard or the CLI.
- [ ] If the sandbox app is broken (another team reported this on 31 Oct), ask at the World booth. A fixture mode exists as a fallback.

## 3. Sepolia (all lanes)
- [ ] A **websocket RPC key** from Alchemy or Infura. The public node flaked during testing (anvil crashed and blocks went missing), and meshyd needs `wss://` for fast revocation.
- [ ] Sepolia ETH for the admin wallet that creates the org, networks, resources and grants. That's about 15 transactions of 0.1–1M gas each, plus one register and one setText per grant.
- [ ] MockUSDC for the `.eth` registration needs no faucet. `MockUSDC.mint` is public, and `meshy-admin org init` mints it automatically.
- [ ] Do **not** use anvil or hardhat default accounts as name owners. They carry EIP-7702 code on Sepolia, and ENSv2 rejects them as owners.

## 4. Org name (Lane A)
- [ ] Choose the demo org label (5+ characters cost about 8 MockUSDC/yr), for example `meshydemo`. Then run:
  `meshy-admin org init --label meshydemo --attestor <provider signer address>`
  using `--rpc <sepolia https>` and `--key <admin key>`. Alternatively, the dashboard onboarding wizard does the same thing.

## 5. Provider infrastructure (Lane B)
- [ ] A small VPS with a public domain, ports 443/tcp, 80/tcp and 3478/udp, for NetBird `combined` plus meshy-sync.
- [ ] The provider signer key. Its address goes in the org's `meshy.attestor` record.

## 6. Dashboard login (Lane A)
- [ ] A Privy app. Allow-list the dashboard origin, and enable "CLI and agent access" (device flow) for the Mac app.

## 7. Repo and submission
- [ ] **Make `jashwanth0712/meshy` public.** Auto mode blocked doing it from Claude. Use Settings → Danger Zone → Change visibility, or run
  `gh repo edit jashwanth0712/meshy --visibility public --accept-visibility-change-consequences`
- [ ] Ronnieraj37 and aizahs31: accept the collaborator invites.
- [ ] Record the demo video: 2:30–3:00, which satisfies ETHGlobal's 2–4 min requirement.
- [ ] Submit by **Sat 7 Nov 08:00 IST** (the hard deadline is 09:00).
- [ ] Partner picks (max 3): **ENS, World, Arkiv**.

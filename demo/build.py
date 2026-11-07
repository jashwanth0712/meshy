import json, re, base64, html as H, os
HERE = os.path.dirname(os.path.abspath(__file__))
A = D = os.path.join(HERE, 'slides')
def aside(path):
    m = re.search(r'<aside>(.*?)</aside>', open(path).read(), re.S)
    return H.unescape(m.group(1)) if m else ''
def note(sid):
    for d in (A, D):
        p = f'{d}/{sid}.html'
        if os.path.exists(p): return aside(p)
    return ''
STEPS = [
  dict(eyebrow='A mesh, not a map', title='Your machines are everywhere', sub='One team’s devices, spread across Chennai, Mumbai, Dubai and San Francisco.', notes=note('map-world')),
  dict(eyebrow='Zoom in', title='Two networks in India', sub='Chennai runs three GPU servers. Mumbai has laptops and the office camera server.', notes=note('map-india')),
  dict(eyebrow='Pick and name', title='Only what you pick joins', sub='Each picked device gets an ENS name. Everything else stays out.', notes=note('map-pick')),
  dict(eyebrow='Then the map disappears', title='One mesh, no centre', sub='Every named device reaches every other directly, over WireGuard.', notes=note('map-mesh')),
  dict(eyebrow='And agents', title='One door, not the building', sub='claude-1 gets one folder on one machine, for one hour. Nothing else.', notes=note('map-agent')),
  dict(eyebrow='A vision agent', title='lens-bot sees one camera', sub='One port on office-streams for 30 minutes. No shell, no files.', notes=note('map-lens')),
  dict(eyebrow='A data agent', title='analyst reads, never writes', sub='/warehouse on cpu-vm, read-only, for four hours.', notes=note('map-analyst')),
  dict(eyebrow='A deploy agent', title='ci-bot runs one command', sub='Only train.sh on gpu-4090, for 15 minutes. Anything else is refused.', notes=note('map-cibot')),
  dict(eyebrow='An on-call human', title='bob runs one network', sub='Delegated admin of mumbai.acme.eth: he grants and revokes there, nowhere else.', notes=note('map-bob')),
  dict(eyebrow='A whole team · roadmap', title='ml-team shares one grant', sub='priya and arjun reach the Dubai GPU together, read-write, for eight hours.', notes=note('map-team')),
  dict(eyebrow='Who runs the tunnels', title='A provider, staked in Chennai', sub='Anyone can run tunnels by locking stake. acme.eth picks one with 1 ETH at risk.',
       notes='Someone has to run the tunnels. acme.eth’s mesh pulls back into a cluster and asks for a provider. Providers are permissionless: anyone can register in ProviderRegistry by locking stake (the contract minimum is 0.01 ETH; this one, chennai-1, locked 1 ETH) and publishing a price per seat, 2 USDC a month here. The provider runs NetBird’s management, signal and relay servers next to meshy-sync, which compiles the mesh’s groups, policies and DNS from ENS. If it ever admits a peer to a name that was already dead, anyone can submit its signed admission attestation to slash() and the contract checks ENS on-chain; half the stake goes to the reporter. Honest status: the provider stack is built but hasn’t run on a live server yet, and admission attestations for slashing aren’t emitted yet.'),
  dict(eyebrow='The control plane', title='ENS sits in between', sub='Many orgs, one provider. ENS decides who can reach what; the provider only follows it.',
       notes='Zoom out to the whole picture. One provider serves many orgs, each in its own shade: acme.eth, lumen.eth, orbit.eth, kite.eth, each with its own networks, devices and agents. ENSv2 is the layer in between. Orgs write their names and grants there; the provider and every device daemon read it. That’s why the provider can be anyone: it never decides who gets access. If it disagrees with ENS, it can be proven wrong on-chain and slashed, and the daemons enforce ENS themselves, killing sessions the block a grant dies.'),
  dict(eyebrow='How it pays', title='Seats in, earnings out', sub='Orgs pay USDC per seat into escrow: 95% to the provider, 5% to the treasury.',
       notes='Billing is a contract, not an invoice. Each org subscribes in MeshySubscription for a number of seats at the provider’s price: acme 5 seats, lumen 8, orbit 3, kite 4, so 20 seats at 2 USDC is 40 USDC a month. The contract sends 5% to the treasury and holds the rest in escrow for the provider, who claims it: 38 USDC a month here. Cancel early and unused months are refunded from escrow. Honest status: the contracts are deployed and verified on Sepolia with 31 tests passing; the provider’s admission check against paid seats isn’t wired yet.'),
  dict(eyebrow='Audit', title='Every move, on the record', sub='Each device writes its own sessions, file ops and blocks to Arkiv: sealed, read-only, checkable by anyone.',
       notes='Back inside one network, acme.eth. Each agent and the team reach their devices along their grant lines, and every action lands as a row: who, which device, what, and which Arkiv entity it became. meshyd on each device writes meshy.session entities for starts, ends and revocations, meshy.blocked for every refused attempt, and meshy.audit-chunk for batches of file operations, sealed to the org key and read-only. Note the two orange rows: claude-1 reaching for /etc/shadow, and analyst trying to write to a read-only folder. Then alice revokes claude-1, and the revoke entity carries its measured latency. Readers only trust entities whose owner matches the device’s addr record in ENS. The rows here are an illustrative sequence; the schema and entity types are the real ones, and the dashboard’s Activity page reads them live today.'),
  dict(eyebrow='From log to insight', title='The audit trail, read back', sub='The same Arkiv entities, queried by anyone: who did what, what was refused, how fast revokes land.',
       notes='The table becomes answers. Every chart here is a query over the same public, read-only entities, so an auditor, the org or a judge gets the same numbers without trusting Meshy’s servers. Actions by actor and outcomes come from the illustrative log on the previous slide. The revocation latency chart is real: the four values, 120, 192, 132 and 216 ms, are read back from the latencyMs attribute of meshy.session revoke entities written by the live daemons on Sepolia.'),
  # 15-21: the ENSv2 chapter, played between billing (12) and audit (13)
  dict(eyebrow='Inside the middle block', title='ENSv2: one namespace per org', sub='The same network, read as names. A grant is one more level down.',
       notes='Each node of the acme network flies to its own line: this is the same network, written as ENS names. acme.eth is the org; every level below it is its own ENSv2 UserRegistry: networks like mumbai.acme.eth, devices like mbp-m3.mumbai.acme.eth, and under a device, grants such as claude-1.mbp-m3.mumbai.acme.eth. Read a grant name right to left: root, org, network, device, agent. The grant is a real subname: its native expiry is the grant’s lifetime, roleBitmap 0 means the holder can’t move or extend it, and unregister() is revoke. Its meshy.policy text record holds the sealed route. People live under people.acme.eth and delegated admins like bob get Enhanced Access Control roles on one network. Honest status: networks, devices, agent grants and delegated admins are built today; teams and grants held by people are on the roadmap.'),
  dict(eyebrow='Inside a grant', title='The route is sealed, not shown', sub='The folder and command rules are encrypted to the device and the org, then stored on the grant’s name.',
       notes='Zoom into claude-1’s grant. The policy is plain JSON: which paths, read-only or read-write, which commands, bound to the grant’s tokenId so it can’t be pasted onto another name. meshy encrypts it with a fresh 32-byte content key using XChaCha20-Poly1305, then wraps that key twice with X25519 and HKDF-SHA256: once to the device’s meshy.enckey and once to the org’s meshy.orgkey. The result goes into the meshy.policy text record. ENS is public, so anyone can see that a grant exists and when it expires, but only mbp-m3 and the org can read what it allows. The JSON shown is illustrative; the cipher suite, the two recipients and the tokenId binding are the real ones in the code.'),
  dict(eyebrow='On the laptop', title='meshy enforces the routes', sub='The device decrypts its routes from ENS and checks every file, command and port against them.',
       notes='Now the laptop itself. meshyd on mbp-m3 reads every grant under its device name, decrypts each meshy.policy with its own key and builds the list of approved routes. Each agent session runs inside a sandbox: Seatbelt on macOS, bubblewrap with the GateFS filesystem on Linux. Anything inside the routes passes; anything else is refused with MESHY_BLOCKED and written to Arkiv. Here claude-1 can read and write ~/code/site but is stopped at ~/.ssh, and ci-bot can run npm test but not curl-pipe-sh. The specific routes and events are illustrative; the sandboxing and the block codes are real.'),
  dict(eyebrow='On every device', title='Same rules, everywhere', sub='Each laptop and server runs meshy and enforces only the routes sealed to it.',
       notes='Zoom out to the whole acme network. Every laptop and server runs the same daemon, and each one enforces only the grants sealed to its own key: mbp-m3 holds claude-1’s routes, office-streams holds lens-bot’s single camera port, cpu-vm holds analyst’s read-only warehouse, gpu-4090 holds ci-bot’s one command. No central gateway sits in the path: the WireGuard mesh carries the traffic, and each device is its own policy enforcement point.'),
  dict(eyebrow='The admin removes one', title='Revoke is one transaction', sub='alice unregisters claude-1’s grant on mbp-m3. The name, and its sealed route, are gone from ENS.',
       notes='alice, the network admin, revokes claude-1. That is one call, unregister() on the device registry, mined in one block. The subname and its meshy.policy record disappear together, so there is nothing left to decrypt. Every other grant is untouched: lens-bot, analyst and ci-bot keep their routes. There is no revocation list to distribute and no server to tell; the chain is the list.'),
  dict(eyebrow='Back on the laptop', title='The laptop sees it in one block', sub='meshy notices the missing grant, kills the live session and refuses the next request.',
       notes='Back on mbp-m3. meshyd re-checks its grants on every new block. At block 12073941 claude-1’s grant is gone, so its routes are struck from the list, the live session is killed, and the next read comes back MESHY_REVOKED. ci-bot carries on as before. These numbers are real: measured kills land between 120 and 216 ms after the daemon sees the block, and if the daemon can’t reach the chain for 45 seconds it fails closed and drops every session.'),
  dict(eyebrow='Zoom out', title='Back to the whole network', sub='Many orgs, one provider, ENS in between: every change is one name, seen by every device.',
       notes='And back out to where we were: many orgs, the Chennai provider, ENS as the layer in between. Everything we just saw, the naming, the sealed routes, the revoke, happens inside that one band, and every device reads it directly. Next: what every one of those moves leaves behind.'),
  # 22: zoom from billing into acme.eth, played before 15
  dict(eyebrow='Zoom into one org', title='acme.eth, up close', sub='Its networks, devices, people, a team and agents, each with its own name. Every line to a device is a grant.',
       notes='Back into acme.eth, one of the orgs on the provider. Three networks, Mumbai, Chennai and Dubai, with their devices; alice and bob under people; ml-team with priya and arjun; and four agents. Each solid line is a grant: claude-1 on mbp-m3, lens-bot on office-streams, analyst on cpu-vm, ci-bot on gpu-4090, ml-team on the Dubai GPU. bob’s dashed line is a delegated admin role on the Mumbai network. Everything on this screen already has an ENS name; the next step shows them as the hierarchy they form.')
]
ORDER = ['cover', 'problem', 'before', 'solve-timeline', *range(13), 22, 15, 16, 17, 18, 19, 20, 21, 13, 14, 'solve', 'architecture', 'architecture-planes', 'stack', 'impact', 'usecases', 'ens', 'crypto', 'close', 'poster-name', 'poster-ssh', 'poster-revoke', 'poster-scope', 'poster-record']
imgs = {'/_blob/e55f0d271f18d62ba61574308a3dc15e': os.path.join(HERE, 'assets/twingate-menu.png'), '/_blob/cc4365a1cd60f713ea9b4151d384caed': os.path.join(HERE, 'assets/twingate-terminal.png')}
parts = []
for sid in ORDER:
    if isinstance(sid, int): continue
    s = open(f'{A}/{sid}.html').read().strip()
    for blob, f in imgs.items():
        if blob in s: s = s.replace(blob, 'data:image/png;base64,' + base64.b64encode(open(f, 'rb').read()).decode())
    assert '/_blob/' not in s, sid
    parts.append(s)
t = open(os.path.join(HERE, 'present.template.html')).read()
out = (t.replace('/*__SLIDES__*/', '\n'.join(parts))
        .replace('/*__SCENE__*/', open(os.path.join(HERE, 'scene.js')).read())
        .replace('/*__DOTS__*/', open(os.path.join(HERE, 'dots.json')).read())
        .replace('/*__STEPS__*/', json.dumps(STEPS, ensure_ascii=False))
        .replace('/*__ORDER__*/', json.dumps(ORDER)))
# a full document for static hosting (the artifact preview adds its own skeleton)
page = ('<!doctype html>\n<html lang="en">\n<head>\n<meta charset="utf-8">\n'
        '<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">\n'
        + out.replace('<div class="viewport"', '<style>html,body{margin:0}</style>\n</head>\n<body>\n<div class="viewport"', 1)
        + '\n</body>\n</html>\n')
open(os.path.join(HERE, '..', 'web', 'public', 'demo.html'), 'w').write(page)
print('bytes', len(out), 'slides', len(ORDER))

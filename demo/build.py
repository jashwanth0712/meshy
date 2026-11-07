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
]
ORDER = ['cover', 'problem', 'before', 'solve-timeline', *range(13), 'network', 'solve', 'architecture', 'architecture-planes', 'stack', 'impact', 'usecases', 'ens', 'crypto', 'close', 'poster-name', 'poster-ssh', 'poster-revoke', 'poster-scope', 'poster-record']
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

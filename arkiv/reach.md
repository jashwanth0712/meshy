# Reach

> "Who is the app for, and would they use it? Can they try it today, and will
> the team keep building it after Mumbai?"

---

## Who it's for

**Primary user:** an engineering team lead at a company that gives AI agents
(Claude Code is the first target) access to production infrastructure. They
already use ENS-named resources on Ethereum and want a tamper-evident, publicly
verifiable audit trail without running a centralised logging backend the same
admins could quietly edit.

**Secondary user:** a security/compliance engineer who needs a 30-day evidence
window for every blocked access attempt, and 90-day evidence for every
revocation, to satisfy an internal audit policy.

Both users care that the trail is *independently* verifiable — a query anyone
can run against the public Arkiv RPC returns exactly what the dashboard shows,
with no server in the middle to filter or rewrite.

---

## Would they use it?

The activity feed, the kill-latency stat (avg 148 ms across 3 real revocations),
and the live-sessions view all read directly from Arkiv with zero backend —
the dashboard is a static Next.js app. The only trust anchor is ENS: the
resource daemon address in the `addr` record is what filters Arkiv queries
(via `$owner`, so a rogue writer with the right `org` attribute is ignored).

That is a meaningfully better story than "logs in an S3 bucket your DevOps team
can delete". Teams already ask for this — the standard audit-log SaaS tools are
either centralised (mutable) or prohibitively expensive for high-volume agent
traffic.

---

## Can they try it today?

Yes. The repo (to be made public after submission) contains:

```bash
git clone https://github.com/jashwanth0712/meshy
cd meshy
# follow docs/QUICKSTART.md for a Sepolia fork + live Arkiv run
bash e2e/live-demo.sh
```

The live Arkiv data is already on Rabdi testnet:

```bash
# Revocations (meshydemo.eth, writer 0x7Dec34A86907eeF26c70A6578df7bAaE7F223384)
curl -s https://rpc.rabdi.db-chain.testnet.arkiv.network \
  -H 'content-type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"arkiv_query","params":[
    "arkiv.schema = str('"'"'meshy.session'"'"') AND kind = str('"'"'session.revoke'"'"') AND org = str('"'"'meshydemo.eth'"'"') ORDER BY ts:u64 DESC",
    {"select":{"key":true,"attributes":true},"limit":"0x14"}]}'
```

This returns 3 revocation entities with real `latencyMs` values (120, 192, 132 ms).
No wallet or access key is required to run the query.

The [Rabdi explorer](https://rabdi.explorer.arkiv.network/address/0x7Dec34A86907eeF26c70A6578df7bAaE7F223384)
shows the writer's 12 successful batches (0 failures).

---

## Will the team keep building?

Yes. The immediate roadmap after Mumbai:

1. **ENS EAC:** delegated network admins so the org admin is not the only one
   who can grant access; per-agent Permissioned Resolvers so agents can rotate
   their own SSH keys.
2. **World ID:** real sandbox credentials for the full device-flow,
   replacing the dev-attestation stand-in.
3. **Dashboard writes** (grant, revoke, approve) tested with a real wallet.
4. **NetBird mesh transport** on a live VPS, so "Tailscale alternative" is
   demonstrated end-to-end.
5. **`watchEntityEvents` subscriptions** in the dashboard instead of polling.

The underlying product need is real: AI agents on production infrastructure are
happening now, and there is no tamper-evident, publicly queryable audit standard
for them yet.

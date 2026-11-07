"use client";
import { useState } from "react";
import { Admins } from "@/components/Admins";
import { Badge, Button, Card, OnChain, PageHeader, Row, Short, cx, inputCls } from "@/components/ui";
import { publicKeyOf } from "@/lib/envelope";
import { useMeshy } from "@/lib/store";

export default function SettingsPage() {
  const { tree, org, orgKey, setOrgKey, toast, account } = useMeshy();
  const [importing, setImporting] = useState(false);
  const [input, setInput] = useState("");
  const isOwner = !!account && !!tree && tree.owner.toLowerCase() === account.toLowerCase();
  function importKey() {
    let k = input.trim();
    try {
      // Accept the raw base64 key or the meshy-admin state JSON (~/.meshy/admin/<org>.json).
      if (k.startsWith("{")) k = (JSON.parse(k) as { orgKeyPriv?: string }).orgKeyPriv ?? "";
      const pub = publicKeyOf(k);
      if (tree?.orgKey && pub !== tree.orgKey) {
        toast("That key doesn't match this org", "err");
        return;
      }
      setOrgKey(k);
      setInput("");
      setImporting(false);
      toast("Org key loaded in this browser", "ok");
    } catch {
      toast("That isn't a valid org key", "err");
    }
  }
  return (
    <>
      <PageHeader title="Settings" sub={<span className="mono">{org}</span>} />

      <Card title="Organization" actions={tree && <OnChain name={org} addr={tree.owner} />}>
        {tree ? (
          <>
            <Row><span className="w-36 shrink-0 text-muted">Owner</span><span className="flex-1"><Short v={tree.owner} />{isOwner && <span className="text-muted"> (you)</span>}</span></Row>
            <Row><span className="w-36 shrink-0 text-muted">Resolver</span><Short v={tree.resolver} /></Row>
            <Row><span className="w-36 shrink-0 text-muted">Attestor</span><span className="flex flex-wrap gap-3">{tree.attestors.map((a) => <Short key={a} v={a} />)}</span></Row>
          </>
        ) : (
          <Row><span className="text-muted">Reading the org…</span></Row>
        )}
      </Card>

      <Card title="Org key" className="mt-10">
        {orgKey ? (
          <Row>
            <span className="flex-1">
              Loaded in this browser{" "}
              {tree?.orgKey === publicKeyOf(orgKey) ? <Badge tone="ok">Matches</Badge> : <Badge tone="bad">Doesn&apos;t match</Badge>}
            </span>
            <Button kind="ghost" className="h-8 px-3" onClick={() => setOrgKey(undefined)}>Forget</Button>
          </Row>
        ) : importing ? (
          <div className="space-y-3 p-5">
            <textarea
              className={cx(inputCls, "h-24")}
              value={input}
              onChange={(e) => setInput(e.target.value)}
              placeholder={`Paste the key, or ~/.meshy/admin/${org}.json`}
              autoFocus
            />
            <div className="flex gap-2">
              <Button kind="primary" onClick={importKey} disabled={!input.trim()}>Import</Button>
              <Button kind="ghost" onClick={() => setImporting(false)}>Cancel</Button>
            </div>
          </div>
        ) : (
          <Row>
            <span className="flex-1">Not loaded. Load it to read encrypted paths and commands. It stays in this browser.</span>
            <Button className="h-8 px-3.5" onClick={() => setImporting(true)}>Import</Button>
          </Row>
        )}
      </Card>

      <Admins />
    </>
  );
}

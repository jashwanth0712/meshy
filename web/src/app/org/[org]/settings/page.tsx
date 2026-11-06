"use client";
import { KeyRound, Trash2 } from "lucide-react";
import { useState } from "react";
import { Badge, Button, Card, Field, Name, OnChain, Short, inputCls } from "@/components/ui";
import { publicKeyOf } from "@/lib/envelope";
import { useMeshy } from "@/lib/store";

export default function SettingsPage() {
  const { tree, org, orgKey, setOrgKey, toast } = useMeshy();
  const [input, setInput] = useState("");
  function importKey() {
    let k = input.trim();
    try {
      // Accept the raw base64 key or the meshy-admin state JSON (~/.meshy/admin/<org>.json).
      if (k.startsWith("{")) k = (JSON.parse(k) as { orgKeyPriv?: string }).orgKeyPriv ?? "";
      const pub = publicKeyOf(k);
      if (tree?.orgKey && pub !== tree.orgKey) {
        toast("That key does not match this org's meshy.orgkey record", "err");
        return;
      }
      setOrgKey(k);
      setInput("");
      toast("Org key loaded in this browser", "ok");
    } catch {
      toast("Not a valid X25519 private key", "err");
    }
  }
  return (
    <div className="grid gap-5 lg:grid-cols-2">
      <Card title="Organization">
        {tree && (
          <div className="space-y-3 text-sm">
            <Name name={org} />
            <OnChain name={org} addr={tree.owner} />
            <div>
              Owner <Short v={tree.owner} />
            </div>
            <div>
              Resolver <Short v={tree.resolver} />
            </div>
            <div>
              Org registry <Short v={tree.orgRegistry} />
            </div>
            <div>
              Trusted attestors {tree.attestors.map((a) => <Short key={a} v={a} />)}
            </div>
          </div>
        )}
      </Card>
      <Card title="Org encryption key" actions={orgKey ? <Badge tone="ok">loaded</Badge> : <Badge tone="warn">not loaded</Badge>}>
        <p className="mb-4 text-sm text-muted">
          Grant policies and Arkiv audit payloads are encrypted to the org&apos;s X25519 key (public half in <span className="mono">meshy.orgkey</span>). The private half stays
          on your machines — import it here to decrypt audit details. It is stored only in this browser.
        </p>
        {orgKey ? (
          <div className="space-y-3">
            <div className="text-sm">
              Public key <Short v={publicKeyOf(orgKey)} n={10} />{" "}
              {tree?.orgKey === publicKeyOf(orgKey) ? <Badge tone="ok">matches ENS</Badge> : <Badge tone="bad">does not match ENS</Badge>}
            </div>
            <Button kind="danger" onClick={() => setOrgKey(undefined)}>
              <Trash2 size={14} /> Forget key
            </Button>
          </div>
        ) : (
          <div className="space-y-3">
            <Field label="Private key or meshy-admin state file" hint={<>Paste the base64 key, or the contents of <span className="mono">~/.meshy/admin/{org}.json</span>.</>}>
              <textarea className={inputCls + " h-28"} value={input} onChange={(e) => setInput(e.target.value)} />
            </Field>
            <Button kind="primary" onClick={importKey} disabled={!input.trim()}>
              <KeyRound size={14} /> Import
            </Button>
          </div>
        )}
      </Card>
    </div>
  );
}

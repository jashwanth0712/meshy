"use client";
import { useState } from "react";
import { Button, Field, Sheet, inputCls } from "@/components/ui";
import { useMeshy } from "@/lib/store";

/** Invite an agent: a link it opens with /meshy:join, which comes back as an approval link. */
export function InviteSheet({ onClose }: { onClose: () => void }) {
  const { org } = useMeshy();
  const [label, setLabel] = useState("claude-2");
  const [copied, setCopied] = useState(false);
  const invite = typeof window !== "undefined" ? `${window.location.origin}/approve?org=${org}&agent=${label}` : "";
  return (
    <Sheet
      title="Invite an agent"
      onClose={onClose}
      footer={
        <>
          <span className="text-[13px] text-muted">No transaction until you approve</span>
          <Button
            kind="primary"
            onClick={() => {
              navigator.clipboard.writeText(invite);
              setCopied(true);
              setTimeout(() => setCopied(false), 1500);
            }}
          >
            {copied ? "Copied" : "Copy invite link"}
          </Button>
        </>
      }
    >
      <Field label="Name" hint={<span className="mono">{label}.agents.{org}</span>}>
        <input className={inputCls} value={label} onChange={(e) => setLabel(e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, ""))} />
      </Field>
      <ol className="list-decimal space-y-2 pl-5 text-[14px] text-muted">
        <li>
          Send the link to the agent. In Claude Code: <span className="mono text-fg">/meshy:join &lt;link&gt;</span>
        </li>
        <li>It sends back an approval link.</li>
        <li>Open it, prove a human is behind the agent with World ID, and approve.</li>
      </ol>
    </Sheet>
  );
}

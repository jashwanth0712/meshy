import { Suspense } from "react";
import { OrgShell } from "@/components/OrgShell";

export default function OrgLayout({ children, params }: LayoutProps<"/org/[org]">) {
  // params is unwrapped inside Suspense (client) so the shell streams instead of blocking.
  return (
    <Suspense fallback={<div className="p-6 text-muted">Loading…</div>}>
      <OrgShell params={params}>{children}</OrgShell>
    </Suspense>
  );
}

import { Suspense } from "react";
import { Shell } from "@/components/Shell";
import { MeshyProvider } from "@/lib/store";

export default async function OrgLayout({ children, params }: LayoutProps<"/org/[org]">) {
  const { org } = await params;
  return (
    <MeshyProvider org={decodeURIComponent(org).toLowerCase()}>
      <Suspense fallback={<div className="p-6 text-muted">Loading…</div>}>
        <Shell>{children}</Shell>
      </Suspense>
    </MeshyProvider>
  );
}

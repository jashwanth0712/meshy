import { Suspense } from "react";
import { ApproveEntry } from "./ApproveEntry";

export default function ApprovePage({ searchParams }: PageProps<"/approve">) {
  return (
    <Suspense fallback={<div className="p-6 text-muted">Loading…</div>}>
      <ApproveEntry searchParams={searchParams} />
    </Suspense>
  );
}

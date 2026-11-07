import { Suspense } from "react";
import { ApproveEntry } from "./ApproveEntry";

export default function ApprovePage({ searchParams }: PageProps<"/approve">) {
  return (
    <Suspense fallback={<div className="mx-auto max-w-5xl px-6 pt-24 text-[14px] text-muted">Loading…</div>}>
      <ApproveEntry searchParams={searchParams} />
    </Suspense>
  );
}

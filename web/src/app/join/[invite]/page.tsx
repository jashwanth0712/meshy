import { Suspense } from "react";
import { JoinClient } from "./JoinClient";

export default function JoinPage({ params }: PageProps<"/join/[invite]">) {
  // params is unwrapped inside Suspense (client) so the page streams instead of blocking.
  return (
    <Suspense fallback={<div className="mx-auto max-w-xl px-6 pt-24 text-[14px] text-muted">Reading the invite…</div>}>
      <JoinClient params={params} />
    </Suspense>
  );
}

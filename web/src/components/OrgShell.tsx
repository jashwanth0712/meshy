"use client";
import { type ReactNode, use } from "react";
import { MeshyProvider } from "@/lib/store";
import { Shell } from "./Shell";

export function OrgShell({ params, children }: { params: Promise<{ org: string }>; children: ReactNode }) {
  const { org } = use(params);
  return (
    <MeshyProvider org={decodeURIComponent(org).toLowerCase()}>
      <Shell>{children}</Shell>
    </MeshyProvider>
  );
}

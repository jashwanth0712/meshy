"use client";
import { use } from "react";
import { DEFAULT_ORG } from "@/lib/config";
import { MeshyProvider } from "@/lib/store";
import { Approve } from "./Approve";

type SP = Record<string, string | string[] | undefined>;

export function ApproveEntry({ searchParams }: { searchParams: Promise<SP> }) {
  const q = use(searchParams);
  const get = (k: string) => (Array.isArray(q[k]) ? q[k]![0] : (q[k] as string | undefined)) ?? "";
  const org = (get("org") || DEFAULT_ORG).toLowerCase();
  return (
    <MeshyProvider org={org}>
      <Approve org={org} agent={get("agent")} addr={get("addr")} ssh={get("ssh")} />
    </MeshyProvider>
  );
}

"use client";
import { Agents } from "@/components/Agents";
import { Devices } from "@/components/Devices";

export default function DirectoryPage() {
  return (
    <div className="mx-auto max-w-5xl space-y-6 py-2">
      <div>
        <h1 className="text-2xl font-semibold">Directory</h1>
        <p className="mt-1 text-sm text-muted">Every device, service and agent is an ENS name — clean names instead of IPs and ports, with their facts as records.</p>
      </div>
      <Devices />
      <Agents />
    </div>
  );
}

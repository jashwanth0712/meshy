import type { Metadata } from "next";
import Link from "next/link";
import { Logo } from "@/components/Shell";
import { DMG_URL, SetupSteps } from "@/components/SetupMac";
import { CopyLine } from "./CopyLine";

export const metadata: Metadata = { title: "Download · Meshy" };

const REL = "https://github.com/jashwanth0712/meshy/releases/latest/download";

const installs = [
  { who: "Agent", what: "meshy + meshy-mesh: join an org, reach devices by ENS name", cmd: "curl -fsSL https://get.meshy.live | sh" },
  { who: "Device", what: "meshyd + meshy-mesh: share folders and ports, sandboxed", cmd: 'curl -fsSL https://get.meshy.live | MESHY_TOOLS="meshyd meshy-mesh" sh' },
  { who: "Admin", what: "meshy-admin: networks, devices, grants from the terminal", cmd: 'curl -fsSL https://get.meshy.live | MESHY_TOOLS="meshy-admin" sh' },
];

const tools = [
  { name: "meshy", desc: "Agent CLI" },
  { name: "meshyd", desc: "Device daemon" },
  { name: "meshy-mesh", desc: "WireGuard peer (userspace)" },
  { name: "meshy-admin", desc: "Org admin CLI" },
];
const plats = [
  { os: "darwin", arch: "arm64", label: "macOS · Apple silicon" },
  { os: "darwin", arch: "amd64", label: "macOS · Intel" },
  { os: "linux", arch: "amd64", label: "Linux · x86-64" },
  { os: "linux", arch: "arm64", label: "Linux · ARM64" },
];

export default function Download() {
  return (
    <main className="mx-auto max-w-3xl px-6 py-14">
      <Link href="/" aria-label="Meshy home">
        <Logo className="h-6" />
      </Link>
      <h1 className="mt-10 text-[34px] font-semibold leading-tight tracking-[-0.03em]">Download Meshy</h1>
      <p className="mt-3 text-[15px] leading-relaxed text-muted">
        Every binary comes from the public GitHub release, and the installer checks each archive against the release&apos;s SHA-256 checksums before installing.
      </p>

      <section className="mt-10 rounded-2xl border border-line bg-panel p-5">
        <div className="flex items-center justify-between gap-4">
          <div>
            <div className="text-[15px] font-semibold">Meshy for Mac</div>
            <div className="text-[13px] text-muted">Menu-bar app with the <span className="mono">meshy</span> CLI built in. macOS 13+, universal.</div>
          </div>
          <div className="flex gap-2">
            <a className="rounded-full bg-accent px-4 py-2 text-[13px] font-semibold text-on-accent hover:opacity-90" href={DMG_URL}>
              Download .dmg
            </a>
            <a className="rounded-full border border-line-strong px-4 py-2 text-[13px] font-semibold hover:border-accent" href={`${REL}/Meshy-mac.zip`}>
              .zip
            </a>
          </div>
        </div>
        <p className="mt-3 text-[12px] text-muted">
          One download: Meshy.app puts <span className="mono">meshy</span>, <span className="mono">meshyd</span> and <span className="mono">meshy-mesh</span> in ~/.local/bin and adds it to your PATH when it opens. Not notarised yet: on first launch, right-click the app and choose Open.
        </p>
        <div className="mt-5 border-t border-line pt-5">
          <SetupSteps />
        </div>
      </section>

      <h2 className="mt-12 text-[13px] font-semibold uppercase tracking-wide text-muted">Without the app (Linux, servers, agents)</h2>
      <section className="mt-3 space-y-3">
        {installs.map((i) => (
          <div key={i.who} className="rounded-2xl border border-line bg-panel p-4">
            <div className="flex items-baseline justify-between gap-4">
              <div className="text-[15px] font-semibold">{i.who}</div>
              <div className="text-[13px] text-muted">{i.what}</div>
            </div>
            <CopyLine text={i.cmd} />
          </div>
        ))}
      </section>

      <section className="mt-10">
        <h2 className="text-[13px] font-semibold uppercase tracking-wide text-muted">All binaries</h2>
        <div className="mt-3 overflow-x-auto rounded-2xl border border-line">
          <table className="w-full text-[13px]">
            <thead className="bg-panel-2 text-left text-muted">
              <tr>
                <th className="px-4 py-2.5 font-medium">Tool</th>
                {plats.map((p) => (
                  <th key={p.label} className="px-3 py-2.5 font-medium">{p.label}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {tools.map((t) => (
                <tr key={t.name} className="border-t border-line">
                  <td className="px-4 py-2.5">
                    <div className="mono font-semibold">{t.name}</div>
                    <div className="text-muted">{t.desc}</div>
                  </td>
                  {plats.map((p) => (
                    <td key={p.label} className="px-3 py-2.5">
                      <a className="mono text-accent hover:underline" href={`${REL}/${t.name}_${p.os}_${p.arch}.tar.gz`}>
                        tar.gz
                      </a>
                    </td>
                  ))}
                </tr>
              ))}
              <tr className="border-t border-line">
                <td className="px-4 py-2.5">
                  <div className="mono font-semibold">meshy-sync</div>
                  <div className="text-muted">Provider (NetBird reconciler)</div>
                </td>
                <td className="px-3 py-2.5 text-muted">–</td>
                <td className="px-3 py-2.5 text-muted">–</td>
                <td className="px-3 py-2.5"><a className="mono text-accent hover:underline" href={`${REL}/meshy-sync_linux_amd64.tar.gz`}>tar.gz</a></td>
                <td className="px-3 py-2.5"><a className="mono text-accent hover:underline" href={`${REL}/meshy-sync_linux_arm64.tar.gz`}>tar.gz</a></td>
              </tr>
            </tbody>
          </table>
        </div>
        <div className="mt-3 flex flex-wrap gap-4 text-[13px]">
          <a className="text-accent hover:underline" href={`${REL}/checksums.txt`}>checksums.txt</a>
          <a className="text-accent hover:underline" href="/install.sh">install.sh</a>
          <a className="text-accent hover:underline" href="https://github.com/jashwanth0712/meshy/releases">All releases</a>
          <a className="text-accent hover:underline" href="https://github.com/jashwanth0712/meshy/tree/main/agent/claude-plugin">Claude Code plugin</a>
          <a className="text-accent hover:underline" href="https://github.com/jashwanth0712/meshy/tree/main/deploy/provider">Run a provider</a>
        </div>
      </section>
    </main>
  );
}

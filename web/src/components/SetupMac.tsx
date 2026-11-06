"use client";
// "Set up your Mac": the same steps on /download and inside the dashboard once you're signed in.
// Meshy for Mac carries the CLI (meshy, meshyd, meshy-mesh) and puts it on your PATH, so the DMG is
// the only download.
import { useEffect, useState } from "react";

export const DMG_URL = "https://github.com/jashwanth0712/meshy/releases/latest/download/Meshy.dmg";

export function SetupSteps({ org }: { org?: string }) {
  const steps: { t: string; d: React.ReactNode }[] = [
    {
      t: "Download and open Meshy.dmg",
      d: (
        <>
          Drag Meshy to Applications and open it (the first time: right-click › Open). It lives in the menu bar. The <span className="mono">meshy</span> command comes with it and is added
          to your PATH; open a new Terminal window to use it.
        </>
      ),
    },
    { t: "Sign In with Ethereum", d: "Click the Meshy icon in the menu bar. Your wallet signs in the browser; your orgs show up by themselves." },
    {
      t: "Link this Mac",
      d: (
        <>
          This Mac › Link This Mac{org ? <> to <span className="mono">{org}</span></> : null}. One transaction tells devices that this Mac is you.
        </>
      ),
    },
    { t: "Share folders, or open what's shared with you", d: "This Mac › Share this Mac… offers folders to others. Shared With Me › Open in Finder mounts folders you were granted as a network volume." },
  ];
  return (
    <ol className="space-y-3">
      {steps.map((s, i) => (
        <li key={s.t} className="flex gap-3">
          <span className="mono flex h-6 w-6 shrink-0 items-center justify-center rounded-full border border-line-strong text-[12px] font-semibold">{i + 1}</span>
          <div>
            <div className="text-[14px] font-semibold">{s.t}</div>
            <div className="text-[13px] leading-relaxed text-muted">{s.d}</div>
          </div>
        </li>
      ))}
    </ol>
  );
}

/** Dashboard card for signed-in people; dismissed per browser. */
export function SetupMacCard({ org }: { org: string }) {
  const [hidden, setHidden] = useState(true);
  useEffect(() => {
    let h = false;
    try {
      h = localStorage.getItem("meshy.setup-mac.hidden") === "1";
    } catch {}
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setHidden(h);
  }, []);
  if (hidden) return null;
  return (
    <div className="mb-8 rounded-2xl border border-line bg-panel p-5">
      <div className="mb-4 flex items-start justify-between gap-4">
        <div>
          <div className="text-[15px] font-semibold">Set up your Mac</div>
          <div className="text-[13px] text-muted">Share folders and open the ones shared with you in Finder. No terminal needed.</div>
        </div>
        <div className="flex shrink-0 items-center gap-3">
          <a className="rounded-full bg-accent px-4 py-2 text-[13px] font-semibold text-on-accent hover:opacity-90" href={DMG_URL}>
            Download for Mac
          </a>
          <button
            className="text-[13px] text-muted hover:text-fg"
            onClick={() => {
              try {
                localStorage.setItem("meshy.setup-mac.hidden", "1");
              } catch {}
              setHidden(true);
            }}
          >
            Hide
          </button>
        </div>
      </div>
      <SetupSteps org={org} />
    </div>
  );
}

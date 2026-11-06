#!/usr/bin/env python3
"""Build the static Meshy UI mockups.

Each page body lives in _src/<page>.html and starts with a one-line JSON header:
    <!-- {"title": "Access", "active": "access", "shell": "app"} -->
This script wraps it in the shared shell (top nav, ⌘K palette, status bar), inlines the
theme, component CSS, JS and the real logo from brand/svg, and writes <page>.html next to
this file. The outputs are self-contained: open them straight from disk.

    python3 docs/ui-revamp/mockups/build.py
"""
import itertools
import json
import re
import urllib.parse
from pathlib import Path

HERE = Path(__file__).resolve().parent
SRC = HERE / "_src"
BRAND = HERE.parents[2] / "brand"

_ids = itertools.count(1)


def _uniq_masks(svg: str) -> str:
    n = next(_ids)
    return re.sub(r'\bm(\d+)\b', lambda m: f"mk{n}", svg)


def lockup(cls: str = "h-7 w-auto") -> str:
    svg = (BRAND / "svg/meshy-lockup.svg").read_text().strip()
    svg = svg.replace('fill="#121417"', 'fill="currentColor"').replace("<title>Meshy</title>", "")
    svg = svg.replace("<svg ", f'<svg class="{cls}" role="img" aria-label="Meshy" ', 1)
    return _uniq_masks(svg)


def icon(cls: str = "h-6 w-6") -> str:
    """App icon tile. Bold favicon cut below 48 px, regular cut above (brand rule)."""
    small = any(s in cls for s in ("h-4", "h-5", "h-6", "h-7", "h-8", "h-9", "h-10"))
    svg = (BRAND / ("favicon/favicon.svg" if small else "svg/meshy-icon.svg")).read_text().strip()
    svg = svg.replace("<title>Meshy</title>", "").replace("<svg ", f'<svg class="{cls}" aria-hidden="true" ', 1)
    return _uniq_masks(svg)


def mark(cls: str = "h-10 w-10") -> str:
    svg = (BRAND / "svg/meshy-mark.svg").read_text().strip()
    svg = svg.replace("#2B3FE0", "currentColor").replace("<title>Meshy</title>", "")
    svg = svg.replace("<svg ", f'<svg class="{cls}" aria-hidden="true" ', 1)
    return _uniq_masks(svg)


def favicon() -> str:
    return "data:image/svg+xml," + urllib.parse.quote((BRAND / "favicon/favicon.svg").read_text().strip())


NAV = [("access", "Access"), ("directory", "Directory"), ("activity", "Activity"), ("billing", "Billing"), ("provider", "Provider"), ("settings", "Settings")]
HREF = {"access": "access.html", "directory": "directory.html", "activity": "activity.html", "billing": "billing.html", "provider": "provider.html", "settings": "settings.html"}

HEAD = """<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{title} · Meshy UI mockup</title>
<link rel="icon" href="{favicon}">
<link rel="preconnect" href="https://fonts.googleapis.com"><link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link href="https://fonts.googleapis.com/css2?family=Geist:wght@400;450;500;600;700&family=Geist+Mono:wght@400;500;600&display=swap" rel="stylesheet">
<script src="https://cdn.jsdelivr.net/npm/@tailwindcss/browser@4"></script>
<script src="https://unpkg.com/lucide@0.469.0/dist/umd/lucide.min.js"></script>
<style type="text/tailwindcss">
{theme}
</style>
<style>
{components}
</style>
</head>
"""


def topnav(active: str) -> str:
    cur = ' aria-current="page"'
    links = "\n".join(f'<a class="navlink" href="{HREF[k]}"{cur if k == active else ""}>{label}</a>' for k, label in NAV)
    return f"""
<header class="sticky top-0 z-40 border-b border-border/70 bg-background/80 backdrop-blur-md">
  <div class="mx-auto flex h-16 max-w-7xl items-center gap-4 px-6 lg:px-8">
    <a href="index.html" class="shrink-0 text-foreground" title="All mockups">{lockup("h-7 w-auto")}</a>
    <button class="ml-1 hidden items-center gap-2 rounded-full border-2 border-border bg-card py-1 pl-1.5 pr-3 text-sm font-medium hover:border-border-strong lg:inline-flex" title="Switch org">
      <span class="grid h-6 w-6 place-items-center rounded-full bg-accent text-[11px] font-bold text-accent-foreground">M</span>
      <span class="mono text-[13px]">meshydemo.eth</span>
      <i data-lucide="chevrons-up-down" class="h-3.5 w-3.5 text-muted-foreground"></i>
    </button>
    <nav class="ml-2 hidden items-center gap-0.5 md:flex" aria-label="Main">{links}</nav>
    <div class="ml-auto flex items-center gap-2">
      <button onclick="openPalette()" class="hidden h-9 w-64 items-center gap-2 whitespace-nowrap rounded-full border-2 border-border bg-card pl-3 pr-1.5 text-sm text-muted-foreground hover:border-border-strong 2xl:inline-flex">
        <i data-lucide="search" class="h-4 w-4"></i><span>Search names, actions…</span><span class="ml-auto flex gap-0.5"><kbd>⌘</kbd><kbd>K</kbd></span>
      </button>
      <button onclick="openPalette()" class="btn btn-outline btn-sm gap-1.5 2xl:hidden" aria-label="Search"><i data-lucide="search"></i><kbd>⌘K</kbd></button>
      <button onclick="toggleTheme()" class="btn btn-ghost btn-icon" aria-label="Toggle theme"><i data-lucide="sun-moon"></i></button>
      <button class="inline-flex h-9 items-center gap-2 whitespace-nowrap rounded-full border-2 border-border bg-card pl-1.5 pr-3 text-sm font-semibold hover:border-border-strong">
        <span class="h-6 w-6 rounded-full" style="background:conic-gradient(from 200deg,#2b3fe0,#8c9bff,#ddf3ea,#fde8d7,#2b3fe0)"></span>
        <span class="mono text-[13px]">0x45e8…E0EA</span>
        <span class="badge b-info h-5 px-2 text-[11px]">Owner</span>
      </button>
    </div>
  </div>
</header>"""


PALETTE = """
<div id="palette" hidden class="fixed inset-0 z-[60]">
  <div class="scrim"></div>
  <div role="dialog" aria-label="Command palette" class="pop fade-in relative z-[51] mx-auto mt-[12vh] w-[min(640px,92vw)] overflow-hidden">
    <div class="flex items-center gap-3 border-b border-border px-4">
      <i data-lucide="search" class="h-4 w-4 text-muted-foreground"></i>
      <input class="h-14 flex-1 bg-transparent text-[15px] outline-none placeholder:text-muted-foreground" placeholder="Type an ENS name, an address, or an action…">
      <kbd>esc</kbd>
    </div>
    <div class="max-h-[420px] overflow-y-auto p-2 text-sm [&_[aria-selected=true]]:bg-accent [&_[aria-selected=true]]:text-accent-foreground">
      <div data-resolve hidden class="mb-1 rounded-xl border border-border bg-muted/50 p-3">
        <div class="eyebrow mb-1.5">Resolved on ENSv2 · Sepolia</div>
        <a href="agent.html" class="flex items-center gap-3">
          <span class="grid h-8 w-8 place-items-center rounded-lg bg-foreground text-background"><i data-lucide="sparkle" class="h-4 w-4"></i></span>
          <span class="min-w-0"><span class="ens block">claude-1<span class="rest">.agents.meshydemo.eth</span></span><span class="text-xs text-muted-foreground">agent · 0x35e7…A62f · human-backed · 1 live grant</span></span>
          <span class="ml-auto text-xs text-muted-foreground">Open ↵</span>
        </a>
      </div>
      <div data-group><div class="eyebrow px-2 pb-1 pt-2">Actions</div>
        <a data-item="give access grant" href="give-access.html" class="flex h-10 items-center gap-3 rounded-lg px-2"><i data-lucide="key-round" class="h-4 w-4"></i>Give access…<span class="ml-auto"><kbd>G</kbd></span></a>
        <a data-item="revoke claude-1 db1.prod" href="revoke.html" class="flex h-10 items-center gap-3 rounded-lg px-2 text-destructive"><i data-lucide="zap" class="h-4 w-4"></i>Revoke <span class="ens">claude-1 → db1.prod</span><span class="ml-auto text-xs text-muted-foreground">live · 58m left</span></a>
        <a data-item="invite agent" href="directory.html" class="flex h-10 items-center gap-3 rounded-lg px-2"><i data-lucide="user-plus" class="h-4 w-4"></i>Invite an agent…</a>
        <a data-item="import org key decrypt" href="settings.html#org-key" class="flex h-10 items-center gap-3 rounded-lg px-2"><i data-lucide="key-square" class="h-4 w-4"></i>Import org key</a>
        <a data-item="copy meshy ssh db1" class="flex h-10 items-center gap-3 rounded-lg px-2"><i data-lucide="terminal" class="h-4 w-4"></i>Copy <code class="text-[13px]">meshy ssh db1.prod.meshydemo.eth</code></a>
      </div>
      <div data-group><div class="eyebrow px-2 pb-1 pt-3">Names</div>
        <a data-item="claude-1.agents.meshydemo.eth agent" href="agent.html" class="flex h-10 items-center gap-3 rounded-lg px-2"><i data-lucide="sparkle" class="h-4 w-4"></i><span class="ens">claude-1<span class="rest">.agents.meshydemo.eth</span></span><span class="badge b-live ml-auto">1 live</span></a>
        <a data-item="db1.prod.meshydemo.eth device" href="directory.html" class="flex h-10 items-center gap-3 rounded-lg px-2"><i data-lucide="server" class="h-4 w-4"></i><span class="ens">db1<span class="rest">.prod.meshydemo.eth</span></span><span class="ml-auto text-xs text-muted-foreground">device</span></a>
        <a data-item="pi.home.meshydemo.eth raspberry device" href="directory.html" class="flex h-10 items-center gap-3 rounded-lg px-2"><i data-lucide="cpu" class="h-4 w-4"></i><span class="ens">pi<span class="rest">.home.meshydemo.eth</span></span><span class="ml-auto text-xs text-muted-foreground">Raspberry Pi 5</span></a>
        <a data-item="camera.pi.home.meshydemo.eth service" href="directory.html" class="flex h-10 items-center gap-3 rounded-lg px-2"><i data-lucide="cctv" class="h-4 w-4"></i><span class="ens">camera<span class="rest">.pi.home.meshydemo.eth</span></span><span class="ml-auto text-xs text-muted-foreground">http · 8001</span></a>
        <a data-item="rig1.home.meshydemo.eth device" href="directory.html" class="flex h-10 items-center gap-3 rounded-lg px-2"><i data-lucide="server" class="h-4 w-4"></i><span class="ens">rig1<span class="rest">.home.meshydemo.eth</span></span><span class="ml-auto text-xs text-muted-foreground">server</span></a>
      </div>
      <div data-group><div class="eyebrow px-2 pb-1 pt-3">Go to</div>
        <a data-item="access home grants" href="access.html" class="flex h-10 items-center gap-3 rounded-lg px-2"><i data-lucide="key-round" class="h-4 w-4"></i>Access<span class="ml-auto"><kbd>g</kbd> <kbd>a</kbd></span></a>
        <a data-item="directory devices" href="directory.html" class="flex h-10 items-center gap-3 rounded-lg px-2"><i data-lucide="network" class="h-4 w-4"></i>Directory<span class="ml-auto"><kbd>g</kbd> <kbd>d</kbd></span></a>
        <a data-item="activity audit arkiv" href="activity.html" class="flex h-10 items-center gap-3 rounded-lg px-2"><i data-lucide="activity" class="h-4 w-4"></i>Activity<span class="ml-auto"><kbd>g</kbd> <kbd>l</kbd></span></a>
        <a data-item="settings admins" href="settings.html" class="flex h-10 items-center gap-3 rounded-lg px-2"><i data-lucide="settings" class="h-4 w-4"></i>Settings<span class="ml-auto"><kbd>g</kbd> <kbd>s</kbd></span></a>
      </div>
    </div>
    <div class="flex items-center gap-4 border-t border-border bg-muted/40 px-4 py-2 text-xs text-muted-foreground">
      <span class="flex items-center gap-1"><kbd>↑</kbd><kbd>↓</kbd> move</span><span class="flex items-center gap-1"><kbd>↵</kbd> open</span>
      <span class="ml-auto">Try <code>0x35e7</code> or <code>db1</code>: names resolve live</span>
    </div>
  </div>
</div>"""

STATUSBAR = """
<footer class="fixed inset-x-0 bottom-0 z-30 border-t border-border/70 bg-background/85 backdrop-blur-md">
  <div class="mx-auto flex h-9 max-w-7xl items-center gap-5 px-6 text-xs text-muted-foreground lg:px-8">
    <span class="flex items-center gap-1.5"><span class="dot dot-live"></span>Sepolia <span class="mono tnum text-foreground">#12,074,308</span></span>
    <span class="hidden items-center gap-1.5 sm:flex"><i data-lucide="radio" class="h-3.5 w-3.5"></i>Arkiv</span>
    <a href="activity.html" class="hidden min-w-0 items-center gap-1.5 truncate hover:text-foreground md:flex"><span class="mono text-foreground">claude-1</span> started a session on <span class="mono text-foreground">db1.prod</span><span>· 1m ago</span></a>
    <span class="ml-auto flex items-center gap-1.5"><i data-lucide="lock" class="h-3.5 w-3.5"></i>Org key not loaded · <a href="settings.html#org-key" class="font-medium text-foreground underline-offset-2 hover:underline">Import</a></span>
  </div>
</footer>"""


def render(page: Path) -> str:
    raw = page.read_text()
    header, body = raw.split("\n", 1)
    meta = json.loads(re.match(r"<!--\s*(\{.*\})\s*-->", header).group(1))
    theme = (SRC / "_theme.css").read_text()
    components = (SRC / "_components.css").read_text()
    js = (SRC / "_shell.js").read_text()
    out = HEAD.replace("{title}", meta["title"]).replace("{favicon}", favicon()).replace("{theme}", theme).replace("{components}", components)
    shell = meta.get("shell", "app")
    out += '<body class="min-h-screen bg-background text-foreground">\n'
    if shell == "app":
        out += topnav(meta.get("active", ""))
        out += '<div class="relative pb-20"><div class="aurora"></div><div class="relative">\n' + body + "\n</div></div>"
        bar = STATUSBAR
        if meta.get("orgkey"):
            bar = bar.replace('<i data-lucide="lock" class="h-3.5 w-3.5"></i>Org key not loaded · <a href="settings.html#org-key" class="font-medium text-foreground underline-offset-2 hover:underline">Import</a>',
                              '<i data-lucide="lock-open" class="h-3.5 w-3.5 text-success"></i>Org key loaded in this browser')
        out += PALETTE + bar
    else:
        out += '<div class="relative"><div class="aurora"></div><div class="relative">\n' + body + "\n</div></div>"
        out += PALETTE
    out += f"\n<script>\n{js}\n</script>\n</body>\n</html>\n"
    out = re.sub(r"\{\{(lockup|icon|mark)(?::([^}]*))?\}\}", lambda m: {"lockup": lockup, "icon": icon, "mark": mark}[m.group(1)](*( [m.group(2)] if m.group(2) else [])), out)
    return out


def main():
    pages = sorted(p for p in SRC.glob("*.html") if not p.name.startswith("_"))
    for p in pages:
        (HERE / p.name).write_text(render(p))
        print("built", p.name)


if __name__ == "__main__":
    main()

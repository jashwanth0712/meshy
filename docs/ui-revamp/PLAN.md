# Meshy web UI revamp: plan

Status: planning and mockups only. Nothing in `web/src` has changed.
Mockups: [`mockups/index.html`](mockups/index.html). Open it from disk; it needs internet only for Tailwind, Geist and Lucide from CDNs.
Written 6 Nov 2026, from the live dashboard at `/org/meshydemo.eth` and from ethglobal.com as it was that day.

**Goal.** Move the dashboard from its current dark, terminal-style look to a calm, minimal style with Meshy's own character. Build it on shadcn/ui, design it around the new logo (`brand/`), and keep the access-first layout the user chose: the home page is the grants feed, not a network map.

**Revision 2 (6 Nov).** The user asked for a Meshy vibe rather than a copy of ethglobal.com. What changed:
- **Colour.** The logo's cobalt and white, plus the hero's gradient colours (`#1e3fd8 · #f6f8ff · #a5d8ff · #38bdf8`). Four directions were sampled in [`mockups/palette.html`](mockups/palette.html), and the user picked **B, Electric**: a white page, electric cobalt, and solid saturated chips (lime, sun, pink, lilac, cyan). See §2.
- **Hero.** A night-sky `GradientASCII` horizon (`shape="horizon"`) replaces the pastel wash. It runs full size on the landing page, and as a short header band behind the nav and title on every app page. See §5.
- **Landing.** No longer a showcase grid: it is a hero, then "three moves", then a numbered list of screens.

ethglobal.com remains the reference for restraint, spacing, type scale and layout (§1). Its pastel wash, uppercase ink-bordered tags and 2 px outlines are dropped.

**Deadline note.** The ETHGlobal Mumbai deadline is Sat 7 Nov 2026, 09:00 IST. The step plan in §9 is ordered so the work can stop after any phase and still ship. See decision D1 in §11.

---

## Contents
1. [What ethglobal.com does](#1-what-ethglobalcom-does)
2. [Brand → palette](#2-brand--palette)
3. [Audit of the current UI](#3-audit-of-the-current-ui)
4. [Design tokens (Tailwind v4 + shadcn CSS variables)](#4-design-tokens)
5. [Layout and navigation](#5-layout-and-navigation)
6. [shadcn components, mapped to every screen element](#6-shadcn-component-map)
7. [Page-by-page redesign](#7-page-by-page-redesign)
8. [UX innovations](#8-ux-innovations)
9. [Implementation plan (file by file)](#9-implementation-plan)
10. [Accessibility](#10-accessibility)
11. [Risks and decisions needed](#11-risks-and-decisions-needed)
12. [Mockup files](#12-mockup-files)

---

## 1. What ethglobal.com does

The values below come from computed styles on `/`, `/events`, `/showcase` and `/events/mumbai/prizes`. The site itself uses shadcn-style tokens: its search input carries `ring-offset-background` and `placeholder:text-muted-foreground`.

| Aspect | What ethglobal.com does | Concrete tokens | What Meshy takes |
|---|---|---|---|
| **Typeface** | One grotesk everywhere. Event banners use a second face. | **Roobert** (commercial, Displaay) at 400 / 500 / 600; banners use Plus Jakarta Sans 700 | **Geist** (free, and already the brand's UI face) for text; **Geist Mono** for ENS names, addresses and numbers |
| **Type scale** | Big, tight display headings; plain 16 px body; small uppercase eyebrows | Display 60/60 · 600 (`text-6xl`); card title 24 · 600; body 16/24 · 400; small 14; eyebrow 14 uppercase `#737374`; nav 17.6 · 500; table header 12 · 700 uppercase, tracking 0.3 px | Display 44/46 · 600 · −0.03em; h2 20 · 600; body 14–16; eyebrow 12 uppercase; table header 11 · 700 uppercase |
| **Colour** | Near-black text on white; grey sections; one pastel tint per context; yellow only for the main event CTA | Text `#0A0A0A`; muted `#737374`, `#6B7280`, `#4B5563`; section bg `#F4F4F5`; borders `#E5E7EB`; tag lavender `#C6C9F6`; table header `#F8F5FF`; amounts `#581C87`; CTA yellow `#FFC750`; "New" badge `#DCFCE7` / `#166534` | Navy ink on white; **electric cobalt** for action; sky and cyan for light (glow, focus, the horizon); solid Electric chips |
| **Background** | White pages. A soft pastel blur (peach, lavender, mint) behind each page header (`projects-blur.png`) | — | **Not taken.** Meshy uses its own night-sky ASCII horizon band instead (§5) |
| **Radius** | 10 px on cards and panels; full pills on buttons and tags | `rounded` = 10 px (30 uses); `9999px` (37 uses); 12 px and 32 px are rare | `--radius: 0.625rem`; pill buttons, tags and badges; cards 12 px |
| **Borders** | Their signature: **2 px** outlines on buttons, cards and tags. Black 2 px card borders on the showcase | `2px solid #0A0A0A` (cards, tags); `2px solid #E5E7EB` (secondary button); `2px solid #F4F4F5` (dividers) | **Not taken.** 1 px blue-tinted hairlines everywhere; pills keep the round shape |
| **Shadow** | Almost none | Panel `0 0 30px 0.5px rgba(40,50,70,.08)`; one drop shadow on a hero card | `--shadow-soft` on cards; `--shadow-pop` only on overlays |
| **Iconography** | Small line icons inside tags (pin, tag); big flat illustrations in heroes | — | Lucide at 16 px, stroke 1.75; the **spark from the logo** stands for an agent; no illustrations |
| **Navigation** | Top bar: logo, 7 text links (one with a green "New" pill), Login as a 2 px outline pill on the right; hover `opacity-60`; no sidebar | Container `max-w-7xl`, `px-6 lg:px-8`, 64 px tall | Same top bar; org switcher pill; ⌘K search pill; wallet pill |
| **Cards and lists** | Showcase: 3-column cards with a 16:9 image, title, two-line description and an uppercase tag. Events: wide banners with a **date tile** ("NOVEMBER 5→7 Thu—Sat") | Tag: 12 px / 600 uppercase, `px-3 py-1`, 2 px ink border, pastel fill | The date tile becomes the expiry **ticket**. Chips are Meshy's own: solid saturated fills, no ink border |
| **Dense data** | Prizes: a white **master-detail** panel (list with dividers on the left, detail on the right) and tables with a lavender header row | `grid-cols-12 rounded-lg bg-white shadow`; `thead bg-purple-50 text-xs font-semibold uppercase tracking-wide text-gray-600` | Directory as master-detail; every table gets the tinted uppercase header |
| **Tabs** | Underline tabs ("All · Hackathons · Summits · Meetups") and counts in black circles ("Featured ②") | — | Same underline tabs, in cobalt, with cobalt count pills |
| **Motion** | Quiet: `transition-colors 200ms`, opacity on hover, carousels | — | 150–200 ms colour and opacity changes; one 350 ms fade-in for overlays; live dots pulse; nothing moves under `prefers-reduced-motion` |
| **Light / dark** | Light only (`color-scheme: normal`) | — | Light by default, plus a dark theme for people who want it (decision D2) |

The ethglobal look in one sentence: generous white space, very dark text, big tight headlines, pill-shaped controls with 2 px outlines, one pastel wash at the top, and colour used sparingly. Meshy keeps the white space, the headlines, the pills and the sparing colour. The rest is its own.

---

## 2. Brand → palette

The logo (`brand/`, from commit `fe22bf8`) is an M drawn as a network. Rings are devices. The solid node is your own device. The agent is a four-point spark held apart by a gap: it reaches only what it is granted.

### Four directions were sampled ([`mockups/palette.html`](mockups/palette.html))
| | Direction | Verdict |
|---|---|---|
| A | **Cobalt horizon.** Ice page, white cards, navy ink, cobalt action; sky and cyan for light; soft "candy" tags with a dot | The calmest of the fun options, but less memorable |
| **B** | **Electric.** White, electric cobalt and cyan, with solid saturated chips (lime, sun, pink, lilac) | **Picked by the user.** Loud, fun and unmistakable. The risk is chips outshouting data on dense tables, so a table row carries at most one chip (its state) |
| C | **All blue.** Every tag a shade of blue or cyan | Serene, but live, expiring and refused look alike. State then rests on words alone |
| D | **Night mesh.** The hero's night sky as the whole theme | Kept as B's **dark theme**, with the same chips. Not the default |

### The picked system (B, Electric)
| Token | Hex | UI role |
|---|---|---|
| Night | `#050B24` | Top nav, ASCII hero band, dark-theme page |
| White | `#FFFFFF` | `--background`, `--card`, `--popover` |
| Mist | `#F3F5FC` / `#F1F4FF` | `--muted` / `--secondary`: fills and the table header row |
| Navy ink | `#0A0F2C` | `--foreground` (bluer than the brand's `#121417`, so text sits in the same family) |
| Electric cobalt | `#1E3FD8` | `--primary` (the hero's cobalt, 7.6 : 1 with white): one main action per view, links, focus ring, live grant bars, tab underline. The logo files keep `#2B3FE0` |
| Cobalt 300 | `#8C9BFF` | `--primary` in the dark theme. The brand forbids cobalt on dark (2.54 : 1) |
| Sky | `#A5D8FF` | Glow, focus ring on dark, the hero gradient |
| Cyan | `#38BDF8` | The horizon edge line, highlights, the sky tag's dot |
| Hairline | `#E6E9F5` / `#CDD5EC` | `--border` / `--border-strong` |
| Accent | `#EAF0FF` | Hover and selected rows |

**Chips: Electric.** Each chip is solid and saturated, with a bold sans label in dark text (white on cobalt), and no outline or dot. Colour means one thing per family:

| Chip | Fill / text | Contrast | Meaning |
|---|---|---|---|
| `lime` | `#BEF264` / `#1A2E05` | 11.2 : 1 | live, healthy, session open |
| `sun` | `#FDE047` / `#3A2E00` | 10.1 : 1 | expiring soon, demo-only, sealed |
| `pink` | `#F9A8D4` / `#4A0D2B` | 8.4 : 1 | refused, revoked, blocked |
| `lilac` | `#C4B5FD` / `#2E1065` | 8.3 : 1 | identity: World ID, agents, Arkiv |
| `cyan` | `#38BDF8` / `#05213A` | 7.6 : 1 | network label (e.g. `home`), kind |
| `cobalt` (default) | `#1E3FD8` / `#FFFFFF` (dark: `#8C9BFF` / `#050B24`) | 7.6 : 1 | ENS names, `prod`, defaults |

State badges (`Session open`, `Revoked`, `Owner`) use the same solid fills, with an icon.

**Rules that keep it from getting loud:**
- At most one chip per table row: its state.
- Facts in rows (scope, paths, addresses) stay plain text.
- Chips never sit inside buttons.
- Only one cobalt-filled button per view.

**On dark surfaces** (the nav and the hero band), an `.on-dark` scope swaps tokens: white text, translucent white cards and borders, and white primary buttons with cobalt text. Chips stay solid (they glow on night). The same components work on the band without extra classes.

- **Agent glyph.** Agents always show the logo's spark, in navy or white, never coloured. The old UI painted agent names yellow, which read as a warning.
- **Logo usage.**
  - Header: the lockup, with the wordmark in `currentColor` so it is white on the night nav, and the cobalt tile kept.
  - Favicon: the bold cut.
  - The mark appears nowhere else. The brand rule is "one field, one subject".

Contrast checks (WCAG AA; computed):

| Pair | Ratio |
|---|---|
| Navy ink on white | 18.8 : 1 |
| `--muted-foreground` `#5B6787` on white | 5.6 : 1 |
| Electric cobalt on white, and white on it | 7.6 : 1 |
| White on night `#050B24` | 19.5 : 1 |
| Muted on the band (`#D6E0FF` at 74 %) on night | ≥ 10 : 1 |
| Dark theme: `#93A0C8` / cobalt 300 on card `#0B1435` | 7.0 / 7.1 : 1 |
| Chip text on its fill (lowest: cyan and cobalt) | 7.6 : 1 (AAA) |

---

## 3. Audit of the current UI

Audited live at `http://localhost:3000/org/meshydemo.eth` (Sepolia block ~12,074,300, no wallet) and in the source.

### Cross-cutting
1. **Loading is a blank screen.** On a hard load, Access shows one grey line, "Reading the org from ENSv2 on Sepolia…", for 20 s or more. Directory shows only its title. There are no skeletons and no progress indication.
2. **Stats lie while loading.** Activity shows *Live sessions 0, Blocked 0, Revocations 0* until Arkiv answers, then jumps to 1 / 7 / 3.
3. **No logo and no brand.** The sidebar uses a generic lucide `Network` icon and the word "Meshy". The new mark and its cobalt appear nowhere.
4. **Three accents compete.** Mint (`accent`) means "ok". Blue (`accent-2`) means links. Yellow (`warn`) marks agent names *and* warnings, so `claude-1` reads as a warning everywhere.
5. **Page headers are inconsistent.** Access and Directory have an H1 and a lede. Activity, Billing, Provider and Settings start straight with cards.
6. **The sidebar costs 240 px for 6 links.** Its footer ("Source of truth: ENSv2 · Sepolia / Audit log / org key not loaded") is low-value and is covered by the Next dev badge. There is no mobile layout: the sidebar stays fixed at every width.
7. **Two broken links:**
   - `components/Agents.tsx` "grant access" points to `/org/${org}/grants`, which returns 404.
   - `app/approve/Approve.tsx` "Grant it access to a resource →" points to the same 404.
8. **Roles are guessed.** `isOwner = !!account` in `Access.tsx`, `Devices.tsx` and `Approve.tsx`. Any connected wallet gets enabled buttons and only fails on-chain. Disabled buttons never say why.
9. **Toasts are bare `div`s.** They have no `aria-live`, no dismiss button, no transaction link, and they vanish on navigation.
10. **Native `<select>`s and mixed controls.** Agents and targets are picked from long monospace `<option>`s; durations are bespoke buttons.
11. **Dates are ambiguous.** Billing says "paid until 06/12/2026", which is `toLocaleDateString()`. It means 6 Dec here but reads as 12 Jun in the US.
12. **Absolute home paths leak.** The username shows in paths like `folders /Users/jashwanthpeddisetty/meshy-demo/pi-srv`. They are long, and they're the same on every row.
13. **Arkiv is polled twice.** Access polls every 6 s and Activity every 5 s, with separate copies of the data and no shared "latest event".
14. **No search and no keyboard paths.** You can't jump to `db1.prod.meshydemo.eth` or find what `0x35e7…` is.
15. **Type is small and mono-heavy.** Most text is 11–13 px; whole sentences are monospace.

### Access (home)
- The information architecture is good: live grants first, then a "Happening now" sentence feed. **Keep it.**
- The progress bar under a live grant always divides by 4 h (`const total = 4 * 3600`), so a fresh 1 h grant shows as 25 % full.
- **Revoke has no confirmation.** One click mounts `RevokeTimeline`, which sends `unregister` straight away; only the wallet prompt stands in the way.
- The revoke modal closes on a backdrop click (`onClick={onDone}`) *while the transaction is in flight*. The timeline is lost and nothing reopens it.
- Ended grants hide behind a small "2 ended" toggle, and their "Re-grant" is a ghost button.
- Feed entries such as "claude-1 was refused on pi.home" are dead ends: there is no way to act on a refusal.
- The org summary is one line of text at the bottom.

### Give access drawer
- It has no focus trap, no Escape handling and no `aria-modal`. Clicking outside closes it, even during a transaction.
- Devices and services are mixed in one `<select>` with optgroups. The folder default `${root}/reports` is arbitrary.
- The preview is dense, and the number of transactions appears only on the submit button.
- The read/write toggle is a 2-letter `ro`/`rw` switch.

### Directory
- Everything sits in one card. The three "Add" forms (network, service, device) open inline *above* the list and push it down.
- A device can't be opened. Its facts are crammed into one muted line: `folders … · daemon 0x6E36…5B7F · meshy ssh …`.
- Agents are a second card further down, with the invite form beside them. There is no agent page.

### Activity
- It has no title. Its filters are tiny text buttons (`all blocked sessions files`), and you can't filter by agent, device or time.
- "Verified writers" is a row of raw truncated addresses.
- Times are relative only ("4h ago"), with no grouping. A proof link is repeated on every row as "entity ↗".

### Billing
- There is no price preview: the user commits to 3 transactions (mint, approve, add seats) without seeing the total.
- The seat meter shows "4 / 5" but not *which* seats.
- The date is ambiguous (see cross-cutting item 11).

### Provider
- The "Register as a provider" form is always on screen for everyone, though few people need it.
- The slash form is a raw JSON `<textarea>` with no validation before signing.
- "World ID disabled" and "dev attest on" are shown neutrally, though both are demo-only risks.

### Settings
- Org records are an unlabeled stack of short addresses.
- **Delegated admins can't be listed.** You type an address and press Check. The one real admin (`0xf846…Fc98` on `home` and `agents`) is invisible.
- "Delegate (5 tx)" gives no breakdown, and "Remove" is always visible in red.

### Approve
- The structure is good. However, the button always says "Approve (3 transactions)", even when the agent already exists and only 1 transaction is needed.
- Its success link is the broken `/grants` URL.

### Landing `/`
- The landing page is a dark hero with the generic icon and no logo.

---

## 4. Design tokens

The full, working token set is in [`mockups/_src/_theme.css`](mockups/_src/_theme.css). It uses shadcn/ui variable names, so `npx shadcn add` components pick it up unchanged. The proposed `web/src/app/globals.css`:

```css
@import "tailwindcss";
@import "tw-animate-css";

@custom-variant dark (&:where(.dark, .dark *));

@theme inline {
  --color-background: var(--background);
  --color-foreground: var(--foreground);
  --color-card: var(--card);
  --color-card-foreground: var(--card-foreground);
  --color-popover: var(--popover);
  --color-popover-foreground: var(--popover-foreground);
  --color-primary: var(--primary);
  --color-primary-foreground: var(--primary-foreground);
  --color-secondary: var(--secondary);
  --color-secondary-foreground: var(--secondary-foreground);
  --color-muted: var(--muted);
  --color-muted-foreground: var(--muted-foreground);
  --color-accent: var(--accent);
  --color-accent-foreground: var(--accent-foreground);
  --color-destructive: var(--destructive);
  --color-destructive-foreground: var(--destructive-foreground);
  --color-border: var(--border);
  --color-border-strong: var(--border-strong);
  --color-input: var(--input);
  --color-ring: var(--ring);
  --color-success: var(--success);
  --color-success-soft: var(--success-soft);
  --color-warning: var(--warning);
  --color-warning-soft: var(--warning-soft);
  --color-danger-soft: var(--danger-soft);
  --color-info-soft: var(--info-soft);
  --color-cobalt-50: #eef0ff;
  --color-cobalt-100: #e0e4ff;
  --color-cobalt-200: #c4cbff;
  --color-cobalt-300: #8c9bff;
  --color-cobalt-400: #5e70f5;
  --color-cobalt-500: #2b3fe0;
  --color-cobalt-600: #2233c4;
  --color-cobalt-700: #1c2a9e;
  --color-cobalt-800: #18237d;
  --color-cobalt-900: #141c5e;
  --color-ink: #0b1533;
  --color-night: #050b24;
  --color-sky: #a5d8ff;
  --color-cyan: #38bdf8;
  --color-ice: #f6f8ff;
  --color-paper: #f6f8ff;

  /* Literal family names: see §9 Phase 3 for the Tailwind v4 font caveat */
  --font-sans: "Geist", "Geist Fallback", ui-sans-serif, system-ui, sans-serif;
  --font-mono: "Geist Mono", "Geist Mono Fallback", ui-monospace, monospace;

  --radius-sm: calc(var(--radius) * 0.6);
  --radius-md: calc(var(--radius) * 0.8);
  --radius-lg: var(--radius);
  --radius-xl: calc(var(--radius) * 1.2);
  --radius-2xl: calc(var(--radius) * 1.6);
}

:root {
  --radius: 0.625rem;
  --background: #ffffff; /* Electric: white page */
  --foreground: #0a0f2c; /* deep navy ink */
  --card: #ffffff;
  --card-foreground: #0a0f2c;
  --popover: #ffffff;
  --popover-foreground: #0a0f2c;
  --primary: #1e3fd8; /* electric cobalt (hero #1e3fd8): 7.6:1 with white */
  --primary-foreground: #ffffff;
  --secondary: #f1f4ff;
  --secondary-foreground: #0a0f2c;
  --muted: #f3f5fc;
  --muted-foreground: #5b6787; /* 5.6:1 on white */
  --accent: #eaf0ff;
  --accent-foreground: #1e3fd8;
  --destructive: #e11d48;
  --destructive-foreground: #ffffff;
  --border: #e6e9f5;
  --border-strong: #cdd5ec;
  --input: #d5dcef;
  --ring: #1e3fd8;
  --success: #15803d;
  --success-soft: #dcfce7;
  --warning: #a16207;
  --warning-soft: #fef9c3;
  --danger-soft: #ffe4e6;
  --info-soft: #e0f2fe;
  /* Electric tags: solid saturated chips, dark text (white on cobalt) */
  --tag-cobalt-bg: #1e3fd8; --tag-cobalt-fg: #ffffff;
  --tag-cyan-bg: #38bdf8;   --tag-cyan-fg: #05213a;
  --tag-lime-bg: #bef264;   --tag-lime-fg: #1a2e05;
  --tag-sun-bg: #fde047;    --tag-sun-fg: #3a2e00;
  --tag-pink-bg: #f9a8d4;   --tag-pink-fg: #4a0d2b;
  --tag-lilac-bg: #c4b5fd;  --tag-lilac-fg: #2e1065;
  --shadow-soft: 0 1px 2px rgb(11 21 51 / 0.04), 0 8px 24px -12px rgb(30 63 216 / 0.12);
  --shadow-pop: 0 18px 48px -12px rgb(11 21 51 / 0.28), 0 2px 6px rgb(11 21 51 / 0.06);
}

.dark {
  --background: #050b24; /* the hero's night sky */
  --foreground: #e8edff;
  --card: #0b1435;
  --card-foreground: #e8edff;
  --popover: #0e1840;
  --popover-foreground: #e8edff;
  --primary: #8c9bff; /* cobalt-300: brand rule, never cobalt-500 on dark */
  --primary-foreground: #050b24;
  --secondary: #111c48;
  --secondary-foreground: #e8edff;
  --muted: #101b45;
  --muted-foreground: #93a0c8;
  --accent: #14205a;
  --accent-foreground: #c4cbff;
  --destructive: #fb7185;
  --destructive-foreground: #050b24;
  --border: #1a2652;
  --border-strong: #26346a;
  --input: #26346a;
  --ring: #8c9bff;
  --success: #4ade80;
  --success-soft: #0d2a26;
  --warning: #facc15;
  --warning-soft: #2a2610;
  --danger-soft: #2e1022;
  --info-soft: #0c2545;
  --tag-cobalt-bg: #8c9bff; --tag-cobalt-fg: #050b24; /* cobalt 300 on dark, per brand */
  --shadow-soft: 0 0 0 1px rgb(140 155 255 / 0.04);
  --shadow-pop: 0 18px 48px -8px rgb(0 0 0 / 0.65);
}

/* Forced-dark scope for the ASCII hero band and the top nav, in both themes */
.on-dark {
  --background: #050b24;
  --foreground: #ffffff;
  --card-foreground: #ffffff;
  --popover: #0e1840;
  --popover-foreground: #ffffff;
  --secondary-foreground: #ffffff;
  --muted-foreground: rgb(214 224 255 / 0.74);
  --card: rgb(255 255 255 / 0.07);
  --muted: rgb(255 255 255 / 0.08);
  --border: rgb(255 255 255 / 0.14);
  --border-strong: rgb(255 255 255 / 0.26);
  --input: rgb(255 255 255 / 0.22);
  --primary: #ffffff;
  --primary-foreground: #1e3fd8;
  --accent: rgb(255 255 255 / 0.12);
  --accent-foreground: #ffffff;
  --ring: #a5d8ff;
  --info-soft: rgb(165 216 255 / 0.16);
  --success: #86efac; --success-soft: rgb(34 197 94 / 0.16);
  --warning: #fde68a; --warning-soft: rgb(234 179 8 / 0.16);
  --destructive: #fda4af; --danger-soft: rgb(251 113 133 / 0.16);
  --secondary: rgb(255 255 255 / 0.08);
  --tag-cobalt-bg: #8c9bff; --tag-cobalt-fg: #050b24;
  color: var(--foreground);
}
```

### Scales and rules
| Scale | Values |
|---|---|
| **Type** | display `clamp(2rem, 3.2vw, 2.75rem)` / 1.05 / 600 / −0.03em · landing hero `clamp(2.6rem, 5.4vw, 4.4rem)` / 0.98 / −0.04em · h2 20/28 · 600 · h3 16/24 · 600 · body 14–15 · small 13 · caption 12 · **eyebrows and table headers in Geist Mono uppercase** (an echo of the ASCII hero) · mono at 0.875em of its context |
| **Spacing** | Tailwind 4-px scale. Page gutter `px-6 lg:px-8`, container `max-w-7xl`, header band `pt-12 pb-8`, card padding 20 px (`p-5`), row padding `px-5 py-4`, gaps between sections 24 px (`gap-6`) |
| **Radius** | buttons, tags and badges full pill · inputs 10 · cards 14 · sheets and dialogs 16 |
| **Border** | 1 px blue-tinted hairlines (`--border`, `--border-strong`) everywhere. No 2 px outlines |
| **Elevation** | Cards: `--shadow-soft` (a faint cobalt-tinted lift). Overlays: `--shadow-pop` plus a 38 % night scrim. Nothing else casts a shadow |
| **Motion** | 150–200 ms `ease-out` for colour and opacity; 350 ms fade-and-rise for sheets, dialogs and toasts; the live dot pulses every 1.8 s; countdown rings tick once a second; everything is off under `prefers-reduced-motion` |
| **Icons** | `lucide-react` (already installed) at 16 px, stroke 1.75. 14 px inside badges. The spark (`Sparkle`) always means "agent" |
| **Numbers** | `tabular-nums` everywhere a number changes (countdowns, blocks, ms, USDC) |

### Old → new class map (used by the codemod in §9 step 1)
| Old (`globals.css @theme`) | New semantic class |
|---|---|
| `bg-bg` | `bg-background` |
| `bg-panel` | `bg-card` |
| `bg-panel-2` | `bg-muted` |
| `border-line` | `border-border` |
| `text-fg` | `text-foreground` |
| `text-muted` | `text-muted-foreground` |
| `text-accent` / `bg-accent/*` / `border-accent/*` (mint "ok") | `text-success` / `bg-success-soft` / `border-success/*` |
| `text-accent-2` (blue links) | `text-primary` |
| `text-warn` / `bg-warn/*` | `text-warning` / `bg-warning-soft` |
| `text-bad` / `bg-bad/*` | `text-destructive` / `bg-danger-soft` |
| `.mono` | `font-mono` |

> ⚠️ **`muted` and `accent` exist in both systems with different meanings.** Today `text-muted` is grey *text* and `accent` is mint. In shadcn, `muted` is a *background* and `accent` is a hover fill. If the shadcn tokens land before the codemod, every `text-muted` turns near-white and disappears. That's why the codemod is step 1.

---

## 5. Layout and navigation

```
┌──────────────────────────────────────────────────────────────────────────────┐
│ [M meshy]  (M meshydemo.eth ⇅)  Access Directory Activity Billing Provider   │
│                                  Settings      (🔍 Search names… ⌘K) ☼ (0x45e8…E0EA Owner) │  ← TopNav: night #050B24 at 62 %, blur, sticky
├──────────────────────────────────────────────────────────────────────────────┤
│ · · = + x X 0 S 8 # @   night sky + ASCII horizon rising on the right  ╭──── │  ← HeaderBand (.on-dark)
│ meshydemo.eth / access                         ← ENS breadcrumb (each label  │
│ Access right now                                  is a link)                 │  ← PageHeader
│ One agent can reach one thing…        [Invite agent] [Give access  G]        │
│ [● 1 live grant] [⧗ next expiry in 58m] [⚡ revoke → dead 1.6 s] ← solid chips │
╞══ cyan → ice → sky edge line (3 px) ═════════════════════════════════════════╡
├──────────────────────────────────────────────┬───────────────────────────────┤
│ main content (cards)                         │ right rail (feed, setup)      │
└──────────────────────────────────────────────┴───────────────────────────────┘
│ ● Sepolia #12,074,308 · ⦿ Arkiv · claude-1 started a session on db1.prod · 1m │ 🔒 Org key not loaded · Import │  ← StatusBar, 36px
```

- **Top nav replaces the sidebar** (decision D3). Six destinations fit comfortably at ≥ 1024 px. Below `md`, the links move into a `Sheet` opened by a menu button. This follows ethglobal.com and gives the content about 240 px back.
- **Org switcher pill.** It shows the current org and lets you paste any `*.eth` name. This replaces the "Organization" box and the landing form.
- **⌘K pill.** It is the full search field at ≥ 1536 px and a compact `🔍 ⌘K` button below that. See innovation 1.
- **Wallet pill.** It shows a generated avatar, the short address and a role badge (Owner, "Admin of home", or Read-only). When disconnected it becomes an outline pill reading "Connect wallet".
- **PageHeader** (one component used by every page) includes:
  - the ENS breadcrumb
  - the display title
  - a one-sentence lede
  - right-aligned actions
  - an optional row of Electric chips with live facts
  - the **header band** behind it (below)
- **Header band** (`HeaderBand`, new). The nav and page header sit on the night sky.
  - A `GradientASCII` canvas with `shape="horizon"` runs behind it. On app pages it is a short band whose planet rises on the right (`cx≈0.8`), so the title and lede on the left stay on calm dark sky. A left-to-right night scrim keeps text contrast at least 10 : 1.
  - A 3 px cyan → ice → sky line closes the band. Content then sits on the white page.
  - The landing page runs the user's exact props at 600 px (`cellSize 18, density .8, glyphOpacity .85, glow .2, flicker .2, softness .5, flow .5, grain .08, mouseInteraction, scrambleRadius .1, clickEffect "wave", intro`).
  - The Approve page uses a centred band with the horizon below the headline.
- **StatusBar** (new, fixed bottom, 36 px) takes over the sidebar footer and the header strip:
  - the chain head with a live dot
  - the latest Arkiv event as a ticker (innovation 7)
  - the org-key state with a link to import it
  - in read-only mode: "Read-only · Connect a wallet to change access"
- **Content width.** `max-w-7xl` overall. Two-column pages use a 360 px right rail; Settings uses a sticky 200 px section nav.
- **Keyboard shortcuts:**
  - `⌘K` / `Ctrl K`: palette
  - `G`: give access, on Access
  - `g a`, `g d`, `g l`, `g s`: jump to Access, Directory, Activity, Settings
  - `Esc` closes any overlay
  - `?` lists all shortcuts

---

## 6. shadcn component map

Install with `npx shadcn@latest add …` into `web/src/components/ui/`. Base: **Radix** (the unified `radix-ui` package), style `new-york`.

| shadcn component | Where it's used |
|---|---|
| `button` (variants extended: `default` = cobalt pill, `ink`, `outline` (1 px hairline), `ghost`, `destructive`, `destructive-outline`; sizes `sm`/`default`/`lg`/`icon`) | Everywhere; replaces `ui.tsx` `Button` |
| `badge` (variants: `live`, `warn`, `bad`, `info`, `muted`, `outline`) + a Meshy `Tag` chip (solid fill, bold sans; `cobalt` · `cyan` · `lime` · `sun` · `pink` · `lilac`) | Grant state, World ID, roles, counts; tags in page headers and the Directory |
| `card` | Every section container |
| `sheet` | **Give access** (right, 580 px); mobile nav (left); **Add device / Add network / Add service** (right, replacing the inline forms); Add admin |
| `alert-dialog` | **Revoke confirm**, Remove agent, Remove admin, Submit fraud proof, Forget org key |
| `dialog` | Revoke live timeline (can be minimised into a toast), keyboard shortcuts help |
| `command` (cmdk) inside `dialog` | **⌘K palette** |
| `sonner` | All toasts, including the **stepper toasts** for multi-transaction flows (innovation 6); replaces the `store.tsx` toast array |
| `tabs` | Access (Live / Ended / All), Directory detail (Overview / Grants / Activity / ENS records), Activity filters, Provider (Providers / Run / Challenge) |
| `toggle-group` | Duration (15 min / 1 h / 4 h), Read vs Read + write, List vs Timeline, Device folders vs One service port, Add seats vs Renew |
| `radio-group` | Give access target picker (radio cards) |
| `table` + `@tanstack/react-table` (shadcn DataTable recipe) | Activity audit log, Directory services, Provider list, Admins, Billing payments, agent refusals |
| `select`, `popover`, `command` (combobox recipe) | Agent picker in Give access; Activity filters (agent, device, time) |
| `input`, `textarea`, `label`, `checkbox` | Forms; fingerprint confirmation on Approve |
| `tooltip` | The reason on every disabled action ("Only admins of `prod` can revoke here"); full values behind shortened addresses |
| `hover-card` | Hovering an ENS name shows its kind, owner, records and live grants |
| `dropdown-menu` | Row "…" menus (copy name, open in ENS app, open on Etherscan, renew); the Directory "Add" menu; the org switcher |
| `breadcrumb` (customised into `EnsBreadcrumb`) | Page headers, Directory detail, agent page |
| `skeleton` | Every loading state (grant rows, feed, stat tiles, tables) |
| `alert` | Org key not loaded; demo-only provider warnings; wrong network |
| `progress` | Seat meter (segmented variant), grant time left on small screens |
| `separator`, `scroll-area`, `collapsible` | Card dividers; palette and feed scrolling; "Verify it yourself" and ENS records |
| `avatar` | Wallet pill and admins |
| `kbd` | Shortcuts shown in buttons and the palette |

Custom Meshy components, built on the above (in `web/src/components/meshy/`):

| Component | What it is |
|---|---|
| `EnsName` / `EnsBreadcrumb` | Splits a name into labels; each label links to its node; copy button; hover card |
| `CountdownRing` | SVG ring around the agent glyph, driven by `expiry`, using the grant's real total duration |
| `GrantRow` | Ring, agent → target, state badge, scope line, time left, Extend / Revoke / more menu |
| `TxSteps` + `txToast()` | Step model shared by sheets, dialogs and stepper toasts |
| `FeedItem` | Arkiv event as a sentence, with an action ("Grant this") |
| `StatTile` | Number plus caption plus optional sparkline; renders a skeleton until data arrives (never a fake 0) |
| `Ticket` | A small date tile for expiries and renewal dates |
| `GradientASCII` (the user's component) + `HeaderBand` | The ASCII horizon: full 600 px hero on `/`, short band behind the nav and PageHeader on every app page, centred band on Approve. Wraps its children in `.on-dark` |
| `PageHeader`, `TopNav`, `StatusBar`, `CommandPalette` | Shell components |

---

## 7. Page-by-page redesign

The mockup for each page is linked in its heading.

### Access home ([mockup](mockups/access.html))
- **Header**
  - The lede adapts to the data: "No agent can reach anything" / "One agent can reach one thing" / "3 agents can reach 5 things".
  - Tags show live facts: live grant count, the next expiry, and the last revoke latency.
- **Grants card.**
  - `Tabs` for Live (count), Ended (count) and All, plus a filter input and a List / Timeline toggle.
  - Each `GrantRow` contains:
    - a countdown ring around the agent spark
    - the ENS breadcrumb, with `claude-1` linking to the agent page and `db1.prod` linking to the device
    - a state badge ("Session open" when Arkiv shows a live session)
    - a scope line ("Folders · sealed policy", "One port · http → 127.0.0.1:8001")
    - who granted it, and when
    - the time left plus the absolute time ("until 15:04 today")
    - actions: **+1h** (renew), **Revoke** (opens an `AlertDialog`) and a "…" menu
  - "Recently ended" rows sit below the live ones, dimmed, with **Grant again**. Grant again reopens the sheet pre-filled with the same scope.
- **Timeline view** ("Who has access, when"). Bars on a time axis with a "now" line: live bars in cobalt, expired ones hatched, revoked ones ending in a red cap.
- **Right rail:**
  - "Happening now": the Arkiv feed as sentences. Refusals carry **Grant this** (innovation 9).
  - The setup checklist (innovation 8).
  - "Your mesh": four counts linking to the Directory.
- **Empty state.** No grants: a big empty card ("No agent can reach anything") with the checklist expanded and one primary CTA.

### Give access sheet ([mockup](mockups/give-access.html))
- A right-side `Sheet` (580 px) with four numbered questions:
  1. Agent: a combobox showing the human-backed badge and live grant count.
  2. What it can reach: a toggle between device folders and one service port, then radio cards per target. A target already granted to this agent says so ("claude-1 has it · 58m").
  3. Folders: one row per folder with a path input (`~` shown for the advertised root), a Read vs Read + write toggle, and remove. "Suggested" chips come from the device's `meshy.roots`.
  4. For how long: 15 min, 1 hour or 4 hours, plus "Expires 15:08 today".
- **"In plain words" preview.** One sentence states the grant, then three facts: the ENS name created, "policy sealed in your browser", and the World ID status.
- **Sticky footer.** The transaction count and rough time ("2 transactions on Sepolia · about 30 s"), then Cancel and Give access.
- **After submit.** The sheet closes and a stepper toast takes over, so the user can keep working. Errors reopen the sheet at the failed step.
- **Pre-fill sources.** `?give=<agent>&target=<name>&path=…` from Grant again, Grant this, Directory "Give access", or the palette.

### Revoke ([mockup](mockups/revoke.html))
1. **`AlertDialog` confirm:**
   - the title names both ends ("Revoke claude-1 on db1.prod?")
   - the body says what breaks ("the open session will be killed in the block that includes the transaction")
   - a mini table: live sessions, time left, cost
   - focus starts on Cancel
2. **Live timeline `Dialog`.** Four steps, each with a timestamp offset and proof:
   - transaction sent (Etherscan link)
   - mined (block number)
   - daemon killed the session (reaction in ms, plus the `MESHY_REVOKED` string the agent saw)
   - recorded on Arkiv (entity link)

   A big ring shows click-to-dead time. A stacked bar splits it into inclusion wait, block propagation and daemon reaction. **Minimise** turns the dialog into a stepper toast; a backdrop click minimises instead of discarding.
3. **Stepper toast.** The same steps as segments, used by every multi-transaction flow.

### Directory ([mockup](mockups/directory.html))
- **Master-detail**, like ethglobal's prize pages.
  - Left (320 px): a filterable tree grouped by network: `home.meshydemo.eth` → `pi` (→ `camera`), `rig1`; `prod.meshydemo.eth` → `db1`; `agents.meshydemo.eth` → `claude-1`. Live grant counts are shown as badges.
  - Right: the selected node.
- **Device detail:**
  - ENS breadcrumb title and fact tags (model, location, daemon online)
  - Give access / Add service buttons
  - `Tabs`: Overview / Grants / Activity / ENS records
  - Overview content:
    - a dark copyable `meshy ssh …` command
    - a facts grid: shared folder (as `~/…`), daemon address, pinned SSH host key, "who can reach it now"
    - a services table
    - the device's recent events, with Grant this
- Network and service nodes get the same layout with fewer facts.
- Selection lives in the URL (`/org/[org]/directory?n=pi.home`), so links and the palette can deep-link.
- **Add network / device / service** move into a "+ Add" dropdown that opens Sheets. The device sheet keeps the `meshyd init` command and join-code paste, and adds a parsed preview of the join code before signing.

### Agent detail ([mockup](mockups/agent.html), new route `/org/[org]/agents/[label]`)
- **Header**: the spark tile, the full ENS name, and tags (Human-backed · World ID, client, live grants).
- **"What claude-1 can touch right now"** (innovation 4): a reach map in the logo's visual language and a plain-words sentence of what it can and cannot do. Actions: Revoke all, Extend.
- **"What it tried and was refused"**: a table of refusals with **Grant this**.
- **Right rail:**
  - Identity: address, SSH key, approved by, World ID attestor, own resolver
  - `agent-context`
  - a grant history timeline
  - a danger zone (Remove agent, behind an `AlertDialog`)

### Activity ([mockup](mockups/activity.html))
- **PageHeader** with a "Live from Arkiv" tag, plus Export CSV and Verify it yourself.
- **Four `StatTile`s**: live sessions, refused (24 h), revocations, daemon kill latency with a sparkline. They show skeletons until loaded, never zeros.
- An **`Alert` for the missing org key**, with Import.
- **DataTable**:
  - tabs by event family
  - filters: agent, device, time range
  - columns: time (absolute, with relative time in a tooltip), event badge, who and where, detail, proof link
  - row tints: live sessions green, revocation kills amber
  - "Load older" pagination
  - a footer naming the verified writer daemons by ENS name
- "Verify it yourself" becomes a `Collapsible` with the runnable `curl`.

### Billing ([mockup](mockups/billing.html))
- **Hero line**: "Paid until 6 Dec 2026" (always `d MMM yyyy`), with days left, what happens after expiry, and a `Ticket` date tile.
- **Segmented seat meter.** One segment per seat, labelled (3 devices: db1, pi, rig1 · 1 agent: claude-1 · 1 free). Notes that services are free.
- Provider summary, and payments from contract events.
- **Change plan.** A toggle between Add seats and Renew, a stepper input, a live price breakdown (seats × price × months), the 3 steps listed, and a primary button with the total ("Add 5 seats · 10 USDC").

### Provider ([mockup](mockups/provider.html))
- `Tabs`: **Providers** (table: endpoint, signer, price, stake bar, status, and "your org's provider"), **Run a provider** (the register form), and **Challenge a provider**.
- meshy-sync health as a checklist. "World ID disabled" and "Dev attestation on · demo only" are shown as warnings, with an explanation.
- **The challenge form validates before signing**: valid JSON, signed by the provider's signer, name revoked before `issuedAt`, and the reward you'd get. Submit sits behind an `AlertDialog`.

### Settings ([mockup](mockups/settings.html))
- A sticky section nav: Organization · Org key · Admins.
- **Organization.** A records table (name, owner "you", resolver, org registry, trusted attestors labelled with what they are, `meshy.orgkey`), with copy buttons and ENS / Etherscan links.
- **Org key.** A drop zone for `~/.meshy/admin/meshydemo.eth.json` or a pasted key, with three trust facts. Loaded state: the fingerprint, "matches ENS", and Forget behind an `AlertDialog`.
- **Delegated admins.**
  - A list of existing admins (`0xf846…Fc98` on `home` and `agents`), with scope tags and plain "can" / "cannot" columns.
  - Remove (behind an `AlertDialog`).
  - A **permission matrix** of network × capability.
  - **Add admin** opens a Sheet that lists every transaction before signing.
  - Listing admins needs a new reader in `lib/ens.ts` (EAC role events). See R6.

### Approve ([mockup](mockups/approve.html))
- A standalone page with only the logo, theme and wallet.
- **Headline**: "claude-2 wants to join", plus "Approving gives it an identity only. It can't reach anything until you grant access."
- A **3-step stepper**: Check the request → Prove a human → Approve.
- **Request card.** A large, chunked SSH fingerprint and a required "It matches what the agent printed" checkbox.
- **World ID card.**
  - Primary: Verify with World ID, which shows the code and a waiting state.
  - Secondary: Use dev attestation. It shows only when the provider allows it, and is labelled forever.
- **Approve card.** It lists the real transaction count, which is 1 when the agent already exists. On success it goes to the agent page with the Give access sheet open (fixing the `/grants` 404).

### Landing `/`
- **Hero** ([mockup](mockups/index.html)): the full 600 px `GradientASCII` horizon with the user's exact props.
  - Headline: "Network access for agents, as ENS names." The last words are in a sky → ice gradient.
  - The org field is a pill input with "Open org".
  - A glass "live grant" card sits on the horizon: countdown ring, `claude-1 → db1.prod`, +1h / Revoke, Electric chips.
  - The canvas answers the mouse (scramble) and clicks (wave).
- **"Three moves"**: name it → grant it → revoke it, each with a real mono example.
- **The list of screens**: numbered rows, not a showcase grid.
- **A brand note** on the spark's gap.

---

## 8. UX innovations

Ten, in recommended build order. Effort: **S** ≤ 2 h, **M** ≈ half a day, **L** ≈ a day or more (for an agent working in this repo).

| # | Innovation | Why it fits Meshy | Effort | Shown in |
|---|---|---|---|---|
| 1 | **⌘K palette that resolves ENS names and addresses.** Type `db1`, `claude`, a full name or `0x35e7…` and get the resolved node (kind, owner, live grants) plus actions: give access, revoke a specific live grant, copy `meshy ssh …`, import org key, jump to a page. | Everything in Meshy *is* a name. Its natural interface is "type the name". Addresses are opaque, and resolving them locally from the loaded org tree (with viem `getEnsName` as a fallback) makes them legible | M | Every page (`?palette=0x35e7`) |
| 2 | **Live countdown ring around the agent, plus one-click +1h.** The ring drains with the grant's real duration (fixing the hard-coded 4 h) and turns amber under 5 min. "+1h" calls `renew(anyId, newExpiry)`, which is already in the ABI. | Expiry is the product's safety net. It should be visible at a glance, and extending it should be as easy as revoking | S (ring), M (renew + role check) | Access, agent |
| 3 | **Revoke you can watch.** An `AlertDialog` that says what breaks, then a timeline from click to dead session with proof per step and a latency breakdown (inclusion / propagation / daemon ms). It can be minimised to a toast. | "Revoke kills live sessions within ~12 s" is the headline claim. Showing it measured is the demo moment, and the breakdown shows the daemon took 216 ms; the rest is Sepolia | M | Revoke |
| 4 | **"What can this agent touch?" explainer.** A reach map drawn in the logo's language (spark held apart, rings for devices, solid cobalt link = live), plus a plain-words sentence that also says what it **cannot** do. | Least privilege is hard to see from a list of names. One sentence plus one picture answers the security reviewer's first question | M | Agent |
| 5 | **ENS breadcrumbs as navigation.** Every label of `claude-1.db1.prod.meshydemo.eth` is a link (grant → device → network → org), with hover cards showing records. | It teaches the hierarchy for free. The name is the address, the permission and the breadcrumb all at once | S | Every page |
| 6 | **Transaction stepper toasts.** Every multi-transaction flow (grant 2 tx, add device 3, delegate admin N, add seats 3) runs as a Sonner toast with segments, Etherscan links per step and retry on failure. It survives navigation because state lives in the store. | Users stop staring at a modal for 30 s, and failures say which step failed | M | Revoke |
| 7 | **Arkiv live ticker in the status bar.** One shared poller in the store, replacing the two per-page ones. It shows the newest event from anywhere, pulses on refusals and revocations, and clicks through to Activity. | It makes the audit log feel alive on every page and halves Arkiv traffic | S | Every page |
| 8 | **Setup checklist from chain state.** Create network → add device → invite + approve agent → first grant → first revoke → load org key. Each step is computed from the tree, Arkiv and local state, never from a stored flag. It becomes the empty state for new orgs. | A new org currently lands on "Nothing live". The checklist doubles as the demo script | S | Access |
| 9 | **"Grant this" on refusals.** `MESHY_NO_GRANT` and `MESHY_BLOCKED path=… ask=…` events become one-click, pre-filled Give access sheets (agent, target, path, read/write). | The agent's error already carries what it wanted. Closing the loop from denial to grant is the core agent-access workflow | S/M | Access, agent, Activity, Directory |
| 10 | **Role-aware UI.** Read the connected wallet's real roles (owner, or `hasRootRoles` per registry, as `Admins.tsx` already does). The wallet pill shows "Admin of home". Out-of-scope actions are disabled with a tooltip ("Only admins of prod can revoke here") instead of failing on-chain. | It replaces `isOwner = !!account` and makes delegated admin, a headline ENSv2 EAC feature, visible | M | Wallet pill, every action |

Also in the mockups, smaller: the access timeline view, the plain-words preview with transaction count before signing, the segmented seat meter, and the expiry `Ticket`.

---

## 9. Implementation plan

Each phase ends with this check, and the app keeps working after every phase:

```bash
cd web && npx next typegen && npx tsc --noEmit && pnpm lint && pnpm build
```

Then smoke-test `/org/meshydemo.eth` in the browser. Stop after any phase if time runs out. Phases 0–3 alone already give the new look.

### Phase 0: prep (15 min)
- Branch from `main`. Take baseline screenshots of the 7 routes.
- Fix the two `/grants` 404s now, as a separate small commit: point them at `/org/${org}?give=<agent>`, and teach Access to open the sheet from `?give=`.

### Phase 1: rename tokens, no visual change (45 min)
1. `web/src/components/ui.tsx` → rename to `web/src/components/meshy/primitives.tsx`, and update imports. This frees `@/components/ui/` for shadcn: `ui.tsx` and a `ui/` folder side by side make `@/components/ui` ambiguous.
2. Codemod every `className` with the old → new map in §4, using `sed` over `src/**/*.tsx`.
3. Rewrite `web/src/app/globals.css` `@theme` to define the **new names with the old dark values**, so the app looks identical:

   ```
   --color-background: #0a0b0d
   --color-card: #111317
   --color-muted-foreground: #8b93a1
   --color-success: #7cf2c6
   --color-primary: #5aa9ff
   ...
   ```
4. Verify with `rg "text-muted\b|bg-panel|border-line|text-fg|accent-2|text-warn|text-bad|bg-bg\b" web/src`, which should return nothing. Screenshots should be unchanged.

### Phase 2: shadcn foundation (45 min)
1. `pnpm add radix-ui class-variance-authority clsx tailwind-merge tw-animate-css sonner cmdk next-themes geist @tanstack/react-table`
2. `pnpm remove @xyflow/react`. The graph is gone; also delete `.react-flow__attribution` from `globals.css`.
3. **Run init carefully (Next 16 + Tailwind v4 via the Turbopack loader):**
   - This app has **no `postcss.config`**. Tailwind runs through `turbopack.rules["*.css"].loaders = ["@tailwindcss/turbopack"]` in `next.config.ts`. If `shadcn init` can't detect Tailwind, or offers to create a PostCSS config, **don't accept**: write `components.json` by hand (below).
   - CLI v4 removed `--style` and `--base-color`. Use `npx shadcn@latest init -d --base radix`, then **review the `globals.css` diff and restore the token block** from §4. Init rewrites the file and may add a circular `--font-sans: var(--font-sans)`.
   - Hand-written `web/components.json`:

     ```json
     { "$schema": "https://ui.shadcn.com/schema.json", "style": "new-york", "rsc": true, "tsx": true,
       "tailwind": { "config": "", "css": "src/app/globals.css", "baseColor": "neutral", "cssVariables": true },
       "iconLibrary": "lucide",
       "aliases": { "components": "@/components", "utils": "@/lib/utils", "ui": "@/components/ui", "lib": "@/lib", "hooks": "@/hooks" } }
     ```
   - Create `web/src/lib/utils.ts` with `cn()` (`clsx` + `tailwind-merge`).
4. Add the components in one go:

   ```bash
   npx shadcn@latest add button badge card input textarea label select tabs toggle-group radio-group sheet dialog alert-dialog command popover tooltip hover-card dropdown-menu table skeleton separator scroll-area sonner progress avatar kbd breadcrumb alert checkbox collapsible
   ```
5. Check the generated components: they must import from `radix-ui` (the unified package), not `@radix-ui/react-*`. Every file that uses hooks needs `"use client"`.
6. Still no visual change: nothing imports the new components yet.

### Phase 3: theme, fonts, brand (45 min)
1. `web/src/app/globals.css`: put in the full §4 token block (light `:root`, `.dark`, `.on-dark`) and the `.tag` / `.band-edge` utilities.
2. Fonts (`web/src/app/layout.tsx`):
   - **Don't use `next/font/google`.** LEARNINGS #39: it fetches at build time on a flaky network.
   - Use the `geist` package (`next/font/local` underneath): `import { GeistSans } from "geist/font/sans"` and `GeistMono`, and put **both `.variable` classes on `<html>`, not `<body>`**.
   - Tailwind v4 caveat: in `@theme inline`, avoid a self-referencing `--font-sans: var(--font-sans)`. Either point at the package variables (`--font-sans: var(--font-geist-sans), ui-sans-serif, …`) and confirm in DevTools that the computed font is Geist, or use the literal family names the package registers.
3. Theme (`web/src/components/ThemeProvider.tsx`, new):
   - Wrap `next-themes` with `attribute="class" defaultTheme="light" enableSystem` (or `"system"`, per D2), and add `suppressHydrationWarning` on `<html>`.
   - It is a client component, so it is fine with `cacheComponents: true`. Don't read cookies for the theme; that would make the route dynamic.
4. Brand, following `brand/README.md` §"Using it in the web app":
   - `favicon.ico` → `src/app/favicon.ico`
   - `favicon.svg` → `src/app/icon.svg`
   - `apple-touch-icon.png` → `src/app/apple-icon.png`
   - `icon-192.png`, `icon-512.png` and `site.webmanifest` → `public/`
   - `meshy-lockup.svg` and `meshy-lockup-white.svg` → `public/`
   - In `layout.tsx`: `manifest: "/site.webmanifest"` and `export const viewport = { themeColor: "#2B3FE0" }`.
5. `<Toaster />` (Sonner) and `<TooltipProvider>` go in `layout.tsx`.
6. The app now shows light colours with the old structure. Fix any contrast bugs before moving on.

### Phase 4: shell (1.5 h)
1. `web/src/components/shell/TopNav.tsx`: lockup, org switcher (`DropdownMenu`), nav links, ⌘K button, theme toggle and wallet pill; `Sheet` nav below `md`.
   - **`usePathname()` must render inside the `<Suspense>` that `OrgShell` already sets up** (LEARNINGS #35), or prerendering breaks.
2. `web/src/components/shell/StatusBar.tsx`: chain head, Arkiv ticker and org-key state. It reads from the store.
3. `web/src/components/shell/PageHeader.tsx`: breadcrumb, title, lede, actions and tags, rendered inside `HeaderBand`.
4. `web/src/components/brand/GradientASCII.tsx`: the user's component, which is canvas-only, so it needs `"use client"`. Load it with `next/dynamic(() => import(...), { ssr: false })` so it never runs during prerender (and doesn't trip `cacheComponents`).
   - Pause it when off-screen (IntersectionObserver).
   - Draw one static frame under `prefers-reduced-motion`.
   - Run `intro` only on the landing page.
   - `HeaderBand.tsx` wraps it with `.on-dark`, the left scrim and the edge line. App pages pass lighter props: `cellSize 16, horizon .62, cx .8`.
   - The mockup's [`_ascii.js`](mockups/_src/_ascii.js) is a vanilla stand-in with the same prop names. It shows the intended look if the real component needs tuning.
5. `web/src/components/Shell.tsx`: replace the sidebar layout with TopNav + main + StatusBar. Keep the `Shell` export so `OrgShell.tsx` doesn't change.
6. `web/src/lib/store.tsx`:
   - `toast(msg, kind)` becomes a thin adapter over `sonner`. Keep the signature, so the ~15 call sites don't change, and remove the `toasts` array.
   - Add `feed` (one shared Arkiv poller), `roles` (innovation 10, a stub for now) and `latestEvent`.
7. `web/src/app/approve/*`: a minimal header (logo, theme, wallet) on a centred `HeaderBand`, and no org nav.

### Phase 5: primitives (1 h)
1. `web/src/components/meshy/primitives.tsx`: re-implement `Card`, `Badge`, `Button`, `Field`, `Empty`, `Ext` and `Short` on top of shadcn, keeping the names and props. Pages compile unchanged and pick up the new look.
2. New components:
   - `meshy/EnsName.tsx` (labels as links, copy button, hover card; replaces `Name`)
   - `meshy/CountdownRing.tsx` (replaces `Ring`, takes the real `total`)
   - `meshy/StatTile.tsx`
   - `meshy/Ticket.tsx`
   - `meshy/Tag.tsx`
3. `web/src/components/Steps.tsx`: restyle it, and add `txToast(steps)`, which drives a Sonner custom toast from the same `Step[]`.

### Phase 6: pages, one commit each (about 5–6 h total)
Pages are in order of demo value.

1. **Access**: `app/org/[org]/page.tsx`
   - `AccessLine` → `GrantRow`.
   - `Tabs` for Live / Ended / All.
   - Use the real duration for the ring and bar. Grant total = expiry − registration time from the `register` log; fall back to 1 h.
   - Feed as `FeedItem`s, plus the checklist and mesh counts.
   - `?give=` opens the sheet.
2. **Give access**: `components/Access.tsx` → split into `components/access/GiveAccessSheet.tsx`.
   - Shadcn `Sheet`, combobox, `RadioGroup` target cards, `ToggleGroup`s, plain-words preview and footer.
   - Submit closes the sheet and hands off to `txToast`.
   - **Keep `submit()`'s transaction logic byte for byte** (sealing, `recordCalls`, step order).
3. **Revoke**: `components/access/RevokeConfirm.tsx` (`AlertDialog`) and `components/access/RevokeProgress.tsx` (`Dialog`, minimisable).
   - Move the async flow out of the component into the store, so closing the UI can't orphan it. This also fixes the backdrop-click bug.
   - **Keep the `fired` ref guard** (LEARNINGS #37).
   - Add timestamps per step for the latency bar.
4. **Directory**: `app/org/[org]/directory/page.tsx`
   - Master-detail with selection in `?n=`.
   - `components/Devices.tsx` → `directory/Tree.tsx`, `directory/NodeDetail.tsx`, `directory/AddDeviceSheet.tsx`, `AddNetworkSheet.tsx`, `AddServiceSheet.tsx` (same transaction code).
   - Show `~` for paths under the home directory.
5. **Agent page**: new `app/org/[org]/agents/[label]/page.tsx` and `components/agents/ReachMap.tsx`.
   - The invite form moves to `directory/InviteAgentSheet.tsx`.
   - Run `npx next typegen` after adding the route (LEARNINGS #38).
6. **Activity**: `app/org/[org]/activity/page.tsx`. DataTable with tabs and filters, `StatTile`s with skeletons, org-key `Alert`, and `Collapsible` verify. It reads `feed` from the store.
7. **Settings**: `app/org/[org]/settings/page.tsx` and `components/Admins.tsx`.
   - Sections, records table and drop zone.
   - Admins list: a new `adminsOf(registry)` in `lib/ens.ts`, reading EAC role-change events with the same Multicall pattern.
   - Permission matrix, and an Add admin `Sheet`.
8. **Billing**: `app/org/[org]/billing/page.tsx`. Use `Intl.DateTimeFormat("en-GB", { day: "numeric", month: "short", year: "numeric" })`, the segmented meter, the price breakdown and the stepper.
9. **Provider**: `app/org/[org]/provider/page.tsx`. `Tabs`, the health checklist and the validated challenge form.
10. **Approve**: `app/approve/Approve.tsx`. Stepper, fingerprint checkbox, the real transaction count, and redirect to the agent page on success.
11. **Landing**: `app/page.tsx`. The full `GradientASCII` hero, the live-grant glass card, the three moves and the org pill input.

### Phase 7: innovations not already covered (3–4 h)
1. ⌘K palette: `components/shell/CommandPalette.tsx`. Index built from `tree` (networks, devices, services, agents, grants) plus static actions; address → name map from `addr` records.
2. Role-aware UI: compute `roles` in the store with `hasRootRoles` per registry for `account`. Wrap action buttons in `<Gate need="prod.revoke">`, which renders a disabled button plus a `Tooltip`.
3. "+1h" renew: `renew(anyId, newExpiry)` on the grant's registry, gated by the RENEW role.
4. Timeline view on Access.

### Phase 8: QA (1 h)
- Keyboard-only run-through of the full lifecycle: grant, see the session, revoke, watch the timeline.
- axe DevTools on each page.
- Dark mode, reduced motion, 375 px and 768 px widths.
- Slow-network throttling, for skeletons.
- Then `pnpm build` and a live Sepolia grant + revoke with the real wallet.

**Don't touch:**
- `src/lib/envelope.ts`: it must stay byte-compatible with Go.
- `src/lib/arkiv.ts` query logic.
- the `send()` / transaction order in any flow.
- anything under `meshy/`, `meshnet/` or `menubar/`, where another session is working.

---

## 10. Accessibility

- **Contrast.** All text pairs meet WCAG AA (the table in §2). State is never shown by colour alone: every badge has an icon and a word ("Revoked", "Session open").
- **Focus.** A visible 2 px `--ring` outline with a 2 px offset on every interactive element. Radix gives focus traps and Escape on `Sheet`, `Dialog`, `AlertDialog` and `Command`. In `AlertDialog`, focus starts on **Cancel** for destructive actions.
- **Live regions.**
  - Sonner toasts are `aria-live="polite"`. Revoke progress and failures are `assertive`.
  - Countdowns are **not** live regions. They would announce every second. Instead they carry `aria-label="58 minutes left, expires 15:04"`, refreshed each minute.
  - The status-bar ticker is `aria-live="off"`, with a "Pause updates" control.
- **Semantics.**
  - Tabs use Radix `Tabs` (`role=tablist`).
  - Tables are real `<table>`s with `<th scope>`.
  - The Directory tree is a `nav` with `aria-current`.
  - The breadcrumb is `nav aria-label="Breadcrumb"`.
  - The reach map has a text alternative: the "In plain words" sentence is its accessible description.
- **Keyboard.** ⌘K, `G`, `g …` sequences and `?` help. Every row action is reachable with Tab; "…" menus open with Enter or Space.
- **Motion.** `prefers-reduced-motion` disables pulse, shimmer, ring transitions and fades, and the ASCII canvas draws one still frame.
- **The ASCII canvas** is `aria-hidden` and purely decorative. No text is ever drawn into it; headings are real DOM text over a night scrim.
- **Targets and text.** Minimum target 32 px (buttons `sm`), 36 px by default. Body text 14 px or larger (up from 11–13 px). Monospace is used only for names, addresses, code and numbers, never for sentences.
- **Language.** Sentences say what happens to people and agents ("claude-1 can read … for 1 hour"), with the protocol detail second. Machine codes (`MESHY_NO_GRANT`) stay visible for debugging, inside `<code>`.

---

## 11. Risks and decisions needed

### Risks
| # | Risk | Mitigation |
|---|---|---|
| R1 | **Deadline.** Judging is tomorrow, 7 Nov 09:00 IST. A half-migrated UI is worse than the current one | Phases are independently shippable. Phases 0–3 (about 2.5 h) give most of the visual change. Merge only after a live Sepolia grant + revoke works in the new UI. Keep `main` deployable |
| R2 | **Token name collision** (`muted`, `accent`) makes text invisible | Phase 1 codemod first, verified with `rg` before any shadcn token lands |
| R3 | **`shadcn init` vs. this setup.** There is no PostCSS config, Tailwind runs through the Turbopack loader, and the CLI rewrites `globals.css` | Use `-d --base radix`, review the diff, restore the tokens; fall back to a hand-written `components.json` |
| R4 | **Next 16 specifics.** `cacheComponents: true`, the `usePathname` Suspense rule, generated `PageProps`, a hydration mismatch from the theme | next-themes with `suppressHydrationWarning`; nav inside the existing Suspense; `next typegen` after adding routes; never read cookies for the theme |
| R5 | **Transaction flows regress while being re-wrapped** (double-send, lost steps) | Keep `submit()` bodies verbatim; keep the `fired` ref; move flows into the store; rerun the full lifecycle on Sepolia |
| R6 | **Listing delegated admins** needs scanning EAC role events: more `eth_getLogs`, and the public node is slow | Reuse `LOGS_RPC` + Multicall; cache in sessionStorage like the tree; show a skeleton; fall back to the current "check an address" |
| R7 | **The slow RPC still means 20 s+ first loads.** The new design only hides it better | Skeletons everywhere; render cached sessionStorage data immediately with a "refreshing" dot; consider `/v1/map` from meshy-sync as a faster first paint |
| R8 | **ASCII canvas cost.** A 30 fps canvas behind every page header, plus backdrop blur, on low-end laptops | Cap at 30 fps; pause off-screen and on hidden tabs; static frame under reduced motion; app bands use a larger cell (fewer glyphs) and no intro; if needed, render app-page bands once and animate only the landing hero (decision D8) |
| R9 | **Bundle growth** (Radix, cmdk, TanStack Table, Sonner) | All tree-shaken, about 60–80 kB gzip in total; `@xyflow/react` is removed |
| R10 | **Concurrent sessions.** Another session edits Go under `meshy/`, `meshnet/` and `menubar/` | This plan touches only `web/`, `docs/ui-revamp/` and copies from `brand/` |

### Decisions needed from the user
- **D1. When to implement.** Before the 7 Nov 09:00 IST deadline (phases 0–3, and 6.1–6.3 if time allows), or after judging (everything)? *Recommendation:* Phase 0 now. It is the broken-link fix. Phases 1–3 and 6.1–6.3 only if they can be verified on Sepolia by about 06:00 IST.
- **D2. Light by default?** *Recommendation:* palette B (Electric, white) by default, with D (night mesh) as the dark theme one click away. The nav and the header band stay night in both. The alternative is to follow the system setting.
- **D3. Top nav instead of the sidebar.** *Recommendation:* the top nav, as ethglobal does. All six destinations fit, and the mobile `Sheet` covers small screens.
- **D4. Typeface.** Geist (free, the brand's choice) instead of ethglobal's commercial Roobert. *Recommendation:* Geist. No licence needed, and the wordmark is drawn in it.
- **D5. Agent page and admin listing scope.** Both need new data code: a route plus `adminsOf()` event scanning. Include them in the first pass?
- **D6. "+1h" renew from the dashboard.** This is a new on-chain write the current UI doesn't have. Do you want it?
- **D7. Mockup sources.** Keep `mockups/_src/` + `build.py` in the repo (recommended: easy to iterate), or keep only the built HTML?
- **D8. How alive should app pages be?** The animated ASCII band on every page (as mocked), or animated on the landing page only with a still frame on app pages? *Recommendation:* animated, with no intro and paused off-screen. Switch to still frames if it costs too much on the demo laptop.

---

## 12. Mockup files

The `mockups/` folder:

| File | Screen |
|---|---|
| `index.html` | Landing: the 600 px `GradientASCII` hero (the user's props), three moves, and a numbered list of screens |
| `palette.html` | Palette study: directions A–D side by side, with B (Electric) picked, and its system (surfaces, blues, chips) |
| `access.html` | Access home (`?theme=dark` for dark; `?palette=0x35e7` opens ⌘K) |
| `give-access.html` | Give access sheet |
| `revoke.html` | Revoke confirm, live timeline, stepper toasts |
| `directory.html` | Directory master-detail (device `pi.home`) |
| `agent.html` | Agent detail (`claude-1`, org key loaded) |
| `activity.html` | Activity audit log |
| `billing.html` | Billing |
| `provider.html` | Provider |
| `settings.html` | Settings |
| `approve.html` | Approve an agent (`claude-2`) |
| `_src/` | Page bodies, `_theme.css` (the tokens), `_components.css` (CSS stand-ins for the shadcn components), `_shell.js` (theme toggle, palette, countdowns) and `_ascii.js` (a vanilla canvas stand-in for `GradientASCII`) |
| `build.py` | `python3 docs/ui-revamp/mockups/build.py` wraps each `_src/*.html` in the shared shell and inlines the real logo from `brand/svg/` |

Data sources: names, addresses, blocks (12,073,047 / 12,073,050 / 12,073,941), latencies (132 / 192 / 216 ms, 1.6 s after the block), seat counts, provider #1 and the admin `0xf846…Fc98` come from the live org and `docs/STATUS.md`. These are illustrative:
- SSH and host-key fingerprints
- the World ID code
- payment rows
- the `claude-2` request
- the decrypted folder path on the agent page
- clock times

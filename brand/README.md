# Meshy brand

<p>
  <img src="svg/meshy-lockup.svg" alt="Meshy logo" height="64">
</p>

The Meshy logo is an **M drawn as a network**. Five nodes joined by links make the letter, and each node type means something:

| Node | Shape | Meaning |
|---|---|---|
| Bottom left | Solid circle | Your device, where the mesh starts |
| Top left, center, bottom right | Rings | The devices and networks you link |
| Top right | Four-point spark, with a gap around it | The agent. It is part of the mesh, but it is held apart from it and reaches only what it is granted |

The gap around the spark is the key detail: it shows that agents get scoped, revocable access, not a seat equal to your own devices.

---

## Design principles

1. **One field, one subject.** The logo is always a single mark on a single flat color. No gradients, shadows, outlines or extra decoration.
2. **Every shape means something.** Nodes are devices, links are tunnels, the spark is the agent. Don't add nodes, links or sparks to make the mark "richer".
3. **The agent stays separate.** Never close the gap between the spark and its link. The gap is the product.
4. **Round and calm.** Round caps, round joins, circles. Meshy handles security, so the mark should feel steady rather than aggressive. No sharp corners except the spark's points.
5. **Built on a grid.** All geometry sits on a 100 × 100 unit grid (below). Scale the files; never redraw them by eye.
6. **Small first.** The mark has to work as a 16 px favicon, so detail stays minimal. Below 48 px, use the bold favicon cut.

---

## Files

| Folder | File | Use it for |
|---|---|---|
| `svg/` | `meshy-mark.svg` | The mark alone, cobalt, transparent background. Default on light backgrounds |
| | `meshy-mark-ink.svg` | Mark in ink, for single-color print or very formal contexts |
| | `meshy-mark-white.svg` | Mark in white, for dark backgrounds and photos |
| | `meshy-icon.svg` | App icon: white mark on a cobalt rounded square |
| | `meshy-icon-square.svg` | Square cobalt tile, for avatars (GitHub, X, Slack) that crop to a circle or rounded square themselves |
| | `meshy-lockup.svg` | Icon plus "meshy" wordmark, ink text, for light backgrounds |
| | `meshy-lockup-white.svg` | Icon plus wordmark, white text, for dark backgrounds |
| `favicon/` | `favicon.ico` | Classic favicon (16, 32 and 48 px in one file) |
| | `favicon.svg` | Modern browsers. Uses the bold cut |
| | `favicon-16.png`, `-32.png`, `-48.png` | PNG fallbacks |
| | `apple-touch-icon.png` | iOS home screen (180 px) |
| | `icon-192.png`, `icon-512.png` | Android and PWA icons |
| | `site.webmanifest` | Web app manifest that points at the icons above |
| `png/` | `meshy-icon-1024.png`, `meshy-icon-square-1024.png` | App stores, social cards, slide decks |
| | `meshy-mark-1024.png`, `meshy-mark-white-1024.png` | Mark alone, transparent |
| | `meshy-lockup.png`, `meshy-lockup-white.png` | Lockup at 6× for documents and slides |

All SVGs have only a `viewBox` (no fixed width or height), so size them with CSS or `width`/`height` attributes. Every cut-out (the ring holes and the spark gap) is a real mask, so the transparent marks work on any background.

---

## Color

| Token | Hex | Role |
|---|---|---|
| `--meshy-cobalt` | `#2B3FE0` | Primary. Icon tile, mark on light backgrounds, links and primary buttons |
| `--meshy-cobalt-300` | `#8C9BFF` | Cobalt for dark UI: links, focus rings and accents on ink |
| `--meshy-ink` | `#121417` | Text, dark backgrounds, single-color mark |
| `--meshy-paper` | `#F6F6F4` | Neutral page background |
| `--meshy-white` | `#FFFFFF` | Mark on cobalt, light backgrounds |

Contrast (WCAG):

| Pair | Ratio | OK for |
|---|---|---|
| White on cobalt | 7.27 : 1 | Any text size (AAA) |
| Cobalt on paper | 6.72 : 1 | Any text size (AA) |
| Ink on white | 18.45 : 1 | Any text size (AAA) |
| Cobalt 300 on ink | 7.25 : 1 | Any text size (AAA) |
| Cobalt on ink | 2.54 : 1 | **Don't use.** On dark backgrounds use the white mark, the cobalt icon tile, or Cobalt 300 |

```css
:root {
  --meshy-cobalt: #2B3FE0;
  --meshy-cobalt-300: #8C9BFF;
  --meshy-ink: #121417;
  --meshy-paper: #F6F6F4;
  --meshy-white: #FFFFFF;
}
```

---

## Typography

- **Wordmark:** "meshy", always lowercase, set in **Geist SemiBold (600)** with letter-spacing **−0.035em**. In the lockup files it is already converted to outlines, so no font is needed to display it.
- **Product UI and site:** Geist for text, Geist Mono for code, ENS names, hostnames and numbers. Both are free on Google Fonts and as the `geist` npm package.
- Write the name as **Meshy** in sentences and **meshy** only in the wordmark and CLI (`meshy connect`).

---

## Construction

Everything is drawn on a 100 × 100 unit grid.

| Element | Spec (regular cut) | Spec (bold / favicon cut) |
|---|---|---|
| Links | Path `M27 71 V31 L50 54 L73 31 V71`, stroke 5.5, round caps and joins | Same path, stroke 8 |
| Rings (top left 27,31 · center 50,54 · bottom right 73,71) | Radius 7, stroke 5.5 (hole radius 4.25) | Radius 7.5, stroke 8 |
| Solid node (bottom left 27,71) | Radius 9.75 | Radius 11.5 |
| Spark (top right 73,31) | Four points, reach 10.5, waist 2.0 | Reach 12.5, waist 2.4 |
| Spark gap | Circle radius 10 cut out of the links | Radius 12 |
| Icon tile | 100 × 100, corner radius 22 (or 0 for the square tile) | Same |

**Lockup:** icon tile height = *H*. Gap between tile and wordmark = 0.28 *H*. Wordmark font size = 0.8 *H*, with the x-height centered on the tile.

**Clear space:** keep at least the diameter of the solid node (about 0.2 × the mark's height) clear on every side. For the icon tile, keep 0.2 × the tile height.

**Minimum sizes**

| Asset | Smallest size |
|---|---|
| Mark, regular cut | 48 px (12 mm in print) |
| Mark or icon below 48 px | Use the bold favicon cut, down to 16 px |
| Lockup | 24 px tile height (wordmark ≈ 19 px) |

---

## Do and don't

**Do**
- Use the cobalt mark on white or paper, and the white mark on ink, cobalt or photos.
- Use the cobalt icon tile wherever the logo has to stand alone (app icons, avatars, favicons).
- Keep the logo flat and at the provided proportions.

**Don't**
- Put the cobalt mark on ink or other dark colors (too little contrast).
- Recolor single nodes, or make the spark a different color from the rest of the mark.
- Close the gap around the spark, or fill in the rings.
- Rotate, stretch, outline or add shadows or gradients.
- Re-set the wordmark in another typeface, in capitals, or with default letter-spacing.
- Rebuild the mark at small sizes by shrinking the regular cut below 48 px. Use the favicon cut.

---

## Using it in the web app (`web/`, Next.js App Router)

When you're ready to integrate:

1. Copy `favicon/favicon.ico` over `web/src/app/favicon.ico`.
2. Copy `favicon/favicon.svg` to `web/src/app/icon.svg` and `favicon/apple-touch-icon.png` to `web/src/app/apple-icon.png`. Next.js picks these up automatically.
3. Copy `favicon/icon-192.png`, `favicon/icon-512.png` and `favicon/site.webmanifest` into `web/public/`, then in `web/src/app/layout.tsx` add `manifest: "/site.webmanifest"` to `metadata` and export `viewport = { themeColor: "#2B3FE0" }` (Next 14+ moved `themeColor` out of `metadata`).
4. For the header logo, copy `svg/meshy-lockup.svg` (or `svg/meshy-lockup-white.svg` for a dark header) into `web/public/` and use `<img src="/meshy-lockup.svg" alt="Meshy" height={28} />`.
5. Add the color tokens above to `web/src/app/globals.css`.

// Mockup behaviour: theme toggle, ⌘K palette, live countdowns. Not production code.
(function () {
  const q = new URLSearchParams(location.search);
  const root = document.documentElement;
  let stored = null;
  try { stored = localStorage.getItem("meshy-mock-theme"); } catch (e) {}
  // ?theme= wins, then a saved choice, then the system setting (so the Mac app follows macOS)
  const sysDark = matchMedia("(prefers-color-scheme: dark)").matches;
  if (q.get("theme") === "dark" || (!q.get("theme") && (stored === "dark" || (!stored && sysDark)))) root.classList.add("dark");
  window.toggleTheme = () => {
    root.classList.toggle("dark");
    try { localStorage.setItem("meshy-mock-theme", root.classList.contains("dark") ? "dark" : "light"); } catch (e) {}
  };

  // ---- command palette (cmdk stand-in) ----
  const pal = document.getElementById("palette");
  if (pal) {
    const input = pal.querySelector("input");
    const items = [...pal.querySelectorAll("[data-item]")];
    const resolved = pal.querySelector("[data-resolve]");
    let idx = 0;
    const visible = () => items.filter((i) => !i.hidden);
    const paint = () => visible().forEach((el, i) => el.setAttribute("aria-selected", i === idx ? "true" : "false"));
    const filter = () => {
      const v = input.value.trim().toLowerCase();
      items.forEach((el) => (el.hidden = v && !el.dataset.item.toLowerCase().includes(v)));
      pal.querySelectorAll("[data-group]").forEach((g) => (g.hidden = ![...g.querySelectorAll("[data-item]")].some((i) => !i.hidden)));
      if (resolved) resolved.hidden = !/^(0x|claude|db1|pi|rig1|camera)/.test(v);
      idx = 0;
      paint();
    };
    window.openPalette = () => { pal.hidden = false; input.value = ""; filter(); setTimeout(() => input.focus(), 10); };
    window.closePalette = () => (pal.hidden = true);
    input.addEventListener("input", filter);
    input.addEventListener("keydown", (e) => {
      const n = visible().length;
      if (e.key === "ArrowDown") { idx = (idx + 1) % n; paint(); e.preventDefault(); }
      if (e.key === "ArrowUp") { idx = (idx - 1 + n) % n; paint(); e.preventDefault(); }
      if (e.key === "Enter") { const el = visible()[idx]; if (el?.href) location.href = el.href; }
    });
    pal.addEventListener("click", (e) => { if (e.target === pal || e.target.classList.contains("scrim")) closePalette(); });
    document.addEventListener("keydown", (e) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") { e.preventDefault(); pal.hidden ? openPalette() : closePalette(); }
      if (e.key === "Escape") closePalette();
    });
    if (q.get("palette")) { openPalette(); if (q.get("palette") !== "1") { input.value = q.get("palette"); filter(); } }
  }

  // ---- countdowns: <span data-left="3518"> and rings <circle data-ring data-left data-total> ----
  const t0 = Date.now();
  const fmt = (s) => {
    if (s <= 0) return "expired";
    const h = Math.floor(s / 3600), m = Math.floor((s % 3600) / 60), x = Math.floor(s % 60);
    return h ? `${h}h ${m}m` : m ? `${m}m ${String(x).padStart(2, "0")}s` : `${x}s`;
  };
  const tick = () => {
    const el = (Date.now() - t0) / 1000;
    document.querySelectorAll("[data-left]").forEach((n) => {
      const left = Math.max(0, +n.dataset.left - el);
      if (n.dataset.ring !== undefined) {
        const c = +n.getAttribute("stroke-dasharray");
        n.setAttribute("stroke-dashoffset", String(c * (1 - left / +n.dataset.total)));
        n.classList.toggle("low", left < 300);
      } else n.textContent = fmt(left);
    });
  };
  tick();
  setInterval(tick, 1000);

  if (window.lucide) lucide.createIcons({ attrs: { "stroke-width": 1.75 } });
})();

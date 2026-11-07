// Insights mockup behaviour: hover tooltips on every mark ([data-tip]), facet chip removal,
// "copy link" feedback, and the replay scrubber. Not production code.
(function () {
  const tip = document.createElement("div");
  tip.className = "tip";
  tip.hidden = true;
  document.body.appendChild(tip);
  const place = (e) => {
    const pad = 14, w = tip.offsetWidth, h = tip.offsetHeight;
    let x = e.clientX + pad, y = e.clientY + pad;
    if (x + w > innerWidth - 8) x = e.clientX - w - pad;
    if (y + h > innerHeight - 8) y = e.clientY - h - pad;
    tip.style.left = x + "px";
    tip.style.top = y + "px";
  };
  document.addEventListener("mouseover", (e) => {
    const t = e.target.closest("[data-tip]");
    if (!t) return;
    tip.innerHTML = t.dataset.tip;
    tip.hidden = false;
    place(e);
  });
  document.addEventListener("mousemove", (e) => { if (!tip.hidden) place(e); });
  document.addEventListener("mouseout", (e) => {
    const t = e.target.closest("[data-tip]");
    if (t && !t.contains(e.relatedTarget)) tip.hidden = true;
  });

  document.querySelectorAll(".chip .x").forEach((x) => x.addEventListener("click", () => x.closest(".chip").remove()));
  document.querySelectorAll("[data-copy]").forEach((b) =>
    b.addEventListener("click", () => {
      const old = b.textContent;
      try { navigator.clipboard.writeText(b.dataset.copy); } catch (e) {}
      b.textContent = "Copied";
      setTimeout(() => (b.textContent = old), 1200);
    }),
  );

  // replay: range input highlights transcript lines up to that step
  const rng = document.getElementById("replay-range");
  if (rng) {
    const lines = [...document.querySelectorAll("[data-step]")];
    const paint = () => lines.forEach((l) => {
      const s = +l.dataset.step, v = +rng.value;
      l.style.opacity = s <= v ? "1" : ".28";
      l.classList.toggle("cur", s === v);
    });
    rng.addEventListener("input", paint);
    paint();
  }
})();

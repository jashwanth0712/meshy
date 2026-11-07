// GradientASCII for static mockups: a dependency-free canvas take on the React
// <GradientASCII shape="horizon" …/> the user picked for the hero. Same prop names.
// Usage: <div data-ascii='{"cellSize":18,"lightMode":false}'></div>
(function () {
  const DEFAULTS = {
    colors: ["#1e3fd8", "#f6f8ff", "#a5d8ff", "#38bdf8"],
    shape: "horizon",
    characters: " =+xX0S8#@",
    cellSize: 18,
    density: 0.8,
    glyphOpacity: 0.85,
    glow: 0.2,
    flicker: 0.2,
    softness: 0.5,
    scale: 1,
    flow: 0.5,
    speed: 1,
    gradientOpacity: 0.9,
    grain: 0.08,
    mouseInteraction: true,
    scrambleRadius: 0.1,
    clickEffect: "wave",
    intro: true,
    lightMode: false,
    horizon: 0.72, // where the horizon line sits (0 top … 1 bottom)
  };
  const reduce = matchMedia("(prefers-reduced-motion: reduce)").matches;
  const hex = (h) => [1, 3, 5].map((i) => parseInt(h.slice(i, i + 2), 16));
  const mix = (a, b, t) => a.map((v, i) => v + (b[i] - v) * t);

  // smooth value noise
  const P = new Uint8Array(512);
  for (let i = 0; i < 256; i++) P[i] = i;
  for (let i = 255; i > 0; i--) { const j = (Math.random() * (i + 1)) | 0; [P[i], P[j]] = [P[j], P[i]]; }
  for (let i = 0; i < 256; i++) P[i + 256] = P[i];
  const fade = (t) => t * t * (3 - 2 * t);
  function noise(x, y) {
    const xi = Math.floor(x) & 255, yi = Math.floor(y) & 255, xf = x - Math.floor(x), yf = y - Math.floor(y);
    const h = (a, b) => P[P[a] + b] / 255;
    const u = fade(xf), v = fade(yf);
    return (h(xi, yi) * (1 - u) + h(xi + 1, yi) * u) * (1 - v) + (h(xi, yi + 1) * (1 - u) + h(xi + 1, yi + 1) * u) * v;
  }

  function mount(el) {
    const o = Object.assign({}, DEFAULTS, JSON.parse(el.dataset.ascii || "{}"));
    const stops = o.colors.map(hex);
    // gradient ramp: deep → bright at the horizon. Reorder so the brightest stop sits at the line.
    const Y = (c) => 0.2126 * c[0] + 0.7152 * c[1] + 0.0722 * c[2];
    const lum = stops.slice().sort((p, q) => Y(p) - Y(q));
    const rampL = (t) => {
      t = Math.min(1, Math.max(0, t)) * (lum.length - 1);
      const i = Math.min(lum.length - 2, Math.floor(t));
      return mix(lum[i], lum[i + 1], t - i);
    };
    const ramp = (t) => {
      t = Math.min(1, Math.max(0, t)) * (stops.length - 1);
      const i = Math.min(stops.length - 2, Math.floor(t));
      return mix(stops[i], stops[i + 1], t - i);
    };
    const canvas = document.createElement("canvas");
    canvas.setAttribute("aria-hidden", "true");
    canvas.style.cssText = "position:absolute;inset:0;width:100%;height:100%;display:block";
    el.prepend(canvas);
    const ctx = canvas.getContext("2d");
    const glowC = document.createElement("canvas"), gctx = glowC.getContext("2d");
    let W = 0, H = 0, cols = 0, rows = 0, dpr = 1, cell = o.cellSize;
    let chars = [], seeds = [];
    const pointer = { x: -1e4, y: -1e4, on: false };
    const waves = [];
    const t0 = performance.now();

    function resize() {
      const r = el.getBoundingClientRect();
      dpr = Math.min(2, devicePixelRatio || 1);
      W = r.width; H = r.height;
      canvas.width = glowC.width = Math.round(W * dpr);
      canvas.height = glowC.height = Math.round(H * dpr);
      cell = o.cellSize * o.scale;
      cols = Math.ceil(W / cell) + 1; rows = Math.ceil(H / cell) + 1;
      chars = new Array(cols * rows).fill(0);
      seeds = Array.from({ length: cols * rows }, () => Math.random());
    }

    // arc of a big "planet" whose rim is the horizon
    function rimY(x) {
      const R = o.arc || 1.6, cx = o.cx ?? 0.5, cy = o.horizon + R * 0.55, ax = (x - cx) * (W / Math.max(H, 1)) * 0.55;
      return cy - Math.sqrt(Math.max(0, (R * 0.55) ** 2 - ax * ax));
    }
    function field(x, y, t) {
      const dy = y - rimY(x);
      const soft = 0.18 + o.softness * 0.36;
      const n = noise(x * 7 + t * 0.25 * o.flow, y * 9 - t * 0.12 * o.flow);
      const n2 = noise(x * 23 - t * 0.4 * o.flow, y * 23 + 7);
      let v;
      if (dy < 0) v = Math.exp(-Math.pow(dy / soft, 2)) * (0.2 + 1.0 * n) + (n2 > 0.86 ? 0.35 * Math.exp(dy * 1.5) : 0);
      else v = Math.exp(-dy / 0.045) * (0.7 + 0.4 * n) + 0.32 * Math.exp(-dy / 0.3) * Math.pow(n, 1.5);
      return Math.min(1, Math.max(0, v + (n2 - 0.5) * 0.16 * v));
    }

    function draw(now) {
      const t = ((now - t0) / 1000) * o.speed;
      const intro = o.intro && !reduce ? Math.min(1, t / 1.8) : 1;
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
      gctx.setTransform(dpr, 0, 0, dpr, 0, 0);
      const L = o.lightMode;
      ctx.fillStyle = L ? "#f6f8ff" : "#040a22"; ctx.fillRect(0, 0, W, H);
      // sky wash
      const sky = ctx.createLinearGradient(0, 0, 0, H);
      const bright = lum[lum.length - 1], deep = lum[0], mid = lum[1];
      if (L) { sky.addColorStop(0, "rgba(246,248,255,1)"); sky.addColorStop(o.horizon, `rgba(${mid},${0.35 * o.gradientOpacity})`); sky.addColorStop(1, `rgba(${bright},1)`); }
      else { sky.addColorStop(0, "rgba(4,10,34,1)"); sky.addColorStop(Math.max(0, o.horizon - 0.2), `rgba(${deep},${0.45 * o.gradientOpacity})`); sky.addColorStop(o.horizon, `rgba(${mid},${0.55 * o.gradientOpacity})`); sky.addColorStop(1, `rgba(${deep},${0.9 * o.gradientOpacity})`); }
      ctx.fillStyle = sky; ctx.fillRect(0, 0, W, H);
      // rim glow along the horizon arc
      ctx.save();
      ctx.beginPath();
      for (let x = 0; x <= W; x += 8) { const y = rimY(x / W) * H; x ? ctx.lineTo(x, y) : ctx.moveTo(x, y); }
      ctx.lineTo(W, H); ctx.lineTo(0, H); ctx.closePath();
      const body = ctx.createLinearGradient(0, o.horizon * H - 0.1 * H, 0, H);
      body.addColorStop(0, L ? `rgba(${lum[2] || bright},0.45)` : `rgba(${lum[2] || bright},${0.85 * o.gradientOpacity})`);
      body.addColorStop(0.18, L ? `rgba(${mid},0.25)` : `rgba(${lum[1]},${0.8 * o.gradientOpacity})`);
      body.addColorStop(0.6, L ? "rgba(246,248,255,0.8)" : `rgba(${deep},0.9)`);
      body.addColorStop(1, L ? "rgba(246,248,255,1)" : "rgba(4,10,34,1)");
      ctx.fillStyle = body; ctx.fill();
      ctx.beginPath();
      for (let x = 0; x <= W; x += 8) { const y = rimY(x / W) * H; x ? ctx.lineTo(x, y) : ctx.moveTo(x, y); }
      ctx.strokeStyle = `rgba(${bright},${(L ? 0.7 : 0.95) * o.gradientOpacity * intro})`;
      ctx.lineWidth = 2.5; ctx.shadowColor = `rgba(${lum[2] || bright},0.9)`; ctx.shadowBlur = 40 * (0.5 + o.glow * 2);
      ctx.stroke(); ctx.stroke();
      ctx.restore();

      gctx.clearRect(0, 0, W, H);
      ctx.font = `${Math.round(cell * 0.74)}px "Geist Mono", ui-monospace, monospace`;
      gctx.font = ctx.font;
      ctx.textAlign = gctx.textAlign = "center";
      ctx.textBaseline = gctx.textBaseline = "middle";
      const cs = o.characters, nC = cs.length - 1;
      const pr = o.scrambleRadius * Math.max(W, 600);
      for (let r = 0; r < rows; r++) {
        for (let c = 0; c < cols; c++) {
          const i = r * cols + c, px = c * cell + cell / 2, py = r * cell + cell / 2;
          const x = px / W, y = py / H;
          let v = field(x, y, t);
          const reveal = Math.min(1, Math.max(0, (intro * 1.5 - Math.abs(y - rimY(x)) * 1.6) * 2.5));
          let scramble = reveal < 1 && reveal > 0;
          if (o.mouseInteraction && pointer.on) {
            const d = Math.hypot(px - pointer.x, py - pointer.y);
            if (d < pr) { scramble = true; v = Math.min(1, v + (1 - d / pr) * 0.45); }
          }
          for (const w of waves) {
            const rad = (now - w.t) * 0.7, d = Math.hypot(px - w.x, py - w.y);
            const k = Math.exp(-Math.pow((d - rad) / 30, 2)) * Math.max(0, 1 - (now - w.t) / 1600);
            if (k > 0.05) { v = Math.min(1, v + k * 0.7); scramble = true; }
          }
          v *= reveal;
          if (v * o.density < 0.07) continue;
          let idx = Math.min(nC, Math.max(1, Math.round(v * o.density * nC * 1.15)));
          if (scramble || (o.flicker && Math.random() < o.flicker * 0.015)) idx = 1 + ((Math.random() * nC) | 0);
          const col = rampL(L ? 0.15 + v * 0.3 : 0.15 + v * 0.85);
          const fl = o.flicker ? 1 - o.flicker * 0.5 * noise(c * 0.7 + t * 6, r * 0.7) : 1;
          const a = o.glyphOpacity * (L ? 0.18 + v * 0.5 : 0.18 + v * 0.82) * fl;
          ctx.fillStyle = `rgba(${col.map((n) => n | 0)},${a})`;
          ctx.fillText(cs[idx], px, py);
          if (o.glow && v > 0.5) { gctx.fillStyle = ctx.fillStyle; gctx.fillText(cs[idx], px, py); }
        }
      }
      if (o.glow) {
        ctx.save(); ctx.setTransform(1, 0, 0, 1, 0, 0);
        ctx.globalAlpha = Math.min(1, o.glow * 2.5); ctx.filter = `blur(${5 * dpr}px)`;
        ctx.globalCompositeOperation = L ? "multiply" : "lighter";
        ctx.drawImage(glowC, 0, 0); ctx.restore();
      }
      if (o.grain) {
        ctx.save(); ctx.setTransform(1, 0, 0, 1, 0, 0); ctx.globalAlpha = o.grain; ctx.globalCompositeOperation = "overlay";
        ctx.fillStyle = grainPattern || (grainPattern = makeGrain()); ctx.fillRect(0, 0, canvas.width, canvas.height); ctx.restore();
      }
      for (let k = waves.length - 1; k >= 0; k--) if (now - waves[k].t > 1700) waves.splice(k, 1);
    }

    let grainPattern = null;
    function makeGrain() {
      const n = document.createElement("canvas"); n.width = n.height = 128;
      const x = n.getContext("2d"), d = x.createImageData(128, 128);
      for (let i = 0; i < d.data.length; i += 4) { const v = Math.random() * 255; d.data[i] = d.data[i + 1] = d.data[i + 2] = v; d.data[i + 3] = 255; }
      x.putImageData(d, 0, 0);
      return ctx.createPattern(n, "repeat");
    }

    let last = 0, raf = 0, visible = true;
    function loop(now) {
      raf = requestAnimationFrame(loop);
      if (!visible || now - last < 33) return; // ~30 fps
      last = now; draw(now);
    }
    resize();
    new ResizeObserver(() => { resize(); if (reduce) draw(performance.now()); }).observe(el);
    new IntersectionObserver(([e]) => (visible = e.isIntersecting)).observe(el);
    if (o.mouseInteraction) {
      const host = el.closest("[data-ascii-host]") || el;
      host.addEventListener("pointermove", (e) => { const r = el.getBoundingClientRect(); pointer.x = e.clientX - r.left; pointer.y = e.clientY - r.top; pointer.on = true; });
      host.addEventListener("pointerleave", () => (pointer.on = false));
      if (o.clickEffect === "wave") host.addEventListener("pointerdown", (e) => { const r = el.getBoundingClientRect(); waves.push({ x: e.clientX - r.left, y: e.clientY - r.top, t: performance.now() }); });
    }
    if (reduce) draw(performance.now() + 5000); else raf = requestAnimationFrame(loop);
  }
  const start = () => document.querySelectorAll("[data-ascii]").forEach(mount);
  (document.fonts?.ready || Promise.resolve()).then(start);
})();

// Meshy "zoom into the mesh" scene. draw(clip, t) paints one frame.
const W = 1920, H = 1080, CX = 960, CY = 660;
const BG = '#0B0C0F', DOT = '#262B36', COB = '#2B3FE0', COB3 = '#8C9BFF', INK = '#F1F1EE', MUTED = '#9AA1AB', DIM = '#5A606A', OR = '#F2A65A', TUN = '#7F8EF0';
const WORLD = { s: 1664 / 360, lon: 10, lat: 11 };
const INDIA = { s: 72, lon: 76.4, lat: 17.0 };

const CITY = {
  mumbai: { lon: 72.88, lat: 19.08, label: 'Mumbai' },
  chennai: { lon: 80.27, lat: 13.08, label: 'Chennai' },
  dubai: { lon: 55.27, lat: 25.2, label: 'Dubai' },
  sf: { lon: -122.42, lat: 37.77, label: 'San Francisco' },
};
// devices: india ones use offsets (px at INDIA zoom) from their city; world ones use px offsets at world zoom
const DEV = [
  { id: 'mbp-m3', city: 'mumbai', kind: 'laptop', sel: true, ens: 'mbp-m3.mumbai.acme.eth', off: [-420, -95] },
  { id: 'mbp-m2', city: 'mumbai', kind: 'laptop', sel: false, off: [-220, -95] },
  { id: 'xps-13', city: 'mumbai', kind: 'laptop', sel: false, off: [-420, 75] },
  { id: 'office-streams', city: 'mumbai', kind: 'server', sel: true, ens: 'office-streams.mumbai.acme.eth', off: [-220, 75] },
  { id: 'gpu-4090', city: 'chennai', kind: 'server', sel: true, ens: 'gpu-4090.chennai.acme.eth', off: [60, -180] },
  { id: 'rw-rtx-3070-ti', city: 'chennai', kind: 'server', sel: false, off: [260, -180] },
  { id: 'cpu-vm', city: 'chennai', kind: 'server', sel: true, ens: 'cpu-vm.chennai.acme.eth', off: [460, -180] },
  { id: 'rw-rtx-4070-12gb', city: 'dubai', kind: 'server', sel: true, ens: 'rtx-4070.dubai.acme.eth', woff: [-17, -40] },
  { id: 'rw-rtx-4060-16gb', city: 'dubai', kind: 'server', sel: false, woff: [17, -40] },
  { id: 'alice', city: 'sf', kind: 'person', sel: true, ens: 'alice.people.acme.eth', woff: [-17, -40] },
  { id: 'mbp-alice', city: 'sf', kind: 'laptop', sel: false, woff: [17, -40], hide: true },
];
// mesh layout (hexagon) for the selected six
const MESH = {
  'alice': [960, 420], 'rw-rtx-4070-12gb': [1380, 545], 'cpu-vm': [1380, 805],
  'gpu-4090': [960, 920], 'office-streams': [540, 805], 'mbp-m3': [540, 545],
};
const AGENT = [250, 930];
const ACTORS = [
  { id: 'lens-bot', kind: 'agent', at: [230, 690], name: 'lens-bot.agents.acme.eth', role: 'vision agent', to: 'office-streams', label: 'camera port · 30 min', lab: [446, 704] },
  { id: 'analyst', kind: 'agent', at: [1700, 700], name: 'analyst.agents.acme.eth', role: 'data agent', to: 'cpu-vm', label: '/warehouse : ro · 4 h', lab: [1490, 716] },
  { id: 'ci-bot', kind: 'agent', at: [1500, 985], name: 'ci-bot.agents.acme.eth', role: 'deploy agent', to: 'gpu-4090', label: 'train.sh only · 15 min', lab: [1270, 1046], right: true },
  { id: 'bob', kind: 'person', at: [230, 440], name: 'bob.people.acme.eth', role: 'on-call admin', to: 'mbp-m3', label: 'admin · mumbai', dash: true, lab: [378, 574] },
  { id: 'ml-team', kind: 'team', at: [1720, 410], name: 'ml-team.teams.acme.eth', role: 'team · roadmap', to: 'rw-rtx-4070-12gb', label: 'team · rw · 8 h', lab: [1478, 452], members: [['priya', [1610, 342]], ['arjun', [1830, 342]]] },
];

const clamp = (x, a = 0, b = 1) => Math.max(a, Math.min(b, x));
const lerp = (a, b, t) => a + (b - a) * t;
const ease = t => { t = clamp(t); return t < 0.5 ? 4 * t * t * t : 1 - Math.pow(-2 * t + 2, 3) / 2; };
const ramp = (t, a, b) => ease((t - a) / (b - a));

function cam(e) { // e: 0 world -> 1 india; India's centre glides to screen centre while the scale grows
  const s = Math.exp(lerp(Math.log(WORLD.s), Math.log(INDIA.s), e));
  const f0x = CX + (INDIA.lon - WORLD.lon) * WORLD.s, f0y = CY - (INDIA.lat - WORLD.lat) * WORLD.s;
  const k = (s - WORLD.s) / (INDIA.s - WORLD.s);           // 0..1, linear in scale
  const fx = lerp(f0x, CX, k), fy = lerp(f0y, CY, k);
  return { s, lon: INDIA.lon - (fx - CX) / s, lat: INDIA.lat + (fy - CY) / s };
}
const proj = (c, lon, lat) => [CX + (lon - c.lon) * c.s, CY - (lat - c.lat) * c.s];

let DOTS = null;
function setDots(d) { DOTS = d; }

function drawDots(g, c, alpha, glow) {
  if (alpha <= 0) return;
  const zr = c.s / WORLD.s;
  const r = Math.min(1.7 * Math.pow(zr, 0.33), 3.4);
  const fineA = clamp((c.s - 14) / 22);
  const gscale = Math.max(0.28, Math.pow(WORLD.s / c.s, 0.6));
  const topFade = clamp((c.s - 8) / 10);
  const paint = (pts, a, fine) => {
    for (const [lon, lat] of pts) {
      const [x, y] = proj(c, lon, lat);
      if (x < -10 || x > W + 10 || y < 300 || y > H + 10) continue;
      let col = DOT, aa = a * lerp(1, clamp((y - 300) / 130), topFade);
      if (fine) aa *= clamp(Math.min(lon - 58.2, 97.2 - lon, lat - 3.3, 30.3 - lat) / 6);
      if (glow) { // tint dots near highlighted places
        let best = 0;
        for (const gp of glow) { const d = Math.hypot(lon - gp.lon, lat - gp.lat); best = Math.max(best, gp.w * clamp(1 - d / (gp.r * gscale))); }
        if (best > 0.02) { col = mix('#262B36', '#3346E6', best); aa = aa * (1 + best * 0.4); }
      }
      if (aa <= 0.01) continue;
      g.globalAlpha = clamp(aa);
      g.fillStyle = col;
      g.beginPath(); g.arc(x, y, r, 0, 6.2832); g.fill();
    }
  };
  paint(DOTS.world, alpha, false);
  if (fineA > 0) paint(DOTS.fine, alpha * fineA, true);
  g.globalAlpha = 1;
}
function mix(a, b, t) {
  const p = h => [1, 3, 5].map(i => parseInt(h.substr(i, 2), 16));
  const A = p(a), B = p(b);
  return '#' + A.map((v, i) => Math.round(lerp(v, B[i], t)).toString(16).padStart(2, '0')).join('');
}

// ---------- glyphs ----------
function icon(g, kind, x, y, sz, mode, a = 1) {
  // mode: 'idle' | 'sel' | 'off'
  if (a <= 0) return;
  g.save(); g.globalAlpha = a; g.translate(x, y);
  const R = sz * 0.78;
  if (mode === 'sel') {
    g.shadowColor = 'rgba(60,85,255,0.75)'; g.shadowBlur = sz * 0.9;
    g.fillStyle = COB; g.beginPath(); g.arc(0, 0, R, 0, 6.2832); g.fill();
    g.shadowBlur = 0;
  } else {
    g.fillStyle = mode === 'off' ? '#14161B' : '#141A2E';
    g.strokeStyle = mode === 'off' ? '#30343C' : '#3A4AB8';
    g.lineWidth = Math.max(1.5, sz * 0.05);
    g.beginPath(); g.arc(0, 0, R, 0, 6.2832); g.fill(); g.stroke();
  }
  const col = mode === 'sel' ? '#FFFFFF' : mode === 'off' ? '#4A505A' : COB3;
  g.strokeStyle = col; g.fillStyle = col; g.lineWidth = Math.max(1.6, sz * 0.07); g.lineJoin = 'round'; g.lineCap = 'round';
  const u = sz * 0.4;
  if (kind === 'server') {
    rr(g, -u * 0.75, -u, u * 1.5, u * 2, u * 0.25); g.stroke();
    for (let i = 0; i < 3; i++) { const yy = -u * 0.5 + i * u * 0.55; g.beginPath(); g.moveTo(-u * 0.4, yy); g.lineTo(u * 0.15, yy); g.stroke(); g.beginPath(); g.arc(u * 0.42, yy, u * 0.08, 0, 6.2832); g.fill(); }
  } else if (kind === 'laptop') {
    rr(g, -u * 0.95, -u * 0.75, u * 1.9, u * 1.25, u * 0.15); g.stroke();
    g.beginPath(); g.moveTo(-u * 1.25, u * 0.78); g.lineTo(u * 1.25, u * 0.78); g.stroke();
  } else if (kind === 'person') {
    g.beginPath(); g.arc(0, -u * 0.45, u * 0.42, 0, 6.2832); g.fill();
    g.beginPath(); g.moveTo(-u * 0.85, u * 0.95); g.quadraticCurveTo(-u * 0.85, u * 0.12, 0, u * 0.12); g.quadraticCurveTo(u * 0.85, u * 0.12, u * 0.85, u * 0.95); g.closePath(); g.fill();
  } else if (kind === 'team') {
    for (const [dx, dy, sc] of [[-u * 0.62, u * 0.18, 0.62], [u * 0.62, u * 0.18, 0.62], [0, -u * 0.05, 0.8]]) {
      g.beginPath(); g.arc(dx, dy - u * 0.45 * sc, u * 0.36 * sc, 0, 6.2832); g.fill();
      g.beginPath(); g.moveTo(dx - u * 0.75 * sc, dy + u * 0.85 * sc); g.quadraticCurveTo(dx - u * 0.75 * sc, dy + u * 0.05 * sc, dx, dy + u * 0.05 * sc); g.quadraticCurveTo(dx + u * 0.75 * sc, dy + u * 0.05 * sc, dx + u * 0.75 * sc, dy + u * 0.85 * sc); g.closePath(); g.fill();
    }
  } else if (kind === 'agent') {
    const w = u * 0.24, r = u * 1.05;
    g.beginPath(); g.moveTo(0, -r); g.lineTo(w, -w); g.lineTo(r, 0); g.lineTo(w, w); g.lineTo(0, r); g.lineTo(-w, w); g.lineTo(-r, 0); g.lineTo(-w, -w); g.closePath(); g.fill();
  }
  g.restore();
}
function rr(g, x, y, w, h, r) { g.beginPath(); g.moveTo(x + r, y); g.arcTo(x + w, y, x + w, y + h, r); g.arcTo(x + w, y + h, x, y + h, r); g.arcTo(x, y + h, x, y, r); g.arcTo(x, y, x + w, y, r); g.closePath(); }
function text(g, s, x, y, { size = 22, color = MUTED, font = 'Geist', weight = 400, align = 'center', a = 1, base = 'alphabetic', halo = false } = {}) {
  if (a <= 0) return;
  g.save(); g.globalAlpha = a; g.fillStyle = color; g.textAlign = align; g.textBaseline = base;
  g.font = `${weight} ${size}px "${font}"`; if (halo) { g.strokeStyle = BG; g.lineWidth = 8; g.lineJoin = 'round'; g.strokeText(s, x, y); } g.fillText(s, x, y); g.restore();
}
function pin(g, x, y, a, pulse = 0) {
  if (a <= 0) return;
  g.save(); g.globalAlpha = a;
  if (pulse > 0) { g.strokeStyle = `rgba(140,155,255,${0.5 * (1 - pulse)})`; g.lineWidth = 2; g.beginPath(); g.arc(x, y, 8 + pulse * 26, 0, 6.2832); g.stroke(); }
  g.fillStyle = COB3; g.beginPath(); g.arc(x, y, 5, 0, 6.2832); g.fill();
  g.strokeStyle = 'rgba(140,155,255,0.45)'; g.lineWidth = 2; g.beginPath(); g.arc(x, y, 11, 0, 6.2832); g.stroke();
  g.restore();
}
function capsule(g, a, b, w, color, alpha) {
  if (alpha <= 0) return;
  g.save(); g.globalAlpha = alpha; g.strokeStyle = color; g.lineWidth = w; g.lineCap = 'round';
  g.beginPath(); g.moveTo(a[0], a[1]); g.lineTo(b[0], b[1]); g.stroke(); g.restore();
}
function seg(g, a, b, p, color, w, alpha = 1, dash = null) {
  if (p <= 0 || alpha <= 0) return;
  g.save(); g.globalAlpha = alpha; g.strokeStyle = color; g.lineWidth = w; g.lineCap = 'round';
  if (dash) g.setLineDash(dash);
  g.beginPath(); g.moveTo(a[0], a[1]); g.lineTo(lerp(a[0], b[0], p), lerp(a[1], b[1], p)); g.stroke(); g.restore();
}

// ---------- state helpers ----------
function cityXY(c, k) { const p = CITY[k]; return proj(c, p.lon, p.lat); }
function devXY(c, d) {
  const [x, y] = cityXY(c, d.city);
  if (d.off) { const k = c.s / INDIA.s; return [x + d.off[0] * k, y + d.off[1] * k]; }
  return [x + d.woff[0], y + d.woff[1]];
}
const indiaZoomVis = c => clamp((c.s - 26) / 30);       // india devices visible when zoomed
const worldVis = c => clamp(1 - (c.s - WORLD.s) / 6);     // world-scale pins/labels

function drawWorldLayer(g, c, t, o) {
  const wv = worldVis(c) * (o.worldA ?? 1);
  // city pins & labels (world scale)
  const india = proj(c, 76.6, 16.1);
  const pins = [['sf', o.pinA?.sf ?? 1], ['dubai', o.pinA?.dubai ?? 1]];
  for (const [k, pa] of pins) { const [x, y] = cityXY(c, k); pin(g, x, y, wv * pa, o.pulse ?? 0); }
  pin(g, india[0], india[1], wv * (o.pinA?.india ?? 1), o.pulse ?? 0);
  // labels
  const la = wv * (o.labelA ?? 1);
  { const [x, y] = cityXY(c, 'sf'); text(g, 'San Francisco', x, y + 40, { a: la, size: 22, color: INK, weight: 600 }); text(g, o.sfSub ?? '1 person · laptop', x, y + 66, { a: la, size: 20 }); }
  { const [x, y] = cityXY(c, 'dubai'); text(g, 'Dubai', x - 36, y + 40, { a: la, size: 22, color: INK, weight: 600 }); text(g, o.dubaiSub ?? '2 servers', x - 36, y + 66, { a: la, size: 20 }); }
  text(g, 'India', india[0] + 34, india[1] + 40, { a: la, size: 22, color: INK, weight: 600 });
  text(g, o.indiaSub ?? '2 networks · 7 devices', india[0] + 34, india[1] + 66, { a: la, size: 20, color: o.indiaSubColor ?? MUTED });
  // world-scale device icons (Dubai, SF)
  for (const d of DEV) {
    if (d.off || d.hide) continue;
    const [x, y] = devXY(c, d);
    const mode = o.mode?.[d.id] ?? 'idle';
    icon(g, d.kind, x, y, 26, mode, wv * (o.devA ?? 1) * (o.devAlpha?.[d.id] ?? 1));
  }
  // mbp-alice shown as a laptop next to alice in world view
  { const d = DEV.find(e => e.id === 'mbp-alice'); const [x, y] = devXY(c, d); icon(g, 'laptop', x, y, 26, o.mode?.['mbp-alice'] ?? 'idle', wv * (o.devA ?? 1) * (o.devAlpha?.['mbp-alice'] ?? 1)); }
}

function drawIndiaLayer(g, c, o) {
  const iv = indiaZoomVis(c) * (o.indiaA ?? 1);
  if (iv <= 0) return;
  const k = c.s / INDIA.s;
  // network halos
  for (const [city, name, ens, box] of [['mumbai', 'Mumbai network', 'mumbai.acme.eth', [-520, -170, 400, 360]], ['chennai', 'Chennai network', 'chennai.acme.eth', [-45, -255, 610, 175]]]) {
    const [x, y] = cityXY(c, city);
    g.save(); g.globalAlpha = iv * 0.9; g.setLineDash([6, 8]); g.strokeStyle = '#3A4AB8'; g.lineWidth = 2;
    rr(g, x + box[0] * k, y + box[1] * k, box[2] * k, box[3] * k, 22 * k); g.stroke(); g.restore();
    const lab = (o.named ?? 0);
    text(g, name, x + box[0] * k + 18, y + box[1] * k - 14, { a: iv * (1 - lab), size: 22, color: INK, weight: 600, align: 'left' });
    text(g, ens, x + box[0] * k + 18, y + box[1] * k - 14, { a: iv * lab, size: 22, color: COB3, weight: 600, align: 'left', font: 'Geist Mono' });
    pin(g, x, y, iv, 0);
    text(g, CITY[city].label, x + 18, y + 7, { a: iv * 0.9, size: 20, color: MUTED, align: 'left' });
  }
  for (const d of DEV) {
    if (!d.off) continue;
    const [x, y] = devXY(c, d);
    const mode = o.mode?.[d.id] ?? 'idle';
    const da = iv * (o.devAlpha?.[d.id] ?? 1);
    icon(g, d.kind, x, y, 44 * Math.min(1, k), mode, da);
    const nameA = d.sel ? (o.nameA?.[d.id] ?? 0) : 0;
    text(g, d.id, x, y + 58 * k, { a: da, size: 20, color: mode === 'off' ? DIM : (nameA > 0.5 ? INK : MUTED), font: 'Geist Mono', weight: nameA > 0.5 ? 600 : 400 });
    if (d.sel) text(g, '.' + d.city + '.acme.eth', x, y + 84 * k, { a: da * nameA, size: 20, color: COB3, font: 'Geist Mono' });
  }
}

// ---------- clips ----------
const CLIPS = [
  { len: 2.8 }, { len: 3.4 }, { len: 4.8 }, { len: 4.4 }, { len: 3.6 }, { len: 2.2 }, { len: 2.2 }, { len: 2.2 }, { len: 2.2 }, { len: 2.2 }, { len: 4.0 }, { len: 3.8 }, { len: 4.4 },
];
function draw(g, clip, t) {
  g.fillStyle = BG; g.fillRect(0, 0, W, H);
  const SELMODE = Object.fromEntries(DEV.map(d => [d.id, d.sel ? 'sel' : 'off']));
  const glowAll = [{ lon: 72.88, lat: 19.08, r: 9, w: 0.55 }, { lon: 80.27, lat: 13.08, r: 9, w: 0.55 }, { lon: 55.27, lat: 25.2, r: 8, w: 0.5 }, { lon: -122.42, lat: 37.77, r: 10, w: 0.5 }];

  if (clip === 0) {
    const c = cam(0);
    // dots sweep in from left
    const sweep = ramp(t, 0, 1.6);
    const g2 = g; g2.save(); g2.beginPath(); g2.rect(0, 0, 160 + sweep * (W + 200), H); g2.clip();
    drawDots(g, c, 1, glowAll.map(p => ({ ...p, w: p.w * ramp(t, 1.4, 2.4) })));
    g2.restore();
    drawWorldLayer(g, c, t, {
      pinA: { sf: ramp(t, 1.0, 1.4), dubai: ramp(t, 1.25, 1.65), india: ramp(t, 1.5, 1.9) },
      labelA: ramp(t, 1.7, 2.4), devA: ramp(t, 1.9, 2.6), pulse: clamp((t - 2.0) / 0.8) % 1,
    });
  }
  if (clip === 1) {
    const e = ramp(t, 0.3, 3.0);
    const c = cam(e);
    drawDots(g, c, 1, glowAll);
    drawWorldLayer(g, c, t, { labelA: 1 - ramp(t, 0.2, 0.7) });
    drawIndiaLayer(g, c, { indiaA: ramp(t, 2.2, 3.2) });
  }
  if (clip === 2) {
    // pick + name at india zoom, then zoom out
    const e = 1 - ramp(t, 1.8, 4.0);
    const c = cam(e);
    const pick = ramp(t, 0.1, 0.9);
    const mode = {}, devAlpha = {}, nameA = {};
    for (const d of DEV) {
      mode[d.id] = pick > 0.5 ? SELMODE[d.id] : 'idle';
      devAlpha[d.id] = d.sel ? 1 : lerp(1, 0.55, pick);
    }
    let i = 0; for (const d of DEV) if (d.off && d.sel) { nameA[d.id] = ramp(t, 0.5 + i * 0.18, 1.1 + i * 0.18); i++; }
    drawDots(g, c, 1, glowAll);
    drawIndiaLayer(g, c, { mode, devAlpha, nameA, named: ramp(t, 1.0, 1.6) });
    const wl = ramp(t, 3.4, 4.4);
    const wmode = { 'rw-rtx-4070-12gb': wl > 0.4 ? 'sel' : 'idle', 'rw-rtx-4060-16gb': wl > 0.4 ? 'off' : 'idle', 'alice': wl > 0.4 ? 'sel' : 'idle', 'mbp-alice': wl > 0.4 ? 'off' : 'idle' };
    drawWorldLayer(g, c, t, { labelA: wl, mode: wmode, devAlpha: { 'rw-rtx-4060-16gb': lerp(1, 0.55, wl), 'mbp-alice': lerp(1, 0.55, wl) },
      indiaSub: '4 picked · named', indiaSubColor: COB3, dubaiSub: 'rtx-4070.dubai', sfSub: 'alice.people' });
  }
  if (clip >= 3) {
    const c = cam(0);
    const p = clip === 3 ? t : 99;
    const t5 = clip === 5 ? t : -1;
    const m = 0;
    const MC = [960, 670];
    // acme's mesh shrinks into a cluster for the provider / ENS / billing scenes
    let zk = 1, zc = MC, ze = 0;
    if (clip === 10) { ze = ramp(t, 0.4, 1.6); zk = lerp(1, 0.55, ze); zc = [lerp(960, 560, ze), lerp(670, 700, ze)]; }
    if (clip === 11) { const f = ramp(t, 0, 1.2); ze = 1; zk = lerp(0.55, 0.36, f); zc = [lerp(560, 330, f), lerp(700, 880, f)]; }
    if (clip === 12) { ze = 1; zk = 0.36; zc = [330, 880]; }
    const M = id => [zc[0] + (MESH[id][0] - MC[0]) * zk, zc[1] + (MESH[id][1] - MC[1]) * zk];
    const mapA = 1 - ramp(p, 0.1, 1.2);
    if (mapA > 0) {
      drawDots(g, c, mapA, glowAll);
      drawWorldLayer(g, c, p, { worldA: mapA, labelA: 1, devA: 1, mode: { 'rw-rtx-4070-12gb': 'sel', 'rw-rtx-4060-16gb': 'off', 'alice': 'sel', 'mbp-alice': 'off' },
        devAlpha: { 'rw-rtx-4070-12gb': 0, 'alice': 0, 'rw-rtx-4060-16gb': 0.55, 'mbp-alice': 0.55 }, indiaSub: '4 picked · named', indiaSubColor: COB3, dubaiSub: 'rtx-4070.dubai', sfSub: 'alice.people' });
    }
    // the six named devices travel to the mesh
    const india = proj(c, 76.6, 16.1);
    const pos = {};
    let i = 0;
    for (const d of DEV) {
      if (!d.sel) continue;
      const start = d.off ? [india[0] + (i % 2 ? 10 : -10), india[1]] : devXY(c, d);
      const f = ramp(p, 0.4 + i * 0.08, 2.4 + i * 0.08);
      const end = M(d.id);
      pos[d.id] = [lerp(start[0], end[0], f), lerp(start[1], end[1], f)];
      pos[d.id].f = f; i++;
    }
    // how dimmed the mesh is: dims for the single agent (step 5), brightens again as everyone arrives (step 6)
    const dim = clip === 4 ? ramp(t, 0.2, 0.9) : clip >= 5 && clip <= 9 ? 1 : clip === 10 ? 1 - ze : 0;
    const fadeNames = clip === 10 ? 1 - ze : clip > 10 ? 0 : 1;
    const capA = ramp(p, 3.0, 3.8) * lerp(1, 0.6, clip >= 10 ? 1 : dim) * fadeNames;
    capsule(g, M('mbp-m3'), M('office-streams'), 150 * zk, '#2B3FE0', 0.1 * capA);
    capsule(g, M('cpu-vm'), M('gpu-4090'), 150 * zk, '#2B3FE0', 0.1 * capA);
    capsule(g, M('rw-rtx-4070-12gb'), M('rw-rtx-4070-12gb'), 150 * zk, '#2B3FE0', 0.1 * capA);
    capsule(g, M('alice'), M('alice'), 150 * zk, '#2B3FE0', 0.1 * capA);
    text(g, 'mumbai.acme.eth', M('mbp-m3')[0], M('mbp-m3')[1] - 92 * zk, { a: capA, size: 22, color: COB3, font: 'Geist Mono', halo: true });
    text(g, 'chennai.acme.eth', M('cpu-vm')[0], M('cpu-vm')[1] + 108, { a: capA, size: 22, color: COB3, font: 'Geist Mono', halo: true });
    text(g, 'dubai.acme.eth', M('rw-rtx-4070-12gb')[0] + lerp(92, 0, m), M('rw-rtx-4070-12gb')[1] + lerp(8, -84, m), { a: capA, size: 22, color: COB3, font: 'Geist Mono', align: m > 0.5 ? 'center' : 'left', halo: true });
    text(g, 'people.acme.eth', M('alice')[0] + lerp(92, 0, m), M('alice')[1] + lerp(8, -84, m), { a: capA, size: 22, color: COB3, font: 'Geist Mono', align: m > 0.5 ? 'center' : 'left', halo: true });
    // mesh lines
    const ids = Object.keys(MESH);
    const lineP = ramp(p, 2.4, 3.6);
    const lineA = clip >= 10 ? lerp(0.28, 0.5, ze) : lerp(0.85, 0.28, dim);
    let n = 0;
    for (let a = 0; a < ids.length; a++) for (let b = a + 1; b < ids.length; b++) {
      seg(g, M(ids[a]), M(ids[b]), clamp(lineP * 1.6 - n * 0.04), TUN, lerp(1.4, 2.2, zk), lineA); n++;
    }
    // one actor per step from clip 5 on: the previous one fades away as the next arrives
    const cur = clip >= 5 && clip <= 9 ? ACTORS[clip - 5] : null;
    const prev = clip >= 6 && clip <= 10 ? ACTORS[clip - 6] : null;
    const out = clip >= 5 ? 1 - ramp(t, 0, 0.6) : 1;   // previous actor (claude-1 for clip 5)
    const brighten = {};
    if (cur) brighten[cur.to] = ramp(t, 1.2, 1.6);
    if (prev) brighten[prev.to] = Math.max(brighten[prev.to] ?? 0, out);
    if (clip === 5) brighten['gpu-4090'] = Math.max(brighten['gpu-4090'] ?? 0, out);
    const drawActor = (A, a, lp, la) => {
      if (!A || a <= 0) return;
      const B = M(A.to);
      const dx = B[0] - A.at[0], dy = B[1] - A.at[1], L = Math.hypot(dx, dy), ux = dx / L, uy = dy / L;
      const s0 = [A.at[0] + ux * 46, A.at[1] + uy * 46], s1 = [B[0] - ux * 44, B[1] - uy * 44];
      seg(g, s0, s1, lp, A.dash ? '#C9CDD2' : COB3, A.dash ? 2.5 : 4, a, A.dash ? [7, 8] : null);
      text(g, A.label, A.lab[0], A.lab[1], { a: a * la, size: 22, color: A.dash ? '#C9CDD2' : COB3, font: 'Geist Mono', weight: 600, halo: true });
      if (A.members) for (const [mn, mp] of A.members) {
        seg(g, mp, A.at, 1, '#3A4AB8', 2, a);
        icon(g, 'person', mp[0], mp[1], 34, 'sel', a);
        text(g, mn, mp[0], mp[1] - 34, { a, size: 20, color: INK, font: 'Geist Mono', weight: 600, halo: true });
      }
      icon(g, A.kind, A.at[0], A.at[1], 58, 'sel', a);
      if (A.right) {
        text(g, A.name, A.at[0] + 58, A.at[1] + 2, { a, size: 22, color: INK, font: 'Geist Mono', weight: 600, align: 'left', halo: true });
        text(g, A.role, A.at[0] + 58, A.at[1] + 30, { a, size: 20, color: MUTED, align: 'left', halo: true });
      } else {
        text(g, A.name, A.at[0], A.at[1] + 76, { a, size: 22, color: INK, font: 'Geist Mono', weight: 600, halo: true });
        text(g, A.role, A.at[0], A.at[1] + 104, { a, size: 20, color: MUTED, halo: true });
      }
    };
    drawActor(prev, out, 1, 1);
    drawActor(cur, cur ? ramp(t, 0.5, 0.9) : 0, cur ? ramp(t, 0.7, 1.3) : 0, cur ? ramp(t, 1.2, 1.6) : 0);
    // nodes + names
    for (const d of DEV) {
      if (!d.sel) continue;
      const [x, y] = pos[d.id];
      const f = pos[d.id].f;
      const sz = lerp(d.off ? 30 : 26, 58, f) * zk;
      let dimNode = 1;
      if (clip === 4 && d.id !== 'gpu-4090') dimNode = lerp(1, 0.55, ramp(t, 0.2, 0.9));
      if (clip >= 5 && clip <= 9) dimNode = lerp(0.55, 1, brighten[d.id] ?? 0);
      if (clip === 10) dimNode = lerp(lerp(0.55, 1, brighten[d.id] ?? 0), 1, ze);
      icon(g, d.kind, x, y, sz, 'sel', dimNode * (d.off ? clamp(f * 4) : 1));
      text(g, d.ens, x, y + 76, { a: ramp(p, 2.2, 3.0) * dimNode * fadeNames, size: lerp(22, 20, m), color: INK, font: 'Geist Mono', weight: 600, halo: true });
    }
    if (clip >= 10) drawEco(g, clip, t, zc, zk);
    if (clip === 4 || clip === 5) {
      const ag = clip === 4 ? ramp(t, 0.4, 1.1) : out;
      icon(g, 'agent', AGENT[0], AGENT[1], 58 * lerp(1, 0.97, m), 'sel', ag);
      text(g, 'claude-1.agents.acme.eth', AGENT[0], AGENT[1] + 76, { a: ag, size: 22, color: INK, font: 'Geist Mono', weight: 600, halo: true });
      const sw = 0;
      text(g, 'human-approved · World ID', AGENT[0], AGENT[1] + 104, { a: ag * (1 - sw), size: 20, color: MUTED });
      text(g, 'coding agent', AGENT[0], AGENT[1] + 104, { a: ag * sw, size: 20, color: MUTED });
      // grant line to gpu-4090
      const G = M('gpu-4090');
      const A = [AGENT[0] + 50, AGENT[1] - 4], B = [G[0] - 46, G[1] + 2];
      const tg = clip === 4 ? t : 99;
      seg(g, A, B, ramp(tg, 1.1, 2.0), COB3, 5, ag);
      text(g, '/models : ro · 1 h', (A[0] + B[0]) / 2, (A[1] + B[1]) / 2 + 44, { a: ramp(tg, 1.8, 2.4) * ag, size: 22, color: COB3, font: 'Geist Mono', weight: 600, halo: true });
      // refused attempt toward mbp-m3 (fades out when everyone else arrives)
      const ra = clip === 5 ? out : 1;
      const C = [AGENT[0] + 30, AGENT[1] - 44], D = [MESH['mbp-m3'][0] - 30, MESH['mbp-m3'][1] + 40];
      const q = ramp(tg, 2.3, 2.9);
      seg(g, C, D, q * 0.55, OR, 3, ra, [3, 9]);
      if (tg > 2.85 && ra > 0) {
        const X = [lerp(C[0], D[0], 0.58), lerp(C[1], D[1], 0.58)];
        const xa = ramp(tg, 2.85, 3.2) * ra;
        g.save(); g.globalAlpha = xa; g.strokeStyle = OR; g.lineWidth = 4.5; g.lineCap = 'round';
        g.beginPath(); g.moveTo(X[0] - 11, X[1] - 11); g.lineTo(X[0] + 11, X[1] + 11); g.moveTo(X[0] - 11, X[1] + 11); g.lineTo(X[0] + 11, X[1] - 11); g.stroke(); g.restore();
        text(g, 'MESHY_NO_GRANT', X[0] - 24, X[1] + 8, { a: xa, size: 22, color: OR, font: 'Geist Mono', weight: 600, align: 'right', halo: true });
      }
    }
  }
}

// ---------- provider · ENS · billing (clips 10-12) ----------
const GREEN = '#34C77B', GREEN_GLOW = 'rgba(52,199,123,0.65)';
const ORGS = [
  { name: 'lumen.eth', x: 760, color: '#5468F0', seats: 8 },
  { name: 'orbit.eth', x: 1160, color: '#7B89F5', seats: 3 },
  { name: 'kite.eth', x: 1590, color: '#3D4FC4', seats: 4 },
];
const ORG_Y = 880, ENS_Y0 = 560, ENS_Y1 = 660, ESCROW = [1420, 395], TREASURY = [1745, 395];
function providerNode(g, x, y, a) {
  if (a <= 0) return;
  g.save(); g.globalAlpha = a;
  g.shadowColor = GREEN_GLOW; g.shadowBlur = 50; g.fillStyle = GREEN;
  g.beginPath(); g.arc(x, y, 48, 0, 6.2832); g.fill(); g.shadowBlur = 0;
  g.strokeStyle = 'rgba(52,199,123,0.45)'; g.lineWidth = 2; g.beginPath(); g.arc(x, y, 64, 0, 6.2832); g.stroke();
  // relay glyph: three stacked bars
  g.fillStyle = '#0B2A1A';
  for (let i = 0; i < 3; i++) { rr(g, x - 20, y - 18 + i * 13, 40, 8, 3); g.fill(); }
  g.fillStyle = GREEN; for (let i = 0; i < 3; i++) { g.beginPath(); g.arc(x + 12, y - 14 + i * 13, 2.2, 0, 6.2832); g.fill(); }
  g.restore();
}
function ethGlyph(g, x, y, a) {
  g.save(); g.globalAlpha = a; g.fillStyle = GREEN;
  g.beginPath(); g.moveTo(x, y - 12); g.lineTo(x + 8, y); g.lineTo(x, y + 4); g.lineTo(x - 8, y); g.closePath(); g.fill();
  g.beginPath(); g.moveTo(x, y + 7); g.lineTo(x + 8, y + 2); g.lineTo(x, y + 13); g.lineTo(x - 8, y + 2); g.closePath(); g.fill();
  g.restore();
}
function miniMesh(g, cx, cy, k, color, a) {
  if (a <= 0) return;
  const ids = Object.keys(MESH);
  const P = ids.map(id => [cx + (MESH[id][0] - 960) * k, cy + (MESH[id][1] - 670) * k]);
  g.save(); g.globalAlpha = a * 0.45; g.strokeStyle = color; g.lineWidth = 1.4;
  for (let i = 0; i < P.length; i++) for (let j = i + 1; j < P.length; j++) { g.beginPath(); g.moveTo(P[i][0], P[i][1]); g.lineTo(P[j][0], P[j][1]); g.stroke(); }
  g.globalAlpha = a; g.fillStyle = color; g.shadowColor = color; g.shadowBlur = 16;
  for (const [x, y] of P) { g.beginPath(); g.arc(x, y, 15, 0, 6.2832); g.fill(); }
  g.restore();
}
function curve(g, a, b, p, color, w, alpha, dash) {
  if (p <= 0 || alpha <= 0) return;
  g.save(); g.globalAlpha = alpha; g.strokeStyle = color; g.lineWidth = w; g.lineCap = 'round'; if (dash) g.setLineDash(dash);
  const c1 = [a[0], lerp(a[1], b[1], 0.6)], c2 = [b[0], lerp(a[1], b[1], 0.4)];
  const N = 40, M2 = Math.max(1, Math.round(N * p));
  g.beginPath();
  for (let i = 0; i <= M2; i++) { const q = i / N; const pt = bez(a, c1, c2, b, q); i ? g.lineTo(pt[0], pt[1]) : g.moveTo(pt[0], pt[1]); }
  g.stroke(); g.restore();
}
function bez(a, c1, c2, b, q) {
  const u = 1 - q;
  return [u * u * u * a[0] + 3 * u * u * q * c1[0] + 3 * u * q * q * c2[0] + q * q * q * b[0], u * u * u * a[1] + 3 * u * u * q * c1[1] + 3 * u * q * q * c2[1] + q * q * q * b[1]];
}
function coin(g, x, y, color, a, r = 11) {
  if (a <= 0) return;
  g.save(); g.globalAlpha = a; g.fillStyle = color; g.shadowColor = color; g.shadowBlur = 12;
  g.beginPath(); g.arc(x, y, r, 0, 6.2832); g.fill(); g.shadowBlur = 0;
  g.fillStyle = '#0B0C0F'; g.font = `600 ${Math.round(r * 1.3)}px "Geist"`; g.textAlign = 'center'; g.textBaseline = 'middle'; g.fillText('$', x, y + 1);
  g.restore();
}
function drawEco(g, clip, t, zc, zk) {
  // provider position: beside acme in clip 10, on top in 11-12
  const P10 = [1360, 690], PTOP = [960, 395];
  const pm = clip === 10 ? 0 : clip === 11 ? ramp(t, 0, 1.2) : 1;
  const P = [lerp(P10[0], PTOP[0], pm), lerp(P10[1], PTOP[1], pm)];
  const pa = clip === 10 ? ramp(t, 1.4, 2.0) : 1;
  // acme label
  const acmeA = clip === 10 ? ramp(t, 1.0, 1.6) : 1;
  const acmeLabelY = zc[1] + 250 * zk + 44;
  text(g, 'acme.eth', zc[0], acmeLabelY, { a: acmeA, size: 24, color: INK, font: 'Geist Mono', weight: 600, halo: true });
  // ---- clip 10: request + accept ----
  if (clip === 10 || clip === 11) {
    const fadeOut = clip === 11 ? 1 - ramp(t, 0, 0.5) : 1;
    const A = [zc[0] + 470 * zk + 26, zc[1]], B = [P[0] - 70, P[1]];
    const rq = clip === 10 ? ramp(t, 2.0, 2.7) : 1;
    seg(g, A, B, rq, COB3, 3, fadeOut, [6, 9]);
    if (clip === 10 && t > 2.0 && t < 2.8) { const q = ramp(t, 2.0, 2.7); coin(g, lerp(A[0], B[0], q), lerp(A[1], B[1], q) - 0, COB3, 1, 6); }
    text(g, 'acme.eth requests a provider', (A[0] + B[0]) / 2, A[1] - 26, { a: (clip === 10 ? ramp(t, 2.2, 2.7) : 1) * fadeOut, size: 22, color: COB3, font: 'Geist Mono', weight: 600, halo: true });
    const ac = clip === 10 ? ramp(t, 2.9, 3.6) : 1;
    seg(g, [B[0], B[1] + 14], [A[0], A[1] + 14], ac, GREEN, 4, fadeOut);
    text(g, 'provider runs its tunnels', (A[0] + B[0]) / 2, A[1] + 56, { a: (clip === 10 ? ramp(t, 3.3, 3.8) : 1) * fadeOut, size: 22, color: GREEN, font: 'Geist Mono', weight: 600, halo: true });
  }
  // ---- ENS band + other orgs + connectors (clip 11-12) ----
  const ensA = clip === 11 ? ramp(t, 0.6, 1.6) : clip === 12 ? 1 : 0;
  if (ensA > 0) {
    const half = 800 * ensA;
    g.save(); g.beginPath(); rr(g, 960 - half, ENS_Y0, half * 2, ENS_Y1 - ENS_Y0, 22); g.clip();
    g.fillStyle = 'rgba(43,63,224,0.10)'; g.fillRect(0, ENS_Y0, W, ENS_Y1 - ENS_Y0);
    g.strokeStyle = 'rgba(140,155,255,0.22)'; g.lineWidth = 2;
    for (let x = 120; x < 1820; x += 18) { g.beginPath(); g.moveTo(x, ENS_Y1); g.lineTo(x + 100, ENS_Y0); g.stroke(); }
    g.restore();
    g.save(); g.globalAlpha = ensA; g.strokeStyle = COB3; g.lineWidth = 2.5; rr(g, 960 - half, ENS_Y0, half * 2, ENS_Y1 - ENS_Y0, 22); g.stroke(); g.restore();
    text(g, 'ENSv2', 960, 606, { a: ensA, size: 32, color: INK, weight: 600, halo: true });
    text(g, 'names · grants · expiry · roles', 960, 640, { a: ensA, size: 20, color: COB3, font: 'Geist Mono', halo: true });
  }
  const conn = clip === 11 ? ramp(t, 2.0, 3.2) : clip === 12 ? 1 : 0;
  if (conn > 0) {
    seg(g, [PTOP[0], PTOP[1] + 66], [PTOP[0], ENS_Y0 - 6], conn, GREEN, 3, 1);
    text(g, 'reads ENS, enforces it', PTOP[0] + 24, 512, { a: conn, size: 20, color: GREEN, font: 'Geist Mono', align: 'left', halo: true });
    const xs = [zc[0], ...ORGS.map(o => o.x)];
    xs.forEach((x, i) => curve(g, [lerp(560, 1360, i / 3), ENS_Y1 + 4], [x, ORG_Y - 110], conn, i ? ORGS[i - 1].color : COB3, 2.5, 0.9));
  }
  ORGS.forEach((o, i) => {
    const a = clip === 11 ? ramp(t, 1.2 + i * 0.25, 1.8 + i * 0.25) : clip === 12 ? 1 : 0;
    miniMesh(g, o.x, ORG_Y, 0.36, o.color, a);
    text(g, o.name, o.x, ORG_Y + 250 * 0.36 + 44, { a, size: 24, color: INK, font: 'Geist Mono', weight: 600, halo: true });
  });
  // provider on top of everything
  providerNode(g, P[0], P[1], pa);
  const lx = lerp(P10[0], 680, pm), ly = lerp(P10[1] + 96, 392, pm);
  text(g, 'chennai-1 · provider', lx, ly, { a: pa, size: 24, color: GREEN, font: 'Geist Mono', weight: 600, halo: true });
  ethGlyph(g, lx - 118, ly + 26, pa);
  text(g, '1 ETH staked · 2 USDC / seat / mo', lx - 100, ly + 32, { a: pa, size: 20, color: MUTED, align: 'left', halo: true });
  // ---- billing (clip 12) ----
  if (clip === 12) {
    const ea = ramp(t, 0.1, 0.6);
    g.save(); g.globalAlpha = ea; g.fillStyle = '#121624'; g.strokeStyle = '#3A4AB8'; g.lineWidth = 2;
    rr(g, ESCROW[0] - 165, ESCROW[1] - 50, 330, 100, 18); g.fill(); g.stroke(); g.restore();
    text(g, 'MeshySubscription', ESCROW[0], ESCROW[1] - 8, { a: ea, size: 22, color: INK, font: 'Geist Mono', weight: 600 });
    const all = [{ x: zc[0], seats: 5, color: COB3 }, ...ORGS];
    const total = all.reduce((s2, o) => s2 + o.seats, 0) * 2;
    // seats chips under each org
    all.forEach((o, i) => text(g, `${o.seats} seats · ${o.seats * 2} USDC`, o.x, ORG_Y + 250 * 0.36 + 72, { a: ramp(t, 0.2 + i * 0.1, 0.6 + i * 0.1), size: 20, color: MUTED, halo: true }));
    // coins: orgs -> escrow
    let arrived = 0;
    all.forEach((o, i) => {
      for (let c = 0; c < 3; c++) {
        const t0 = 0.6 + i * 0.22 + c * 0.12, q = ramp(t, t0, t0 + 1.1);
        if (q >= 1) { arrived += o.seats * 2 / 3; continue; }
        if (q <= 0) continue;
        const a2 = [o.x, ORG_Y - 100], b2 = [ESCROW[0] + (i - 1.5) * 40, ESCROW[1] + 50];
        const pt = bez(a2, [a2[0], lerp(a2[1], b2[1], 0.5)], [b2[0], lerp(a2[1], b2[1], 0.6)], b2, q);
        coin(g, pt[0], pt[1], o.color, 1);
      }
    });
    const shown = Math.min(total, Math.round(arrived));
    text(g, `${shown} USDC / month`, ESCROW[0], ESCROW[1] + 24, { a: ea, size: 20, color: COB3, font: 'Geist Mono' });
    // split: 95% provider, 5% treasury
    const sp = ramp(t, 2.6, 3.6);
    for (let c = 0; c < 4; c++) {
      const q = clamp(sp * 1.4 - c * 0.13);
      if (q > 0 && q < 1) coin(g, lerp(ESCROW[0] - 170, PTOP[0] + 60, q), ESCROW[1], GREEN, 1, 12);
    }
    { const q = clamp(sp * 1.2); if (q > 0 && q < 1) coin(g, lerp(ESCROW[0] + 170, TREASURY[0] - 26, q), TREASURY[1], '#9AA1AB', 1, 8); }
    seg(g, [ESCROW[0] - 170, ESCROW[1]], [PTOP[0] + 70, PTOP[1]], ramp(t, 2.4, 2.8), GREEN, 2, 0.6, [4, 8]);
    seg(g, [ESCROW[0] + 170, ESCROW[1]], [TREASURY[0] - 26, TREASURY[1]], ramp(t, 2.4, 2.8), '#5A606A', 2, 0.8, [4, 8]);
    text(g, '95%', (ESCROW[0] - 170 + PTOP[0] + 70) / 2, ESCROW[1] - 18, { a: ramp(t, 2.5, 2.9), size: 22, color: GREEN, font: 'Geist Mono', weight: 600, halo: true });
    text(g, '5%', (ESCROW[0] + 170 + TREASURY[0]) / 2, ESCROW[1] - 18, { a: ramp(t, 2.5, 2.9), size: 22, color: MUTED, font: 'Geist Mono', weight: 600, halo: true });
    const ta = ramp(t, 2.4, 2.8);
    g.save(); g.globalAlpha = ta; g.fillStyle = '#1A1D24'; g.strokeStyle = '#3A3F47'; g.lineWidth = 2; g.beginPath(); g.arc(TREASURY[0], TREASURY[1], 24, 0, 6.2832); g.fill(); g.stroke(); g.restore();
    text(g, 'treasury', TREASURY[0], TREASURY[1] + 56, { a: ta, size: 20, color: MUTED, halo: true });
    // provider earnings
    const earn = Math.round(38 * ramp(t, 3.0, 3.9));
    const ea2 = ramp(t, 3.0, 3.4);
    g.save(); g.globalAlpha = ea2; g.fillStyle = 'rgba(52,199,123,0.14)'; g.strokeStyle = GREEN; g.lineWidth = 2;
    rr(g, 605, 446, 200, 52, 26); g.fill(); g.stroke(); g.restore();
    text(g, `+${earn} USDC/mo`, 705, 480, { a: ea2, size: 22, color: GREEN, font: 'Geist Mono', weight: 600 });
  }
}

window.SCENE = { draw, setDots, CLIPS, W, H };

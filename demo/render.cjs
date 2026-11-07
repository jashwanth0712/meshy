const { chromium } = require('playwright');
const fs = require('fs'), path = require('path');
// usage: node render.cjs frames <clip> <outdir> [fps]   |  node render.cjs stills <out.png> <clip> <t> [<out> <clip> <t> ...]
(async () => {
  const [mode, ...args] = process.argv.slice(2);
  const html = `<!doctype html><html><head><meta charset="utf-8">
<link rel="stylesheet" href="https://fonts.googleapis.com/css2?family=Geist:wght@400;600&family=Geist+Mono:wght@400;600&display=swap">
<style>html,body{margin:0;background:#0B0C0F}canvas{display:block}</style></head><body><canvas id="c" width="1920" height="1080"></canvas>
<script>${fs.readFileSync(path.join(__dirname, 'scene.js'), 'utf8')}</script>
<script>SCENE.setDots(${fs.readFileSync(path.join(__dirname, 'dots.json'), 'utf8')});</script></body></html>`;
  const b = await chromium.launch();
  const pg = await b.newPage({ viewport: { width: 1920, height: 1080 }, deviceScaleFactor: 1 });
  await pg.setContent(html, { waitUntil: 'networkidle' });
  await pg.evaluate(async () => { await Promise.all(['400 20px "Geist"', '600 20px "Geist"', '400 20px "Geist Mono"', '600 20px "Geist Mono"'].map(f => document.fonts.load(f))); });
  const frame = async (clip, t) => {
    const b64 = await pg.evaluate(([clip, t]) => { const cv = document.getElementById('c'); SCENE.draw(cv.getContext('2d'), clip, t); return cv.toDataURL('image/png').split(',')[1]; }, [clip, t]);
    return Buffer.from(b64, 'base64');
  };
  if (mode === 'stills') {
    for (let i = 0; i < args.length; i += 3) fs.writeFileSync(args[i], await frame(+args[i + 1], +args[i + 2]));
  } else {
    const [clip, out, fps = '30'] = args;
    fs.mkdirSync(out, { recursive: true });
    const len = await pg.evaluate(c => SCENE.CLIPS[c].len, +clip);
    const n = Math.round(len * +fps) + 1;
    for (let i = 0; i < n; i++) fs.writeFileSync(path.join(out, `f${String(i).padStart(4, '0')}.png`), await frame(+clip, i / +fps));
    console.log('clip', clip, 'frames', n);
  }
  await b.close();
})();

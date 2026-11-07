import fs from 'fs';
import { feature } from 'topojson-client';
import { geoContains } from 'd3-geo';
const topo = JSON.parse(fs.readFileSync('node_modules/world-atlas/land-50m.json'));
const land = feature(topo, topo.objects.land);
const grid = (lon0, lon1, lat0, lat1, step) => {
  const out = [];
  for (let lat = lat1; lat >= lat0; lat -= step)
    for (let lon = lon0; lon <= lon1; lon += step) {
      const p = [Math.round(lon * 100) / 100, Math.round(lat * 100) / 100];
      if (geoContains(land, p)) out.push(p);
    }
  return out;
};
const t = Date.now();
const world = grid(-180, 180, -56, 78, 1.5);
const fine = grid(58.2, 97.2, 3.3, 30.3, 0.3);
fs.writeFileSync('dots.json', JSON.stringify({ world, fine }));
console.log('world', world.length, 'fine', fine.length, (Date.now() - t) + 'ms');

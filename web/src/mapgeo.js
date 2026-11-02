import { COUNTRIES } from './worldmap.js';

// The middle of a country on the map's own drawing: the centre of its biggest piece, so a country
// with far islands (the US with Alaska) is not pulled off to the side.
const centreCache = {};
export function centreOf(cc) {
  if (centreCache[cc]) return centreCache[cc];
  const d = COUNTRIES[cc]?.d;
  if (!d) return null;
  let best = null;
  d.split('M').filter(Boolean).forEach((piece) => {
    const nums = [...piece.matchAll(/(-?\d+(?:\.\d+)?) (-?\d+(?:\.\d+)?)/g)].map((m) => [+m[1], +m[2]]);
    if (!best || nums.length > best.length) best = nums;
  });
  if (!best?.length) return null;
  const xs = best.map((q) => q[0]), ys = best.map((q) => q[1]);
  centreCache[cc] = [(Math.min(...xs) + Math.max(...xs)) / 2, (Math.min(...ys) + Math.max(...ys)) / 2];
  return centreCache[cc];
}


// Latitude and longitude on the map's own drawing. The map is a plain grid of degrees, fitted to the
// country outlines, so a point lands within about a degree (roughly 100 km) of its real place.
export const project = (lat, lon) => [Math.round(500 + lon * 2.7788), Math.round(272 - lat * 2.99)];
export const unproject = (x, y) => [Math.round(((272 - y) / 2.99) * 100) / 100, Math.round(((x - 500) / 2.7788) * 100) / 100];

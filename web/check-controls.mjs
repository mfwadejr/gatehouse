// Fails the build when a screen uses a browser's own dropdown, clock or calendar. Those cannot be
// styled, so they never match the site. Use Select, TimeField, DateField, DateTimeField or
// DateRange from ui.jsx instead.
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join } from 'node:path';

const walk = (d) => readdirSync(d).flatMap((n) => { const p = join(d, n); return statSync(p).isDirectory() ? walk(p) : /\.jsx?$/.test(p) ? [p] : []; });
let bad = 0;
for (const f of walk('src')) {
  readFileSync(f, 'utf8').split('\n').forEach((line, i) => {
    const code = line.replace(/\/\/.*$/, '');
    if (/<select\b|<datalist\b|createElement\(\s*['"]select['"]/.test(code)) { console.log(`${f}:${i + 1}: a raw <select>; use Select from ui.jsx`); bad++; }
    if (/type\s*=\s*['"{`]+\s*(date|time|datetime-local|month|week|color|range)\b/.test(code)) { console.log(`${f}:${i + 1}: a native date, time or similar input; use TimeField, DateField, DateTimeField or DateRange from ui.jsx`); bad++; }
  });
}
if (bad) { console.log(`${bad} raw control(s). The browser draws these in its own style.`); process.exit(1); }
console.log('controls ok: every dropdown, time and date control is the site\'s own');

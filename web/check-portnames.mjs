// Port names check: a screen that lists the box's network ports (it says "cable connected") must use
// usePortNames, so a port the person has renamed (LAN, T-Mobile) shows under that name and not as
// enp0s20f1. The first-run wizard is exempt: nothing has been named yet.
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join } from 'node:path';
const walk = (d) => readdirSync(d).flatMap((n) => { const p = join(d, n); return statSync(p).isDirectory() ? walk(p) : /\.jsx$/.test(p) ? [p] : []; });
const exempt = ['src/pages/Wizard.jsx'];
let bad = 0;
for (const f of walk('src')) {
  if (exempt.includes(f.replace(/\\/g, '/'))) continue;
  const s = readFileSync(f, 'utf8');
  if (/cable connected/.test(s) && !/usePortNames/.test(s)) { console.error(`${f}: lists network ports but does not use usePortNames`); bad++; }
}
if (bad) process.exit(1);
console.log('port names check ok');

// Fails the build when a React hook is called after an early return in the same component.
// That crashes a screen with React error #310 as soon as its data arrives.
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join } from 'node:path';

const walk = (d) => readdirSync(d).flatMap((n) => { const p = join(d, n); return statSync(p).isDirectory() ? walk(p) : p.endsWith('.jsx') ? [p] : []; });
const hook = /\buse(State|Effect|Memo|Action|Data|StepUp|Callback|Ref|Stream|Toast|Layout\w*)\(/;
let bad = 0;
for (const f of walk('src')) {
  let early = 0;
  readFileSync(f, 'utf8').split('\n').forEach((l, i) => {
    if (/^(export (default )?)?function \w+/.test(l) || /^const \w+ = .*=> *\{?$/.test(l)) early = 0;
    if (/^  if \(.*\) return\b/.test(l)) early = early || i + 1;
    if (early && i + 1 > early && /^  \S/.test(l) && hook.test(l)) { console.log(`${f}:${i + 1}: a hook is called after the early return on line ${early}`); bad++; }
  });
}
process.exit(bad ? 1 : 0);

// Style-token check. Fails the build when
//  1. a var(--name) is used that no stylesheet defines (it silently falls back to a guess), or
//  2. a spacing or radius number is written inline in a screen instead of a token, or
//  3. a colour is written out (#hex, rgb(), hsl()) in a screen or in styles.css instead of a token.
// tokens.css is the one place colours are allowed. Add to ALLOW only with a reason.
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join } from 'node:path';

const walk = (d) => readdirSync(d).flatMap((n) => { const p = join(d, n); return statSync(p).isDirectory() ? walk(p) : /\.(jsx?|css)$/.test(p) ? [p] : []; });
const files = walk('src');
const css = files.filter((f) => f.endsWith('.css'));
const defined = new Set();
for (const f of css) for (const m of readFileSync(f, 'utf8').matchAll(/(--[a-z0-9-]+)\s*:/g)) defined.add(m[1]);
// custom properties set from a screen (style={{ '--x': ... }}) count as defined
for (const f of files.filter((x) => !x.endsWith('.css'))) for (const m of readFileSync(f, 'utf8').matchAll(/['"](--[a-z0-9-]+)['"]\s*:/g)) defined.add(m[1]);

// Status colours are red, yellow, green and blue and nothing else (decided 11 Nov 2026). Blue is the
// ordinary accent, so the status tokens are green (good), yellow (warning) and red (bad, blocked).
// A new status colour, or the old coral, fails here so it cannot creep back.
const STATUS_OK = new Set(['good', 'warning', 'bad', 'bad-border', 'blocked']);
let bad = 0;
for (const f of css) {
  for (const m of readFileSync(f, 'utf8').matchAll(/--color-status-([a-z-]+)\s*:/g)) {
    if (!STATUS_OK.has(m[1])) { console.log(`${f}: --color-status-${m[1]} is not one of the four status colours (red, yellow, green, blue)`); bad++; }
  }
  if (/coral|serious/i.test(readFileSync(f, 'utf8').replace(/\/\*[\s\S]*?\*\//g, ''))) { console.log(`${f}: the coral status colour is gone; use red (--color-status-bad)`); bad++; }
}
const ALLOW = [
  'src/worldmap.js', // map drawing data
  'src/providers.js',
];
const prefixOK = (v) => v === '--color-chart-series-' || v === '--c'; // built from a number or set per element
for (const f of files) {
  if (f.endsWith('tokens.css') || ALLOW.includes(f)) continue;
  readFileSync(f, 'utf8').split('\n').forEach((line, i) => {
    for (const m of line.matchAll(/var\((--[a-z0-9-]+)/g)) if (!defined.has(m[1]) && !prefixOK(m[1])) { console.log(`${f}:${i + 1}: ${m[1]} is not defined anywhere`); bad++; }
    const code = line.replace(/\/\/.*$/, '');
    if (!f.endsWith('.css') && /\b(?:padding|margin|gap|rowGap|columnGap)(?:Top|Bottom|Left|Right|Inline|Block)?: *[1-9]\d*(?=[,\s}])|\bborderRadius: *[1-9]\d*(?=[,\s}])/.test(code)) { console.log(`${f}:${i + 1}: a spacing or radius number is written inline; use a var(--space-*) or var(--radius-*) token`); bad++; }
    if (/#[0-9a-fA-F]{3,8}\b/.test(code.replace(/&#\d+;/g, '')) && !/^\s*(\*|\/\*)/.test(line)) { console.log(`${f}:${i + 1}: a colour is written out; use a token`); bad++; }
    else if (/\brgba?\(|\bhsla?\(/.test(code)) { console.log(`${f}:${i + 1}: a colour is written out; use a token`); bad++; }
  });
}
console.log(`${files.length} files checked: ${bad} problem${bad === 1 ? '' : 's'}`);
process.exit(bad ? 1 : 0);

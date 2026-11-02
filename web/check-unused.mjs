// Unused-style check: a semantic design token or a CSS class that nothing refers to is dead weight and
// fails the build. Primitive palette steps are the source colours and are allowed to be spare. Names
// built in code (chart series, state classes) are listed in KEEP.
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join } from 'node:path';

const walk = (d) => readdirSync(d).flatMap((n) => { const p = join(d, n); return statSync(p).isDirectory() ? walk(p) : [p]; });
const files = walk('src');
const code = files.filter((f) => /\.jsx?$/.test(f)).map((f) => readFileSync(f, 'utf8')).join('\n');
const css = readFileSync('src/styles.css', 'utf8');
const tok = readFileSync('src/tokens.css', 'utf8');

// Names that are assembled in code, so a text search cannot see their use.
const KEEP_TOKENS = [/^color-chart-series-\d+$/];
const KEEP_CLASSES = new Set(['lim']); // st-${state} and the matrix cell states are built in FirewallRules.jsx

const defs = [...new Set([...tok.matchAll(/--([\w-]+)\s*:/g)].map((m) => m[1]))];
const refs = {};
for (const m of tok.matchAll(/--([\w-]+)\s*:\s*([^;]+);/g)) { (refs[m[1]] ??= new Set()); for (const r of m[2].matchAll(/var\(--([\w-]+)/g)) refs[m[1]].add(r[1]); }
const live = new Set();
const queue = [...new Set([...(css + code).matchAll(/var\(--([\w-]+)/g)].map((m) => m[1]))];
while (queue.length) { const t = queue.pop(); if (live.has(t)) continue; live.add(t); for (const r of refs[t] || []) queue.push(r); }
const deadTokens = defs.filter((d) => !d.startsWith('primitive-') && !live.has(d) && !KEEP_TOKENS.some((k) => k.test(d)));

const classes = [...new Set([...css.matchAll(/\.([a-zA-Z][\w-]*)/g)].map((m) => m[1]))];
const deadClasses = classes.filter((c) => {
  if (KEEP_CLASSES.has(c) || /^st-/.test(c)) return false;
  return !new RegExp(`(^|[^\\w-])${c.replace(/-/g, '\\-')}([^\\w-]|$)`).test(code + readFileSync('index.html', 'utf8'));
});

if (deadTokens.length || deadClasses.length) {
  if (deadTokens.length) console.log(`unused tokens: ${deadTokens.join(' ')}`);
  if (deadClasses.length) console.log(`unused CSS classes: ${deadClasses.join(' ')}`);
  process.exit(1);
}
console.log(`unused check ok (${defs.length} tokens, ${classes.length} classes)`);

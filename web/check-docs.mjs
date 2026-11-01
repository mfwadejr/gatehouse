// Docs check: every console page has a chapter with the standard sections, every bold control name a
// chapter mentions exists somewhere in the screens, and every image link resolves.
import { readFileSync, existsSync, readdirSync, statSync } from 'node:fs';
import { join } from 'node:path';
if (!existsSync('../docs/help')) { console.log('docs check skipped: the docs folder is not next to the web source'); process.exit(0); }
const walk = (d) => readdirSync(d).flatMap((n) => { const p = join(d, n); return statSync(p).isDirectory() ? walk(p) : p.endsWith('.jsx') || p.endsWith('.js') ? [p] : []; });
const code = walk('src').map((f) => readFileSync(f, 'utf8')).join('\n').replace(/&amp;/g, '&').toLowerCase();
const ids = [...readFileSync('src/App.jsx', 'utf8').matchAll(/\{ id: '([a-z-]+)', label/g)].map((m) => m[1]);
let bad = 0; const fail = (m) => { console.error('docs: ' + m); bad++; };
if (!ids.length) fail('could not read the page list from App.jsx');
const need = ['What it is for', 'What is on the page', 'Common tasks', 'If something goes wrong'];
for (const id of ids) {
  const f = `../docs/help/${id}.md`;
  if (!existsSync(f)) { fail(`no chapter for the ${id} page (docs/${id}.md)`); continue; }
  const md = readFileSync(f, 'utf8');
  for (const h of need) if (!md.includes(`## ${h}`)) fail(`${id}.md has no "${h}" section`);
  for (const m of md.matchAll(/!\[[^\]]*\]\(([^)]+)\)/g)) if (!m[1].startsWith('http') && !existsSync(join('../docs/help', m[1]))) fail(`${id}.md: missing image ${m[1]}`);
  for (const m of md.matchAll(/\*\*([^*]{2,60})\*\*/g)) {
    const t = m[1].replace(/[:.]$/, '').toLowerCase();
    if (!code.includes(t)) fail(`${id}.md mentions **${m[1]}**, which is not on any screen`);
  }
}
for (const f of ['getting-started', 'recovery', 'reference']) if (!existsSync(`../docs/help/${f}.md`)) fail(`missing docs/${f}.md`);
if (bad) { console.error(`docs check: ${bad} problem(s)`); process.exit(1); }
console.log(`docs check ok (${ids.length} chapters)`);

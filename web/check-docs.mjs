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
// Every page's "Help with this page" link must open a chapter that is built and listed.
const order = [...readFileSync('build-docs.mjs', 'utf8').matchAll(/\['([a-z-]+)', '[^']+'\]/g)].map((m) => m[1]);
const helpPages = [...readFileSync('src/ui.jsx', 'utf8').match(/const HELP_PAGES = \[([^\]]*)\]/)[1].matchAll(/'([a-z-]+)'/g)].map((m) => m[1]);
for (const id of ids) {
  if (!order.includes(id)) fail(`the ${id} chapter is not in the list in build-docs.mjs, so it is never built`);
  if (!helpPages.includes(id)) fail(`the ${id} page's Help link has no chapter in HELP_PAGES (src/ui.jsx)`);
}
for (const h of helpPages) if (!ids.includes(h)) fail(`HELP_PAGES names ${h}, which is not a page`);
for (const f of ['getting-started', 'recovery', 'reference']) if (!existsSync(`../docs/help/${f}.md`)) fail(`missing docs/${f}.md`);

// How it works chapters (docs/how): the seven agreed sections, bold control names that exist on a screen,
// numbers that come from the code (the build fills them in and fails on a missing constant), and every
// "Why this value?" link in the console pointing at a section that exists.
if (existsSync('../docs/how')) {
  const secs = ['What the page does', 'Every tab and control', 'Timers, thresholds and limits', 'How it is measured and reported', 'What it reads, writes and calls', 'Options considered and not taken', 'When something goes wrong'];
  const slugOf = (t) => t.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '');
  const anchors = {};
  for (const f of readdirSync('../docs/how').filter((n) => n.endsWith('.md'))) {
    const id = f.replace(/\.md$/, '');
    const md = readFileSync(join('../docs/how', f), 'utf8');
    if (!ids.includes(id)) fail(`docs/how/${f} is not a console page`);
    for (const h of secs) if (!md.includes(`## ${h}`)) fail(`how/${id}.md has no "${h}" section`);
    for (const m of md.matchAll(/\*\*([^*]{2,60})\*\*/g)) if (!code.includes(m[1].replace(/[:.]$/, '').toLowerCase())) fail(`how/${id}.md mentions **${m[1]}**, which is not on any screen`);
    if (/\{\{go:/.test(md) === false && /\b\d+ ?(s|ms|seconds|minutes|min|hours)\b/.test(md.replace(/Needs confirming[^\n]*/g, ''))) console.warn(`how/${id}.md has a time written by hand; if it comes from the code, use {{go:file#Name}}`);
    anchors[id] = new Set([...md.matchAll(/^#{1,3} (.*)$/gm)].map((m) => slugOf(m[1].replace(/[*`]/g, ''))));
  }
  const howPagesJs = readFileSync('src/howpages.js', 'utf8');
  for (const id of Object.keys(anchors)) if (!howPagesJs.includes(`"${id}"`)) fail(`src/howpages.js does not list the ${id} chapter; run the docs build`);
  for (const f of walk('src')) {
    for (const m of readFileSync(f, 'utf8').matchAll(/<HowLink page="([a-z-]+)" section="([a-z0-9-]+)"/g)) {
      if (!anchors[m[1]]) fail(`${f}: a Why this value? link points at the ${m[1]} chapter, which does not exist`);
      else if (!anchors[m[1]].has(m[2])) fail(`${f}: a Why this value? link points at #${m[2]}, which is not a heading in how/${m[1]}.md`);
    }
  }
}
if (bad) { console.error(`docs check: ${bad} problem(s)`); process.exit(1); }
console.log(`docs check ok (${ids.length} chapters)`);

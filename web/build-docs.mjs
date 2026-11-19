// Builds the help pages: docs/*.md become plain HTML in public/help/, which ships inside the
// console (and so on the box, at /help/). No dependencies; the Markdown used in the docs is small:
// headings, paragraphs, bullet and numbered lists, **bold**, `code`, and ![alt](img) images.
import { readFileSync, writeFileSync, mkdirSync, rmSync, existsSync, readdirSync, copyFileSync } from 'node:fs';
import { join } from 'node:path';

if (!existsSync('../docs/help')) { console.log('docs build skipped: the docs folder is not next to the web source'); process.exit(0); }

export const ORDER = [
  ['getting-started', 'Getting started'], ['recovery', 'If you are locked out'],
  ['overview', 'Overview'], ['internet', 'Internet'], ['networks', 'Networks'], ['dns', 'DNS'], ['dhcp', 'DHCP'],
  ['firewall', 'Firewall'], ['protection', 'Protection'], ['threats', 'Threats'], ['vpn', 'VPN'], ['policies', 'Policies'], ['review', 'Review changes'],
  ['devices', 'Devices & access'], ['logs', 'Logs'], ['diagnostics', 'Diagnostics'], ['updates', 'Updates'], ['settings', 'Settings'], ['reference', 'Reference'], ['how-it-works', 'How it works'], ['ease-of-use', 'How easy is it?'],
];
const esc = (s) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
const inline = (s) => esc(s)
  .replace(/!\[([^\]]*)\]\(([^)]+)\)/g, '<img src="$2" alt="$1" loading="lazy">')
  .replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>')
  .replace(/`([^`]+)`/g, '<code>$1</code>');

export const slug = (t) => t.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '');

export function render(md) {
  const out = []; let list = null; let para = []; const seen = new Set();
  const flushP = () => { if (para.length) { out.push(`<p>${inline(para.join(' '))}</p>`); para = []; } };
  const flushL = () => { if (list) { out.push(`</${list}>`); list = null; } };
  let code = false;
  for (const line of md.split('\n')) {
    if (line.startsWith('```')) { flushP(); flushL(); out.push(code ? '</pre>' : '<pre>'); code = !code; continue; }
    if (code) { out.push(esc(line)); continue; }
    let m;
    if ((m = line.match(/^(#{1,3}) (.*)/))) { flushP(); flushL(); let id = slug(m[2]); while (seen.has(id)) id += '-2'; seen.add(id); out.push(`<h${m[1].length} id="${id}">${inline(m[2])}</h${m[1].length}>`); continue; }
    if ((m = line.match(/^\s*[-*] (.*)/))) { flushP(); if (list !== 'ul') { flushL(); out.push('<ul>'); list = 'ul'; } out.push(`<li>${inline(m[1])}</li>`); continue; }
    if ((m = line.match(/^\s*\d+\. (.*)/))) { flushP(); if (list !== 'ol') { flushL(); out.push('<ol>'); list = 'ol'; } out.push(`<li>${inline(m[1])}</li>`); continue; }
    if (!line.trim()) { flushP(); flushL(); continue; }
    para.push(line.trim());
  }
  flushP(); flushL();
  return out.join('\n');
}

const dir = 'public/help';
rmSync(dir, { recursive: true, force: true });
mkdirSync(dir, { recursive: true });
const have = ORDER.filter(([id]) => existsSync(`../docs/help/${id}.md`));
const nav = (cur) => `<nav aria-label="Help chapters"><a href="./" class="home">Help</a>${have.map(([id, t]) => `<a href="${id}.html"${id === cur ? ' aria-current="page"' : ''}>${esc(t)}</a>`).join('')}</nav>`;
const page = (title, cur, body) => `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>${esc(title)} - NopeWarden help</title><link rel="stylesheet" href="help.css"></head><body>${nav(cur)}<main>${body}<p class="back"><a href="../">Back to the console</a></p></main></body></html>`;
for (const [id, title] of have) writeFileSync(join(dir, `${id}.html`), page(title, id, render(readFileSync(`../docs/help/${id}.md`, 'utf8'))));
const cards = have.map(([id, t]) => `<li><a href="${id}.html">${esc(t)}</a></li>`).join('');
writeFileSync(join(dir, 'index.html'), page('Help', '', `<h1>NopeWarden help</h1><p>Start with <strong>Getting started</strong>. If you cannot sign in, open <strong>If you are locked out</strong>. Every page of the console has its own chapter.</p><ul class="chapters">${cards}</ul><p>Want the reasons behind the numbers? Open <a href="how/">How it works</a>, the technical reference.</p>`));
if (existsSync('../docs/help/img')) { mkdirSync(join(dir, 'img'), { recursive: true }); for (const f of readdirSync('../docs/help/img')) copyFileSync(join('../docs/help/img', f), join(dir, 'img', f)); }
writeFileSync(join(dir, 'help.css'), `
body{margin:0;font:16px/1.55 system-ui,-apple-system,Segoe UI,sans-serif;color:#1c2330;background:#f6f8fb;display:flex;min-height:100vh}
nav{width:15rem;flex:none;padding:1rem;background:#fff;border-right:1px solid #dde3ec;display:flex;flex-direction:column;gap:.15rem}
nav a{padding:.35rem .6rem;border-radius:6px;color:#1c2330;text-decoration:none}
nav a:hover{background:#eef3fb}nav a[aria-current=page]{background:#dbe8fb;font-weight:600}nav .home{font-weight:700;color:#2a6bd1}
main{max-width:46rem;padding:1.5rem 2rem 4rem}h1{margin-top:0}h2{margin-top:2rem}
img{max-width:100%;border:1px solid #dde3ec;border-radius:8px;margin:.5rem 0}
code,pre{background:#eef1f6;border-radius:4px;padding:.1rem .3rem}pre{padding:.7rem;overflow:auto}
.chapters{columns:2;padding-left:1.2rem}.back{margin-top:3rem}
a{color:#2a6bd1}
@media(max-width:700px){body{display:block}nav{width:auto;flex-direction:row;flex-wrap:wrap;border-right:0;border-bottom:1px solid #dde3ec}main{padding:1rem}}
@media(prefers-color-scheme:dark){body{background:#12161d;color:#e4e9f1}nav{background:#1a2029;border-color:#2b3441}nav a{color:#e4e9f1}nav a:hover{background:#243044}nav a[aria-current=page]{background:#25395a}img{border-color:#2b3441}code,pre{background:#222a36}a{color:#7fb0f5}nav .home{color:#7fb0f5}}
`);

// ---------------------------------------------------------------- How it works (technical reference)
// docs/how/<page>.md is the technical chapter for a console page. A number that comes from the code is
// written {{go:internal/app/routes_fw.go#confirmWindow}} and filled in here from the Go source, so a
// changed constant changes the text, and a constant that has been renamed or removed fails the build.
const HOW_SECTIONS = ['What the page does', 'Every tab and control', 'Timers, thresholds and limits', 'How it is measured and reported', 'What it reads, writes and calls', 'Options considered and not taken', 'When something goes wrong'];
const UNITS = { Millisecond: ['ms', 1], Second: ['s', 1], Minute: ['min', 1], Hour: ['h', 1] };
export function goConst(file, name) {
  const src = readFileSync(join('..', file), 'utf8');
  const m = src.match(new RegExp(`(?:^|\\n)\\s*(?:const\\s+|var\\s+)?${name}\\s+(?:[A-Za-z0-9_.\\[\\]*]+\\s+)?=\\s*([^\\n]+)`));
  if (!m) throw new Error(`the constant ${name} is not in ${file}`);
  const expr = m[1].replace(/\s*\/\/.*$/, '').trim();
  let t;
  if ((t = expr.match(/^(\d+(?:\.\d+)?)\s*\*\s*time\.(Millisecond|Second|Minute|Hour)$/))) return `${t[1]} ${UNITS[t[2]][0]}`;
  if ((t = expr.match(/^time\.(Millisecond|Second|Minute|Hour)\s*\*\s*(\d+(?:\.\d+)?)$/))) return `${t[2]} ${UNITS[t[1]][0]}`;
  if ((t = expr.match(/^time\.(Millisecond|Second|Minute|Hour)$/))) return `1 ${UNITS[t[1]][0]}`;
  if (/^-?\d+(\.\d+)?$/.test(expr)) return expr;
  if ((t = expr.match(/^"([^"]*)"$/))) return t[1];
  throw new Error(`${name} in ${file} is "${expr}", which the docs build cannot turn into a number; give it a plain value or extend goConst`);
}
const HOW_DIR = '../docs/how';
const howErrors = [];
const howOpen = []; // reasons that are not written down anywhere: listed in docs/how/NEEDS-CONFIRMING.txt, never shown to readers
const howHave = [];
const stripOpen = (md, id) => md.split('\n').filter((l) => { if (/^Needs confirming:/.test(l)) { howOpen.push(`${id}: ${l.replace(/^Needs confirming:\s*/, '')}`); return false; } return true; }).join('\n');
const fillConsts = (md, id) => stripOpen(md, id).replace(/\{\{go:([^#}]+)#([A-Za-z0-9_]+)\}\}/g, (all, f, n) => {
  try { return goConst(f, n); } catch (e) { howErrors.push(`${id}.md: ${e.message}`); return all; }
});
const howPages = [...readFileSync('src/App.jsx', 'utf8').matchAll(/\{ id: '([a-z-]+)', label: '([^']+)'/g)].map((m) => [m[1], m[2]]);
if (existsSync(HOW_DIR)) {
  mkdirSync(join(dir, 'how'), { recursive: true });
  for (const [id, label] of howPages) if (existsSync(`${HOW_DIR}/${id}.md`)) howHave.push([id, label]);
  const hnav = (cur) => `<nav aria-label="How it works chapters"><a href="../" class="home">Help</a><a href="./" class="home">How it works</a>${howHave.map(([id, t]) => `<a href="${id}.html"${id === cur ? ' aria-current="page"' : ''}>${esc(t)}</a>`).join('')}</nav>`;
  const hpage = (title, cur, body) => `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>${esc(title)} - How it works - NopeWarden</title><link rel="stylesheet" href="../help.css"></head><body>${hnav(cur)}<main>${body}<p class="back"><a href="../">Back to the help</a> &middot; <a href="../../">Back to the console</a></p></main></body></html>`;
  for (const [id, label] of howHave) {
    const md = fillConsts(readFileSync(`${HOW_DIR}/${id}.md`, 'utf8'), id);
    writeFileSync(join(dir, 'how', `${id}.html`), hpage(label, id, render(md)));
    writeFileSync(join(dir, 'how', `${id}.md`), md); // numbers filled in; the website build reads these
  }
  const written = howHave.map(([id, t]) => `<li><a href="${id}.html">${esc(t)}</a></li>`).join('');
  const todo = howPages.filter(([id]) => !howHave.some(([h]) => h === id)).map(([, t]) => `<li>${esc(t)}</li>`).join('');
  writeFileSync(join(dir, 'how', 'index.html'), hpage('How it works', '', `<h1>How it works</h1><p>The technical reference: for each page, what it does, every timer, threshold and limit with its value and why it was chosen, how things are measured, what the page reads and changes, what was considered and not done, and what happens when something goes wrong. The short chapters in <a href="../">Help</a> tell you how to do things; these tell you how and why it behaves as it does.</p><p>Numbers marked from the code are filled in from NopeWarden's own source when this help is built, so they cannot drift from what the box does.</p><h2>Written</h2><ul class="chapters">${written || '<li>None yet.</li>'}</ul>${todo ? `<h2>Still to be written</h2><ul class="chapters">${todo}</ul>` : ''}`));
}
writeFileSync('src/howpages.js', `// Generated by build-docs.mjs: the console pages that have a technical chapter in docs/how.\nexport const HOW_PAGES = ${JSON.stringify(howHave.map(([id]) => id))};\n`);
writeFileSync('../docs/how/NEEDS-CONFIRMING.txt', howOpen.length ? `Reasons the technical chapters could not source. Each needs a yes or a reason from the owner.\n\n${[...new Set(howOpen)].join('\n\n')}\n` : 'Nothing to confirm.\n');
if (howOpen.length) console.log(`docs: ${[...new Set(howOpen)].length} reason(s) in the technical chapters need confirming (docs/how/NEEDS-CONFIRMING.txt)`);
writeFileSync(join(dir, 'how.json'), JSON.stringify({ chapters: howHave.map(([id]) => id), sections: HOW_SECTIONS, errors: howErrors }));
if (howErrors.length) { howErrors.forEach((e) => console.error('docs: ' + e)); process.exit(1); }

// What the console's search reads to find help: one entry per heading, with the words under it.
const index = [];
for (const [id, title, srcFile] of [...have.map(([i, t]) => [i, t, `../docs/help/${i}.md`]), ...howHave.map(([i, t]) => [`how/${i}`, `How it works: ${t}`, `${HOW_DIR}/${i}.md`])]) {
  const seen = new Set(); let cur = null;
  const push = () => { if (cur) index.push({ file: id, chapter: title, heading: cur.h, slug: cur.slug, text: cur.t.join(' ').replace(/[*`]/g, '').replace(/!\[[^\]]*\]\([^)]*\)/g, '').slice(0, 400) }); };
  for (const line of fillConsts(readFileSync(srcFile, 'utf8'), id).split('\n')) {
    const m = line.match(/^(#{1,3}) (.*)/);
    if (m) { push(); let sl = slug(m[2]); while (seen.has(sl)) sl += '-2'; seen.add(sl); cur = { h: m[2].replace(/[*`]/g, ''), slug: sl, t: [] }; }
    else if (cur && line.trim()) cur.t.push(line.trim().replace(/^[-*] |^\d+\. /, ''));
  }
  push();
}
writeFileSync(join(dir, 'search.json'), JSON.stringify(index));
console.log(`docs: built ${have.length} help pages`);

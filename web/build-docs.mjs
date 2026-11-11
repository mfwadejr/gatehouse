// Builds the help pages: docs/*.md become plain HTML in public/help/, which ships inside the
// console (and so on the box, at /help/). No dependencies; the Markdown used in the docs is small:
// headings, paragraphs, bullet and numbered lists, **bold**, `code`, and ![alt](img) images.
import { readFileSync, writeFileSync, mkdirSync, rmSync, existsSync, readdirSync, copyFileSync } from 'node:fs';
import { join } from 'node:path';

if (!existsSync('../docs/help')) { console.log('docs build skipped: the docs folder is not next to the web source'); process.exit(0); }

export const ORDER = [
  ['getting-started', 'Getting started'], ['recovery', 'If you are locked out'],
  ['overview', 'Overview'], ['internet', 'Internet'], ['networks', 'Networks'], ['dns', 'DNS'], ['dhcp', 'DHCP'],
  ['firewall', 'Firewall'], ['protection', 'Protection'], ['threats', 'Threats'], ['services', 'Services'], ['vpn', 'VPN'], ['routes', 'Routes'], ['policies', 'Policies'], ['review', 'Review changes'], ['profiles', 'Profiles'],
  ['devices', 'Devices & access'], ['logs', 'Logs'], ['diagnostics', 'Diagnostics'], ['settings', 'Settings'], ['reference', 'Reference'], ['ease-of-use', 'How easy is it?'],
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
writeFileSync(join(dir, 'index.html'), page('Help', '', `<h1>NopeWarden help</h1><p>Start with <strong>Getting started</strong>. If you cannot sign in, open <strong>If you are locked out</strong>. Every page of the console has its own chapter.</p><ul class="chapters">${cards}</ul>`));
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
// What the console's search reads to find help: one entry per heading, with the words under it.
const index = [];
for (const [id, title] of have) {
  const seen = new Set(); let cur = null;
  const push = () => { if (cur) index.push({ file: id, chapter: title, heading: cur.h, slug: cur.slug, text: cur.t.join(' ').replace(/[*`]/g, '').replace(/!\[[^\]]*\]\([^)]*\)/g, '').slice(0, 400) }); };
  for (const line of readFileSync(`../docs/help/${id}.md`, 'utf8').split('\n')) {
    const m = line.match(/^(#{1,3}) (.*)/);
    if (m) { push(); let sl = slug(m[2]); while (seen.has(sl)) sl += '-2'; seen.add(sl); cur = { h: m[2].replace(/[*`]/g, ''), slug: sl, t: [] }; }
    else if (cur && line.trim()) cur.t.push(line.trim().replace(/^[-*] |^\d+\. /, ''));
  }
  push();
}
writeFileSync(join(dir, 'search.json'), JSON.stringify(index));
console.log(`docs: built ${have.length} help pages`);

// Search check: every result the search can open must lead somewhere real. The page must exist, the
// tab must be one of that page's tabs, and the card it scrolls to must be a title that appears in the
// screens. Without this a renamed card or tab quietly turns a search result into a dead end.
import { readFileSync, readdirSync, statSync } from 'node:fs';
import { join } from 'node:path';
const walk = (d) => readdirSync(d).flatMap((n) => { const p = join(d, n); return statSync(p).isDirectory() ? walk(p) : p.endsWith('.jsx') || p.endsWith('.js') ? [p] : []; });
const files = walk('src');
const code = files.filter((f) => !f.endsWith('search.jsx')).map((f) => readFileSync(f, 'utf8')).join('\n').replace(/&amp;/g, '&').replace(/&apos;|&#39;/g, "'").toLowerCase();
const app = readFileSync('src/App.jsx', 'utf8');
const pages = new Set([...app.matchAll(/\{ id: '([a-z-]+)', label/g)].map((m) => m[1]));
// Tabs: pages that use useHashTab list them; Settings and Internet keep their own lists.
const tabs = {};
for (const f of files) {
  const t = readFileSync(f, 'utf8');
  for (const m of t.matchAll(/useHashTab\('([a-z]+)', \[([^\]]*)\], '[a-z]+'(?:,\s*(\d))?\)/g)) {
    const list = [...m[2].matchAll(/'([a-z]+)'/g)].map((x) => x[1]);
    const key = m[3] === '2' ? `${m[1]}/threats` : m[1];
    tabs[key] = list;
  }
}
const settings = readFileSync('src/pages/Settings.jsx', 'utf8');
tabs.settings = [...settings.match(/const TABS = \[([\s\S]*?)\];/)[1].matchAll(/value: '([a-z]+)'/g)].map((m) => m[1]);
tabs.internet = ['connections', 'speed', 'troubleshoot', 'usage'];
const src = readFileSync('src/search.jsx', 'utf8');
const block = src.slice(src.indexOf('const ROWS = ['), src.indexOf('];', src.indexOf('const ROWS = [')));
let bad = 0; const fail = (m) => { console.error('search: ' + m); bad++; };
let n = 0;
for (const m of block.matchAll(/^\s*\[('(?:[^'\\]|\\.)*'|"(?:[^"\\]|\\.)*"), '([^']*)'(?:, ('(?:[^'\\]|\\.)*'|"(?:[^"\\]|\\.)*"), ('(?:[^'\\]|\\.)*'|"(?:[^"\\]|\\.)*")(?:, ('(?:[^'\\]|\\.)*'|"(?:[^"\\]|\\.)*"))?)?\],?$/gm)) {
  n++;
  const label = m[1].slice(1, -1);
  const [page, tab, inner] = m[2].split('/');
  if (!pages.has(page)) { fail(`"${label}" opens the ${page} page, which does not exist`); continue; }
  if (tab && !(tabs[page] || []).includes(tab)) fail(`"${label}" opens ${page}/${tab}, but ${page} has no such tab`);
  if (inner && !(tabs[`${page}/${tab}`] || []).includes(inner)) fail(`"${label}" opens ${m[2]}, but there is no such inner tab`);
  const find = m[5] ? m[5].slice(1, -1).replace(/\\'/g, "'") : '';
  if (find && !code.includes(find.toLowerCase())) fail(`"${label}" scrolls to "${find}", which is not a title on any screen`);
}
// Links inside the screens: go('page') and href="#/page/tab" must open a page that exists.
let links = 0;
for (const f of files) {
  const t = readFileSync(f, 'utf8');
  for (const m of t.matchAll(/\bgo\('([a-z-]+)(?:\/([a-z]+))?'\)|href="#\/([a-z-]+)(?:\/([a-z]+))?"/g)) {
    const page = m[1] || m[3]; const tab = m[2] || m[4];
    links++;
    // go('x') inside Protection/Summary switches a tab of its own page; those names are tabs, not pages.
    if (m[1] && !tab && !pages.has(page) && Object.values(tabs).some((l) => l.includes(page))) continue;
    if (!pages.has(page)) fail(`${f}: a link opens the ${page} page, which does not exist`);
    else if (tab && !(tabs[page] || []).includes(tab)) fail(`${f}: a link opens ${page}/${tab}, but ${page} has no such tab`);
  }
}
if (n < 60) fail(`only ${n} search rows could be read; the check cannot tell whether the list is right`);
if (bad) { console.error(`search check: ${bad} problem(s)`); process.exit(1); }
console.log(`search check ok (${n} results, ${links} links)`);

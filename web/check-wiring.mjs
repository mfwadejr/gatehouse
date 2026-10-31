// Wiring check: every API call in the screens must match a route the Go server registers, with the
// same method. Fails on a call to a route that does not exist. With --unused it also lists routes no
// screen calls (informational: some are used by scripts, the installer or other boxes).
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join } from 'node:path';
import { existsSync } from 'node:fs';
if (!existsSync('../internal')) { console.log('wiring check skipped: the server source is not next to the web source'); process.exit(0); }

const walk = (d, ext) => readdirSync(d).flatMap((n) => { const p = join(d, n); return statSync(p).isDirectory() ? (n === 'node_modules' || n === 'dist' ? [] : walk(p, ext)) : ext.some((e) => p.endsWith(e)) ? [p] : []; });

// ---- routes from the Go code: "METHOD /api/path" in any string
const routes = [];
for (const f of walk('../internal', ['.go']).filter((x) => !x.endsWith('_test.go'))) {
  for (const m of readFileSync(f, 'utf8').matchAll(/"(GET|POST|PUT|DELETE|PATCH) (\/[^"\s]*)"/g)) routes.push({ method: m[1], path: m[2], file: f, used: false });
}
const toRe = (p) => new RegExp(`^${p.replace(/[.*+?^$()|[\]\\]/g, '\\$&').replace(/\{[^}]*\}/g, '[^/]+')}$`);
for (const r of routes) r.re = toRe(r.path.replace(/\/$/, '') || '/');

// ---- calls from the screens
const calls = [];
const norm = (s) => s.replace(/\$\{[^}]*\}/g, 'X').split('?')[0];
const METHODS = { get: 'GET', getWatched: 'GET', useData: 'GET', useStream: null, post: 'POST', put: 'PUT', del: 'DELETE', patch: 'PATCH' };
for (const f of walk('src', ['.jsx', '.js'])) {
  const src = readFileSync(f, 'utf8');
  const line = (i) => src.slice(0, i).split('\n').length;
  for (const m of src.matchAll(/\b(get|getWatched|useData|post|put|del|patch)\(\s*(?:'[^']*',\s*)?([`'"])(\/api\/[^`'"]*)\2/g)) calls.push({ method: METHODS[m[1]], path: norm(m[3]), at: `${f}:${line(m.index)}` });
  for (const m of src.matchAll(/\bapi\(\s*'(GET|POST|PUT|DELETE|PATCH)',\s*([`'"])(\/api\/[^`'"]*)\2/g)) calls.push({ method: m[1], path: norm(m[3]), at: `${f}:${line(m.index)}` });
}

let bad = 0;
for (const c of calls) {
  const p = c.path.replace(/\/$/, '');
  const hits = routes.filter((r) => r.re.test(p) || r.re.test(p.replace(/\/X$/, '')));
  const exact = hits.filter((r) => r.method === c.method);
  exact.forEach((r) => { r.used = true; });
  if (!exact.length) {
    // a segment built from a variable can stand for several routes
    const loose = routes.filter((r) => r.method === c.method && r.path.split('/').length === p.split('/').length && r.path.split('/').every((s, i) => s.startsWith('{') || p.split('/')[i] === 'X' || s === p.split('/')[i]));
    loose.forEach((r) => { r.used = true; });
    if (!loose.length) { console.log(`${c.at}: ${c.method} ${c.path} is not a route the server has${hits.length ? ` (it has ${hits.map((r) => r.method).join('/')})` : ''}`); bad++; }
  }
}
console.log(`${calls.length} calls checked against ${routes.length} routes: ${bad} problem${bad === 1 ? '' : 's'}`);
if (process.argv.includes('--unused')) {
  for (const r of routes.filter((x) => !x.used && x.path.startsWith('/api/')).sort((a, b) => a.path.localeCompare(b.path))) console.log(`unused  ${r.method} ${r.path}   (${r.file.replace('../', '')})`);
}
process.exit(bad ? 1 : 0);

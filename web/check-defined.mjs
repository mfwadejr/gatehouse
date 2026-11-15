// Fails the build when a page uses a component (<Something />) that is neither imported nor defined
// in the same file. Such a page builds fine and then fails in the browser with "X is not defined".
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join } from 'node:path';

const files = [];
const walk = (d) => { for (const n of readdirSync(d)) { const p = join(d, n); if (statSync(p).isDirectory()) walk(p); else if (p.endsWith('.jsx')) files.push(p); } };
walk('src');
let bad = 0;
for (const f of files) {
  const src = readFileSync(f, 'utf8');
  const code = src.replace(/\/\/.*$/gm, '').replace(/\/\*[\s\S]*?\*\//g, '');
  const known = new Set(['Fragment']);
  for (const m of code.matchAll(/\bimport\s+([\s\S]*?)\s+from\s+['"][^'"]+['"]/g)) {
    for (const w of m[1].match(/[A-Za-z_$][\w$]*/g) || []) known.add(w);
  }
  for (const m of code.matchAll(/\b(?:function|const|let|var|class)\s+([A-Za-z_$][\w$]*)/g)) known.add(m[1]);
  for (const m of code.matchAll(/[{,]\s*([A-Za-z_$][\w$]*)\s*(?=[,}=])/g)) known.add(m[1]);
  const used = new Set();
  for (const m of code.matchAll(/<([A-Z][\w$]*)\b/g)) used.add(m[1]);
  for (const name of used) if (!known.has(name)) { console.error(`${f}: <${name}> is used but never imported or defined`); bad++; }
}
if (bad) { console.error(`check-defined: ${bad} problem(s)`); process.exit(1); }
console.log(`check-defined: ${files.length} files, every component is defined.`);

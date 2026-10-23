// Generates tokens.css from tokens.json.
// Usage: node build-tokens.mjs [tokens.json] [tokens.css]
//
// Naming: path segments are joined with dashes, e.g. color.bg.surface -> --color-bg-surface.
// Primitives come out as --primitive-*; lint rules should forbid using them outside this file.
import { readFileSync, writeFileSync } from 'node:fs';

const src = process.argv[2] ?? 'tokens.json';
const out = process.argv[3] ?? 'tokens.css';
const json = JSON.parse(readFileSync(src, 'utf8'));

// Flatten { a: { b: { $value } } } into Map('a.b' -> value)
const flat = new Map();
(function walk(node, path) {
  for (const [k, v] of Object.entries(node)) {
    if (k.startsWith('$')) continue;
    const p = [...path, k];
    if (v && typeof v === 'object' && '$value' in v) flat.set(p.join('.'), String(v.$value));
    else if (v && typeof v === 'object') walk(v, p);
  }
})(Object.fromEntries(Object.entries(json).filter(([k]) => k !== 'themes' && !k.startsWith('$'))), []);

const ref = /^\{([^}]+)\}$/;
const resolve = (value, seen = []) => {
  const m = value.match(ref);
  if (!m) return value;
  const key = m[1];
  if (seen.includes(key)) throw new Error('Circular token reference: ' + [...seen, key].join(' -> '));
  if (!flat.has(key)) throw new Error('Unknown token reference: {' + key + '}');
  return resolve(flat.get(key), [...seen, key]);
};

// A reference to another token is emitted as var(--...) so themes cascade; a literal stays literal.
const cssVar = (path) => '--' + path.replace(/\./g, '-');
const emit = (path, value) => {
  const m = value.match(ref);
  if (m && !m[1].startsWith('primitive.')) return `  ${cssVar(path)}: var(${cssVar(m[1])});`;
  return `  ${cssVar(path)}: ${resolve(value)};`;
};

const lines = ['/* Generated from tokens.json by build-tokens.mjs. Do not edit by hand. */', ':root {', '  color-scheme: dark;'];
const groups = [...flat.keys()];
let last = '';
for (const path of groups) {
  const group = path.split('.').slice(0, path.startsWith('primitive.') ? 2 : 2).join('.');
  if (group !== last) { lines.push(''); lines.push(`  /* ${group} */`); last = group; }
  lines.push(emit(path, flat.get(path)));
}
lines.push('}');

for (const [name, overrides] of Object.entries(json.themes ?? {})) {
  lines.push('', `:root[data-theme="${name}"] {`, `  color-scheme: ${name};`);
  for (const [path, value] of Object.entries(overrides)) {
    if (!flat.has(path)) throw new Error(`Theme "${name}" overrides unknown token: ${path}`);
    lines.push(emit(path, value));
  }
  lines.push('}');
}

writeFileSync(out, lines.join('\n') + '\n');
console.log(`Wrote ${out}: ${flat.size} tokens, ${Object.keys(json.themes ?? {}).length} extra theme(s).`);

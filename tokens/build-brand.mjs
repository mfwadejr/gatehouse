// Generates the files that cannot read tokens.css, from tokens.json:
//   internal/gfx/palette_gen.go   the console's 16 colours (the Go installer and status screens)
//   web/public/favicon.svg        the badge
//   brand/logo.svg                the badge, as a stand-alone file
// brand/make_brand.py reads the same tokens for the boot and console pictures.
//
// Usage (from web/):  node ../tokens/build-brand.mjs            write the files
//                     node ../tokens/build-brand.mjs --check    fail if a file is out of date, or if a
//                                                               brand colour is written out anywhere else
import { readFileSync, writeFileSync, readdirSync, statSync, existsSync } from 'node:fs';
import { join, resolve, dirname, relative } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const check = process.argv.includes('--check');
const json = JSON.parse(readFileSync(join(root, 'tokens/tokens.json'), 'utf8'));

const flat = new Map();
(function walk(node, path) {
  for (const [k, v] of Object.entries(node)) {
    if (k.startsWith('$') || k === 'themes') continue;
    const p = [...path, k];
    if (v && typeof v === 'object' && '$value' in v) flat.set(p.join('.'), String(v.$value));
    else if (v && typeof v === 'object') walk(v, p);
  }
})(json, []);
const value = (key, seen = []) => {
  const v = flat.get(key);
  if (v === undefined) throw new Error('Unknown token: ' + key);
  const m = v.match(/^\{([^}]+)\}$/);
  if (!m) return v;
  if (seen.includes(m[1])) throw new Error('Circular token reference at ' + key);
  return value(m[1], [...seen, key]);
};
const hex = (key) => {
  const v = value(key).toLowerCase();
  if (!/^#[0-9a-f]{6}$/.test(v)) throw new Error(`${key} must be a six-digit hex colour, got ${v}`);
  return v;
};

const start = hex('brand.badge-start'), end = hex('brand.badge-end'), mark = hex('brand.badge-mark');

const svg = (extra = '') =>
  `<svg xmlns="http://www.w3.org/2000/svg"${extra} viewBox="0 0 32 32" fill="none" aria-hidden="true"><defs><linearGradient id="lg" x1="0" y1="0" x2="1" y2="1"><stop stop-color="${start}"/><stop offset="1" stop-color="${end}"/></linearGradient></defs><path d="M16 2.5 5 6.5v8.2c0 6.6 4.5 12.2 11 14.8 6.5-2.6 11-8.2 11-14.8V6.5L16 2.5Z" fill="url(#lg)"/><path d="M11 15.5h10" stroke="${mark}" stroke-width="2.6" stroke-linecap="round"/></svg>\n`;

// The console slots: 0 background, 1 red, 2 green, 3 amber, 4 blue, 5 purple, 6 cyan, 7 text, 8 muted, 9-14 the
// bright forms of 1-6, 15 bright text.
const names = ['bg', 'red', 'green', 'amber', 'blue', 'purple', 'cyan', 'text', 'muted'];
const slots = [...names, 'red', 'green', 'amber', 'blue', 'purple', 'cyan', 'bright'].map((n) => hex('console.' + n));
const rgb = (h) => `{0x${h.slice(1, 3)}, 0x${h.slice(3, 5)}, 0x${h.slice(5, 7)}, 255}`;
const go = [
  '// Code generated from tokens/tokens.json by tokens/build-brand.mjs. DO NOT EDIT.',
  '',
  'package gfx',
  '',
  'import "image/color"',
  '',
  "// Palette is the console's 16 colours: the design tokens, as in the web console.",
  'var Palette = [16]color.RGBA{',
  ...[0, 4, 8, 12].map((i) => '\t' + slots.slice(i, i + 4).map(rgb).join(', ') + ','),
  '}',
  '',
  '// PaletteEscapes sets the same 16 colours on a Linux console (ESC ] P n rrggbb).',
  'const PaletteEscapes = "" +',
  ...slots.map((h, i) => `\t"\\x1b]P${i.toString(16).toUpperCase()}${h.slice(1)}"${i < 15 ? ' +' : ''}`),
  '',
].join('\n');

const files = {
  'internal/gfx/palette_gen.go': go,
  'web/public/favicon.svg': svg(),
  'brand/logo.svg': svg(' width="96" height="96" role="img" aria-label="NopeWarden"'),
};

let bad = 0;
for (const [rel, body] of Object.entries(files)) {
  const p = join(root, rel);
  const have = existsSync(p) ? readFileSync(p, 'utf8') : null;
  if (check) {
    if (have !== body) { console.log(`${rel}: out of date; run npm run build to regenerate it`); bad++; }
  } else if (have !== body) writeFileSync(p, body);
}

if (check) {
  // The brand colours must exist in one place. Anything else that spells them out has drifted from the tokens.
  const generated = new Set(Object.keys(files));
  const skip = new Set(['node_modules', 'dist', '.git', 'archive']);
  const walk = (d) => readdirSync(d).flatMap((n) => {
    if (skip.has(n)) return [];
    const p = join(d, n);
    return statSync(p).isDirectory() ? walk(p) : /\.(jsx?|css|go|py|svg|mjs|html|sh)$/.test(n) ? [p] : [];
  });
  const needles = [start, end].map((h) => new RegExp(h.slice(1), 'i'));
  const decimal = [start, end].map((h) => `${parseInt(h.slice(1, 3), 16)}, ${parseInt(h.slice(3, 5), 16)}, ${parseInt(h.slice(5, 7), 16)}`);
  for (const f of walk(root)) {
    const rel = relative(root, f);
    if (generated.has(rel) || rel === 'tokens/tokens.json' || rel === 'web/src/tokens.css') continue;
    const text = readFileSync(f, 'utf8');
    if (needles.some((n) => n.test(text)) || decimal.some((d) => text.includes(d))) {
      console.log(`${rel}: a badge colour is written out; take it from tokens/tokens.json`);
      bad++;
    }
  }
}
console.log(check ? `brand check: ${bad} problem${bad === 1 ? '' : 's'}` : 'Brand files written.');
process.exit(bad ? 1 : 0);

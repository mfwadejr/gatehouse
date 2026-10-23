// Reports raw design values written outside the token files.
// Usage: node check-tokens.mjs <file> [<file> ...]   (exit code 1 if any are found)
//
// Flags: hex colors, rgb()/rgba()/hsl() colors, and pixel lengths. Values inside var(--...) are fine.
// A real CI version would be a stylelint plugin plus an ESLint rule for inline styles and SVG attributes;
// this is the same idea in its simplest form.
import { readFileSync } from 'node:fs';

const patterns = {
  hex:   /#[0-9a-fA-F]{3,8}\b/g,
  fn:    /\b(?:rgba?|hsla?)\(/g,
  px:    /\b\d+(?:\.\d+)?px\b/g
};

let total = 0;
for (const file of process.argv.slice(2)) {
  const text = readFileSync(file, 'utf8');
  const counts = Object.fromEntries(Object.entries(patterns).map(([k, re]) => [k, (text.match(re) ?? []).length]));
  const sum = counts.hex + counts.fn + counts.px;
  total += sum;
  console.log(`${file}: ${counts.hex} hex, ${counts.fn} rgb/hsl, ${counts.px} px  (${sum} raw values)`);
}
console.log(`Total raw values: ${total}`);
process.exit(total > 0 ? 1 : 0);

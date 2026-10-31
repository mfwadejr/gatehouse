// Checks how the firewall "What for" box reads a typed or pasted list. Run: node check-picker.mjs
import fs from 'fs';
const src = fs.readFileSync(new URL('./src/AddrPicker.jsx', import.meta.url), 'utf8');
const a = src.indexOf('const rePort'), b = src.indexOf('export function ServicePicker');
const code = src.slice(a, b);
const { portOK, splitEntries } = new Function(code + '; return { portOK, splitEntries };')();
const names = [{ name: 'Remote Desktop', tok: 'Remote Desktop' }, { name: 'DNS', tok: 'DNS' }];
const eq = (x, y, m) => { const ok = JSON.stringify(x) === JSON.stringify(y); console.log(ok ? 'ok  ' : 'FAIL', m, ok ? '' : JSON.stringify(x)); if (!ok) process.exitCode = 1; };
eq(splitEntries('tcp/80, tcp/443', names), { good: ['tcp/80', 'tcp/443'], bad: [] }, 'comma list');
eq(splitEntries('tcp/80 tcp/443 udp/1000-2000', names), { good: ['tcp/80', 'tcp/443', 'udp/1000-2000'], bad: [] }, 'spaces');
eq(splitEntries('TCP/80,,udp/53', names), { good: ['tcp/80', 'udp/53'], bad: [] }, 'empty piece and case');
eq(splitEntries('tcp/99999, tcp/80', names), { good: ['tcp/80'], bad: ['tcp/99999'] }, 'out of range kept');
eq(splitEntries('Remote Desktop, tcp/80', names), { good: ['Remote Desktop', 'tcp/80'], bad: [] }, 'service with space');
eq(splitEntries('tcp/90-80', names).bad, ['tcp/90-80'], 'reversed range');
eq(splitEntries('tcp/0', names).bad, ['tcp/0'], 'port zero');
eq(splitEntries('foo', names).bad, ['foo'], 'unknown word');
eq(splitEntries('tcp / 80', names).good, ['tcp/80'], 'spaces around slash');

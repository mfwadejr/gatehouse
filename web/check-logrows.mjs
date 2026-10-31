import assert from 'node:assert/strict';
import { groupRows, rowTitle } from './src/logrows.js';

const c = (o) => ({ kind: 'firewall', verdict: 'allowed', who: 'macbookpro', addr: '192.168.50.106', proto: 'tcp', port: 443, dst: '17.1.1.1', dst_name: 'accounts.meta.com', ...o });
let ok = 0;
const t = (name, fn) => { fn(); ok += 1; console.log('ok  ', name); };

t('repeats merge into one row with a count, newest first', () => {
  const g = groupRows([c({ key: 'a', ts: 30 }), c({ key: 'b', ts: 20 }), c({ key: 'c', ts: 10 })]);
  assert.equal(g.length, 1); assert.equal(g[0].items.length, 3); assert.equal(g[0].key, 'a');
});
t('a different device, port, result or exit stays separate', () => {
  const g = groupRows([c({ key: 'a' }), c({ key: 'b', port: 80 }), c({ key: 'c', verdict: 'blocked' }), c({ key: 'd', via: 'VPN' }), c({ key: 'e', who: 'phone' })]);
  assert.equal(g.length, 5);
});
t('only neighbours merge', () => {
  const g = groupRows([c({ key: 'a' }), c({ key: 'b', port: 80 }), c({ key: 'c' })]);
  assert.equal(g.length, 3);
});
t('an app name replaces the host name on the first line', () => {
  assert.deepEqual(rowTitle(c({ app: 'Instagram', service: 'HTTPS' })), { title: 'Instagram', sub: 'TCP 443 · HTTPS' });
});
t('without an app the host name is the title, the address when there is no name', () => {
  assert.equal(rowTitle(c({})).title, 'accounts.meta.com');
  assert.equal(rowTitle(c({ dst_name: '' })).title, '17.1.1.1');
});
t('DNS and event rows are a name then the kind', () => {
  assert.deepEqual(rowTitle({ kind: 'dns', what: 'api.apple.com', detail: 'A lookup' }), { title: 'api.apple.com', sub: 'DNS · A lookup' });
  assert.deepEqual(rowTitle({ kind: 'event', what: 'Updated', detail: '' }), { title: 'Updated', sub: 'Event' });
});
t('DNS lookups of one name merge too', () => {
  const d = (key) => ({ key, kind: 'dns', verdict: 'allowed', who: 'a', addr: '1', what: 'x.com', detail: 'A lookup' });
  assert.equal(groupRows([d(1), d(2)]).length, 1);
});
console.log(`${ok} checks passed`);

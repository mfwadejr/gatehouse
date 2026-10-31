// How the Logs list turns raw entries into short rows: at most two lines each, near-identical
// consecutive entries merged into one row with a count, and the full detail kept for the click.

export const KIND_NAME = { firewall: 'Connection', dns: 'DNS', event: 'Event' };

const sig = (r) => [r.kind, r.verdict, r.who || '', r.addr || '', r.what || '', r.proto || '', r.dst_name || r.dst || '', r.port || '', r.via || '', r.detail || ''].join('|');

// Consecutive entries that differ only in time become one group, newest first.
export function groupRows(rows) {
  const out = [];
  for (const r of rows) {
    const last = out[out.length - 1];
    const s = sig(r);
    if (last && last.sig === s) last.items.push(r);
    else out.push({ sig: s, key: r.key, first: r, items: [r] });
  }
  return out;
}

// The two lines shown for an entry. An app name wins over a host name; the host stays in the details.
export function rowTitle(r) {
  if (r.kind === 'firewall') {
    const title = r.app || r.dst_name || r.dst || '';
    const where = [r.proto ? r.proto.toUpperCase() : '', r.port ? String(r.port) : ''].filter(Boolean).join(' ');
    const sub = [where, r.service].filter(Boolean).join(' · ');
    return { title, sub };
  }
  const sub = [KIND_NAME[r.kind] || r.kind, r.detail].filter(Boolean).join(' · ');
  return { title: r.what || '', sub };
}

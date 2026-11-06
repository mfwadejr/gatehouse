import React, { useEffect, useState } from 'react';
import { post, put, useData } from '../api.js';
import { Button, Card, Field, Note, NumberInput, useAction, fmt } from '../ui.jsx';

const bytes = (n) => {
  if (!n) return '0 B';
  const u = ['B', 'kB', 'MB', 'GB', 'TB'];
  let i = 0, v = n;
  while (v >= 1000 && i < u.length - 1) { v /= 1000; i++; }
  return `${v >= 100 || i === 0 ? Math.round(v) : v.toFixed(1)} ${u[i]}`;
};

// Storage: what NopeWarden keeps on its own disk, how much room each kind takes, and when it goes.
// Every kind that grows has a rule, so nothing is kept forever by accident.
export default function Storage() {
  const { data, reload } = useData('/api/logs/storage', { every: 30000 });
  const [run, busy] = useAction();
  const [f, setF] = useState(null);
  useEffect(() => { if (data && !f) setF({ long_days: data.long_days, revisions_keep: data.revisions_keep }); }, [data, f]);
  if (!data) return <Card title="Storage"><span className="muted">Loading…</span></Card>;
  const rows = [...data.tables].sort((a, b) => b.bytes - a.bytes);
  const total = data.tables.reduce((a, t) => a + t.bytes, 0);
  const free = data.disk_free, size = data.disk_total;
  const low = size > 0 && free / size < 0.1;
  return (
    <Card title="Storage" desc="What NopeWarden keeps on its own disk, how much room each kind takes, and when it is removed. Sizes are estimates.">
      <div className="stack">
        {low && <Note tone="warn" title="This machine is running low on disk space">{bytes(free)} free of {bytes(size)}. Lower the number of days or the size limit under Logs, What gets recorded, or the settings below.</Note>}
        <p className="small muted">Logs and history use about {bytes(data.logs_bytes)} (limit {fmt.n(data.max_mb)} MB; the limit trims the oldest connections and lookups first, then the smaller histories). Settings and rules use {bytes(data.config_bytes)}.{size > 0 ? ` The disk has ${bytes(free)} free of ${bytes(size)}.` : ''}</p>
        <div className="scroll">
          <table className="table">
            <thead><tr><th>Kept</th><th className="num">Entries</th><th className="num">About</th><th>Oldest</th><th>Removed when</th></tr></thead>
            <tbody>
              {rows.map((t) => (
                <tr key={t.key}>
                  <td><b>{t.label}</b></td>
                  <td className="num">{fmt.n(t.rows)}</td>
                  <td className="num">{bytes(t.bytes)}</td>
                  <td className="small">{t.oldest ? fmt.dt(t.oldest) : '–'}</td>
                  <td className="small muted">{t.policy}</td>
                </tr>
              ))}
            </tbody>
            <tfoot><tr><td><b>Total</b></td><td /><td className="num"><b>{bytes(total)}</b></td><td /><td /></tr></tfoot>
          </table>
        </div>
        {f && (
          <div className="formgrid">
            <Field label="Keep alerts and outages for (days)" hint="Threat alerts and internet outages. 7 to 3650."><NumberInput value={f.long_days} min={7} max={3650} onChange={(v) => setF({ ...f, long_days: v })} /></Field>
            <Field label="Keep this many firewall change copies" hint="The copies you can go back to. The one in use is always kept. 20 to 5000."><NumberInput value={f.revisions_keep} min={20} max={5000} onChange={(v) => setF({ ...f, revisions_keep: v })} /></Field>
          </div>
        )}
        <p className="small muted">Connections, lookups, activity and device traffic follow the days set under Logs, What gets recorded ({data.days} days now). Cleaned up automatically once an hour{data.last_prune ? `; last at ${fmt.dt(data.last_prune)}` : ''}.</p>
        <div className="actions">
          <Button kind="primary" disabled={busy || !f} onClick={() => run(() => put('/api/logs/storage', f), 'Saved.').then(reload)}>Save</Button>
          <Button disabled={busy} onClick={() => run(() => post('/api/logs/prune', {}), 'Cleaned up.').then(reload)}>Clean up now</Button>
        </div>
      </div>
    </Card>
  );
}

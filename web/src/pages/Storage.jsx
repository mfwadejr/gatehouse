import React, { useEffect, useState } from 'react';
import { post, put, useData } from '../api.js';
import { Button, Card, DangerStep, Field, Modal, Note, NumberInput, useAction, useStepUp, fmt } from '../ui.jsx';

const bytes = (n) => {
  if (!n) return '0 B';
  const u = ['B', 'kB', 'MB', 'GB', 'TB'];
  let i = 0, v = n;
  while (v >= 1000 && i < u.length - 1) { v /= 1000; i++; }
  return `${v >= 100 || i === 0 ? Math.round(v) : v.toFixed(1)} ${u[i]}`;
};

const NAMES = { connections: 'Connections', lookups: 'DNS lookups', events: 'What happened (activity)', traffic: 'Data used by each device', countries: 'Data by country', history: 'Overview charts', speed: 'Speed tests', threats: 'Threat alerts', outages: 'Internet outages' };

// Clear by hand: tick what to clear, then confirm with the password (and the code when two-factor is on).
// Admin activity and the firewall change copies are not offered: the first is the record of what people did.
function ClearModal({ kinds, rows, onClose, onDone }) {
  const su = useStepUp();
  const [run, busy] = useAction();
  const [pw, setPw] = useState('');
  const [code, setCode] = useState('');
  const ready = pw.length > 0 && (su.danger !== 'both' || code.trim().length >= 6);
  const n = (k) => rows.find((r) => r.key === k)?.rows || 0;
  return (
    <Modal title="Clear selected" onClose={onClose}
      footer={<><Button onClick={onClose}>Cancel</Button><Button kind="danger" disabled={busy || !ready} onClick={async () => { if (await run(() => post('/api/logs/clear', { kinds, password: pw, code }), 'Cleared.')) { onDone(); onClose(); } }}>Clear selected</Button></>}>
      <p>This removes every entry of these kinds. It cannot be undone:</p>
      <ul>{kinds.map((k) => <li key={k}><b>{NAMES[k]}</b>: {fmt.n(n(k))} {n(k) === 1 ? 'entry' : 'entries'}</li>)}</ul>
      <p className="small muted">Admin activity is not touched. That this was done is written to it.</p>
      <DangerStep su={su} code={code} setCode={setCode} pw={pw} setPw={setPw} />
    </Modal>
  );
}

function ClearByHand({ rows, reload }) {
  const [sel, setSel] = useState([]);
  const [ask, setAsk] = useState(false);
  const kinds = Object.keys(NAMES);
  const all = sel.length === kinds.length;
  return (
    <details className="more">
      <summary>Clear by hand</summary>
      <div className="stack">
        <p className="small muted">Tick what to clear. Admin activity and the firewall change copies are not listed: admin activity is the record of what people did here, and it only ages out.</p>
        <label className="inline"><input type="checkbox" checked={all} onChange={(e) => setSel(e.target.checked ? kinds : [])} /> <b>Select all</b></label>
        <div className="stack">
          {kinds.map((k) => {
            const r = rows.find((x) => x.key === k);
            return <label key={k} className="inline"><input type="checkbox" checked={sel.includes(k)} onChange={(e) => setSel(e.target.checked ? [...sel, k] : sel.filter((x) => x !== k))} /> {NAMES[k]} <span className="muted small">{fmt.n(r?.rows || 0)}</span></label>;
          })}
        </div>
        <div className="actions"><Button kind="danger" disabled={!sel.length} onClick={() => setAsk(true)}>Clear selected…</Button></div>
      </div>
      {ask && <ClearModal kinds={kinds.filter((k) => sel.includes(k))} rows={rows} onClose={() => setAsk(false)} onDone={() => { setSel([]); reload(); }} />}
    </details>
  );
}

const PV = { connections: 'connections', lookups: 'DNS lookups', events: 'activity entries', admin: 'admin activity entries', traffic: 'device traffic rows', countries: 'country rows', threats: 'threat alerts', outages: 'outages' };

// What the rules would remove if they ran right now.
function Preview({ pv }) {
  const parts = Object.keys(PV).filter((k) => pv[k] > 0).map((k) => `${fmt.n(pv[k])} ${PV[k]}`);
  return <p className="small">{parts.length ? `If you clean up now, this would be removed: ${parts.join(', ')}.` : 'If you clean up now, nothing would be removed: everything is inside its period.'}</p>;
}

// Storage: what NopeWarden keeps on its own disk, how much room each kind takes, and when it goes.
// Every kind that grows has a rule, so nothing is kept forever by accident.
export default function Storage() {
  const { data, reload } = useData('/api/logs/storage', { every: 30000 });
  const [run, busy] = useAction();
  const [f, setF] = useState(null);
  const { data: pv, reload: reloadPv } = useData('/api/logs/cleanup-preview', { every: 30000 });
  useEffect(() => { if (data && !f) setF({ days: data.days, long_days: data.long_days, admin_days: data.admin_days, revisions_keep: data.revisions_keep }); }, [data, f]);
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
            <Field label="Keep traffic and events for (days)" hint="Connections, DNS lookups, general activity, device traffic and country data. 1 to 365."><NumberInput value={f.days} min={1} max={365} onChange={(v) => setF({ ...f, days: v })} /></Field>
            <Field label="Keep alerts and outages for (days)" hint="Threat alerts and internet outages. 7 to 3650."><NumberInput value={f.long_days} min={7} max={3650} onChange={(v) => setF({ ...f, long_days: v })} /></Field>
            <Field label="Keep admin activity for (days)" hint="Who signed in and what they changed. Default 365. 30 to 3650."><NumberInput value={f.admin_days} min={30} max={3650} onChange={(v) => setF({ ...f, admin_days: v })} /></Field>
            <Field label="Keep this many firewall change copies" hint="The copies you can go back to. The one in use is always kept. 20 to 5000."><NumberInput value={f.revisions_keep} min={20} max={5000} onChange={(v) => setF({ ...f, revisions_keep: v })} /></Field>
          </div>
        )}
        {pv && <Preview pv={pv.would_remove} last={pv} />}
        <p className="small muted">Cleaned up automatically once an hour{data.last_prune ? `; last at ${fmt.dt(data.last_prune)}${data.last_prune && pv ? `, ${fmt.n(pv.last_removed)} entries removed` : ''}` : ''}.</p>
        <ClearByHand rows={data.tables} reload={() => { reload(); reloadPv(); }} />
        <div className="actions">
          <Button kind="primary" disabled={busy || !f} onClick={() => run(() => put('/api/logs/storage', f), 'Saved.').then(() => { reload(); reloadPv(); })}>Save</Button>
          <Button disabled={busy} onClick={() => run(() => post('/api/logs/prune', {}), 'Cleaned up.').then(() => { reload(); reloadPv(); })}>Clean up now</Button>
        </div>
      </div>
    </Card>
  );
}

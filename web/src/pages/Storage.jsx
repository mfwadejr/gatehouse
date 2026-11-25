import React, { useEffect, useState } from 'react';
import { post, put, useData } from '../api.js';
import { Button, Card, DangerStep, Field, Modal, Note, NumberInput, Select, useAction, useStepUp, fmt } from '../ui.jsx';

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

const HISTORY_LABEL = { 90: '90 days', 365: '1 year', 730: '2 years', 1825: '5 years', 3650: '10 years', 0: 'Everything' };
const SPEED_LABEL = HISTORY_LABEL;

// What the rules would remove if they ran right now.
function Preview({ pv }) {
  const parts = Object.keys(PV).filter((k) => pv[k] > 0).map((k) => `${fmt.n(pv[k])} ${PV[k]}`);
  return <p className="small">{parts.length ? `If you clean up now, this would be removed: ${parts.join(', ')}.` : 'If you clean up now, nothing would be removed: everything is inside its period.'}</p>;
}

// Where each kind of record is shown, so the table says where to look as well as how long it is kept.
const SEEN = {
  connections: 'Logs, Reports, Threats', lookups: 'Logs, DNS, Reports', events: 'Logs', admin: 'Logs, Admin activity',
  traffic: 'Logs, Internet usage', countries: 'Logs, Overview map', history: 'Overview, DNS, DHCP, Networks, Reports',
  revisions: 'Firewall, Change history; Review', speed: 'Internet, Speed; Reports', threats: 'Threats, Overview', outages: 'Internet, Reports',
};

// Everything that is kept, in four groups. The keep rule for a group sits right under its rows.
const GROUPS = [
  { id: 'history', title: 'History you look at', keys: ['connections', 'lookups', 'events', 'admin', 'traffic', 'countries'] },
  { id: 'charts', title: 'Charts', keys: ['history'] },
  { id: 'safety', title: 'Safety copies', keys: ['revisions'] },
  { id: 'records', title: 'Records', keys: ['speed', 'threats', 'outages'] },
];

function Rows({ rows, extra = [] }) {
  return (
    <div className="scroll">
      <table className="table">
        <thead><tr><th>Kept</th><th>Seen on</th><th className="num">Entries</th><th className="num">About</th><th>Oldest</th><th>Removed when</th></tr></thead>
        <tbody>
          {rows.map((t) => (
            <tr key={t.key}>
              <td><b>{t.label}</b></td>
              <td className="small muted">{SEEN[t.key] || ''}</td>
              <td className="num">{fmt.n(t.rows)}</td>
              <td className="num">{bytes(t.bytes)}</td>
              <td className="small">{t.oldest ? fmt.dt(t.oldest) : '–'}</td>
              <td className="small muted">{t.policy}</td>
            </tr>
          ))}
          {extra.map((t) => (
            <tr key={t.label}>
              <td><b>{t.label}</b></td>
              <td className="small muted">{t.seen}</td>
              <td className="num">{t.n == null ? '–' : fmt.n(t.n)}</td>
              <td className="num">–</td>
              <td className="small">–</td>
              <td className="small muted">{t.policy}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

// Storage: everything NopeWarden keeps on its own disk, how much room each kind takes, where you see it
// and how long it stays. Every kind that grows has a rule, so nothing is kept forever by accident.
export default function Storage() {
  const { data, reload } = useData('/api/logs/storage', { every: 30000 });
  const { data: bk } = useData('/api/backups', { every: 60000 });
  const [run, busy] = useAction();
  const [f, setF] = useState(null);
  const { data: pv, reload: reloadPv } = useData('/api/logs/cleanup-preview', { every: 30000 });
  useEffect(() => {
    if (data && !f) setF({ days: data.days, long_days: data.long_days, admin_days: data.admin_days, revisions_keep: data.revisions_keep, history_days: data.history_days, hourly_days: data.hourly_days, speed_days: data.speed_days, max_mb: data.max_mb });
  }, [data, f]);
  if (!data) return <Card title="Storage"><span className="muted">Loading…</span></Card>;
  const by = Object.fromEntries(data.tables.map((t) => [t.key, t]));
  const total = data.tables.reduce((a, t) => a + t.bytes, 0);
  const free = data.disk_free, size = data.disk_total;
  const low = size > 0 && free / size < 0.1;
  const set = (k) => (v) => setF({ ...f, [k]: v });
  const rowsOf = (g) => g.keys.map((k) => by[k]).filter(Boolean);
  const safetyExtra = [
    { label: 'Backup files', seen: 'Settings, Backup & recovery', n: bk ? (bk.backups || []).length : null, policy: bk ? `The newest ${bk.config.keep}. Set on Backup & recovery` : 'Set on Backup & recovery' },
    { label: 'Backup history', seen: 'Settings, Backup & recovery', n: null, policy: 'The newest 500 lines. Fixed. Kept when you restore' },
  ];
  const save = () => run(() => put('/api/logs/storage', f), 'Saved.').then(() => { reload(); reloadPv(); });
  return (
    <>
      {low && <Note tone="warn" title="This machine is running low on disk space">{bytes(free)} free of {bytes(size)}. Lower the days or the size limit below.</Note>}
      <Card title="Storage" desc={`Everything NopeWarden keeps on its own disk, where you see it and how long it stays. Sizes are estimates. About ${bytes(data.logs_bytes)} of logs and history (limit ${fmt.n(data.max_mb)} MB) and ${bytes(total)} in all. ${bytes(free)} of ${bytes(size)} is free on this disk.`}>
        <p className="small muted">Settings and rules use {bytes(data.config_bytes)}. Each group below lists what is kept, then how long, right under the table.</p>
      </Card>
      {f && GROUPS.map((g) => (
        <Card key={g.id} title={g.title}>
          <div className="stack">
            <Rows rows={rowsOf(g)} extra={g.id === 'safety' ? safetyExtra : []} />
            {g.id === 'history' && (
              <>
                <div className="formgrid">
                  <Field label="Keep traffic and events for (days)" hint="Connections, DNS lookups, general activity, device traffic and country data. 1 to 365."><NumberInput value={f.days} min={1} max={365} onChange={set('days')} /></Field>
                  <Field label="Never use more than (MB)" hint="The size limit for these logs. 10 to 20000."><NumberInput value={f.max_mb} min={10} max={20000} onChange={set('max_mb')} /></Field>
                  <Field label="Keep admin activity for (days)" hint="Who signed in and what they changed. Default 365. 30 to 3650."><NumberInput value={f.admin_days} min={30} max={3650} onChange={set('admin_days')} /></Field>
                </div>
                <p className="small muted">The size limit trims the oldest connections and lookups first, then the smaller histories, so something can go sooner than the days say. Totals for each whole internet connection are kept for at least 100 days whatever you choose here, because billing periods and the provider report need them. Admin activity is never trimmed by size.</p>
              </>
            )}
            {g.id === 'charts' && (
              <>
                <div className="formgrid">
                  <Field label="Keep chart history for" hint={`One point a day for every chart, with the day's lowest and highest. About ${bytes(data.history_year_bytes)} for each year kept. The size limit never trims it.`}>
                    <Select value={String(f.history_days)} onChange={(v) => setF({ ...f, history_days: Number(v) })} options={(data.history_choices || []).map((d) => ({ value: String(d), label: HISTORY_LABEL[d] || `${d} days` }))} />
                  </Field>
                  <Field label="Keep hourly chart detail for" hint={`Hourly points for the charts. About ${bytes(data.hourly_month_bytes)} for each extra 30 days. 31 days is the default.`}>
                    <Select value={String(f.hourly_days)} onChange={(v) => setF({ ...f, hourly_days: Number(v) })} options={(data.hourly_choices || []).map((d) => ({ value: String(d), label: `${d} days` }))} />
                  </Field>
                </div>
                {f.history_days === 0 && <Note tone="warn">Everything is kept until you clear it by hand or reset the box. That is a small amount of data (about {bytes(data.history_year_bytes)} a year).</Note>}
              </>
            )}
            {g.id === 'safety' && (
              <div className="formgrid">
                <Field label="Keep this many firewall change copies" hint="The copies you can go back to. The one in use is always kept. 20 to 5000."><NumberInput value={f.revisions_keep} min={20} max={5000} onChange={set('revisions_keep')} /></Field>
              </div>
            )}
            {g.id === 'records' && (
              <>
                <div className="formgrid">
                  <Field label="Keep speed tests for" hint="Every test, kept for the Speed page and the provider report. A test is small, so even ten years takes little room. The size limit never trims them.">
                    <Select value={String(f.speed_days)} onChange={(v) => setF({ ...f, speed_days: Number(v) })} options={(data.speed_choices || []).map((d) => ({ value: String(d), label: SPEED_LABEL[d] || `${d} days` }))} />
                  </Field>
                  <Field label="Keep alerts and outages for (days)" hint="Threat alerts and internet outages. 7 to 3650."><NumberInput value={f.long_days} min={7} max={3650} onChange={set('long_days')} /></Field>
                </div>
                {f.speed_days === 0 && <Note tone="warn">Speed tests are kept until you clear them by hand or reset the box.</Note>}
                {pv && pv.would_remove.speed > 0 && f.speed_days === data.speed_days && <p className="small">Saved as it is, {fmt.n(pv.would_remove.speed)} older speed tests would be removed at the next clean-up.</p>}
              </>
            )}
          </div>
        </Card>
      ))}
      <Card title="Clean up">
        <div className="stack">
          {pv && <Preview pv={pv.would_remove} last={pv} />}
          {f && f.speed_days !== data.speed_days && f.speed_days > 0 && (f.speed_days < data.speed_days || data.speed_days === 0) && <Note tone="warn" title="Saving this will remove older speed tests">Tests older than {SPEED_LABEL[f.speed_days] || `${f.speed_days} days`} are removed when you save, and cannot come back.</Note>}
          <p className="small muted">Cleaned up automatically once an hour{data.last_prune ? `; last at ${fmt.dt(data.last_prune)}${data.last_prune && pv ? `, ${fmt.n(pv.last_removed)} entries removed` : ''}` : ''}.</p>
          <ClearByHand rows={data.tables} reload={() => { reload(); reloadPv(); }} />
          <div className="actions">
            <Button kind="primary" disabled={busy || !f} onClick={save}>Save</Button>
            <Button disabled={busy} onClick={() => run(() => post('/api/logs/prune', {}), 'Cleaned up.').then(() => { reload(); reloadPv(); })}>Clean up now</Button>
          </div>
        </div>
      </Card>
    </>
  );
}

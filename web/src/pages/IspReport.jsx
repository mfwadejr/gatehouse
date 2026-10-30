import React, { useState } from 'react';
import { put, useData } from '../api.js';
import { Bar, Button, Card, Field, Input, Modal, Note, Pill, Select, ToggleRow, fmt, useAction } from '../ui.jsx';

const gb = (n) => (n >= 1e12 ? `${(n / 1e12).toFixed(2)} TB` : `${(n / 1e9).toFixed(1)} GB`);
const day = (t) => new Date(t * 1000).toLocaleDateString([], { day: 'numeric', month: 'short' });
const dur = (s) => (s >= 3600 ? `${Math.floor(s / 3600)} h ${Math.floor((s % 3600) / 60)} min` : `${Math.max(1, Math.floor(s / 60))} min`);

// Data use in the current billing period, with an optional monthly limit. Off by default: many plans
// have no limit. Usage here is the whole internet connection, split by device below.
export function DataUse() {
  const { data, reload } = useData('/api/usage', { every: 60000 });
  const [edit, setEdit] = useState(null);
  const [run, busy] = useAction();
  if (!data) return null;
  const total = data.up + data.down;
  const save = async () => {
    const r = await run(() => put('/api/usage', { on: edit.on, cap_gb: Number(edit.cap_gb) || 0, start_day: Number(edit.start_day) || 1, levels: edit.levels }), 'Saved.');
    if (r) { setEdit(null); reload(); }
  };
  return (
    <Card title="Data use" desc={`${day(data.from)} to ${day(data.to - 1)}${data.on ? ` · limit ${data.cap_gb} GB` : ' · no limit set'}`}
      actions={<Button small onClick={() => setEdit({ on: data.on, cap_gb: data.cap_gb || '', start_day: data.start_day, levels: data.levels })}>{data.on ? 'Change limit' : 'Set a limit'}</Button>}>
      <div className="stack">
        <div><strong>{gb(total)}</strong> used <span className="muted small">· {gb(data.down)} down, {gb(data.up)} up</span></div>
        {data.on && data.cap_gb > 0 && (
          <div><Bar pct={Math.min(100, data.pct)} /><span className="small muted">{data.pct.toFixed(0)}% of the limit {data.pct >= 100 && <Pill tone="bad">Over</Pill>}{data.pct >= 80 && data.pct < 100 && <Pill tone="warn">Close</Pill>}</span></div>
        )}
        {data.top.length > 0 && (
          <div className="list">
            {data.top.map((d) => <div className="item" key={d.mac}><div className="name">{d.name}</div><div className="meta">{gb(d.up + d.down)}</div></div>)}
          </div>
        )}
        <span className="small muted">Counted for the whole internet connection, and per device only for as long as device history is kept (Logs settings).</span>
      </div>
      {edit && (
        <Modal title="Monthly data limit" onClose={() => setEdit(null)} footer={<><Button onClick={() => setEdit(null)}>Cancel</Button><Button kind="primary" disabled={busy} onClick={save}>Save</Button></>}>
          <ToggleRow title="My plan has a monthly limit" desc="Leave off for an unlimited plan." checked={edit.on} onChange={(v) => setEdit({ ...edit, on: v })} />
          {edit.on && (
            <div className="formgrid">
              <Field label="Limit (GB)"><Input value={String(edit.cap_gb)} onChange={(v) => setEdit({ ...edit, cap_gb: v })} inputMode="numeric" /></Field>
              <Field label="Billing period starts on day" hint="For example 15 for the 15th to the 14th."><Select value={String(edit.start_day)} onChange={(v) => setEdit({ ...edit, start_day: Number(v) })} options={Array.from({ length: 28 }, (_, i) => ({ value: String(i + 1), label: String(i + 1) }))} /></Field>
            </div>
          )}
          <Note>Warnings at {edit.levels.join('% and ')}% go out as alerts if you turn on "I am close to my data limit" in Settings, Alerts.</Note>
        </Modal>
      )}
    </Card>
  );
}

// The provider report: outages, and speed against the plan, for one month, printable.
export function IspReport() {
  const now = new Date();
  const months = Array.from({ length: 6 }, (_, i) => { const d = new Date(now.getFullYear(), now.getMonth() - i, 1); const v = `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}`; return { value: v, label: d.toLocaleDateString([], { month: 'long', year: 'numeric' }) }; });
  const [m, setM] = useState(months[0].value);
  const { data } = useData(`/api/isp/report?month=${m}`, { every: 120000 });
  return (
    <Card title="Provider report card" desc="Outages and speed for a month, to show your provider. Built from what Gatehouse measured itself."
      actions={<a className="btn small" href={`/api/isp/report/print?month=${m}`} target="_blank" rel="noreferrer">Open to print</a>}>
      <div style={{ maxWidth: '16rem' }}><Select value={m} onChange={setM} options={months} /></div>
      {data && (
        <div className="stack" style={{ marginTop: 'var(--space-2)' }}>
          <div><strong>{data.up_pct.toFixed(2)}% up</strong> <span className="muted small">· down {dur(data.down_secs)} in {data.outages.length} outage{data.outages.length === 1 ? '' : 's'} (a minute or longer)</span></div>
          {data.links.length === 0 ? <span className="small muted">No speed tests this month. Turn on the daily test under Speed test to build this part.</span>
            : data.links.map((l) => <div key={l.link} className="small">{l.link}: {l.tests} test{l.tests === 1 ? '' : 's'}, average {l.avg_down_mbps.toFixed(0)} down / {l.avg_up_mbps.toFixed(0)} up Mb/s{l.plan_down_mbps ? `, plan ${l.plan_down_mbps} Mb/s, ${l.below_plan} test(s) under 80%` : ' (enter your plan speed to compare)'}</div>)}
          <span className="small muted">Built on {data.speed_tests} speed test{data.speed_tests === 1 ? '' : 's'}. Packet loss is not measured.</span>
        </div>
      )}
    </Card>
  );
}

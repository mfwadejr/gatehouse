import React, { useEffect, useState } from 'react';
import { put, useData } from '../api.js';
import { Bar, Button, Card, DateRange, Field, Input, Note, Pill, Select, ToggleRow, useAction } from '../ui.jsx';

const gb = (n) => (n >= 1e12 ? `${(n / 1e12).toFixed(2)} TB` : `${(n / 1e9).toFixed(1)} GB`);
const day = (t) => new Date(t * 1000).toLocaleDateString([], { day: 'numeric', month: 'short' });
const dur = (s) => (s >= 3600 ? `${Math.floor(s / 3600)} h ${Math.floor((s % 3600) / 60)} min` : `${Math.max(1, Math.floor(s / 60))} min`);

// Data use for one internet connection or for all of them, over a period you choose. A limit
// belongs to one connection (each plan has its own size and billing day), so it lives in its own
// card below and is set per connection. Off by default: many plans have no limit.
const RANGES = [
  { value: 'billing', label: 'This billing period' },
  { value: 'this_month', label: 'This month' },
  { value: 'last_month', label: 'Last month' },
  { value: 'last_30', label: 'Last 30 days' },
  { value: 'last_7', label: 'Last 7 days' },
  { value: 'custom', label: 'Choose dates…' },
];

export function DataUse() {
  const [link, setLink] = useState(null); // null until the first answer says which connection to open on
  const [rng, setRng] = useState('billing');
  const [custom, setCustom] = useState({ from: '', to: '' });
  const path = link === null ? '/api/usage' : `/api/usage?link=${encodeURIComponent(link)}&range=${rng}${rng === 'custom' ? `&from=${custom.from}&to=${custom.to}` : ''}`;
  const { data, reload } = useData(path, { every: 60000, enabled: rng !== 'custom' || (!!custom.from && !!custom.to) || link === null });
  useEffect(() => { if (link === null && data) { setLink(data.link || ''); setRng(data.link ? 'billing' : 'this_month'); } }, [data, link]);
  if (!data) return null;
  const links = data.links || [];
  const total = data.up + data.down;
  const linkOpts = [{ value: '', label: 'All connections' }, ...links.map((l) => ({ value: l.name, label: l.name }))];
  const rangeOpts = link ? RANGES : RANGES.filter((r) => r.value !== 'billing');
  const pickLink = (v) => { setLink(v); if (!v && rng === 'billing') setRng('this_month'); };
  return (
    <>
      <Card title="Data use" desc={`${day(data.from)} to ${day(data.to - 1)}${data.link && data.range === 'billing' ? (data.on ? ` · limit ${data.cap_gb} GB` : ' · no limit set') : ''}`}>
        <div className="stack">
          <div className="formgrid">
            {links.length > 0 && <Field label="Connection"><Select value={link ?? ''} onChange={pickLink} options={linkOpts} /></Field>}
            <Field label="Period"><Select value={rng} onChange={setRng} options={rangeOpts} /></Field>
            {rng === 'custom' && <Field label="From and to"><DateRange from={custom.from} to={custom.to} onChange={(f, t) => setCustom({ from: f, to: t })} max={new Date().toISOString().slice(0, 10)} /></Field>}
          </div>
          <div><strong>{gb(total)}</strong> used <span className="muted small">· {gb(data.down)} down, {gb(data.up)} up</span></div>
          {data.link && data.range === 'billing' && data.on && data.cap_gb > 0 && (
            <div><Bar pct={Math.min(100, data.pct)} /><span className="small muted">{data.pct.toFixed(0)}% of the limit {data.pct >= 100 && <Pill tone="bad">Over</Pill>}{data.pct >= 80 && data.pct < 100 && <Pill tone="warn">Close</Pill>}</span></div>
          )}
          {data.note && <Note>{data.note}</Note>}
          {data.top.length > 0 && (
            <div className="list">
              {data.top.map((d) => <div className="item" key={d.mac}><div className="name">{d.name}</div><div className="meta">{gb(d.up + d.down)}</div></div>)}
            </div>
          )}
          <span className="small muted">{data.link ? `Counted on ${data.link}, and per device for what each device sent and received through it` : 'Counted across every internet connection together, and per device'}, for as long as device history is kept (Logs settings). Traffic that goes through a VPN is counted on the VPN.</span>
        </div>
      </Card>
      <DataLimit data={data} link={data.link} onSaved={reload} />
    </>
  );
}

// The limit for one connection: its size, the day its billing period starts, and when to warn.
function DataLimit({ data, link, onSaved }) {
  const [f, setF] = useState(null);
  const [run, busy] = useAction();
  useEffect(() => { setF({ on: data.on, cap_gb: data.cap_gb || '', start_day: data.start_day, levels: data.levels }); }, [link, data.on, data.cap_gb, data.start_day, (data.levels || []).join()]);
  if (!f) return null;
  if (!link) {
    return (
      <Card title="Data limit" desc="A limit belongs to one internet connection, because each plan has its own size and billing day.">
        <span className="small muted">Choose a connection above to set or change its limit.</span>
      </Card>
    );
  }
  const same = f.on === data.on && String(f.cap_gb || '') === String(data.cap_gb || '') && f.start_day === data.start_day && f.levels.join() === (data.levels || []).join();
  const save = async () => {
    const r = await run(() => put(`/api/usage/limit?link=${encodeURIComponent(link)}`, { on: f.on, cap_gb: Number(f.cap_gb) || 0, start_day: Number(f.start_day) || 1, levels: f.levels }), 'Saved.');
    if (r) onSaved();
  };
  const lv = [50, 80, 90, 100];
  const toggleLevel = (n) => setF({ ...f, levels: f.levels.includes(n) ? f.levels.filter((x) => x !== n) : [...f.levels, n].sort((a, b) => a - b) });
  return (
    <Card title={`Data limit on ${link}`} desc="Your plan's monthly allowance for this connection. NopeWarden counts against it and warns you; it never cuts you off.">
      <div className="stack">
        <ToggleRow title="This connection has a monthly limit" desc="Leave off for an unlimited plan." checked={f.on} onChange={(v) => setF({ ...f, on: v })} />
        {f.on && (
          <>
            <div className="formgrid">
              <Field label="Limit (GB)"><Input value={String(f.cap_gb)} onChange={(v) => setF({ ...f, cap_gb: v })} inputMode="numeric" /></Field>
              <Field label="Billing period starts on day" hint="For example 15 for the 15th to the 14th."><Select value={String(f.start_day)} onChange={(v) => setF({ ...f, start_day: Number(v) })} options={Array.from({ length: 28 }, (_, i) => ({ value: String(i + 1), label: String(i + 1) }))} /></Field>
            </div>
            <Field label="Warn me at">
              <div className="inline">{lv.map((n) => <label key={n} className="inline"><input type="checkbox" checked={f.levels.includes(n)} onChange={() => toggleLevel(n)} /> {n}%</label>)}</div>
            </Field>
            <Note title="At the limit">NopeWarden sends an alert at each level you tick, once per billing period, if you turn on "I am close to my data limit" in Settings, Alerts. Nothing is blocked or slowed.</Note>
          </>
        )}
        <div className="actions"><Button kind="primary" disabled={busy || same} onClick={save}>Save</Button></div>
      </div>
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
    <Card title="Provider report card" desc="Outages and speed for a month, to show your provider. Built from what NopeWarden measured itself."
      actions={<a className="btn small" href={`/api/isp/report/print?month=${m}`} target="_blank" rel="noreferrer">Open to print</a>}>
      <div style={{ maxWidth: '16rem' }}><Select value={m} onChange={setM} options={months} /></div>
      {data && (
        <div className="stack mt2">
          <div><strong>{data.up_pct.toFixed(2)}% up</strong> <span className="muted small">· down {dur(data.down_secs)} in {data.outages.length} outage{data.outages.length === 1 ? '' : 's'} (a minute or longer)</span></div>
          {data.links.length === 0 ? <span className="small muted">No speed tests this month. Turn on the daily test under Speed test to build this part.</span>
            : data.links.map((l) => <div key={l.link} className="small">{l.link}: {l.tests} test{l.tests === 1 ? '' : 's'}, average {l.avg_down_mbps.toFixed(0)} down / {l.avg_up_mbps.toFixed(0)} up Mb/s{l.plan_down_mbps ? `, plan ${l.plan_down_mbps} Mb/s, ${l.below_plan} test(s) under 80%` : ' (enter your plan speed to compare)'}</div>)}
          <span className="small muted">Built on {data.speed_tests} speed test{data.speed_tests === 1 ? '' : 's'}. Packet loss is not measured.</span>
        </div>
      )}
    </Card>
  );
}

import React, { useCallback, useEffect, useRef, useState } from 'react';
import { hashQuery, setHashQuery } from '../hashtab.js';
import { del, post, saveFile, useData } from '../api.js';
import { go } from '../App.jsx';
import { Logo } from '../logo.jsx';
import { Bar, Button, Card, DateRange, Empty, Field, Input, Limited, Note, Segmented, Select, useAction } from '../ui.jsx';

// Reports: pick what to report on, the period, filters and grouping, then see a summary, a chart and
// the rows. A report can be saved by name, run again, printed, or exported as PDF, CSV, Excel or JSON.

export const REPORT_PRESET_KEY = 'nw.report.preset';

const SUBJECTS = [
  { value: 'links', label: 'Internet links' },
  { value: 'dns', label: 'DNS' },
  { value: 'threats', label: 'Threats' },
  { value: 'firewall', label: 'Firewall connections' },
];
const RANGES = [
  { value: '1', label: '24 h' }, { value: '7', label: '7 days' }, { value: '30', label: '30 days' },
  { value: '60', label: '60 days' }, { value: '90', label: '90 days' }, { value: '365', label: '1 year' }, { value: 'custom', label: 'Exact dates' },
];
const TIME = [{ value: '', label: 'Automatic' }, { value: 'hour', label: 'By hour' }, { value: 'day', label: 'By day' }, { value: 'week', label: 'By week' }, { value: 'month', label: 'By month' }];
const GROUPS = {
  links: TIME,
  dns: [...TIME, { value: 'device', label: 'By device' }, { value: 'name', label: 'By name' }, { value: 'result', label: 'By how it was answered' }, { value: 'list', label: 'By list that blocked it' }],
  threats: [...TIME, { value: 'category', label: 'By kind' }, { value: 'device', label: 'By device' }, { value: 'rule', label: 'By what it looked for' }, { value: 'severity', label: 'By severity' }],
  firewall: [...TIME, { value: 'port', label: 'By port' }, { value: 'source', label: 'By source' }, { value: 'destination', label: 'By destination' }, { value: 'network', label: 'By network' }, { value: 'rule', label: 'By rule' }, { value: 'action', label: 'By allowed or blocked' }],
};
const ANY = { value: '', label: 'Any' };
const FILTERS = {
  links: [
    { key: 'link', label: 'Which connection', kind: 'link' },
    { key: 'min_delay', label: 'Average delay at least (ms)', kind: 'number' },
    { key: 'min_loss', label: 'Average loss at least (%)', kind: 'number' },
    { key: 'min_drop', label: 'Drops of at least (seconds)', kind: 'number' },
  ],
  dns: [
    { key: 'result', label: 'Answer', options: [ANY, { value: 'blocked', label: 'Blocked' }, { value: 'allowed', label: 'Allowed' }, { value: 'failed', label: 'Failed' }] },
    { key: 'list', label: 'Blocked by list or rule', kind: 'text' },
    { key: 'device', label: 'Device', kind: 'text' },
    { key: 'name', label: 'Name contains', kind: 'text' },
    { key: 'slow_ms', label: 'Answer took at least (ms)', kind: 'number' },
  ],
  threats: [
    { key: 'rule', label: 'What it looked for contains', kind: 'text' },
    { key: 'device', label: 'Device', kind: 'text' },
    { key: 'severity', label: 'Severity', options: [ANY, { value: '1', label: 'High only' }, { value: '2', label: 'Medium and higher' }, { value: '3', label: 'All' }] },
    { key: 'category', label: 'Kind', kind: 'text' },
  ],
  firewall: [
    { key: 'action', label: 'Result', options: [ANY, { value: 'blocked', label: 'Blocked' }, { value: 'allowed', label: 'Allowed' }] },
    { key: 'direction', label: 'Direction', options: [ANY, { value: 'in', label: 'In from the internet' }, { value: 'out', label: 'Out to the internet' }] },
    { key: 'port', label: 'Port', kind: 'number' },
    { key: 'network', label: 'Network', kind: 'text' },
    { key: 'address', label: 'Address or device', kind: 'text' },
    { key: 'rule', label: 'Rule contains', kind: 'text' },
  ],
};

const LINE = ['rc-s0', 'rc-s1', 'rc-s2', 'rc-s3', 'rc-s4', 'rc-s5'];
const SWATCH = ['rc-b0', 'rc-b1', 'rc-b2', 'rc-b3', 'rc-b4', 'rc-b5'];
const ymd = (d) => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
const dayMs = (k) => { const [y, m, d] = k.split('-').map(Number); return new Date(y, m - 1, d).getTime(); };
const when = (t) => new Date(t * 1000).toLocaleString([], { year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' });
const dayOnly = (t) => new Date(t * 1000).toLocaleDateString([], { year: 'numeric', month: 'short', day: 'numeric' });

// The ISP report: how each connection behaved over the last 30 days, ready to send to a provider.
const ISP_TEMPLATE = { subject: 'links', range: '30', group: 'day', filters: {} };

// What a report is made from, so the page can tell when the form no longer matches the result.
const reqKey = (s) => JSON.stringify({ subject: s.subject, range: s.range, custom: s.range === 'custom' ? s.custom : null, group: s.group || '', filters: toRequest(s).filters });

function toRequest(s) {
  const now = Math.floor(Date.now() / 1000);
  let from, to = 0;
  if (s.range === 'custom' && s.custom?.from && s.custom?.to) {
    from = Math.floor(dayMs(s.custom.from) / 1000);
    to = Math.floor((dayMs(s.custom.to) + 86400000) / 1000);
    if (to > now) to = 0;
  } else from = now - Number(s.range === 'custom' ? 7 : s.range) * 86400;
  const filters = {};
  Object.entries(s.filters || {}).forEach(([k, v]) => { if (String(v).trim() !== '') filters[k] = String(v).trim(); });
  return { subject: s.subject, from, to, group: s.group || '', filters };
}

// The width the chart is really drawn at, so its text and lines are the size they are everywhere else
// instead of being stretched with the page.
function useWidth() {
  const ref = useRef(null);
  const [w, setW] = useState(640);
  useEffect(() => {
    const el = ref.current;
    if (!el) return undefined;
    const read = () => setW(Math.max(280, Math.round(el.getBoundingClientRect().width)));
    read();
    if (typeof ResizeObserver === 'undefined') return undefined;
    const ro = new ResizeObserver(read);
    ro.observe(el);
    return () => ro.disconnect();
  }, []);
  return [ref, w];
}

function LineChart({ chart: raw, from, to }) {
  const chart = { ...raw, series: (raw.series || []).map((x) => ({ ...x, pts: x.pts || [] })) };
  const [ref, W] = useWidth();
  const H = 200, L = 48, B = 24;
  const all = chart.series.flatMap((s) => s.pts.map((p) => p[1]));
  if (!all.length) return <Empty>No data in this period.</Empty>;
  const hi = Math.max(1e-9, ...all), lo = Math.min(0, ...all);
  const span = Math.max(1, to - from);
  const x = (t) => L + ((t - from) / span) * (W - L - 6);
  const y = (v) => H - B - ((v - lo) / (hi - lo)) * (H - B - 8);
  const tick = (v) => (Math.abs(v) >= 100 ? Math.round(v) : Math.round(v * 10) / 10);
  return (
    <figure className="report-chart" ref={ref}>
      <svg width={W} height={H} viewBox={`0 0 ${W} ${H}`} role="img" aria-label={chart.title}>
        {[0, 1, 2, 3, 4].map((i) => {
          const v = lo + ((hi - lo) * i) / 4;
          return <g key={i}><line x1={L} x2={W - 6} y1={y(v)} y2={y(v)} className="rc-grid" /><text x={L - 6} y={y(v) + 3} textAnchor="end" className="rc-tick">{tick(v)}</text></g>;
        })}
        {chart.series.map((s, i) => (s.pts.length === 1
          ? <circle key={s.name} cx={x(s.pts[0][0])} cy={y(s.pts[0][1])} r="3" className={LINE[i % 6]} />
          : <polyline key={s.name} className={`rc-line ${LINE[i % 6]}`} points={s.pts.map((p) => `${x(p[0]).toFixed(1)},${y(p[1]).toFixed(1)}`).join(' ')} />))}
        <text x={L} y={H - 7} className="rc-tick">{dayOnly(from)}</text>
        <text x={W - 6} y={H - 7} textAnchor="end" className="rc-tick">{dayOnly(to)}</text>
      </svg>
      <figcaption className="rc-legend small">
        {chart.series.map((s, i) => <span key={s.name}><i className={`rc-sw ${SWATCH[i % 6]}`} />{s.name}</span>)}
        {chart.unit && <span className="muted">Shown in {chart.unit}</span>}
      </figcaption>
    </figure>
  );
}

function Bars({ chart }) {
  const max = Math.max(1, ...(chart.bars || []).map((b) => b.value));
  return (
    <ul className="toplist report-bars">
      {(chart.bars || []).map((b) => <li key={b.label}><span className="rb-label">{b.label}</span><Bar pct={(b.value / max) * 100} thin /><span className="muted small">{Math.round(b.value).toLocaleString()}</span></li>)}
    </ul>
  );
}

function Filters({ subject, filters, setFilter, links }) {
  return (
    <div className="report-filters">
      {FILTERS[subject].map((f) => (
        <Field key={f.key} label={f.label}>
          {f.options || f.kind === 'link'
            ? <Select value={filters[f.key] || ''} onChange={(v) => setFilter(f.key, v)} options={f.options || [ANY, ...links.map((l) => ({ value: l, label: l }))]} />
            : <Input value={filters[f.key] || ''} onChange={(v) => setFilter(f.key, v)} inputMode={f.kind === 'number' ? 'numeric' : undefined} />}
        </Field>
      ))}
    </div>
  );
}

// What the form asks for, in words: shown above the result and used for the chips.
function chipsFor(spec, links) {
  return (FILTERS[spec.subject] || []).flatMap((f) => {
    const v = String(spec.filters?.[f.key] ?? '').trim();
    if (!v) return [];
    const opt = f.options?.find((o) => o.value === v);
    return [{ key: f.key, text: `${f.label}: ${opt ? opt.label : v}` }];
  });
}
const SAME_WORD = { links: 'Internet links', dns: 'DNS lookups', threats: 'Threats', firewall: 'Firewall connections' };
const rangeWord = (spec) => (spec.range === 'custom' ? 'exact dates' : ({ 1: 'last 24 hours', 7: 'last 7 days', 30: 'last 30 days', 60: 'last 60 days', 90: 'last 90 days', 365: 'last year' }[spec.range] || ''));
// Reports of up to 30 days are made again by themselves when the form changes. Longer ones can read a great
// deal of history, so they wait for the button.
const AUTO = new Set(['1', '7', '30']);

// Open Reports with the subject, link and period already filled in, and show the result.
// Print always uses the light colours, so the page is dark text on white whatever theme is on screen.
function printLight() {
  const root = document.documentElement;
  const was = root.getAttribute('data-theme');
  root.setAttribute('data-theme', 'light');
  const back = () => { if (was === null) root.removeAttribute('data-theme'); else root.setAttribute('data-theme', was); window.removeEventListener('afterprint', back); };
  window.addEventListener('afterprint', back);
  window.print();
}

export function openReport(spec) {
  try { sessionStorage.setItem(REPORT_PRESET_KEY, JSON.stringify(spec)); } catch { /* ignore */ }
  go('logs/reports?run=1');
}

export default function Reports({ me, admin }) {
  const [spec, setSpec] = useState(() => {
    let p = null;
    try { p = JSON.parse(sessionStorage.getItem(REPORT_PRESET_KEY) || 'null'); sessionStorage.removeItem(REPORT_PRESET_KEY); } catch { /* ignore */ }
    return { subject: 'links', range: '7', group: '', filters: {}, custom: { from: '', to: '' }, ...(p || {}) };
  });
  const [res, setRes] = useState(null);
  const [err, setErr] = useState('');
  const [name, setName] = useState('');
  const [run, busy] = useAction();
  const { data: usage } = useData('/api/usage');
  const { data: saved, reload } = useData('/api/reports/saved');
  const links = (usage?.links || []).map((l) => l.name);
  const lastReq = useRef(null);
  const [shown, setShown] = useState(null); // the form the result on screen was made from
  const set = (patch) => setSpec((s) => ({ ...s, ...patch }));
  const setFilter = (k, v) => setSpec((s) => ({ ...s, filters: { ...s.filters, [k]: v } }));
  const pickSubject = (subject) => setSpec((s) => ({ ...s, subject, group: '', filters: {} }));
  const chips = chipsFor(spec, links);
  const needsDates = spec.range === 'custom' && (!spec.custom?.from || !spec.custom?.to);

  const show = useCallback(async (s) => {
    const req = toRequest(s);
    lastReq.current = req;
    setErr('');
    try { setRes(await post('/api/reports/run', req)); setShown(s); } catch (e) { setRes(null); setShown(null); setErr(e.message); }
  }, []);
  const stale = !!res && !!shown && reqKey(shown) !== reqKey(spec);
  useEffect(() => {
    if (!res || !stale || needsDates || !AUTO.has(spec.range)) return undefined;
    const t = setTimeout(() => show(spec), 500);
    return () => clearTimeout(t);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [reqKey(spec), !!res]);
  useEffect(() => {
    try { if (hashQuery('run') === '1') { setHashQuery('run', ''); show(spec); } } catch { /* ignore */ }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const exportAs = (fmt) => run(async () => {
    const req = lastReq.current || toRequest(spec);
    const ext = { csv: 'csv', json: 'json', xlsx: 'xlsx', pdf: 'pdf' }[fmt];
    await saveFile('POST', `/api/reports/export?format=${fmt}`, req, `nopewarden-report.${ext}`);
  });
  const save = () => run(async () => {
    await post('/api/reports/saved', { name, spec: { subject: spec.subject, range: spec.range, custom: spec.custom, group: spec.group, filters: toRequest(spec).filters } });
    setName(''); reload();
  }, 'Report saved.');
  const open = (s) => { setSpec(s); show(s); };
  const remove = (id) => run(async () => { await del(`/api/reports/saved/${id}`); reload(); }, 'Report removed.');

  return (
    <div className="stack report">
      <Card title="Make a report" desc="Pick what to report on and the period, then narrow it down if you want.">
        <div className="stack">
          <Segmented value={spec.subject} onChange={pickSubject} options={SUBJECTS} />
          <div className="report-row">
            <Segmented value={spec.range} onChange={(range) => set({ range })} options={RANGES} />
            {spec.range === 'custom' && <DateRange from={spec.custom?.from} to={spec.custom?.to} max={ymd(new Date())} onChange={(from, to) => set({ custom: { from, to } })} />}
          </div>
          <div className="report-group"><Field label="Group the results"><Select value={spec.group} onChange={(group) => set({ group })} options={GROUPS[spec.subject]} /></Field></div>
          <details className="report-narrow" open={chips.length > 0 || undefined}>
            <summary>Narrow it down{chips.length > 0 ? ` (${chips.length})` : ''}</summary>
            <Filters subject={spec.subject} filters={spec.filters} setFilter={setFilter} links={links} />
          </details>
          {chips.length > 0 && (
            <div className="report-chips" aria-label="Filters in use">
              {chips.map((c) => <button type="button" key={c.key} className="report-chip" title="Remove this filter" onClick={() => setFilter(c.key, '')}>{c.text} <span aria-hidden="true">×</span></button>)}
            </div>
          )}
          <div className="actions">
            <Button kind="primary" disabled={needsDates} onClick={() => show(spec)}>{res && stale ? 'Update report' : 'Show report'}</Button>
            {spec.subject === 'links' && <Button onClick={() => open({ ...spec, ...ISP_TEMPLATE, filters: spec.filters.link ? { link: spec.filters.link } : {} })}>ISP report: last 30 days</Button>}
            {res && stale && !AUTO.has(spec.range) && <span className="small muted">Your choices changed. Press Update report to see them.</span>}
          </div>
        </div>
      </Card>

      {saved?.length > 0 && (
        <Card className="saved" title="Saved reports" desc="Run one again to get fresh numbers for the same question.">
          <ul className="toplist">
            {saved.map((s) => (
              <li key={s.id}>
                <span><button type="button" className="link" onClick={() => open({ subject: 'links', range: '7', group: '', filters: {}, custom: { from: '', to: '' }, ...s.spec })}>{s.name}</button> <span className="small muted">{SUBJECTS.find((x) => x.value === s.spec.subject)?.label}</span></span>
                {admin && <Button small kind="danger" onClick={() => remove(s.id)}>Remove</Button>}
              </li>
            ))}
          </ul>
        </Card>
      )}

      {err && <Note tone="bad" title="That report could not be made">{err}</Note>}
      {res && (
        <div className={`report-out${stale ? ' stale' : ''}`}>
          {stale && (
            <Note tone="info" title="This is the earlier report">
              It shows {SAME_WORD[shown.subject]}, {rangeWord(shown)}. {AUTO.has(spec.range) ? 'It is being updated to match your choices.' : `Your choices now ask for ${SAME_WORD[spec.subject]}, ${rangeWord(spec)}. Press Update report.`}
            </Note>
          )}
          <div className="report-print-head">
            <Logo size={36} /><div><strong>NopeWarden</strong><div className="small">Hard on threats. Easy on humans.</div></div>
          </div>
          <Card title={res.title} desc={`${when(res.from)} to ${when(res.to)}. Made ${when(res.made)}.${res.filters?.length ? ` Filters: ${res.filters.join('; ')}.` : ' No filters.'}`}
            actions={<div className="actions no-print">
              <Button small onClick={printLight}>Print</Button>
              <Button small disabled={busy} onClick={() => exportAs('pdf')}>PDF</Button>
              <Button small disabled={busy} onClick={() => exportAs('csv')}>CSV</Button>
              <Button small disabled={busy} onClick={() => exportAs('xlsx')}>Excel</Button>
              <Button small disabled={busy} onClick={() => exportAs('json')}>JSON</Button>
            </div>}>
            <div className="stack">
              {res.notes?.map((n) => (
                <Note key={n} tone="info" title="Good to know">{n} {/Keep chart history|Keep hourly/.test(n) && <button type="button" className="link no-print" onClick={() => go('settings/alerts')}>Open Settings, Storage</button>}</Note>
              ))}
              {res.summary?.length > 0 && (
                <div className="tiles">{res.summary.map((s) => <div key={s.label} className="card tile"><span className="lbl">{s.label}</span><div className="line"><span className="val">{s.value}</span></div></div>)}</div>
              )}
              {res.charts?.map((c) => (
                <div key={c.id}><h3 className="report-h">{c.title}</h3>{c.bars?.length ? <Bars chart={c} /> : <LineChart chart={c} from={res.from} to={res.to} />}</div>
              ))}
              {res.rows?.length > 0 ? (
                <div className="report-grid" role="table" aria-label={`${res.title} rows`}>
                  <div className="rr rr-head" role="row" style={{ gridTemplateColumns: `repeat(${(res.columns || []).length}, minmax(0, 1fr))` }}>{(res.columns || []).map((c) => <span key={c} role="columnheader">{c}</span>)}</div>
                  <Limited items={res.rows} n={15} noun="rows" render={(r, i) => <div className="rr" role="row" key={i} style={{ gridTemplateColumns: `repeat(${(res.columns || []).length}, minmax(0, 1fr))` }}>{r.map((v, j) => <span key={j} role="cell">{v}</span>)}</div>} />
                </div>
              ) : <Empty>Nothing matched in this period.</Empty>}
            </div>
          </Card>
          <Card className="no-print" title="Save this report" desc="Saved reports keep the subject, period, filters and grouping, so you can run them again later.">
            {admin ? (
              <div className="inline"><div style={{ flex: '1 1 16rem' }}><Input value={name} onChange={setName} placeholder="For example: Fibre, last 30 days" aria-label="Report name" maxLength={80} /></div>
                <Button disabled={!name.trim() || busy} onClick={save}>Save report</Button></div>
            ) : <p className="muted small">Only administrators can save reports.</p>}
          </Card>
        </div>
      )}
      {!res && !err && <Card><Empty>Pick what to report on, then choose <b>Show report</b>.</Empty></Card>}
    </div>
  );
}

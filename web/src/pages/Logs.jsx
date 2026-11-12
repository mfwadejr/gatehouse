import { hashQuery, setHashQuery, useHashTab } from '../hashtab.js';
import { Hit } from './AdminActivity.jsx';
import AdminActivity from './AdminActivity.jsx';
import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { del, get, getWatched, post, put, setFailing, useData, useStream } from '../api.js';
import { RuleForm } from './FirewallRules.jsx';
import { COUNTRIES, MAP_H, MAP_W } from '../worldmap.js';
import { groupRows, rowTitle } from '../logrows.js';
import { Button, Card, Empty, Input, Modal, NumberInput, Note, Page, Pill, Segmented, Select, DateTimeField, Switch, Tabs, ToggleRow, Trend, useAction, fmt , useDelayedDelete } from '../ui.jsx';

const KINDS = [
  { value: '', label: 'Everything' },
  { value: 'firewall', label: 'Connections' },
  { value: 'dns', label: 'Names (DNS)' },
  { value: 'event', label: 'Events' },
];
const VERDICT = { allowed: ['good', 'Allowed'], blocked: ['bad', 'Blocked'], warning: ['warn', 'Problem'], info: ['', 'Info'] };
const RANGES = [{ value: '1', label: '1 h' }, { value: '24', label: '24 h' }, { value: '168', label: '7 d' }, { value: '720', label: '30 d' }, { value: 'custom', label: 'Custom' }];
const FILTER_NAMES = { device: 'Device', dest: 'Destination', network: 'Network', service: 'Service', port: 'Port', proto: 'Protocol', direction: 'Direction', rule: 'Rule', country: 'Country', app: 'App' };
const DIRECTIONS = { outbound: 'Out to the internet', inbound: 'In from the internet', between: 'Between your networks', router: 'To NopeWarden itself' };
const ALL_COLS = [
  { id: 'time', label: 'Time' }, { id: 'device', label: 'Device' }, { id: 'what', label: 'What' }, { id: 'where', label: 'Networks' },
  { id: 'via', label: 'Out through' }, { id: 'result', label: 'Result' }, { id: 'why', label: 'Why' },
];
const DEFAULT_COLS = ['time', 'device', 'what', 'via', 'result'];
const regionName = (cc) => { try { return new Intl.DisplayNames(undefined, { type: 'region' }).of(cc); } catch { return cc; } };
const bytes = (n) => {
  if (!n) return '0 B';
  const u = ['B', 'KB', 'MB', 'GB', 'TB'];
  let i = 0, v = n;
  while (v >= 1000 && i < u.length - 1) { v /= 1000; i++; }
  return `${v >= 100 || i === 0 ? Math.round(v) : v.toFixed(1)} ${u[i]}`;
};
// The detail of a row that is opened. A DNS answer: the response code in words, how long it took, who
// answered. A connection: how it ended, bytes each way, how long it lasted, and the rule that decided it.
const RCODES = { NOERROR: 'answered', NXDOMAIN: 'that name does not exist', SERVFAIL: 'the server failed to answer', REFUSED: 'the server refused', FORMERR: 'the question was malformed', NOTIMP: 'not supported' };
const rcodeText = (c) => (c ? `${c}${RCODES[c] ? ` (${RCODES[c]})` : ''}` : 'not recorded');
const answerTime = (us) => (!us ? 'under a millisecond' : us < 1000 ? 'under a millisecond' : us < 100000 ? `${(us / 1000).toFixed(us < 10000 ? 1 : 0)} ms` : `${(us / 1e6).toFixed(2)} seconds`);
const lastedText = (s) => (s == null ? 'not known' : s < 10 ? 'under 10 seconds' : s < 120 ? `${s} seconds` : s < 7200 ? `${Math.round(s / 60)} minutes` : `${(s / 3600).toFixed(1)} hours`);
const OUTCOME_TONE = { Completed: 'good', 'Still open': 'good', Blocked: 'bad', Reset: 'warn', 'Timed out': 'warn' };

const bits = (n) => {
  if (!n || n < 1000) return n ? '<1 kb/s' : '0';
  const u = ['kb/s', 'Mb/s', 'Gb/s'];
  let i = 0, v = n / 1000;
  while (v >= 1000 && i < u.length - 1) { v /= 1000; i++; }
  return `${v >= 100 ? Math.round(v) : v.toFixed(1)} ${u[i]}`;
};
const dtLocal = (ts) => { const d = new Date(ts * 1000 - new Date().getTimezoneOffset() * 60000); return d.toISOString().slice(0, 16); };

function Recording({ onClose, state, onSaved }) {
  const delayedDelete = useDelayedDelete();
  const [run, busy] = useAction();
  const [f, setF] = useState({ record_allowed: state.record_allowed, record_blocked: state.record_blocked });
  const { data: u, reload } = useData('/api/logs/usage');
  const { data: ap, reload: reloadApps } = useData('/api/apps');
  const [keep, setKeep] = useState(null);
  useEffect(() => { if (u && !keep) setKeep({ days: u.days, max_mb: u.max_mb }); }, [u, keep]);
  return (
    <Modal title="What gets recorded" onClose={onClose}
      footer={<><Button onClick={onClose}>Cancel</Button><Button kind="primary" disabled={busy} onClick={async () => {
        const ok = await run(async () => { await put('/api/logs/settings', f); if (keep) await put('/api/logs/retention', keep); }, 'Saved.');
        if (ok) { onSaved(); onClose(); }
      }}>Save</Button></>}>
      <div className="stack">
        <ToggleRow title="Allowed connections" desc="Records each new connection that the firewall let through, and the rule that allowed it. Turn off to keep the log smaller." checked={f.record_allowed} onChange={(v) => setF({ ...f, record_allowed: v })} />
        <ToggleRow title="Blocked connections" desc="Records each new connection that the firewall stopped, and why." checked={f.record_blocked} onChange={(v) => setF({ ...f, record_blocked: v })} />
        {ap && <ToggleRow title="Name the apps" desc="Shows which app a connection belongs to, like YouTube or Spotify, using the website names devices look up through NopeWarden. It never looks inside traffic. Turn off if you would rather not see which apps each person uses." checked={ap.enabled} onChange={(v) => run(() => put('/api/apps/settings', { enabled: v }), v ? 'App names are on.' : 'App names are off.').then(reloadApps)} />}
        {keep && (
          <div className="stack">
            <div className="formgrid">
              <label className="field"><span className="label">Keep logs for (days)</span><NumberInput value={keep.days} min={1} max={365} onChange={(v) => setKeep({ ...keep, days: v })} /></label>
              <label className="field"><span className="label">Never use more than (MB)</span><NumberInput value={keep.max_mb} min={10} max={20000} onChange={(v) => setKeep({ ...keep, max_mb: v })} /></label>
            </div>
            {u && <p className="small muted">Using {bytes(u.used_bytes)} now: {fmt.n(u.connections)} connections, {fmt.n(u.lookups)} name lookups, {fmt.n(u.events)} events{u.oldest ? `, going back to ${fmt.dt(u.oldest)}` : ''}. The oldest entries are removed first when either limit is reached.</p>}
            {u && <div className="inline"><span className="small muted">{u.last_prune ? `Last cleaned up ${fmt.dt(u.last_prune)}, ${fmt.n(u.last_removed)} entries removed.` : 'Cleaned up automatically once an hour.'}</span><Button small disabled={busy} onClick={async () => { if (await run(() => post('/api/logs/prune', {}), 'Cleaned up.')) { reload(); onSaved(); } }}>Clean up now</Button></div>}
          </div>
        )}
        <p className="small muted">Only the first packet of each connection is recorded, and a flood is capped, so a busy network cannot fill the disk. Names asked for through DNS are always recorded.</p>
        <Button kind="danger" onClick={() => { delayedDelete('Every log entry', () => del('/api/logs'), () => { reload(); onSaved(); }); onClose(); }}>Delete all log entries</Button>
      </div>
    </Modal>
  );
}

// Other pages (the Overview map) open the log already filtered by leaving the filter here.
export const PRESET_KEY = 'gh.logs.preset';
function takePreset() {
  try {
    const raw = sessionStorage.getItem(PRESET_KEY);
    if (!raw) return {};
    const p = JSON.parse(raw);
    return p && typeof p === 'object' ? p : {};
  } catch { return {}; }
}

// ---------------------------------------------------------------- the map

export function WorldMap({ countries }) {
  const by = useMemo(() => Object.fromEntries((countries || []).map((c) => [c.key, c.n])), [countries]);
  const max = Math.max(1, ...Object.values(by));
  const [hover, setHover] = useState(null);
  return (
    <div className="worldmap">
      <svg viewBox={`0 0 ${MAP_W} ${MAP_H}`} role="img" aria-label="Connections by country">
        {Object.entries(COUNTRIES).filter(([cc]) => cc !== 'AQ').map(([cc, c]) => {
          const n = by[cc] || 0;
          return <path key={cc} d={c.d} className={n ? 'hit' : ''} style={n ? { opacity: 0.35 + 0.65 * Math.sqrt(n / max) } : undefined}
            onMouseEnter={() => setHover(cc)} onMouseLeave={() => setHover(null)}><title>{`${regionName(cc)}${n ? `: ${fmt.n(n)} connections` : ''}`}</title></path>;
        })}
      </svg>
      <div className="small muted">{hover ? `${regionName(hover)}${by[hover] ? `: ${fmt.n(by[hover])} connections` : ''}` : 'Darker means more connections.'}</div>
    </div>
  );
}

function TopList({ title, items, field, onPick, render }) {
  return (
    <Card title={title} desc="Most connections first. Click one to filter the list to it.">
      {!items?.length ? <Empty>Nothing yet</Empty> : (
        <ul className="toplist">
          {items.map((i) => (
            <li key={i.key}>
              <button className="link" onClick={() => onPick(field, i.key === i.label || field === 'dest' ? i.label : i.key)}>{render ? render(i) : i.label}</button>
              <span className="muted small">{fmt.n(i.n)}</span>
            </li>
          ))}
        </ul>
      )}
    </Card>
  );
}

// ---------------------------------------------------------------- the log

export default function Logs({ me }) {
  const admin = me?.user?.role === 'admin';
  const preset = useRef(null);
  if (preset.current === null) preset.current = takePreset();
  useEffect(() => { try { sessionStorage.removeItem(PRESET_KEY); } catch { /* ignore */ } }, []);
  const [tab, setTab] = useHashTab('logs', ['log','summary','activity','admin'], 'log');
  const [q, setQ] = useState(() => hashQuery('q'));
  useEffect(() => { setHashQuery('q', q.trim()); }, [q]);
  const words = q.trim().split(/\s+/).filter(Boolean);
  const { data: keepInfo } = useData('/api/logs/usage', { every: 300000 });
  const [kind, setKind] = useState('');
  const [verdict, setVerdict] = useState(preset.current.verdict || '');
  const [hours, setHours] = useState(preset.current.hours || '1');
  const [custom, setCustom] = useState({ from: dtLocal(Date.now() / 1000 - 86400), to: dtLocal(Date.now() / 1000) });
  const [flt, setFlt] = useState(preset.current.flt || {});
  const [rows, setRows] = useState([]);
  const [more, setMore] = useState(false);
  const [meta, setMeta] = useState({ record_allowed: true, record_blocked: true });
  const [live, setLive] = useState(true);
  const [own, setOwn] = useState(() => { try { return localStorage.getItem('gh.logs.own') === '1'; } catch { return false; } });
  const chooseOwn = (v) => { setOwn(v); try { localStorage.setItem('gh.logs.own', v ? '1' : '0'); } catch { /* ignore */ } };
  const [loading, setLoading] = useState(true);
  const [rec, setRec] = useState(false);
  const [cols, setCols] = useState(DEFAULT_COLS);
  const [open, setOpen] = useState({});
  const [pageSize, setPageSize] = useState(15);
  const [mk, setMk] = useState(null);
  const t = useRef(null);
  const paged = useRef(false);
  const { data: ov } = useData('/api/fw/overview');
  const { data: pickers, reload: reloadPickers } = useData('/api/fw/pickers');
  const names = (ov?.matrix_zones || []).map((z) => z.name);

  useEffect(() => {
    getWatched('logs.prefs', '/api/prefs/logs', (p) => { if (Array.isArray(p.cols) && p.cols.length) setCols(p.cols); if (p.rows) setPageSize(p.rows); });
  }, []);
  const savePrefs = (c, r) => put('/api/prefs/logs', { cols: c, rows: r }).catch(() => {});

  const params = useCallback((before) => {
    const p = new URLSearchParams({ limit: String(pageSize) });
    if (hours === 'custom') {
      const f = Math.floor(new Date(custom.from).getTime() / 1000), tt = Math.floor(new Date(custom.to).getTime() / 1000);
      if (f > 0) p.set('from', String(f));
      if (tt > 0) p.set('to', String(tt));
    } else p.set('hours', hours);
    if (q) p.set('q', q);
    if (kind) p.set('kind', kind);
    if (verdict) p.set('verdict', verdict);
    if (own) p.set('own', '1');
    Object.entries(flt).forEach(([k, v]) => v !== '' && v != null && p.set(k, String(v)));
    if (before) p.set('before', String(before));
    return p;
  }, [q, kind, verdict, hours, custom, flt, pageSize, own]);

  const load = useCallback(async () => {
    try {
      const r = await get(`/api/logs?${params()}`);
      setRows(r.items); setMore(r.more); setMeta({ record_allowed: r.record_allowed, record_blocked: r.record_blocked });
      paged.current = false;
      setFailing('logs.list', false);
    } catch { setFailing('logs.list', true); }
    setLoading(false);
  }, [params]);

  const older = async () => {
    const last = rows[rows.length - 1];
    if (!last) return;
    const r = await get(`/api/logs?${params(last.ts)}`);
    const seen = new Set(rows.map((x) => x.key));
    setRows((x) => [...x, ...r.items.filter((i) => !seen.has(i.key))]);
    setMore(r.more); paged.current = true;
  };

  useEffect(() => { clearTimeout(t.current); t.current = setTimeout(load, 250); return () => clearTimeout(t.current); }, [load]);
  const tick = useRef(0);
  useStream(live && tab === 'log' ? ['logs.live', 'dns.live'] : [], () => {
    if (paged.current || Date.now() - tick.current < 1000) return;
    tick.current = Date.now();
    load();
  });

  const pick = (field, value) => { setFlt((f) => ({ ...f, [field]: value })); setTab('log'); };
  const unpick = (field) => setFlt((f) => { const n = { ...f }; delete n[field]; return n; });
  const exportUrl = () => { const p = params(); p.delete('limit'); return `/api/logs/export?${p}`; };
  const show = (id) => cols.includes(id);
  const toggleCol = (id) => { const n = cols.includes(id) ? cols.filter((c) => c !== id) : ALL_COLS.map((c) => c.id).filter((c) => c === id || cols.includes(c)); if (n.length) { setCols(n); savePrefs(n, pageSize); } };

  const mkRule = (r, action) => setMk({
    name: `${action === 'accept' ? 'Allow' : 'Block'} ${r.service || r.proto?.toUpperCase() || 'traffic'} to ${r.dst_name || r.dst}`.slice(0, 60),
    from: r.from_zone && names.includes(r.from_zone) ? r.from_zone : undefined,
    to: r.to_zone === 'Router' ? 'self' : (r.to_zone && names.includes(r.to_zone) ? r.to_zone : undefined),
    service: r.port ? `${r.proto}/${r.port}` : '', action,
  });

  const groups = useMemo(() => groupRows(rows), [rows]);
  // While a connection row is open its rule's match count follows the firewall, as long as Live is on.
  const ruleOpen = groups.some((g) => open[g.key] && g.first.kind === 'firewall' && g.first.rule);
  const [ruleHits, setRuleHits] = useState({});
  useEffect(() => {
    if (!ruleOpen) return undefined;
    let dead = false;
    const pull = async () => { try { const j = await get('/api/fw/rules/counts?all=1'); if (!dead) setRuleHits(j.hits || {}); } catch { /* keep the last count */ } };
    pull();
    if (!live) return () => { dead = true; };
    const iv = setInterval(pull, 2000);
    return () => { dead = true; clearInterval(iv); };
  }, [ruleOpen, live]);
  const none = !loading && !rows.length;
  const chips = Object.entries(flt).filter(([, v]) => v !== '' && v != null);
  return (
    <Page title="Logs" sub="What your network did: connections, names looked up, devices joining or leaving, and threat alerts. For how NopeWarden itself is running, open Diagnostics."
      actions={<><label className="inline small"><Switch checked={live} onChange={setLive} label="Live updates" />Live</label><Button small onClick={() => setRec(true)}>Recording &amp; storage</Button></>}>
      {(!meta.record_allowed || !meta.record_blocked) && (
        <Note tone="warn" title="Some connections are not being recorded">
          {!meta.record_allowed && !meta.record_blocked ? 'Neither allowed nor blocked connections are recorded.' : !meta.record_allowed ? 'Allowed connections are not recorded, so you cannot see why something was let through.' : 'Blocked connections are not recorded, so you cannot see why something was stopped.'}{' '}
          <button className="link" onClick={() => setRec(true)}>Change this</button>
        </Note>
      )}
      <Tabs value={tab} onChange={setTab} tabs={[{ value: 'log', label: 'Log' }, { value: 'summary', label: 'Summary & map' }, { value: 'activity', label: 'Activity' }, ...(admin ? [{ value: 'admin', label: 'Admin activity' }] : [])]} />
      {tab === 'admin' ? <AdminActivity /> : tab === 'activity' ? null : (
      <Card>
        <div className="stack">
          <div className="inline">
            <div style={{ flex: '1 1 18rem' }}><Input value={q} onChange={setQ} placeholder="Search a device, address, website, port or reason" aria-label="Search the logs" /></div>
            <div style={{ flex: '0 0 13rem' }}><Select value={verdict} onChange={setVerdict} options={[{ value: '', label: 'Allowed and blocked' }, { value: 'blocked', label: 'Blocked only' }, { value: 'allowed', label: 'Allowed only' }]} aria-label="Result" /></div>
            <Segmented value={hours} onChange={setHours} options={RANGES} />
          </div>
          {hours === 'custom' && (
            <div className="inline small">
              <label>From <DateTimeField value={custom.from} onChange={(v) => setCustom({ ...custom, from: v })} /></label>
              <label>To <DateTimeField value={custom.to} onChange={(v) => setCustom({ ...custom, to: v })} /></label>
            </div>
          )}
          {tab === 'log' && <Segmented value={kind} onChange={setKind} options={KINDS} />}
          {tab === 'log' && <label className="inline small" title="NopeWarden checks that its own DNS is answering every few seconds. These checks are hidden unless you tick this."><input type="checkbox" checked={own} onChange={(e) => chooseOwn(e.target.checked)} /> Show NopeWarden's own checks</label>}
          {words.length > 0 && keepInfo && <p className="small muted">Searching the time range above, up to {keepInfo.days} days back, which is how long NopeWarden keeps its logs. {fmt.n(rows.length)} shown so far{more ? '; press Show older for more' : ''}.</p>}
          {chips.length > 0 && (
            <div className="chips">
              {chips.map(([k, v]) => (
                <span className="chip" key={k}>{FILTER_NAMES[k] || k}: {k === 'direction' ? DIRECTIONS[v] || v : k === 'country' ? regionName(v) : v}
                  <button type="button" className="chipx" aria-label={`Remove ${FILTER_NAMES[k]} filter`} onClick={() => unpick(k)}>×</button></span>
              ))}
              <button className="link small" onClick={() => setFlt({})}>Clear filters</button>
            </div>
          )}
        </div>
      </Card>
      )}

      {tab === 'log' && (
        <Card>
          <div className="inline between small" style={{ marginBottom: 'var(--space-2)' }}>
            <details className="more"><summary>Columns &amp; rows</summary>
              <div className="stack">
                <div className="chips">{ALL_COLS.map((c) => <label key={c.id} className="inline small"><input type="checkbox" checked={cols.includes(c.id)} onChange={() => toggleCol(c.id)} />{c.label}</label>)}</div>
                <Select value={String(pageSize)} onChange={(v) => { setPageSize(Number(v)); savePrefs(cols, Number(v)); }} options={[10, 15, 25, 50, 100, 200, 300].map((n) => ({ value: String(n), label: `${n} rows at a time` }))} aria-label="Rows" />
              </div>
            </details>
            <a className="btn small" href={exportUrl()} download>Download as spreadsheet (CSV)</a>
          </div>
          <div className="scroll">
            <table className="table logtable">
              <thead><tr>{show('time') && <th>Time</th>}{show('device') && <th>Device</th>}{show('what') && <th>What</th>}{show('where') && <th>Networks</th>}{show('via') && <th>Out through</th>}{show('result') && <th>Result</th>}{show('why') && <th>Why</th>}<th /></tr></thead>
              <tbody>
                {groups.map((g) => {
                  const r = g.first;
                  const [tone, label] = r.verdict === 'blocked' && (r.why || '').startsWith('Not a problem') ? ['', 'Blocked · expected'] : (VERDICT[r.verdict] || ['', r.verdict]);
                  const conn = r.kind === 'firewall';
                  const { title, sub } = rowTitle(r);
                  const n = g.items.length;
                  const isOpen = !!open[g.key];
                  const toggle = () => setOpen((o) => ({ ...o, [g.key]: !o[g.key] }));
                  return (
                    <React.Fragment key={g.key}>
                      <tr className={`logrow ${isOpen ? 'open' : ''}`} tabIndex={0} aria-expanded={isOpen} onClick={toggle} onKeyDown={(e) => { if (e.target === e.currentTarget && (e.key === 'Enter' || e.key === ' ')) { e.preventDefault(); toggle(); } }}>
                        {show('time') && <td className="mono nowrap" title={fmt.dt(r.ts)}>{fmt.time(r.ts)}</td>}
                        {show('device') && <td><div className="l1">{r.who ? <Hit text={r.who} words={words} /> : <span className="muted">–</span>}</div>{r.addr && r.addr !== r.who && <div className="l2 small muted mono"><Hit text={r.addr} words={words} /></div>}</td>}
                        {show('what') && (
                          <td>
                            <div className="l1" title={title}><Hit text={title} words={words} />{n > 1 && <span className="count"> ×{n}</span>}</div>
                            <div className="l2 small muted"><Hit text={sub} words={words} /></div>
                          </td>
                        )}
                        {show('where') && <td className="small">{(conn || r.from_zone) && r.to_zone ? <>{r.from_zone} → {r.to_zone}</> : <span className="muted">–</span>}</td>}
                        {show('via') && <td className="small">{r.via ? <Pill tone={r.via_kind === 'tunnel' ? 'good' : ''}>{r.via}</Pill> : <span className="muted">–</span>}</td>}
                        {show('result') && <td className="nowrap"><Pill tone={tone}>{label}</Pill></td>}
                        {show('why') && <td className="small sec"><div className="l1"><Hit text={r.why} words={words} /></div></td>}
                        <td className="nowrap chev" aria-hidden="true">{isOpen ? '▾' : '▸'}</td>
                      </tr>
                      {isOpen && (
                        <tr className="logdetail">
                          <td colSpan={cols.length + 1}>
                            <div className="stack">
                              {r.why && <div><b>Why:</b> {r.why}{conn && r.rule && <> <button className="link" onClick={() => pick('rule', r.rule)}>Same rule</button></>}</div>}
                              {conn && <div className="small">{r.dst_name && <>Host <span className="mono">{r.dst_name}</span> · </>}Address <span className="mono">{r.dst}</span>{r.port ? <> · port <button className="link" title="Show only this port" onClick={() => pick('port', r.port)}>{r.port}</button></> : null}{r.country && <> · <button className="link" onClick={() => pick('country', r.country)}>{regionName(r.country)}</button></>}</div>}
                              {conn && <div className="small">{r.from_zone && <button className="link" onClick={() => pick('network', r.from_zone)}>{r.from_zone}</button>} → {r.to_zone && <button className="link" onClick={() => pick('network', r.to_zone)}>{r.to_zone}</button>}{r.service && <> · <button className="link" onClick={() => pick('service', r.service)}>{r.service}</button></>}{r.app && <> · <button className="link" title="Show only this app" onClick={() => pick('app', r.app)}>{r.app}</button></>}</div>}
                              {r.kind === 'dns' && (
                                <div className="small logfacts">
                                  <span>Response <b>{rcodeText(r.rcode)}</b></span>
                                  <span>Answer time <b>{answerTime(r.answer_us)}</b></span>
                                  <span>Answered by <b>{r.server || 'not recorded'}</b></span>
                                </div>
                              )}
                              {conn && (
                                <div className="small logfacts">
                                  <span>Outcome <b>{r.outcome ? <Pill tone={OUTCOME_TONE[r.outcome] || ''}>{r.outcome}</Pill> : 'not known yet'}</b></span>
                                  {r.verdict !== 'blocked' && <span>Sent <b>{r.bytes_out != null ? bytes(r.bytes_out) : 'not known'}</b></span>}
                                  {r.verdict !== 'blocked' && <span>Received <b>{r.bytes_in != null ? bytes(r.bytes_in) : 'not known'}</b></span>}
                                  {r.verdict !== 'blocked' && <span>Lasted <b>{lastedText(r.dur_s)}</b></span>}
                                  {r.rule_name && <span>Rule <b>{r.rule_name}</b>{(ruleHits[r.rule]?.hits ?? r.rule_hits) != null && <> · matched <b>{fmt.n(ruleHits[r.rule]?.hits ?? r.rule_hits)}</b> so far{live ? ' (live)' : ''}</>}</span>}
                                </div>
                              )}
                              {r.who && r.addr && <div className="small"><button className="link" onClick={() => pick('device', r.addr)}>Show only {r.who}</button></div>}
                              {n > 1 && <div className="small muted">{n} times: {g.items.slice(0, 20).map((x) => fmt.time(x.ts)).join(', ')}{n > 20 ? ', …' : ''}</div>}
                              {conn && r.dst && <div><Button small onClick={() => mkRule(r, r.verdict === 'blocked' ? 'accept' : 'drop')}>{r.verdict === 'blocked' ? 'Allow this' : 'Block this'}</Button></div>}
                            </div>
                          </td>
                        </tr>
                      )}
                    </React.Fragment>
                  );
                })}
              </tbody>
            </table>
            {none && <Empty>{q || kind || verdict || chips.length ? 'Nothing matches that search in this time.' : 'Nothing has been recorded yet.'}</Empty>}
          </div>
          {more && <div className="actions"><Button onClick={older}>Show older</Button></div>}
        </Card>
      )}
      {tab === 'summary' && <Summary params={params} pick={pick} />}
      {tab === 'activity' && <Activity />}
      {rec && <Recording state={meta} onClose={() => setRec(false)} onSaved={load} />}
      {mk && <RuleForm initial={mk} zoneNames={names} pickers={pickers} reloadPickers={reloadPickers} onClose={() => setMk(null)} onSaved={() => { setMk(null); }} />}
    </Page>
  );
}

function Summary({ params, pick }) {
  const [s, setS] = useState(null);
  const p = params(); p.delete('limit'); p.delete('before'); p.delete('kind');
  const qs = p.toString();
  useEffect(() => { getWatched('logs.summary', `/api/logs/summary?${qs}`, setS); }, [qs]);
  const { data: geo } = useData('/api/logs/countries');
  if (!s) return <Card><Empty>Loading…</Empty></Card>;
  return (
    <div className="stack">
      <div className="tiles">
        <Card title="Connections" desc="Every connection in the period you chose."><div className="big">{fmt.n(s.total)}</div></Card>
        <Card title="Allowed" desc="Let through by a rule or by the default."><div className="big">{fmt.n(s.allowed)}</div></Card>
        <Card title="Blocked" desc="Stopped by the firewall."><div className="big">{fmt.n(s.blocked)}</div></Card>
      </div>
      <Card title="Where connections go" desc="By the country of the address on the far side. Click a country to see its connections.">
        {geo && !geo.ready && <Note tone="warn" title="Country data not downloaded yet">NopeWarden downloads the country address lists the first time they are needed and keeps them up to date weekly. Until then the map stays empty.</Note>}
        <WorldMap countries={geo?.countries || s.countries} />
        {(s.countries || []).length > 0 && (
          <ul className="toplist">{s.countries.slice(0, 8).map((c) => <li key={c.key}><button className="link" onClick={() => pick('country', c.key)}>{regionName(c.key)}</button><span className="muted small">{fmt.n(c.n)}</span></li>)}</ul>
        )}
      </Card>
      <div className="grid2">
        {s.apps && <TopList title="Busiest apps" items={s.apps} field="app" onPick={pick} />}
        <TopList title="Busiest destinations" items={s.destinations} field="dest" onPick={pick} />
        <TopList title="Busiest devices" items={s.devices} field="device" onPick={(f, v) => pick(f, v)} />
        <TopList title="Busiest services" items={s.services} field="service" onPick={pick} />
      </div>
    </div>
  );
}

function Activity() {
  const [range, setRange] = useState('1h');
  const [d, setD] = useState(null);
  const [usage, setUsage] = useState([]);
  const [sites, setSites] = useState(null);
  const [apps, setApps] = useState(null);
  useEffect(() => {
    const go = () => {
      getWatched('act.series', `/api/traffic?range=${range}`, setD);
      getWatched('act.usage', `/api/traffic/usage?range=${range}`, setUsage);
      getWatched('act.sites', `/api/traffic/domains?range=${range}`, setSites);
      getWatched('act.apps', `/api/traffic/apps?range=${range}`, setApps);
    };
    go();
    const i = setInterval(go, 30000);
    return () => clearInterval(i);
  }, [range]);
  const pts = d?.points || [];
  const span = d ? d.to - d.from : 3600;
  const peak = (k) => Math.max(0, ...pts.map((p) => p[k]));
  // Small speeds are drawn in kb/s so the scale does not read "0.1 Mb/s" twice.
  const [per, speedUnit] = Math.max(peak('down'), peak('up')) < 1e6 ? [1e3, ' kb/s'] : [1e6, ' Mb/s'];
  return (
    <div className="stack">
      <Segmented value={range} onChange={setRange} options={[{ value: '30m', label: '30 min' }, { value: '1h', label: '1 h' }, { value: '1d', label: '24 h' }, { value: '1w', label: '7 d' }, { value: '1m', label: '30 d' }]} />
      <div className="grid2">
        <Card title="Download speed" desc={`Highest ${bits(peak('down'))}`}><Trend values={pts.map((p) => p.down / per)} unit={speedUnit} seconds={span} series="Internet download" empty="No traffic recorded yet" /></Card>
        <Card title="Upload speed" desc={`Highest ${bits(peak('up'))}`}><Trend values={pts.map((p) => p.up / per)} unit={speedUnit} seconds={span} series="Internet upload" empty="No traffic recorded yet" /></Card>
      </div>
      <Card title="Data used by each device" desc="Counted at the internet connection, including VPN tunnels.">
        {!usage.length ? <Empty>Nothing recorded in this time yet.</Empty> : (
          <table className="table"><thead><tr><th>Device</th><th>Downloaded</th><th>Uploaded</th><th>Total</th></tr></thead>
            <tbody>{usage.map((u) => <tr key={u.mac}><td>{u.name || <span className="mono">{u.mac}</span>}</td><td>{bytes(u.down)}</td><td>{bytes(u.up)}</td><td><b>{bytes(u.total)}</b></td></tr>)}</tbody></table>
        )}
      </Card>
      {apps?.enabled && (
        <Card title="Apps that probably used the most data" desc="An estimate, worked out from the websites each device looked up. Only apps NopeWarden is sure about are listed.">
          {!apps.apps.length ? <Empty>Not enough information yet.</Empty> : (
            <ul className="toplist">{apps.apps.slice(0, 12).map((x) => <li key={x.app}><span>{x.app} <span className="small muted">{x.category}</span></span><span className="muted small">about {bytes(x.bytes)}</span></li>)}</ul>
          )}
        </Card>
      )}
      <Card title="Sites that probably used the most data" desc="An estimate. Each device's data is shared between the sites it looked up around the same time, so treat this as a guide, not a measurement.">
        {!sites?.domains?.length ? <Empty>Not enough information yet.</Empty> : (
          <ul className="toplist">{sites.domains.slice(0, 15).map((x) => <li key={x.domain}><span>{x.domain}</span><span className="muted small">about {bytes(x.bytes)}</span></li>)}</ul>
        )}
      </Card>
    </div>
  );
}

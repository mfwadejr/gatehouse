import { WhatChanged } from '../whatchanged.jsx';
import { useHashTab } from '../hashtab.js';
import ResolverCard from './Resolver.jsx';
import { ScheduleEditor } from '../ScheduleEditor.jsx';
import EncryptedDns from './EncryptedDns.jsx';
import EncryptedDnsServer from './EncryptedDnsServer.jsx';
import ForcedDns from './ForcedDns.jsx';
import React, { useState, useEffect } from 'react';
import { del, get, patch, post, put, useData, useStream } from '../api.js';
import { go } from '../App.jsx';
import { Chart, StackedArea } from '../charts.jsx';
import { usePeriod } from '../period.js';
import { PauseMenu } from '../policykit.jsx';
import { Button, Card, Empty, Field, Input, Modal, NumberInput, Note, Page, Pill, Segmented, Select, Trend, Tabs, ToggleRow, useAction, useUndoDelete, fmt, Bar, Limited , useDelayedDelete } from '../ui.jsx';

const BLOCK_PAUSES = [{ m: 5, t: '5 minutes' }, { m: 30, t: '30 minutes' }, { m: 60, t: '1 hour' }, { m: -1, t: 'Until I resume' }];

// Pausing blocking is for the moment a page will not load and you want to know whether blocking is the reason.
function PauseBar({ pause, onChange }) {
  const [run, busy] = useAction();
  const on = !!pause?.paused;
  const until = on ? (pause.until || Date.now() / 1000 + 10 * 365 * 86400) : 0;
  const act = (m) => run(async () => {
    const r = m === 0 ? await post('/api/dns/resume', {}) : await post('/api/dns/pause', { minutes: m < 0 ? 0 : m });
    onChange(r);
  }, m === 0 ? 'Blocking is back on.' : 'Blocking is paused.');
  if (on) {
    return (
      <Note tone="warn" title="Blocking is paused">
        {pause.until ? `Every name is being let through until ${new Date(pause.until * 1000).toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' })}. Blocking starts again by itself.` : 'Every name is being let through until you resume.'}
        {' '}<Button small kind="primary" disabled={busy} onClick={() => act(0)}>Resume now</Button>
      </Note>
    );
  }
  return (
    <div className="inline" style={{ justifyContent: 'flex-end' }}>
      <span className="small muted">Page not loading? Switch blocking off for a few minutes to see whether blocking is the reason.</span>
      <PauseMenu until={until} name="blocking" label="Pause blocking…" choices={BLOCK_PAUSES} disabled={busy} onPause={(m) => act(m)} />
    </div>
  );
}

function Overview() {
  const { data, setData } = useData('/api/dns/summary', { every: 10000 });
  useStream(['dns.stats'], (t, p) => p && setData((d) => (d ? { ...d, stats: p } : d)));
  const { data: ups } = useData('/api/dns/upstreams', { topics: ['dns.upstreams'], every: 10000 });
  const [range, setRange, rangeOptions] = usePeriod('dns', { live: true });
  const { data: h } = useData(`/api/overview/history?range=${range}`, { every: 30000, enabled: range !== 'live' });
  const { data: d24 } = useData('/api/overview/history?range=24h', { every: 60000 });
  const { data: day } = useData('/api/dns/top?hours=24', { every: 30000 });
  const [feed, setFeed] = useState([]);
  useStream(['dns.live'], (t, p) => {
    if (!Array.isArray(p)) return;
    const fresh = p.filter((e) => !(e.qname === 'health.nopewarden.invalid')).reverse();
    if (fresh.length) setFeed((f) => [...fresh, ...f].slice(0, 8));
  });
  const st = data?.stats;
  const total = st?.hour_total || 0;
  const outcomes = st ? [['Forwarded', st.outcomes?.forwarded], ['Blocked', st.outcomes?.blocked], ['Cached', st.outcomes?.cached], ['Local', st.outcomes?.local]] : [];
  const hist = st?.history || [];
  const peak = Math.max(0, ...hist);
  const list = ups || [];
  const working = list.filter((u) => u.state !== 'down').length;
  const slow = list.filter((u) => u.state === 'degraded').length;
  const upTone = !list.length ? '' : working === 0 ? 'bad' : working < list.length || slow ? 'warn' : 'good';
  const series = [{ key: 'dns.qps', name: 'Queries per second', color: 'var(--color-chart-series-1)', pts: h?.series?.['dns.qps'] || [] }];
  const vals = (src, key) => (src?.series?.[key] || []).map((p) => p[1]);
  const rest = (tot, b, c) => tot.map((v, i) => Math.max(0, v - (b[i] || 0) - (c[i] || 0)));
  const liveLayers = (() => {
    const b = st?.history_blocked || [], c = st?.history_cached || [];
    return [['answered', 'Answered', 'var(--color-chart-series-1)', rest(hist, b, c)], ['cached', 'From memory', 'var(--color-chart-series-3)', c], ['blocked', 'Blocked', 'var(--color-status-bad)', b]].map(([key, name, color, values]) => ({ key, name, color, values }));
  })();
  const histLayers = (() => {
    const tot = vals(h, 'dns.qps'), b = vals(h, 'dns.blocked'), c = vals(h, 'dns.cached');
    const pad = (a) => (a.length >= tot.length ? a.slice(a.length - tot.length) : [...new Array(tot.length - a.length).fill(0), ...a]);
    const bb = pad(b), cc = pad(c);
    return [['answered', 'Answered', 'var(--color-chart-series-1)', rest(tot, bb, cc)], ['cached', 'From memory', 'var(--color-chart-series-3)', cc], ['blocked', 'Blocked', 'var(--color-status-bad)', bb]].map(([key, name, color, values]) => ({ key, name, color, values }));
  })();
  const SPAN = { '1h': 3600, '24h': 86400, '7d': 604800 }[range] || 60;
  const mini = (key, color = 'var(--color-chart-series-1)') => [{ key, name: '', color, pts: d24?.series?.[key] || [] }];
  const miniChart = (key, unit, label, opts = {}) => <Chart series={mini(key, opts.color)} from={d24?.from || 0} to={d24?.to || 1} unit={unit} kind={opts.kind} height={56} compact axes label={label} />;
  const msSeries = [{ key: 'dns.ms', name: 'Answer time', color: 'var(--color-chart-series-4)', pts: h?.series?.['dns.ms'] || d24?.series?.['dns.ms'] || [] }];
  const msWindow = h?.series?.['dns.ms'] ? h : d24;
  return (
    <>
      {data && data.lists_enabled > 0 && !data.blocking_active && (
        <Note tone="warn" title="Blocking is not active yet">Your blocklists have not downloaded yet, so nothing is being blocked. NopeWarden keeps trying and starts blocking as soon as one arrives. The Blocklists tab shows how it is going.</Note>
      )}
      <PauseBar pause={data?.pause} onChange={(p) => setData((d) => (d ? { ...d, pause: p } : d))} />
      <section className="dns-tiles">
        <button type="button" className="card tile tilebtn" onClick={() => { window.location.hash = '#/logs'; }}><span className="lbl">Lookups, 24 hours</span><div><span className="val">{day ? fmt.n(day.total) : '–'}</span></div><div className="tspark">{miniChart('dns.qps', 'lookups/s', 'Lookups per second, last 24 hours')}</div><span className="foot">{fmt.d1(st?.qps)} a second right now</span></button>
        <button type="button" className="card tile tilebtn" onClick={() => { window.location.hash = '#/logs'; }}><span className="lbl">Blocked</span><div><span className="val">{fmt.d1(st?.blocked_pct)}</span><span className="unit">%</span></div><div className="tspark">{miniChart('dns.blocked', 'blocked/s', 'Blocked lookups per second, last 24 hours', { color: 'var(--color-status-bad)' })}</div><span className="foot">{total > 0 && !(st?.blocked_pct > 0) ? 'Nothing blocked in the last hour' : `${fmt.n(st?.outcomes?.blocked || 0)} blocked in the last hour`} · {fmt.n(data?.unique_blocked ?? data?.blocked_entries)} names on your lists</span></button>
        <div className="card tile"><span className="lbl">Answered from memory</span><div><span className="val">{fmt.d1(st?.cached_pct)}</span><span className="unit">%</span></div><div className="tspark">{miniChart('dns.cached', 'from memory/s', 'Answers from memory per second, last 24 hours')}</div><span className="foot">{fmt.n(st?.cache_size)} saved answers</span></div>
        <div className="card tile"><span className="lbl">Response time</span><div><span className="val">{fmt.d1(st?.avg_ms)}</span><span className="unit">ms</span></div><div className="tspark">{miniChart('dns.ms', 'ms', 'Response time, last 24 hours', { kind: 'ms' })}</div><span className="foot">95% under {fmt.d1(st?.p95_ms)} ms</span></div>
        <button type="button" className="card tile tilebtn" onClick={() => { window.location.hash = '#/dns/servers'; }}><span className="lbl">Upstream servers</span><div><span className="val">{working}</span><span className="unit">of {list.length} working</span></div><span className="foot"><i className={`sdot ${upTone}`} aria-hidden="true" /> {slow ? `${slow} slow` : upTone === 'bad' ? 'None answering' : 'All answering normally'}</span></button>
      </section>
      <Card title="Lookups over time" desc={range === 'live' ? 'Lookups per second, last minute, split by how each was answered' : 'Lookups per second, averaged over each period, split by how each was answered'}
        actions={<Segmented value={range} onChange={setRange} options={rangeOptions} />}>
        {range === 'live' ? (
          <>
            <div className="small muted" style={{ textAlign: 'right' }}>{peak > 0 ? `peak ${fmt.d1(peak)} a second` : ''}</div>
            <StackedArea layers={liveLayers} span={60} height={120} unit="/s" xLabels={['60 s ago', '30 s', 'now']} empty="No lookups in the last minute" label="Lookups per second, last minute, by how they were answered" />
            <div className="livefeed" aria-live="off">
              <div className="small muted">Latest lookups: the newest 8, starting from when you opened this page. Older ones are in <button type="button" className="link" onClick={() => { window.location.hash = '#/logs'; }}>Logs</button>.</div>
              {!feed.length ? <div className="small muted">Waiting for the next lookup.</div> : feed.map((e, i) => (
                <div className="lf-row" key={`${e.ts}-${e.qname}-${i}`}>
                  <span>{new Date(e.ts * 1000).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' })}</span>
                  <span className="lf-who">{e.client_name || e.client}</span>
                  <span className="lf-name">{e.qname}</span>
                  <Pill tone={e.result === 'blocked' ? 'bad' : e.result === 'failed' ? 'warn' : ''}>{{ forwarded: 'Answered', cached: 'From memory', blocked: 'Blocked', local: 'Your names', rate_limited: 'Slowed', failed: 'Failed' }[e.result] || e.result}</Pill>
                </div>
              ))}
            </div>
          </>
        ) : (
          <StackedArea layers={histLayers} span={SPAN} height={150} unit="/s" xLabels={[`${{ '1h': '60 min', '24h': '24 h', '7d': '7 days' }[range]} ago`, '', 'now']} empty="Nothing recorded for this period yet" label={`Lookups per second, ${range}, by how they were answered`} />
        )}
      </Card>
      <div className="dns-cards">
        <Card title="How queries were answered" desc="Last hour">
          {total === 0 ? <Empty>No queries yet.</Empty> : (
            <div className="stack">
              {outcomes.map(([k, v]) => (
                <div key={k} className="stack gap1">
                  <div className="spread small"><span>{k}</span><span className="muted">{fmt.n(v || 0)} · {fmt.d1((100 * (v || 0)) / total)}%</span></div>
                  <Bar pct={(100 * (v || 0)) / total} thin />
                </div>
              ))}
            </div>
          )}
        </Card>
        <Card title="Answer time" desc="How long forwarded lookups took to answer. Lower is better.">
          <Chart series={msSeries} from={msWindow?.from || 0} to={msWindow?.to || 1} kind="ms" height={110} label="Answer time" empty="Collecting data. Answer times appear after a few minutes of lookups." />
        </Card>
      </div>
      <WhatChanged match={/dns|resolver|forwarder|blocklist|lookups|upstream|encrypted/i} range={range} />
    </>
  );
}

// Who asked and what was stopped, for a period of your choice.
function Activity() {
  const [range, setRange, rangeOptions] = usePeriod('dns-activity');
  const hours = { '1h': 1, '24h': 24, '7d': 168 }[range] || 24;
  const words = hours === 1 ? 'last hour' : hours === 24 ? 'last 24 hours' : 'last 7 days';
  const [self, setSelf] = useState(false);
  const { data: top } = useData(`/api/dns/top?hours=${hours}${self ? '&self=1' : ''}`, { every: 30000 });
  const biggest = (rows) => Math.max(1, ...(rows || []).map((r) => r.n));
  return (
    <>
      <div className="spread"><span className="small muted">Who asked, and what was stopped.</span><Segmented value={range} onChange={setRange} options={rangeOptions} /></div>
      <div className="dns-cards">
        <Card title="Busiest devices" desc={`DNS queries, ${words}`}
          actions={<button type="button" className="link small" onClick={() => setSelf((x) => !x)}>{self ? 'Hide this router' : 'Show this router'}</button>}>
          {!top?.clients?.length ? <Empty>No activity yet.</Empty> : (
            <div className="rank">
              {top.clients.slice(0, 6).map((c) => {
                const me = c.addrs?.length === 1 && (c.addrs[0] === '127.0.0.1' || c.addrs[0] === '::1');
                return (
                  <div key={c.key} className="stack gap1">
                    <div className="rk-top"><span className="name">{c.label || (c.addrs || [])[0] || c.key}{me && ' (this router)'}</span><b>{fmt.n(c.n)}</b></div>
                    {c.label && (c.addrs || [])[0] && <div className="rk-sub mono">{c.addrs[0]}</div>}
                    <Bar pct={(100 * c.n) / biggest(top.clients)} thin />
                  </div>
                );
              })}
            </div>
          )}
        </Card>
        <Card title="What blocked it" desc={`Which list or rule stopped the lookups, ${words}`}>
          {!top?.blocked_by?.length ? <Empty>Nothing has been blocked in this period.</Empty> : (
            <div className="rank">
              {top.blocked_by.map((c) => (
                <div key={c.key} className="stack gap1">
                  <div className="rk-top"><span className="name">{c.key}</span><b>{fmt.n(c.n)}</b></div>
                  <Bar pct={(100 * c.n) / biggest(top.blocked_by)} thin />
                </div>
              ))}
            </div>
          )}
        </Card>
        <Card title="Most blocked" desc={`Names that were stopped by a blocklist or your own rules, ${words}`}>
          {!top?.blocked?.length ? <Empty>Nothing has been blocked in this period.</Empty> : (
            <div className="rank">
              {top.blocked.slice(0, 6).map((c) => (
                <div key={c.key} className="stack gap1">
                  <div className="rk-top"><span className="name mono">{c.key}</span><b>{fmt.n(c.n)}</b></div>
                  <Bar pct={(100 * c.n) / biggest(top.blocked)} thin />
                </div>
              ))}
            </div>
          )}
        </Card>
        <Card title="Most asked" desc={`Names devices looked up most, ${words}`}>
          {!top?.domains?.length ? <Empty>No lookups yet.</Empty> : (
            <div className="rank">
              {top.domains.slice(0, 6).map((c) => (
                <div key={c.key} className="stack gap1">
                  <div className="rk-top"><span className="name mono">{c.key}</span><b>{fmt.n(c.n)}</b></div>
                  <Bar pct={(100 * c.n) / biggest(top.domains)} thin />
                </div>
              ))}
            </div>
          )}
        </Card>
      </div>
    </>
  );
}

// Shows, before a pattern is saved, whether it is written correctly and what it would catch.
function PatternTry({ rule, setRule }) {
  const [name, setName] = useState('');
  const [res, setRes] = useState(null);
  useEffect(() => {
    if (!rule.domain) { setRes(null); return undefined; }
    const h = setTimeout(() => { post('/api/dns/rules/try', { kind: rule.kind, pattern: rule.domain, name }).then(setRes).catch(() => setRes(null)); }, 350);
    return () => clearTimeout(h);
  }, [rule.domain, rule.kind, name]);
  return (
    <div className="stack gap1">
      <p className="small muted" style={{ margin: 0 }}>
        {rule.kind === 're'
          ? 'A regular expression is matched against the whole name. Only use this if you know them.'
          : 'A pattern is matched against the whole name. * stands for any run of letters, digits, dots and dashes, and ? for one of them. ads*.example.com catches ads1.example.com but not www.example.com.'}
      </p>
      <details>
        <summary className="small">More options</summary>
        <div className="small" style={{ marginTop: 'var(--space-2)' }}>
          <ToggleRow title="Use a regular expression instead" desc="For people who already know regular expressions. They are checked for mistakes before they are saved." checked={rule.kind === 're'} onChange={(v) => setRule({ ...rule, kind: v ? 're' : 'glob' })} />
        </div>
      </details>
      <div className="inline">
        <div style={{ flex: '1 1 14rem' }}><Input value={name} onChange={setName} aria-label="Try a name" placeholder="Try a name, such as ads1.example.com" /></div>
      </div>
      {res?.error && <div className="small" style={{ color: 'var(--color-status-bad)' }}>{res.error}.</div>}
      {res && !res.error && (
        <div className="small muted">
          {name && <div>{res.name_matches ? <b>That name would be caught.</b> : 'That name would not be caught.'}</div>}
          {res.recent?.length > 0
            ? <div>In the last day this would have caught: {res.recent.join(', ')}{res.more ? ` and ${res.more} more` : ''}.</div>
            : <div>It would not have caught anything asked in the last day.</div>}
        </div>
      )}
    </div>
  );
}

// Names that only one server knows (an office network, say) are asked of that server.
function DifferentServers() {
  const { data, reload } = useData('/api/dns/forwards');
  const [f, setF] = useState({ domain: '', server: '' });
  const [res, setRes] = useState({});
  const [run, busy] = useAction();
  const rules = data?.rules || [];
  const save = (next) => run(() => put('/api/dns/forwards', { rules: next }), 'Saved.').then((ok) => { if (ok !== false) reload(); return ok; });
  const test = async (r) => {
    const out = await run(() => post('/api/dns/forwards/test', { domain: r.domain, servers: [r.servers[0]] }));
    if (out) setRes((x) => ({ ...x, [r.domain]: out }));
  };
  return (
    <Card title="Names that use a different server" desc="For names only one server knows, such as an office network. Names ending in the domain you type are asked of that server instead of your usual ones. Your own names and your blocks still come first.">
      <div className="inline">
        <div style={{ flex: '1 1 12rem' }}><Input value={f.domain} onChange={(v) => setF({ ...f, domain: v })} aria-label="Domain" placeholder="office.example" /></div>
        <div style={{ flex: '1 1 10rem' }}><Input value={f.server} onChange={(v) => setF({ ...f, server: v })} aria-label="Address of the server" placeholder="10.0.0.2" /></div>
        <Button kind="primary" disabled={busy || !f.domain || !f.server} onClick={() => save([...rules, { domain: f.domain, servers: [f.server] }]).then((ok) => { if (ok !== false) setF({ domain: '', server: '' }); })}>Add</Button>
      </div>
      <div className="list">
        {rules.map((r) => (
          <div className="item startalign" key={r.domain}>
            <div>
              <div className="name">{r.domain}</div>
              <div className="small muted">Asked of {r.servers.join(', ')}</div>
              {res[r.domain] && <div className="small" style={{ color: res[r.domain].ok ? 'var(--color-status-good)' : 'var(--color-status-bad)' }}>{res[r.domain].verdict}{res[r.domain].ms ? ` (${res[r.domain].ms} ms)` : ''}</div>}
            </div>
            <div className="actions">
              <Button small disabled={busy} onClick={() => test(r)}>Test</Button>
              <Button small kind="danger" icon="trash" disabled={busy} onClick={() => save(rules.filter((x) => x.domain !== r.domain))}>Remove</Button>
            </div>
          </div>
        ))}
        {!rules.length && <Empty>None. All names use your usual servers.</Empty>}
      </div>
    </Card>
  );
}

// Where names are looked up: the DNS servers NopeWarden asks, encrypted DNS, and forcing devices to use it.
function Servers() {
  const { data: ups, reload: ru } = useData('/api/dns/upstreams', { topics: ['dns.upstreams'], every: 10000 });
  // Forced DNS lives here. Its settings are the firewall's, so they are read and saved the same way.
  const { data: fwov, reload: reloadFw } = useData('/api/fw/overview', { topics: ['fw.status'] });
  const { data: pickers } = useData('/api/fw/pickers');
  const [runForced] = useAction();
  const fs = fwov?.settings;
  const saveForced = (k) => (v) => runForced(() => put('/api/fw/settings', { ...fs, [k]: v })).then(reloadFw);
  const [add, setAdd] = useState(false);
  const list = ups || [];
  return (
    <>
      {fs && <ForcedDns s={fs} save={saveForced} pickers={pickers} reload={reloadFw} />}
      <EncryptedDns onChanged={ru} />
      <ResolverCard />
      {(list || []).some((u) => u.id < 0)
        ? <Card title="Upstream DNS servers" desc="Not used right now."><p className="small muted" style={{ margin: 0 }}>Resolve directly is on, so NopeWarden is not asking forwarders. Your forwarders are kept and take over by themselves if Unbound stops answering. To see or change them, choose Encrypted forwarders above.</p></Card>
        : <Upstreams ups={list} reload={ru} onAdd={() => setAdd(true)} />}
      <DifferentServers />
      <EncryptedDnsServer />
      {add && <AddUpstream onClose={() => setAdd(false)} onSaved={ru} />}
    </>
  );
}

// How a server is doing, in words a person can act on. Only every server being down is "No upstream".
function UpstreamRow({ u, num, first, last, ordered, onMove, onRemove, reload }) {
  const [res, setRes] = useState(null);
  const [run, busy] = useAction();
  const tone = u.state === 'healthy' ? 'good' : u.state === 'degraded' ? 'warn' : 'bad';
  const label = { healthy: 'Healthy', degraded: 'Slow', down: 'Down' }[u.state] || u.state;
  const ago = (t) => (t ? fmt.ago(t) : 'never');
  const test = async () => {
    const r = await run(() => post(`/api/dns/upstreams/${u.id}/test`, {}));
    if (r) { setRes(r); reload(); }
  };
  return (
    <div className="item startalign">
      <div style={{ display: 'flex', gap: '0.75rem', minWidth: 0 }}>
        {ordered && <span className="muted" style={{ fontVariantNumeric: 'tabular-nums', minWidth: '1.2rem', paddingTop: '0.1rem' }} aria-label={`Number ${num}`}>{num}</span>}
        <div style={{ minWidth: 0 }}>
          <div className="name">{u.name}</div>
          <div className="meta">{u.addr} · {u.proto.toUpperCase()}{u.reused ? ' · connection kept open' : ''}</div>
          {u.state !== 'healthy' && u.reason && <div className="small" style={{ color: u.state === 'down' ? 'var(--color-status-bad)' : 'var(--color-status-warning)', marginTop: '0.25rem' }}>{u.reason}{u.state === 'down' && u.retry_in ? ` Trying again in ${u.retry_in} s.` : ''}</div>}
          {(u.state !== 'healthy' || res) && <div className="small muted" style={{ marginTop: '0.2rem' }}>Last answered {ago(u.last_ok)}{u.failures ? ` · ${fmt.n(u.failures)} failed so far` : ''}</div>}
          {res && (
            <div className="small" style={{ marginTop: '0.35rem' }} role="status">
              <strong>{res.ok ? 'Test: answered' : 'Test: no answer'}</strong>
              {res.ok && <> · {res.connect_ms > 0 ? <>connecting {fmt.d1(res.connect_ms)} ms · </> : null}answer {fmt.d1(res.answer_ms)} ms</>}
              <div className="muted">{res.verdict}</div>
            </div>
          )}
        </div>
      </div>
      <div className="actions">
        <span className="muted small">{fmt.d1(u.latency_ms)} ms · {fmt.n(u.queries)} queries</span>
        <Pill tone={tone}>{label}</Pill>
        {ordered && <><Button small icon="up" disabled={first} aria-label={`Move ${u.name} up`} onClick={() => onMove(-1)} /><Button small icon="down" disabled={last} aria-label={`Move ${u.name} down`} onClick={() => onMove(1)} /></>}
        <Button small disabled={busy} onClick={test}>{busy ? 'Testing…' : 'Test'}</Button>
        <Button small kind="danger" icon="trash" onClick={onRemove}>Remove</Button>
      </div>
    </div>
  );
}

function Upstreams({ ups, reload, onAdd }) {
  const { data: mode, reload: rm } = useData('/api/dns/upstream-mode');
  const [run] = useAction();
  const ordered = mode?.mode === 'ordered';
  const sorted = [...ups].sort((a, b) => a.position - b.position);
  const move = async (i, d) => {
    const ids = sorted.map((u) => u.id);
    [ids[i], ids[i + d]] = [ids[i + d], ids[i]];
    await run(() => put('/api/dns/upstreams/order', { ids }));
    reload();
  };
  const setMode = async (m) => { await run(() => put('/api/dns/upstream-mode', { mode: m })); rm(); };
  const allDown = ups.length > 0 && ups.every((u) => u.state === 'down');
  return (
    <Card title="Upstream DNS servers" desc="Where NopeWarden asks when it does not already know the answer."
      actions={<><Button small icon="plus" onClick={onAdd}>Add</Button><Button small onClick={() => run(() => post('/api/dns/cache/flush'), 'Memory cleared.')}>Clear saved answers</Button></>}>
      <Field label="Which server is asked first" hint={ordered ? 'The first server in the list is asked; the next one only if it does not answer. Use the arrows to change the order.' : 'NopeWarden measures how fast each one answers and asks the fastest first. Slower ones are the backup.'}>
        <Segmented value={ordered ? 'ordered' : 'fastest'} onChange={setMode} options={[{ value: 'fastest', label: 'Fastest first' }, { value: 'ordered', label: 'In my order' }]} />
      </Field>
      {allDown && <Note tone="warn" title="No server is answering">Names that are not already saved cannot be looked up until one of these answers again.</Note>}
      <div className="list">
        {sorted.map((u, i) => (
          <UpstreamRow key={u.id} u={u} num={i + 1} first={i === 0} last={i === sorted.length - 1} ordered={ordered} onMove={(d) => move(i, d)} reload={reload}
            onRemove={() => run(() => del(`/api/dns/upstreams/${u.id}`)).then(reload)} />
        ))}
        {!ups.length && <Empty>No upstream servers.</Empty>}
      </div>
    </Card>
  );
}

const PRESETS = [
  { label: 'Cloudflare (private, encrypted)', address: '1.1.1.1:853', proto: 'tls', tlsname: 'cloudflare-dns.com' },
  { label: 'Quad9 (blocks malware, encrypted)', address: '9.9.9.9:853', proto: 'tls', tlsname: 'dns.quad9.net' },
  { label: 'Google (encrypted)', address: '8.8.8.8:853', proto: 'tls', tlsname: 'dns.google' },
];
// A bare address gets the usual port for its kind, so 8.8.8.8 works without typing :853.
const withPort = (a, proto) => { const t = (a || '').trim(); return /:\d+$/.test(t) || t.startsWith('[') || t.includes('://') ? t : `${t}:${proto === 'tls' ? 853 : 53}`; };
function AddUpstream({ onClose, onSaved }) {
  const [f, setF] = useState({ name: '', address: '', proto: 'tls', tlsname: '' });
  const [run, busy] = useAction();
  const set = (k) => (v) => setF((x) => ({ ...x, [k]: v }));
  return (
    <Modal title="Add a DNS server" onClose={onClose} footer={<><Button onClick={onClose}>Cancel</Button><Button kind="primary" disabled={busy || !f.address} onClick={async () => { if (await run(() => post('/api/dns/upstreams', { ...f, name: f.name || f.address, address: withPort(f.address, f.proto) }), 'Added.')) { onSaved(); onClose(); } }}>Add</Button></>}>
      <div className="stack">
        <span className="small muted">Pick a well-known one, or fill in your own.</span>
        <div className="inline">{PRESETS.map((p) => <Button small key={p.label} onClick={() => setF({ name: p.label.split(' (')[0], address: p.address, proto: p.proto, tlsname: p.tlsname })}>{p.label}</Button>)}</div>
      </div>
      <div className="formgrid">
        <Field label="Name"><Input value={f.name} onChange={set('name')} /></Field>
        <Field label="Address and port" hint="Like 1.1.1.1. The usual port is added for you."><Input value={f.address} onChange={set('address')} /></Field>
        <Field label="Kind"><Select value={f.proto} onChange={set('proto')} options={[{ value: 'tls', label: 'Encrypted (DNS-over-TLS)' }, { value: 'udp', label: 'Plain (UDP)' }]} /></Field>
        {f.proto === 'tls' && <Field label="Server name" hint="Used to check its certificate"><Input value={f.tlsname} onChange={set('tlsname')} /></Field>}
      </div>
    </Modal>
  );
}

function ListMeta({ l }) {
  const [details, setDetails] = useState(false);
  const hhmm = (t) => new Date(t * 1000).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
  const counts = `${fmt.n(l.entries)} blocked${l.allowed ? ` · ${fmt.n(l.allowed)} allowed` : ''}${l.skipped ? ` · ${fmt.n(l.skipped)} not used` : ''}`;
  const total = `${fmt.n(l.entries + l.allowed)} in all${l.skipped ? `, ${fmt.n(l.skipped)} lines NopeWarden cannot use` : ''}`;
  if (l.status === 'downloading') return <div className="meta">Downloading…</div>;
  if (l.status === 'new') return <div className="meta">{l.enabled ? 'Waiting to download. NopeWarden does this by itself.' : 'Switched off'}</div>;
  if (l.status === 'error') {
    return (
      <div className="meta">
        <span className="errtext">{l.plain || "Couldn't download this list."}</span>
        {l.retry_at ? ` NopeWarden will try again at ${hhmm(l.retry_at)}.` : ''}
        {l.last_ok ? <> Using the copy from {fmt.ago(l.last_ok)} ({counts}).</> : ' Nothing is blocked from this list yet.'}
        {' '}<button className="linkbtn" onClick={() => setDetails(!details)}>{details ? 'Hide details' : 'Details'}</button>
        {details && <div className="small mono muted">{l.error}</div>}
      </div>
    );
  }
  return <div className="meta" title={total}>{counts} · {l.status === 'stale' ? <span style={{ color: 'var(--color-status-warning)' }}>not updated for a while ({fmt.ago(l.last_ok)})</span> : `updated ${fmt.ago(l.last_ok)}`}</div>;
}

function Lists() {
  const delayedDelete = useDelayedDelete();
  const { data: lists, reload } = useData('/api/dns/lists', { topics: ['dns.lists'], every: 15000 });
  const { data: sum } = useData('/api/dns/summary', { every: 30000 });
  const { data: rules, reload: rr } = useData('/api/dns/rules', { topics: ['dns.lists'] });
  const [add, setAdd] = useState(false);
  const [run] = useAction();
  const undoDelete = useUndoDelete();
  const [rule, setRule] = useState({ domain: '', action: 'block', note: '', kind: '' });
  const [q, setQ] = useState('');
  const [why, setWhy] = useState(null);
  const [names, setNames] = useState(null);
  return (
    <>
      <Card title="Ad, tracker and malware lists" desc="Lists of ad, tracker and malware names. Blocked names get no answer. Blocks for an app, category or site use the App and category lists on the Protection page." actions={<Button small icon="plus" onClick={() => setAdd(true)}>Add list</Button>}>
        {sum?.unique_blocked > 0 && <p className="small muted">Different names blocked across all lists: <b>{fmt.n(sum.unique_blocked)}</b>. A name that is on two lists counts once.</p>}
        <ScheduleEditor id="lists" label="How often to refresh the lists" hideRun />
        <div className="list">
          {<Limited items={(lists || [])} noun="lists" render={(l) => (
            <div className="item" key={l.id}>
              <div>
                <div className="name">{l.name} {l.local && <Pill>Yours</Pill>}</div>
                <ListMeta l={l} />
              </div>
              <div className="actions">
                <Button small onClick={() => setNames(l)}>View names</Button>
                {!l.local && <Button small icon="refresh" onClick={() => run(() => post(`/api/dns/lists/${l.id}/refresh`), 'Updated.').then(reload)}>Update</Button>}
                <Button small kind="danger" icon="trash" onClick={async () => (l.local ? delayedDelete(`The list "${l.name}"`, () => del(`/api/dns/lists/${l.id}`), reload, 'Its names are not kept.') : undoDelete(`The list "${l.name}"`, () => del(`/api/dns/lists/${l.id}`), () => post('/api/dns/lists', { name: l.name, url: l.url, format: l.format, interval_hours: l.interval_hours }), reload))}>Delete</Button>
                <button role="switch" aria-checked={l.enabled} aria-label={`Use ${l.name}`} className="switch" onClick={() => run(() => patch(`/api/dns/lists/${l.id}`, { enabled: !l.enabled })).then(reload)} />
              </div>
            </div>
          )} />}
          {!lists?.length && <Empty>No lists yet.</Empty>}
        </div>
      </Card>

      <Card enter title="Always allow or always block" desc="Your own exceptions to names beat every list. To let something through for one device, profile or network, use Exceptions on the Protection page, Blocks tab.">
        <div className="inline">
          <Select aria-label="Kind of rule" value={rule.kind === 're' ? 'glob' : rule.kind} onChange={(v) => setRule({ ...rule, kind: v, domain: '' })} options={[{ value: '', label: 'A name' }, { value: 'glob', label: 'A pattern' }]} />
          <div style={{ flex: '1 1 14rem' }}><Input value={rule.domain} onChange={(v) => setRule({ ...rule, domain: v })} aria-label={rule.kind ? 'Pattern to always allow or block' : 'Name to always allow or block'} placeholder={rule.kind === 're' ? '^track[0-9]+\\.' : rule.kind ? 'ads*.example.com' : 'example.com'} /></div>
          <Select aria-label="Allow or block" value={rule.action} onChange={(v) => setRule({ ...rule, action: v })} options={[{ value: 'block', label: 'Block' }, { value: 'allow', label: 'Allow' }]} />
          <Button kind="primary" disabled={!rule.domain} onClick={() => run(() => post('/api/dns/rules', rule), 'Saved.').then((ok) => { if (ok !== false) { setRule({ ...rule, domain: '' }); rr(); } })}>Add</Button>
        </div>
        {rule.kind ? <PatternTry rule={rule} setRule={setRule} /> : <p className="small muted" style={{ margin: 0 }}>A name also covers everything under it, so youtube.com covers music.youtube.com. Choose A pattern to match many names at once.</p>}
        <div className="list">
          {<Limited items={(rules || [])} noun="rules" render={(r) => (
            <div className="item" key={r.id}>
              <div>
                <div className="inline"><Pill tone={r.action === 'allow' ? 'good' : 'bad'}>{r.action}</Pill>{r.kind !== 'name' && <Pill>{r.kind === 're' ? 'Expression' : 'Pattern'}</Pill>}<span className="name">{r.kind === 'name' ? r.domain : r.text}</span></div>
                {r.action === 'block' && r.hits_24h > 0 && <div className="small muted">Stopped {fmt.n(r.hits_24h)} lookup{r.hits_24h === 1 ? '' : 's'} in the last day.</div>}
              </div>
              <Button small kind="danger" icon="trash" onClick={() => run(() => del(`/api/dns/rules/${r.id}`)).then(rr)}>Remove</Button>
            </div>
          )} />}
        </div>
      </Card>

      <Card title="Why is this blocked?" desc="Type a name to see which list is responsible. To find out why one device cannot reach a site, use Why can't it reach that? on the Diagnostics page, Tools tab.">
        <div className="inline">
          <div style={{ flex: '1 1 14rem' }}><Input value={q} onChange={setQ} aria-label="Name to check" placeholder="ads.example.com" /></div>
          <Button disabled={!q} onClick={() => run(() => get(`/api/dns/explain?domain=${encodeURIComponent(q)}`)).then(setWhy)}>Check</Button>
        </div>
        {why && (
          <Note tone={why.blocked ? 'bad' : 'good'} title={why.blocked ? 'Blocked' : 'Not blocked'}>
            {why.reason === 'custom-block' && ` by your own rule "${why.rule}".`}
            {why.reason === 'list' && ` by ${why.rule}.`}
            {why.reason === 'custom-allow' && ' Your own allow rule lets it through.'}
            {why.reason === 'none' && ' No list contains it.'}
            {why.reason === 'unfiltered' && ' Filtering is off for the default group.'}
          </Note>
        )}
      </Card>
      {add && <AddList onClose={() => setAdd(false)} onSaved={reload} />}
      {names && <ListNames l={names} onClose={() => setNames(null)} onAllowed={rr} />}
    </>
  );
}

function ListNames({ l, onClose, onAllowed }) {
  const [q, setQ] = useState('');
  const [d, setD] = useState(null);
  const [run] = useAction();
  const [done, setDone] = useState([]);
  React.useEffect(() => {
    const t = setTimeout(() => { get(`/api/dns/lists/${l.id}/names?q=${encodeURIComponent(q)}`).then(setD).catch(() => setD({ names: [], total: 0 })); }, 250);
    return () => clearTimeout(t);
  }, [q, l.id]);
  return (
    <Modal title={l.name} onClose={onClose} footer={<Button kind="primary" onClick={onClose}>Done</Button>}>
      <div className="stack">
        <p className="small muted">{d ? `${fmt.n(d.total)} names blocked. ` : ''}{d?.updated ? `Last updated ${fmt.ago(d.updated)}.` : 'Not downloaded yet.'}</p>
        <Input value={q} onChange={setQ} placeholder="Search names" aria-label="Search names" />
        <div className="list" style={{ maxHeight: '18rem', overflow: 'auto' }}>
          {(d?.names || []).map((n) => (
            <div className="item" key={n.name}>
              <span className="name mono">{n.name}</span>
              {done.includes(n.name) ? <Pill tone="good">Allowed</Pill> : <Button small onClick={async () => { if (await run(() => post('/api/dns/rules', { domain: n.name, action: 'allow', note: '' }), 'Allowed.')) { setDone([...done, n.name]); onAllowed?.(); } }}>Allow</Button>}
            </div>
          ))}
          {d && !d.names.length && <Empty>{q ? 'No name matches.' : 'Nothing to show.'}</Empty>}
        </div>
        {d && d.total > 100 && !q && <p className="small muted">Showing the first 100. Search to find a name.</p>}
      </div>
    </Modal>
  );
}

const SUGGESTED = [
  { name: 'StevenBlack unified hosts', url: 'https://raw.githubusercontent.com/StevenBlack/hosts/master/hosts', format: 'hosts' },
  { name: 'OISD small', url: 'https://small.oisd.nl/domainswild2', format: 'domains' },
  { name: 'Newly registered domains (7 days)', url: 'https://cdn.jsdelivr.net/gh/hagezi/nrd@latest/domains/nrd7.txt', format: 'domains', note: 'Blocks websites registered in the last 7 days, which scammers rely on. Honest new sites are caught too, so allow any you trust. It is a big list, about 3 million names and about 250 MB of memory, so check Diagnostics afterwards on a small machine.' },
];
function AddList({ onClose, onSaved }) {
  const [f, setF] = useState({ name: '', url: '', text: '', format: 'domains', interval_hours: 24 });
  const [mode, setMode] = useState('url');
  const [note, setNote] = useState('');
  const [run, busy] = useAction();
  const set = (k) => (v) => setF((x) => ({ ...x, [k]: v }));
  return (
    <Modal title="Add a blocklist" onClose={onClose} footer={<><Button onClick={onClose}>Cancel</Button><Button kind="primary" disabled={busy || !f.name || (mode === 'url' ? !f.url : !f.text)} onClick={async () => { if (await run(() => post('/api/dns/lists', { ...f, url: mode === 'url' ? f.url : '', text: mode === 'text' ? f.text : '' }), 'Added. Downloading now.')) { onSaved(); onClose(); } }}>Add</Button></>}>
      <div className="inline">{SUGGESTED.map((s) => <Button small key={s.name} onClick={() => { setMode('url'); setF((x) => ({ ...x, name: s.name, url: s.url, format: s.format })); setNote(s.note || ''); }}>{s.name}</Button>)}</div>
      {note && <Note tone="warn" title="Before you add this one">{note}</Note>}
      <Field label="Name"><Input value={f.name} onChange={set('name')} /></Field>
      <div className="seg"><button className={mode === 'url' ? 'on' : ''} onClick={() => setMode('url')}>From a web address</button><button className={mode === 'text' ? 'on' : ''} onClick={() => setMode('text')}>Paste names</button></div>
      {mode === 'url' ? (
        <div className="formgrid">
          <Field label="Web address"><Input value={f.url} onChange={set('url')} placeholder="https://" /></Field>
          <Field label="Format"><Select value={f.format} onChange={set('format')} options={[{ value: 'domains', label: 'One name per line' }, { value: 'hosts', label: 'Hosts file' }, { value: 'adblock', label: 'Adblock style' }]} /></Field>
        </div>
      ) : (
        <Field label="Names, one per line"><textarea className="in" rows={8} value={f.text} onChange={(e) => set('text')(e.target.value)} /></Field>
      )}
    </Modal>
  );
}

function Records() {
  const { data, reload } = useData('/api/dns/records');
  const { data: dset } = useData('/api/dns/settings');
  const [f, setF] = useState({ name: '', type: 'A', value: '', ttl: 300 });
  const [label, setLabel] = useState('firewall');
  const [changing, setChanging] = useState(false);
  const [run, busy] = useAction();
  // "Name this firewall": <name>.<home domain> pointing at the address this page was opened on.
  const host = typeof window !== 'undefined' ? window.location.hostname : '';
  const isV4 = /^\d{1,3}(\.\d{1,3}){3}$/.test(host);
  const domain = dset?.local_domain || 'lan';
  const clean = (v) => v.toLowerCase().replace(new RegExp(`\\.${domain}$`), '').replace(/[^a-z0-9-]/g, '').replace(/^-+|-+$/g, '');
  const mine = (data || []).find((r) => r.type === 'A' && r.value === host && !r.problem);
  const full = `${clean(label) || 'firewall'}.${domain}`;
  const saveName = async () => {
    if (!(await run(() => post('/api/dns/records', { name: full, type: 'A', value: host, ttl: 300 }), 'Saved.'))) return;
    if (mine && mine.name !== full) await run(() => del(`/api/dns/records/${mine.id}`));
    setChanging(false);
    reload();
  };
  // Where the ending of every name comes from, and where to change it.
  const domainLine = (
    <>Names here end in <b>.{domain}</b>. That ending is your Home domain; change it in <button type="button" className="link" onClick={() => { window.location.hash = '#/dns/settings'; }}>DNS, Settings</button>. Changing it does not rename names you have already added.</>
  );
  const nameBox = (
    <div className="stack gap2">
      <div className="namerow">
        <Input value={label} onChange={setLabel} aria-label="Name for this firewall" placeholder="firewall" />
        <span className="muted mono">.{domain}</span>
        <Button small kind="primary" disabled={busy || !clean(label) || (mine && mine.name === full)} onClick={saveName}>{mine ? 'Save name' : `Add ${full}`}</Button>
        {mine && <Button small onClick={() => setChanging(false)}>Cancel</Button>}
      </div>
      <span className="small muted">Then you can open the console at <b>https://{full}</b> instead of remembering {host}.</span>
    </div>
  );
  return (
    <Card enter title="Your own names" desc={`Make a name point somewhere on your network, like nas.${domain} → 192.168.1.10. Devices that get an address from NopeWarden already have a name automatically.`}>
      {dset && <p className="small muted">{domainLine}</p>}
      {data && dset && isV4 && !mine && <Note title="Give this firewall a name">{nameBox}</Note>}
      {data && dset && isV4 && mine && !changing && (
        <p className="small muted">Open the console at <a href={`https://${mine.name}`}>https://{mine.name}</a>. Type the https:// part, or the browser will search the web instead.{' '}
          <Button small onClick={() => { setLabel(clean(mine.name)); setChanging(true); }}>Change</Button></p>
      )}
      {data && dset && isV4 && mine && changing && <Note title="Change this firewall's name">{nameBox}</Note>}
      <div className="ownrow">
        <Input value={f.name} onChange={(v) => setF({ ...f, name: v })} aria-label="Name" placeholder={`nas.${domain}`} />
        <Select aria-label="Record type" value={f.type} onChange={(v) => setF({ ...f, type: v })} options={['A', 'AAAA', 'CNAME', 'TXT', 'MX']} />
        <Input value={f.value} onChange={(v) => setF({ ...f, value: v })} aria-label="Points to" placeholder="192.168.1.10" />
        <Button kind="primary" disabled={!f.name || !f.value} onClick={() => run(() => post('/api/dns/records', f), 'Saved.').then(() => { setF({ ...f, name: '', value: '' }); reload(); })}>Add</Button>
      </div>
      <div className="list">
        {(data || []).map((r) => <div className="item" key={r.id}><div><span className="name">{r.name.includes('.') ? r.name : `${r.name}.${domain}`}</span> <span className="muted">{r.type} → {r.value}</span>{r.problem && <div className="small" style={{ color: 'var(--color-status-warning)' }}>{r.problem}</div>}</div><Button small kind="danger" icon="trash" onClick={() => run(() => del(`/api/dns/records/${r.id}`)).then(reload)}>Remove</Button></div>)}
        {!data?.length && <Empty>No custom names.</Empty>}
      </div>
    </Card>
  );
}

function SettingsTab() {
  const { data, reload } = useData('/api/dns/settings');
  const [f, setF] = useState(null);
  const [run, busy] = useAction();
  const v = f || data;
  if (!v) return null;
  const set = (k) => (x) => setF({ ...v, [k]: x });
  return (
    <Card title="DNS settings" desc="Where names are looked up when this device does not already know the answer, and how answers are kept.">
      <Field label="What to do with blocked names">
        <Select value={v.block_mode} onChange={set('block_mode')} options={[{ value: 'null', label: 'Answer with an empty address (fastest to fail)' }, { value: 'nxdomain', label: 'Say the name does not exist' }, { value: 'refused', label: 'Refuse' }]} />
      </Field>
      <Field label="How long devices remember a blocked answer (seconds)" hint="After you allow a name, a device can take this long to try it again. Lower is quicker to notice a change. Most people never change it."><NumberInput value={v.block_ttl} onChange={set('block_ttl')} /></Field>
      <div className="formgrid">
        <Field label="Home domain" hint="Names like printer.lan are answered by NopeWarden."><Input value={v.local_domain} onChange={set('local_domain')} /></Field>
        <Field label="Queries per second per device" hint="A device asking more than this is slowed down."><NumberInput value={v.rate_limit} onChange={set('rate_limit')} /></Field>
        <Field label="Short burst allowed"><NumberInput value={v.rate_burst} onChange={set('rate_burst')} /></Field>
        <Field label="Remember answers (entries)"><NumberInput value={v.cache_size} onChange={set('cache_size')} /></Field>
        <Field label="Keep the query history for (days)"><NumberInput value={v.log_retention_days} onChange={set('log_retention_days')} /></Field>
      </div>
      <ToggleRow title="Hide device addresses in the history" desc="Stores a masked address instead of the real one." checked={v.anonymize} onChange={set('anonymize')} />
      <h3 className="small muted" style={{ margin: 'var(--space-3) 0 0' }}>Apps that skip your DNS</h3>
      <ToggleRow title="Tell browsers to use this DNS" desc="Stops browsers from silently switching to their own encrypted DNS." checked={v.canary} onChange={set('canary')} />
      <ToggleRow title="Block iCloud Private Relay" desc="Apple devices then tell their owner that Private Relay is off on this network, and it stops working here. Without this, Private Relay hides where those devices go from your logs and your blocks." checked={v.private_relay} onChange={set('private_relay')} />
      <div className="actions"><Button kind="primary" disabled={busy || !f} onClick={async () => { if (await run(() => put('/api/dns/settings', f), 'Saved.')) { setF(null); reload(); } }}>Save</Button></div>
    </Card>
  );
}

export default function DNS() {
  const [tab, setTab] = useHashTab('dns', ['overview','activity','servers','lists','records','settings'], 'overview');
  return (
    <Page title="DNS" sub="Translates names to addresses for every device, and blocks the ones you do not want.">
      <Tabs value={tab} onChange={setTab} tabs={[{ value: 'overview', label: 'Overview' }, { value: 'activity', label: 'Activity' }, { value: 'servers', label: 'Servers' }, { value: 'lists', label: 'Blocking' }, { value: 'records', label: 'Your names' }, { value: 'settings', label: 'Settings' }]} />
      {tab === 'overview' && <Overview />}
      {tab === 'activity' && <Activity />}
      {tab === 'servers' && <Servers />}
      {tab === 'lists' && <><Lists /><p className="small muted">Which devices get which lists is set per profile, on the <a href="#/devices/profiles">Profiles</a> tab of Devices & access. Blocking an app or a category for one device or at set hours is under <a href="#/protection">Protection</a>.</p></>}
      {tab === 'records' && <Records />}
      {tab === 'settings' && <SettingsTab />}
    </Page>
  );
}

import EncryptedDns from './EncryptedDns.jsx';
import React, { useState } from 'react';
import { del, get, patch, post, put, useData, useStream } from '../api.js';
import { go } from '../App.jsx';
import { Chart } from '../charts.jsx';
import { Button, Card, Empty, Field, Input, Modal, NumberInput, Note, Page, Pill, Segmented, Select, Spark, Trend, Tabs, ToggleRow, useAction, fmt, Bar, confirmBox } from '../ui.jsx';

function Overview() {
  const { data, setData } = useData('/api/dns/summary', { every: 10000 });
  useStream(['dns.stats'], (t, p) => p && setData((d) => (d ? { ...d, stats: p } : d)));
  const { data: ups, reload: ru } = useData('/api/dns/upstreams', { topics: ['dns.upstreams'], every: 10000 });
  const [self, setSelf] = useState(false);
  const { data: top } = useData(`/api/dns/top?hours=24${self ? '&self=1' : ''}`, { every: 30000 });
  const { data: force } = useData('/api/fw/dnsforce', { every: 60000 });
  const [range, setRange] = useState('live');
  const { data: h } = useData(`/api/overview/history?range=${range}`, { every: 30000, enabled: range !== 'live' });
  const [add, setAdd] = useState(false);
  const st = data?.stats;
  const total = st?.hour_total || 0;
  const outcomes = st ? [['Forwarded', st.outcomes?.forwarded], ['Blocked', st.outcomes?.blocked], ['Cached', st.outcomes?.cached], ['Local', st.outcomes?.local]] : [];
  const hist = st?.history || [];
  const peak = Math.max(0, ...hist);
  const list = ups || [];
  const working = list.filter((u) => u.state !== 'down').length;
  const slow = list.filter((u) => u.state === 'degraded').length;
  const upTone = !list.length ? '' : working === 0 ? 'bad' : working < list.length || slow ? 'warn' : 'good';
  const forcedOn = (force?.networks || []).filter((n) => n.mode !== 'off').map((n) => n.name);
  const series = [{ key: 'dns.qps', name: 'Queries per second', color: 'var(--color-chart-series-1)', pts: h?.series?.['dns.qps'] || [] }];
  return (
    <>
      {data && data.lists_enabled > 0 && !data.blocking_active && (
        <Note tone="warn" title="Blocking is not active yet">Your blocklists have not downloaded yet, so nothing is being blocked. Gatehouse keeps trying and starts blocking as soon as one arrives. The Blocklists tab shows how it is going.</Note>
      )}
      <section className="dns-tiles">
        <div className="card tile"><span className="lbl">Lookups, 24 hours</span><div><span className="val">{top ? fmt.n(top.total) : '–'}</span></div><span className="foot">{fmt.d1(st?.qps)} a second right now</span></div>
        <div className="card tile"><span className="lbl">Blocked</span><div><span className="val">{fmt.d1(st?.blocked_pct)}</span><span className="unit">%</span></div><span className="foot">{fmt.n(data?.unique_blocked ?? data?.blocked_entries)} different names on your lists</span></div>
        <div className="card tile"><span className="lbl">Answered from memory</span><div><span className="val">{fmt.d1(st?.cached_pct)}</span><span className="unit">%</span></div><span className="foot">{fmt.n(st?.cache_size)} saved answers</span></div>
        <div className="card tile"><span className="lbl">Response time</span><div><span className="val">{fmt.d1(st?.avg_ms)}</span><span className="unit">ms</span></div><span className="foot">95% under {fmt.d1(st?.p95_ms)} ms</span></div>
        <div className="card tile"><span className="lbl">Upstream servers</span><div><span className="val">{working}</span><span className="unit">of {list.length} working</span></div><span className="foot">{slow ? `${slow} slow` : upTone === 'bad' ? 'None answering' : 'All answering normally'}</span></div>
      </section>
      <Card title="Lookups over time" desc={range === 'live' ? 'Queries per second, last minute' : 'Queries per second, averaged over each period'}
        actions={<Segmented value={range} onChange={setRange} options={[{ value: 'live', label: 'Live' }, { value: '1h', label: '1 h' }, { value: '24h', label: '24 h' }, { value: '7d', label: '7 d' }]} />}>
        {range === 'live' ? (
          <>
            <div className="small muted" style={{ textAlign: 'right' }}>{peak > 0 ? `peak ${fmt.d1(peak)} a second` : ''}</div>
            <Trend values={hist} unit="/s" series="Queries per second" height={96} empty="No queries in the last minute" />
          </>
        ) : (
          <Chart series={series} from={h?.from || 0} to={h?.to || 1} unit="queries/s" height={160} label={`DNS queries per second, ${range}`} />
        )}
      </Card>
      <div className="dns-cards">
        <Card title="How queries were answered" desc="Last hour">
          {total === 0 ? <Empty>No queries yet.</Empty> : (
            <div className="stack">
              {outcomes.map(([k, v]) => (
                <div key={k} className="stack" style={{ gap: 'var(--space-1)' }}>
                  <div className="spread small"><span>{k}</span><span className="muted">{fmt.n(v || 0)} · {fmt.d1((100 * (v || 0)) / total)}%</span></div>
                  <Bar pct={(100 * (v || 0)) / total} thin />
                </div>
              ))}
            </div>
          )}
        </Card>
        <Card title="Busiest devices" desc="DNS queries, last 24 hours"
          actions={<button type="button" className="link small" onClick={() => setSelf((x) => !x)}>{self ? 'Hide this router' : 'Show this router'}</button>}>
          {!top?.clients?.length ? <Empty>No activity yet.</Empty> : (
            <div className="list">
              {top.clients.slice(0, 6).map((c) => { const self = c.addrs?.length === 1 && (c.addrs[0] === '127.0.0.1' || c.addrs[0] === '::1'); return <div className="item" key={c.key}><div><div className="name">{c.label || (c.addrs || [c.key])[0]}</div><div className="meta">{self ? 'This router, asking for itself' : (c.addrs || [c.key]).join(' · ')}</div></div><span>{fmt.n(c.n)}</span></div>; })}
            </div>
          )}
        </Card>
        <Card title="Most blocked" desc="Last 24 hours">
          {!top?.blocked?.length ? <Empty>Nothing has been blocked yet.</Empty> : (
            <div className="list">
              {top.blocked.slice(0, 6).map((c) => <div className="item" key={c.key}><span className="name" style={{ wordBreak: 'break-all' }}>{c.key}</span><span>{fmt.n(c.n)}</span></div>)}
            </div>
          )}
        </Card>
        <Card title="Most asked" desc="Names devices looked up most, last 24 hours">
          {!top?.domains?.length ? <Empty>No lookups yet.</Empty> : (
            <div className="list">
              {top.domains.slice(0, 6).map((c) => <div className="item" key={c.key}><span className="name" style={{ wordBreak: 'break-all' }}>{c.key}</span><span>{fmt.n(c.n)}</span></div>)}
            </div>
          )}
        </Card>
      </div>
      <Card title="Forced DNS" desc="Devices that use a DNS server of their own are sent to the one you chose.">
        <div className="spread">
          <span>{!force ? 'Loading…' : !force.master ? 'Off. Devices may use any DNS server.' : forcedOn.length ? `On for ${forcedOn.join(', ')}.${force.redirected ? ` ${fmt.n(force.redirected)} lookups redirected so far.` : ''}` : 'On, but no network is forced.'}</span>
          <button type="button" className="link small" onClick={() => go('firewall')}>Change →</button>
        </div>
      </Card>
      <EncryptedDns onChanged={ru} />
      <Upstreams ups={list} reload={ru} onAdd={() => setAdd(true)} />
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
    <div className="item" style={{ alignItems: 'flex-start' }}>
      <div style={{ display: 'flex', gap: '0.75rem', minWidth: 0 }}>
        {ordered && <span className="muted" style={{ fontVariantNumeric: 'tabular-nums', minWidth: '1.2rem', paddingTop: '0.1rem' }} aria-label={`Number ${num}`}>{num}</span>}
        <div style={{ minWidth: 0 }}>
          <div className="name">{u.name}</div>
          <div className="meta">{u.addr} · {u.proto.toUpperCase()}{u.reused ? ' · connection kept open' : ''}</div>
          {u.state !== 'healthy' && u.reason && <div className="small" style={{ color: u.state === 'down' ? 'var(--color-status-serious)' : 'var(--color-status-warning)', marginTop: '0.25rem' }}>{u.reason}{u.state === 'down' && u.retry_in ? ` Trying again in ${u.retry_in} s.` : ''}</div>}
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
    <Card title="Upstream DNS servers" desc="Where Gatehouse asks when it does not already know the answer."
      actions={<><Button small icon="plus" onClick={onAdd}>Add</Button><Button small onClick={() => run(() => post('/api/dns/cache/flush'), 'Memory cleared.')}>Clear saved answers</Button></>}>
      <Field label="Which server is asked first" hint={ordered ? 'The first server in the list is asked; the next one only if it does not answer. Use the arrows to change the order.' : 'Gatehouse measures how fast each one answers and asks the fastest first. Slower ones are the backup.'}>
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
  const total = `${fmt.n(l.entries + l.allowed)} in all${l.skipped ? `, ${fmt.n(l.skipped)} lines Gatehouse cannot use` : ''}`;
  if (l.status === 'downloading') return <div className="meta">Downloading…</div>;
  if (l.status === 'new') return <div className="meta">{l.enabled ? 'Waiting to download. Gatehouse does this by itself.' : 'Switched off'}</div>;
  if (l.status === 'error') {
    return (
      <div className="meta">
        <span style={{ color: 'var(--color-status-serious)' }}>{l.plain || "Couldn't download this list."}</span>
        {l.retry_at ? ` Gatehouse will try again at ${hhmm(l.retry_at)}.` : ''}
        {l.last_ok ? <> Using the copy from {fmt.ago(l.last_ok)} ({counts}).</> : ' Nothing is blocked from this list yet.'}
        {' '}<button className="linkbtn" onClick={() => setDetails(!details)}>{details ? 'Hide details' : 'Details'}</button>
        {details && <div className="small mono muted">{l.error}</div>}
      </div>
    );
  }
  return <div className="meta" title={total}>{counts} · {l.status === 'stale' ? <span style={{ color: 'var(--color-status-warning)' }}>not updated for a while ({fmt.ago(l.last_ok)})</span> : `updated ${fmt.ago(l.last_ok)}`}</div>;
}

function Lists() {
  const { data: lists, reload } = useData('/api/dns/lists', { topics: ['dns.lists'], every: 15000 });
  const { data: sum } = useData('/api/dns/summary', { every: 30000 });
  const { data: rules, reload: rr } = useData('/api/dns/rules', { topics: ['dns.lists'] });
  const [add, setAdd] = useState(false);
  const [run] = useAction();
  const [rule, setRule] = useState({ domain: '', action: 'block', note: '' });
  const [q, setQ] = useState('');
  const [why, setWhy] = useState(null);
  return (
    <>
      <Card title="Blocklists" desc="Lists of ad, tracker and malware names. Blocked names get no answer." actions={<Button small icon="plus" onClick={() => setAdd(true)}>Add list</Button>}>
        {sum?.unique_blocked > 0 && <p className="small muted">Different names blocked across all lists: <b>{fmt.n(sum.unique_blocked)}</b>. A name that is on two lists counts once.</p>}
        <div className="list">
          {(lists || []).map((l) => (
            <div className="item" key={l.id}>
              <div>
                <div className="name">{l.name} {l.local && <Pill>Yours</Pill>}</div>
                <ListMeta l={l} />
              </div>
              <div className="actions">
                {!l.local && <Button small icon="refresh" onClick={() => run(() => post(`/api/dns/lists/${l.id}/refresh`), 'Updated.').then(reload)}>Update</Button>}
                <Button small kind="danger" icon="trash" onClick={async () => await confirmBox(`Delete ${l.name}?`) && run(() => del(`/api/dns/lists/${l.id}`)).then(reload)}>Delete</Button>
                <button role="switch" aria-checked={l.enabled} aria-label={`Use ${l.name}`} className="switch" onClick={() => run(() => patch(`/api/dns/lists/${l.id}`, { enabled: !l.enabled })).then(reload)} />
              </div>
            </div>
          ))}
          {!lists?.length && <Empty>No lists yet.</Empty>}
        </div>
      </Card>

      <Card enter title="Always allow or always block" desc="Your own exceptions beat every list.">
        <div className="inline">
          <div style={{ flex: '1 1 14rem' }}><Input value={rule.domain} onChange={(v) => setRule({ ...rule, domain: v })} aria-label="Name to always allow or block" placeholder="example.com" /></div>
          <Select aria-label="Allow or block" value={rule.action} onChange={(v) => setRule({ ...rule, action: v })} options={[{ value: 'block', label: 'Block' }, { value: 'allow', label: 'Allow' }]} />
          <Button kind="primary" disabled={!rule.domain} onClick={() => run(() => post('/api/dns/rules', rule), 'Saved.').then(() => { setRule({ ...rule, domain: '' }); rr(); })}>Add</Button>
        </div>
        <div className="list">
          {(rules || []).map((r) => <div className="item" key={r.id}><div className="inline"><Pill tone={r.action === 'allow' ? 'good' : 'bad'}>{r.action}</Pill><span className="name">{r.domain}</span></div><Button small kind="danger" icon="trash" onClick={() => run(() => del(`/api/dns/rules/${r.id}`)).then(rr)}>Remove</Button></div>)}
        </div>
      </Card>

      <Card title="Why is this blocked?" desc="Type a name to see which list is responsible.">
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
    </>
  );
}

const SUGGESTED = [
  { name: 'StevenBlack unified hosts', url: 'https://raw.githubusercontent.com/StevenBlack/hosts/master/hosts', format: 'hosts' },
  { name: 'OISD small', url: 'https://small.oisd.nl/domainswild2', format: 'domains' },
];
function AddList({ onClose, onSaved }) {
  const [f, setF] = useState({ name: '', url: '', text: '', format: 'domains', interval_hours: 24 });
  const [mode, setMode] = useState('url');
  const [run, busy] = useAction();
  const set = (k) => (v) => setF((x) => ({ ...x, [k]: v }));
  return (
    <Modal title="Add a blocklist" onClose={onClose} footer={<><Button onClick={onClose}>Cancel</Button><Button kind="primary" disabled={busy || !f.name || (mode === 'url' ? !f.url : !f.text)} onClick={async () => { if (await run(() => post('/api/dns/lists', { ...f, url: mode === 'url' ? f.url : '', text: mode === 'text' ? f.text : '' }), 'Added. Downloading now.')) { onSaved(); onClose(); } }}>Add</Button></>}>
      <div className="inline">{SUGGESTED.map((s) => <Button small key={s.name} onClick={() => { setMode('url'); setF((x) => ({ ...x, name: s.name, url: s.url, format: s.format })); }}>{s.name}</Button>)}</div>
      <Field label="Name"><Input value={f.name} onChange={set('name')} /></Field>
      <div className="seg"><button className={mode === 'url' ? 'on' : ''} onClick={() => setMode('url')}>From a web address</button><button className={mode === 'text' ? 'on' : ''} onClick={() => setMode('text')}>Paste names</button></div>
      {mode === 'url' ? (
        <div className="formgrid">
          <Field label="Web address"><Input value={f.url} onChange={set('url')} placeholder="https://" /></Field>
          <Field label="Format"><Select value={f.format} onChange={set('format')} options={[{ value: 'domains', label: 'One name per line' }, { value: 'hosts', label: 'Hosts file' }, { value: 'adblock', label: 'Adblock style' }]} /></Field>
          <Field label="Refresh every (hours)"><NumberInput value={f.interval_hours} onChange={set('interval_hours')} /></Field>
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
  const nameBox = (
    <div className="stack" style={{ gap: 'var(--space-2)' }}>
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
    <Card enter title="Your own names" desc="Make a name point somewhere on your network, like nas.lan → 192.168.1.10. Devices that get an address from Gatehouse already have a name automatically.">
      {data && dset && isV4 && !mine && <Note title="Give this firewall a name">{nameBox}</Note>}
      {data && dset && isV4 && mine && !changing && (
        <p className="small muted">Open the console at <a href={`https://${mine.name}`}>https://{mine.name}</a>. Type the https:// part, or the browser will search the web instead.{' '}
          <Button small onClick={() => { setLabel(clean(mine.name)); setChanging(true); }}>Change</Button></p>
      )}
      {data && dset && isV4 && mine && changing && <Note title="Change this firewall's name">{nameBox}</Note>}
      <div className="ownrow">
        <Input value={f.name} onChange={(v) => setF({ ...f, name: v })} aria-label="Name" placeholder="nas.lan" />
        <Select aria-label="Record type" value={f.type} onChange={(v) => setF({ ...f, type: v })} options={['A', 'AAAA', 'CNAME', 'TXT', 'MX']} />
        <Input value={f.value} onChange={(v) => setF({ ...f, value: v })} aria-label="Points to" placeholder="192.168.1.10" />
        <Button kind="primary" disabled={!f.name || !f.value} onClick={() => run(() => post('/api/dns/records', f), 'Saved.').then(() => { setF({ ...f, name: '', value: '' }); reload(); })}>Add</Button>
      </div>
      <div className="list">
        {(data || []).map((r) => <div className="item" key={r.id}><div><span className="name">{r.name}</span> <span className="muted">{r.type} → {r.value}</span>{r.problem && <div className="small" style={{ color: 'var(--color-status-warning)' }}>{r.problem}</div>}</div><Button small kind="danger" icon="trash" onClick={() => run(() => del(`/api/dns/records/${r.id}`)).then(reload)}>Remove</Button></div>)}
        {!data?.length && <Empty>No custom names.</Empty>}
      </div>
    </Card>
  );
}

function Groups() {
  const { data: groups, reload } = useData('/api/dns/groups');
  const { data: lists } = useData('/api/dns/lists');
  const [edit, setEdit] = useState(null);
  const [run] = useAction();
  return (
    <Card title="Filtering profiles" desc="Choose which lists apply to which devices. Assign a device to a profile on the Devices page." actions={<Button small icon="plus" onClick={() => setEdit({ name: '', filtering: true, description: '', lists: [] })}>Add profile</Button>}>
      <div className="list">
        {(groups || []).map((g) => (
          <div className="item" key={g.id}>
            <div><div className="name">{g.name}</div><div className="meta">{g.filtering ? `${g.lists?.length || 0} lists` : 'No filtering'} · {g.devices} device{g.devices === 1 ? '' : 's'}{g.description ? ` · ${g.description}` : ''}</div></div>
            <div className="actions"><Button small icon="edit" onClick={() => setEdit(g)}>Edit</Button><Button small kind="danger" icon="trash" onClick={async () => await confirmBox(`Delete ${g.name}?`) && run(() => del(`/api/dns/groups/${g.id}`)).then(reload)}>Delete</Button></div>
          </div>
        ))}
      </div>
      {edit && (
        <Modal title={edit.id ? `Edit ${edit.name}` : 'New profile'} onClose={() => setEdit(null)} footer={<><Button onClick={() => setEdit(null)}>Cancel</Button><Button kind="primary" disabled={!edit.name} onClick={async () => { if (await run(() => put('/api/dns/groups', edit), 'Saved.')) { reload(); setEdit(null); } }}>Save</Button></>}>
          <Field label="Name"><Input value={edit.name} onChange={(v) => setEdit({ ...edit, name: v })} /></Field>
          <Field label="Note"><Input value={edit.description} onChange={(v) => setEdit({ ...edit, description: v })} /></Field>
          <ToggleRow title="Filter ads and trackers" checked={edit.filtering} onChange={(v) => setEdit({ ...edit, filtering: v })} />
          {edit.filtering && (lists || []).map((l) => (
            <ToggleRow key={l.id} title={l.name} desc={`${fmt.n(l.entries)} blocked`} checked={(edit.lists || []).includes(l.id)}
              onChange={(v) => setEdit({ ...edit, lists: v ? [...(edit.lists || []), l.id] : edit.lists.filter((x) => x !== l.id) })} />
          ))}
        </Modal>
      )}
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
    <Card title="DNS settings">
      <Field label="What to do with blocked names">
        <Select value={v.block_mode} onChange={set('block_mode')} options={[{ value: 'null', label: 'Answer with an empty address (fastest to fail)' }, { value: 'nxdomain', label: 'Say the name does not exist' }, { value: 'refused', label: 'Refuse' }]} />
      </Field>
      <div className="formgrid">
        <Field label="Home domain" hint="Names like printer.lan are answered by Gatehouse."><Input value={v.local_domain} onChange={set('local_domain')} /></Field>
        <Field label="Queries per second per device" hint="A device asking more than this is slowed down."><NumberInput value={v.rate_limit} onChange={set('rate_limit')} /></Field>
        <Field label="Short burst allowed"><NumberInput value={v.rate_burst} onChange={set('rate_burst')} /></Field>
        <Field label="Remember answers (entries)"><NumberInput value={v.cache_size} onChange={set('cache_size')} /></Field>
        <Field label="Keep the query history for (days)"><NumberInput value={v.log_retention_days} onChange={set('log_retention_days')} /></Field>
      </div>
      <ToggleRow title="Hide device addresses in the history" desc="Stores a masked address instead of the real one." checked={v.anonymize} onChange={set('anonymize')} />
      <ToggleRow title="Tell browsers to use this DNS" desc="Stops browsers from silently switching to their own encrypted DNS." checked={v.canary} onChange={set('canary')} />
      <div className="actions"><Button kind="primary" disabled={busy || !f} onClick={async () => { if (await run(() => put('/api/dns/settings', f), 'Saved.')) { setF(null); reload(); } }}>Save</Button></div>
    </Card>
  );
}

export default function DNS() {
  const [tab, setTab] = useState('overview');
  return (
    <Page title="DNS" sub="Translates names to addresses for every device, and blocks the ones you do not want.">
      <Tabs value={tab} onChange={setTab} tabs={[{ value: 'overview', label: 'Overview' }, { value: 'lists', label: 'Blocking' }, { value: 'records', label: 'Your names' }, { value: 'groups', label: 'Profiles' }, { value: 'settings', label: 'Settings' }]} />
      {tab === 'overview' && <Overview />}
      {tab === 'lists' && <Lists />}
      {tab === 'records' && <Records />}
      {tab === 'groups' && <Groups />}
      {tab === 'settings' && <SettingsTab />}
    </Page>
  );
}

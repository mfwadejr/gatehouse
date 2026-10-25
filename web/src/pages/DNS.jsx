import React, { useState } from 'react';
import { del, get, patch, post, put, useData, useStream } from '../api.js';
import { Button, Card, Empty, Field, Input, Modal, NumberInput, Note, Page, Pill, Segmented, Select, Spark, Trend, Tabs, ToggleRow, useAction, fmt, Bar, confirmBox } from '../ui.jsx';

function Overview() {
  const { data, setData } = useData('/api/dns/summary', { every: 10000 });
  useStream(['dns.stats'], (t, p) => p && setData((d) => (d ? { ...d, stats: p } : d)));
  const { data: ups, reload: ru } = useData('/api/dns/upstreams', { topics: ['dns.upstreams'], every: 10000 });
  const { data: top } = useData('/api/dns/top?hours=24', { every: 30000 });
  const [run] = useAction();
  const [add, setAdd] = useState(false);
  const st = data?.stats;
  const total = st?.hour_total || 0;
  const outcomes = st ? [['Forwarded', st.outcomes?.forwarded], ['Blocked', st.outcomes?.blocked], ['Cached', st.outcomes?.cached], ['Local', st.outcomes?.local]] : [];
  const hist = st?.history || [];
  const peak = Math.max(0, ...hist);
  return (
    <>
      <section className="grid">
        <div className="card tile trend">
          <span className="lbl">Queries per second</span>
          <div className="line"><span className="val">{fmt.d1(st?.qps)}</span><span className="small muted">{peak > 0 ? `peak ${fmt.d1(peak)} a second` : ''}</span></div>
          <Trend values={hist} unit="/s" series="Queries per second" height={72} empty="No queries in the last minute" />
        </div>
        <div className="card tile"><span className="lbl">Blocked</span><div><span className="val">{fmt.d1(st?.blocked_pct)}</span><span className="unit">%</span></div><span className="foot">{fmt.n(data?.blocked_entries)} names on your lists</span></div>
        <div className="card tile"><span className="lbl">Answered from memory</span><div><span className="val">{fmt.d1(st?.cached_pct)}</span><span className="unit">%</span></div><span className="foot">{fmt.n(st?.cache_size)} saved answers</span></div>
        <div className="card tile"><span className="lbl">Response time</span><div><span className="val">{fmt.d1(st?.avg_ms)}</span><span className="unit">ms</span></div><span className="foot">95% under {fmt.d1(st?.p95_ms)} ms</span></div>
      </section>
      <div className="row">
        <div className="grow">
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
        </div>
        <div className="grow">
          <Card title="Busiest devices" desc="DNS queries, last 24 hours">
            {!top?.clients?.length ? <Empty>No activity yet.</Empty> : (
              <div className="list">
                {top.clients.slice(0, 6).map((c) => <div className="item" key={c.key}><div><div className="name">{c.label || c.key}</div><div className="meta">{c.key}</div></div><span>{fmt.n(c.n)}</span></div>)}
              </div>
            )}
          </Card>
        </div>
        <div className="grow">
          <Card title="Most blocked" desc="Last 24 hours">
            {!top?.blocked?.length ? <Empty>Nothing has been blocked yet.</Empty> : (
              <div className="list">
                {top.blocked.slice(0, 6).map((c) => <div className="item" key={c.key}><span className="name" style={{ wordBreak: 'break-all' }}>{c.key}</span><span>{fmt.n(c.n)}</span></div>)}
              </div>
            )}
          </Card>
        </div>
      </div>
      <Upstreams ups={ups || []} reload={ru} onAdd={() => setAdd(true)} />
      {add && <AddUpstream onClose={() => setAdd(false)} onSaved={ru} />}
    </>
  );
}

// How a server is doing, in words a person can act on. Only every server being down is "No upstream".
function UpstreamRow({ u, first, last, ordered, onMove, onRemove, reload }) {
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
        {ordered && <span className="muted" style={{ fontVariantNumeric: 'tabular-nums', minWidth: '1.2rem', paddingTop: '0.1rem' }} aria-label={`Number ${u.position}`}>{u.position}</span>}
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
          <UpstreamRow key={u.id} u={u} first={i === 0} last={i === sorted.length - 1} ordered={ordered} onMove={(d) => move(i, d)} reload={reload}
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
function AddUpstream({ onClose, onSaved }) {
  const [f, setF] = useState({ name: '', address: '', proto: 'tls', tlsname: '' });
  const [run, busy] = useAction();
  const set = (k) => (v) => setF((x) => ({ ...x, [k]: v }));
  return (
    <Modal title="Add a DNS server" onClose={onClose} footer={<><Button onClick={onClose}>Cancel</Button><Button kind="primary" disabled={busy || !f.address} onClick={async () => { if (await run(() => post('/api/dns/upstreams', { ...f, name: f.name || f.address }), 'Added.')) { onSaved(); onClose(); } }}>Add</Button></>}>
      <div className="stack">
        <span className="small muted">Pick a well-known one, or fill in your own.</span>
        <div className="inline">{PRESETS.map((p) => <Button small key={p.label} onClick={() => setF({ name: p.label.split(' (')[0], address: p.address, proto: p.proto, tlsname: p.tlsname })}>{p.label}</Button>)}</div>
      </div>
      <div className="formgrid">
        <Field label="Name"><Input value={f.name} onChange={set('name')} /></Field>
        <Field label="Address and port" hint="Like 1.1.1.1:53"><Input value={f.address} onChange={set('address')} /></Field>
        <Field label="Kind"><Select value={f.proto} onChange={set('proto')} options={[{ value: 'tls', label: 'Encrypted (DNS-over-TLS)' }, { value: 'udp', label: 'Plain (UDP)' }, { value: 'tcp', label: 'Plain (TCP)' }]} /></Field>
        {f.proto === 'tls' && <Field label="Server name" hint="Used to check its certificate"><Input value={f.tlsname} onChange={set('tlsname')} /></Field>}
      </div>
    </Modal>
  );
}

function Lists() {
  const { data: lists, reload } = useData('/api/dns/lists', { topics: ['dns.lists'], every: 15000 });
  const { data: rules, reload: rr } = useData('/api/dns/rules', { topics: ['dns.lists'] });
  const [add, setAdd] = useState(false);
  const [run] = useAction();
  const [rule, setRule] = useState({ domain: '', action: 'block', note: '' });
  const [q, setQ] = useState('');
  const [why, setWhy] = useState(null);
  return (
    <>
      <Card title="Blocklists" desc="Lists of ad, tracker and malware names. Blocked names get no answer." actions={<Button small icon="plus" onClick={() => setAdd(true)}>Add list</Button>}>
        <div className="list">
          {(lists || []).map((l) => (
            <div className="item" key={l.id}>
              <div>
                <div className="name">{l.name} {l.local && <Pill>Yours</Pill>}</div>
                <div className="meta">{fmt.n(l.entries)} names · {l.status === 'ok' ? `updated ${fmt.ago(l.last_ok)}` : l.status === 'new' ? 'waiting for first download' : <span style={{ color: 'var(--color-status-serious)' }}>{l.status}: {l.error}</span>}</div>
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
  const [f, setF] = useState({ name: '', type: 'A', value: '', ttl: 300 });
  const [run] = useAction();
  return (
    <Card enter title="Your own names" desc="Make a name point somewhere on your network, like nas.lan → 192.168.1.10. Devices that get an address from Gatehouse already have a name automatically.">
      <div className="inline">
        <div style={{ flex: '1 1 10rem' }}><Input value={f.name} onChange={(v) => setF({ ...f, name: v })} aria-label="Name" placeholder="nas.lan" /></div>
        <Select aria-label="Record type" value={f.type} onChange={(v) => setF({ ...f, type: v })} options={['A', 'AAAA', 'CNAME', 'TXT', 'MX']} />
        <div style={{ flex: '1 1 10rem' }}><Input value={f.value} onChange={(v) => setF({ ...f, value: v })} aria-label="Points to" placeholder="192.168.1.10" /></div>
        <Button kind="primary" disabled={!f.name || !f.value} onClick={() => run(() => post('/api/dns/records', f), 'Saved.').then(() => { setF({ ...f, name: '', value: '' }); reload(); })}>Add</Button>
      </div>
      <div className="list">
        {(data || []).map((r) => <div className="item" key={r.id}><div><span className="name">{r.name}</span> <span className="muted">{r.type} → {r.value}</span></div><Button small kind="danger" icon="trash" onClick={() => run(() => del(`/api/dns/records/${r.id}`)).then(reload)}>Remove</Button></div>)}
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
            <ToggleRow key={l.id} title={l.name} desc={`${fmt.n(l.entries)} names`} checked={(edit.lists || []).includes(l.id)}
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

import { WhatChanged } from '../whatchanged.jsx';
import { useHashTab } from '../hashtab.js';
import React, { useState } from 'react';
import { del, get, holdAnd, patch, post, put, useData } from '../api.js';
import { usePeriod } from '../period.js';
import { Chart } from '../charts.jsx';
import { SAVED_LATER, SaveChoice } from '../changes.jsx';
import { Bar, Button, Card, Empty, Field, Input, Modal, NumberInput, Note, Page, Pill, Segmented, Select, Tabs, ToggleRow, useAction, fmt, usePortNames, Limited , useDelayedDelete } from '../ui.jsx';

const HOURS = [{ value: 3600, label: '1 hour' }, { value: 14400, label: '4 hours' }, { value: 43200, label: '12 hours' }, { value: 86400, label: '1 day' }, { value: 604800, label: '1 week' }];

export function ScopeForm({ initial, ifaces, onClose, onSaved }) {
  const pn = usePortNames();
  const [s, setS] = useState({ mode: 'serve', lease_seconds: 43200, domain: 'lan', enabled: true, dns: '', ntp: '', relay_addr: '', ...initial });
  const [run, busy] = useAction();
  const set = (k) => (v) => setS((x) => ({ ...x, [k]: v }));
  // Offer sensible values once the network is typed.
  const fill = () => {
    const m = /^(\d+)\.(\d+)\.(\d+)\.\d+\/(\d+)$/.exec(s.subnet || '');
    if (!m) return;
    const base = `${m[1]}.${m[2]}.${m[3]}`;
    setS((x) => ({ ...x, range_start: x.range_start || `${base}.100`, range_end: x.range_end || `${base}.200`, router: x.router || `${base}.1`, dns: x.dns || `${base}.1` }));
  };
  return (
    <Modal title={s.id ? `DHCP for ${s.name}` : `Set up DHCP for ${s.name || 'this network'}`} onClose={onClose}
      footer={<><Button onClick={onClose}>Cancel</Button><SaveChoice busy={busy} onSave={async (later) => { if (await run(() => (later ? holdAnd(() => put('/api/dhcp/scopes', s)) : put('/api/dhcp/scopes', s)), later ? SAVED_LATER : 'Saved.')) { onSaved(); onClose(); } }} /></>}>
      <div className="formgrid">
        <Field label="Name"><Input value={s.name} onChange={set('name')} /></Field>
        <Field label="Port or VLAN"><Select value={s.iface} onChange={set('iface')} options={[{ value: '', label: 'Choose…' }, ...ifaces.map((i) => ({ value: i.name, label: pn(i.name) }))]} /></Field>
      </div>
      <Field label="Who hands out addresses here?">
        <Select value={s.mode} onChange={set('mode')} options={[{ value: 'serve', label: 'NopeWarden' }, { value: 'relay', label: 'Another router, relayed to NopeWarden' }, { value: 'off', label: 'Another device (NopeWarden stays out of it)' }]} />
      </Field>
      <Field label="Network" hint="Like 192.168.1.0/24"><input className="in" value={s.subnet || ''} onChange={(e) => set('subnet')(e.target.value)} onBlur={fill} /></Field>
      {s.mode !== 'off' && (
        <div className="formgrid">
          <Field label="First address to give out"><Input value={s.range_start} onChange={set('range_start')} /></Field>
          <Field label="Last address to give out"><Input value={s.range_end} onChange={set('range_end')} /></Field>
          <Field label="Gateway" hint="Where devices send internet traffic"><Input value={s.router} onChange={set('router')} /></Field>
          <Field label="DNS server" hint="Usually this device"><Input value={s.dns} onChange={set('dns')} /></Field>
          <Field label="How long an address is kept"><Select value={String(s.lease_seconds)} onChange={(v) => set('lease_seconds')(Number(v))} options={HOURS.map((h) => ({ value: String(h.value), label: h.label }))} /></Field>
          <Field label="Home domain"><Input value={s.domain} onChange={set('domain')} /></Field>
        </div>
      )}
      {s.mode === 'relay' && <Field label="Relay address" hint="The router that forwards requests"><Input value={s.relay_addr} onChange={set('relay_addr')} /></Field>}
      <ToggleRow title="In use" checked={s.enabled} onChange={set('enabled')} />
    </Modal>
  );
}

function Scopes() {
  const delayedDelete = useDelayedDelete();
  const pn = usePortNames();
  const { data: scopes, reload } = useData('/api/dhcp/scopes', { topics: ['dhcp.leases'] });
  const { data: util } = useData('/api/dhcp/utilisation', { topics: ['dhcp.utilisation'], every: 10000 });
  const { data: ifs } = useData('/api/net/interfaces');
  const [edit, setEdit] = useState(null);
  const [checking, setChecking] = useState(0);
  const [result, setResult] = useState({});
  const [run] = useAction();
  const check = async (s) => {
    setChecking(s.id);
    const r = await run(() => post(`/api/dhcp/scopes/${s.id}/probe`));
    setChecking(0);
    if (r) setResult((x) => ({ ...x, [s.id]: r }));
  };
  return (
    <Card title="Address pools" desc="Each network gets its own pool of addresses. Add networks on the Networks page.">
      <div className="list">
        {(scopes || []).map((s) => {
          const u = (util || []).find((x) => x.scope_id === s.id);
          const pct = u && u.size ? (100 * u.active) / u.size : 0;
          return (
            <div className="item" key={s.id}>
              <div className="fill">
                <div className="name">{s.name} {!s.enabled && <Pill>Off</Pill>}{s.mode !== 'serve' && <Pill>{s.mode === 'relay' ? 'Relayed' : 'Another device'}</Pill>}</div>
                <div className="meta">{pn(s.iface)} · {s.subnet}{s.mode !== 'off' ? ` · ${s.range_start} – ${s.range_end}` : ''}</div>
                {result[s.id] && (
                  <Note tone={result[s.id].verdict === 'ok' ? 'good' : 'warn'} title={result[s.id].verdict === 'conflict' ? 'Another device is handing out addresses here' : result[s.id].verdict === 'none' ? 'Nothing is handing out addresses here' : 'No other servers found'}>
                    {result[s.id].verdict === 'conflict' && `Devices may get an address from ${result[s.id].found.map((f) => f.server).join(', ')} instead of NopeWarden. Turn that server's address handing off, or switch this network to "Another device".`}
                    {result[s.id].verdict === 'none' && 'This network is set to use another device for addresses, but none answered. New devices will not get an address.'}
                    {result[s.id].verdict === 'ok' && 'Only NopeWarden answered on this network.'}
                  </Note>
                )}
                {u && s.mode === 'serve' && u.size > 0 && u.active / u.size > 0.8 && (
                  <Note tone="warn" title={u.active >= u.size ? 'No addresses left' : 'Nearly out of addresses'}>
                    {u.active} of {u.size} are in use. {u.active >= u.size ? 'New devices cannot join until some are freed. ' : ''}Widen the range with Edit, or keep addresses for less time so unused ones come back sooner.
                  </Note>
                )}
                {u && s.mode !== 'off' && <div className="stack" style={{ gap: 'var(--space-1)', marginTop: 'var(--space-2)', maxWidth: '24rem' }}><Bar pct={pct} thin /><span className="small muted">{u.active} of {u.size} addresses in use</span></div>}
              </div>
              <div className="actions"><Button small icon="refresh" disabled={checking === s.id || s.mode === 'relay'} onClick={() => check(s)}>{checking === s.id ? 'Listening…' : 'Check for other servers'}</Button><Button small icon="edit" onClick={() => setEdit(s)}>Edit</Button><Button small kind="danger" icon="trash" onClick={() => delayedDelete(`The network ${s.name}`, () => del(`/api/dhcp/scopes/${s.id}`), reload, 'Its reserved addresses go with it.')}>Delete</Button></div>
            </div>
          );
        })}
        {!scopes?.length && <Empty>No networks yet. Add one on the Networks page and devices get addresses automatically.</Empty>}
      </div>
      {edit && <ScopeForm initial={edit} ifaces={ifs || []} onClose={() => setEdit(null)} onSaved={reload} />}
    </Card>
  );
}

function Leases() {
  const { data, reload } = useData('/api/dhcp/leases', { topics: ['dhcp.leases'], every: 10000 });
  const { data: scopes } = useData('/api/dhcp/scopes');
  const [run] = useAction();
  const [resv, setResv] = useState(null);
  return (
    <Card title="Devices with an address" desc="Reserve an address to give a device the same one every time.">
      <div className="scroll">
        <table className="table">
          <thead><tr><th>Name</th><th>Address</th><th>Device (MAC)</th><th>Network</th><th>Until</th><th /></tr></thead>
          <tbody>
            {<Limited items={(data || [])} noun="reservations" render={(l) => (
              <tr key={`${l.scope_id}-${l.ip}`}>
                <td>{l.hostname || <span className="muted">unnamed</span>}</td>
                <td className="mono">{l.ip}</td><td className="mono">{l.mac}</td><td>{l.scope}</td>
                <td>{l.static ? <Pill>Reserved</Pill> : fmt.dt(l.expires)}</td>
                <td className="r"><div className="actions endalign">
                  {!l.static && <Button small onClick={() => setResv({ scope_id: l.scope_id, mac: l.mac, ip: l.ip, hostname: l.hostname })}>Reserve</Button>}
                  <Button small kind="danger" onClick={() => run(() => del(`/api/dhcp/leases/${l.scope_id}/${l.ip}`), 'Released.').then(reload)}>Release</Button>
                </div></td>
              </tr>
            )} />}
          </tbody>
        </table>
        {!data?.length && <Empty>No devices have asked for an address yet.</Empty>}
      </div>
      {resv && <ResvForm initial={resv} scopes={scopes || []} onClose={() => setResv(null)} onSaved={reload} />}
    </Card>
  );
}

function ResvForm({ initial, scopes, onClose, onSaved }) {
  const [r, setR] = useState({ scope_id: scopes[0]?.id, mac: '', ip: '', hostname: '', ...initial });
  const [run, busy] = useAction();
  const set = (k) => (v) => setR((x) => ({ ...x, [k]: v }));
  return (
    <Modal title="Reserve an address" onClose={onClose} footer={<><Button onClick={onClose}>Cancel</Button><SaveChoice busy={busy} label="Reserve and apply" onSave={async (later) => { if (await run(() => (later ? holdAnd(() => post('/api/dhcp/reservations', r)) : post('/api/dhcp/reservations', r)), later ? SAVED_LATER : 'Reserved.')) { onSaved?.(); onClose(); } }} /></>}>
      <Field label="Network"><Select value={String(r.scope_id || '')} onChange={(v) => set('scope_id')(Number(v))} options={scopes.map((s) => ({ value: String(s.id), label: s.name }))} /></Field>
      <div className="formgrid">
        <Field label="Device address (MAC)" hint="Like aa:bb:cc:dd:ee:ff"><Input value={r.mac} onChange={set('mac')} /></Field>
        <Field label="Address to keep for it"><Input value={r.ip} onChange={set('ip')} /></Field>
        <Field label="Name"><Input value={r.hostname} onChange={set('hostname')} /></Field>
      </div>
    </Modal>
  );
}

function Reservations() {
  const { data, reload } = useData('/api/dhcp/reservations');
  const { data: scopes } = useData('/api/dhcp/scopes');
  const [add, setAdd] = useState(false);
  const [run] = useAction();
  return (
    <Card title="Reserved addresses" desc="Devices that always get the same address from DHCP, so you can find them or point rules at them." actions={<Button small icon="plus" onClick={() => setAdd(true)}>Add reservation</Button>}>
      <div className="list">
        {(data || []).map((r) => <div className="item" key={r.id}><div><div className="name">{r.hostname || r.mac}</div><div className="meta mono">{r.ip} · {r.mac}</div></div><Button small kind="danger" icon="trash" onClick={() => run(() => del(`/api/dhcp/reservations/${r.id}`)).then(reload)}>Remove</Button></div>)}
        {!data?.length && <Empty>No reservations.</Empty>}
      </div>
      {add && <ResvForm initial={{}} scopes={scopes || []} onClose={() => setAdd(false)} onSaved={reload} />}
    </Card>
  );
}

// Gives an unnamed device a name from the lease list. The device is found by its hardware address; one
// not in the device list yet is added.
function NameDevice({ lease, onClose, onSaved }) {
  const [name, setName] = useState('');
  const [run, busy] = useAction();
  const save = () => run(async () => {
    const rows = await get('/api/devices');
    const mac = String(lease.mac || '').toLowerCase();
    const found = (Array.isArray(rows) ? rows : rows?.devices || []).find((d) => String(d.mac || '').toLowerCase() === mac);
    if (found) return patch(`/api/devices/${found.id}`, { name: name.trim() });
    return post('/api/devices', { mac, name: name.trim() });
  }, 'Named.').then((okay) => { if (okay) { onSaved(); onClose(); } });
  return (
    <Modal title="Name this device" onClose={onClose} footer={<><Button onClick={onClose}>Cancel</Button><Button kind="primary" disabled={busy || !name.trim()} onClick={save}>Save name</Button></>}>
      <Field label="Name" hint={`${lease.ip} · ${lease.mac}`}><Input value={name} onChange={setName} autoFocus /></Field>
    </Modal>
  );
}

const ago = (t) => {
  if (!t) return '–';
  const m = Math.max(0, Math.round((Date.now() / 1000 - t) / 60));
  if (m < 1) return 'just now';
  if (m < 60) return `${m} min ago`;
  if (m < 1440) return `${Math.round(m / 60)} h ago`;
  return `${Math.round(m / 1440)} d ago`;
};

// The first tab: how it is going right now, before any setting. Pools in use, who joined lately.
function Overview({ goTab }) {
  const { data: scopes } = useData('/api/dhcp/scopes', { topics: ['dhcp.leases'] });
  const { data: util } = useData('/api/dhcp/utilisation', { topics: ['dhcp.utilisation', 'dhcp.leases'], every: 15000 });
  const { data: leases, reload: reloadLeases } = useData('/api/dhcp/leases', { topics: ['dhcp.leases'], every: 10000 });
  const { data: devs } = useData('/api/devices', { topics: ['dhcp.leases'], every: 15000 });
  const { data: resv } = useData('/api/dhcp/reservations', { topics: ['dhcp.leases'] });
  const [range, setRange, rangeOptions] = usePeriod('dhcp');
  const { data: h } = useData(`/api/overview/history?range=${range}`, { every: 30000 });
  const [naming, setNaming] = useState(null);
  const serving = (scopes || []).filter((x) => x.enabled && x.mode === 'serve');
  const rows = leases || [];
  const active = rows.length;
  const pools = (util || []).filter((u) => serving.some((x) => x.id === u.scope_id));
  const tight = pools.filter((u) => u.size > 0 && u.active / u.size > 0.8);
  const used = pools.reduce((n, u) => n + u.active, 0);
  const size = pools.reduce((n, u) => n + u.size, 0);
  const now = Date.now() / 1000;
  const soon = rows.filter((l) => !l.static && l.expires && l.expires - now < 3600 && l.expires > now).length;
  const recent = [...rows].filter((l) => !l.static).sort((a, b) => (b.start || 0) - (a.start || 0)).slice(0, 6);
  const link = (tab, label) => <button type="button" className="link" onClick={() => goTab(tab)}>{label}</button>;
  const known = {};
  for (const d of (Array.isArray(devs) ? devs : devs?.devices || [])) if (d.mac && d.name) known[String(d.mac).toLowerCase()] = d.name;
  const nameOf = (l) => l.hostname || known[String(l.mac || '').toLowerCase()] || '';
  const joined = (h?.series?.['devices.online'] || []);
  // Say plainly how far back the chart goes: it starts when this box began recording.
  const firstAt = joined.length ? joined[0][0] : 0;
  const shortBy = h && firstAt && firstAt > (h.from || 0) + (h.step || 60) * 3;
  const series = [{ key: 'devices.online', name: 'Devices online', color: 'var(--color-chart-series-1)', pts: joined }];
  return (
    <>
      {scopes && !scopes.length
        ? <Note>No network hands out addresses yet. Add one under {link('scopes', 'Address pools')}, and devices get addresses by themselves.</Note>
        : <Note tone={tight.length ? 'warn' : undefined} title={tight.length ? 'A pool is nearly full' : undefined}>
          {tight.length
            ? `${tight.map((u) => u.name).join(', ')} ${tight.length === 1 ? 'is' : 'are'} nearly out of addresses. Widen the range or keep addresses for less time under `
            : `NopeWarden hands out addresses on ${serving.length} ${serving.length === 1 ? 'network' : 'networks'}. ${active} ${active === 1 ? 'device has' : 'devices have'} an address right now. Change how it works under `}
          {link('scopes', 'Address pools')}.
        </Note>}
      <section className="dns-tiles">
        <button type="button" className="card tile tilebtn" onClick={() => goTab('scopes')}><span className="lbl">Addresses in use</span><div><span className="val">{used}</span><span className="unit">of {size}</span></div><span className="foot">{size ? `${Math.round((100 * used) / size)}% of all pools` : 'No pools yet'}</span></button>
        <button type="button" className="card tile tilebtn" onClick={() => goTab('leases')}><span className="lbl">Devices online</span><div><span className="val">{active}</span></div><span className="foot">with an address right now</span></button>
        <button type="button" className="card tile tilebtn" onClick={() => goTab('resv')}><span className="lbl">Reservations</span><div><span className="val">{(resv || []).length}</span></div><span className="foot">always get the same address</span></button>
        <button type="button" className="card tile tilebtn" onClick={() => goTab('leases')}><span className="lbl">Leases ending soon</span><div><span className="val">{soon}</span></div><span className="foot">{soon ? 'within the next hour' : 'none in the next hour'}</span></button>
      </section>
      <Card title="Devices over time" desc={shortBy ? `How many devices had an address. This box has only been recording since ${new Date(firstAt * 1000).toLocaleString([], { dateStyle: 'medium', timeStyle: 'short' })}, so the chart starts there.` : 'How many devices had an address, over the chosen period. It is recorded every 30 seconds and kept for as long as your chart history setting says.'} actions={<Segmented value={range} onChange={setRange} options={rangeOptions} />}>
        <Chart series={series} from={h?.from || 0} to={h?.to || 1} height={130} label="Devices with an address over time" empty="Collecting data. The first points appear within a minute." />
      </Card>
      <Card title="Address pools" desc="How full each pool is. A pool that fills up cannot give new devices an address.">
        <div className="list">
          {pools.map((u) => {
            const pct = u.size ? (100 * u.active) / u.size : 0;
            return (
              <div className="item" key={u.scope_id}>
                <div className="fill">
                  <div className="name">{u.name} {pct > 80 && <Pill tone="warn">Nearly full</Pill>}</div>
                  <div className="stack" style={{ gap: 'var(--space-1)', marginTop: 'var(--space-2)', maxWidth: '24rem' }}><Bar pct={pct} thin /><span className="small muted">{u.active} of {u.size} addresses in use</span></div>
                </div>
                <Button small onClick={() => goTab('scopes')}>Open</Button>
              </div>
            );
          })}
          {util && !pools.length && <Empty>No pool is handing out addresses.</Empty>}
        </div>
      </Card>
      <Card title="Most recently active" desc="The devices that asked for an address most lately." actions={<Button small onClick={() => goTab('leases')}>All devices</Button>}>
        <div className="scroll">
          <table className="table">
            <thead><tr><th>Name</th><th>Address</th><th>Network</th><th>Last seen</th><th>Lease ends</th></tr></thead>
            <tbody>
              {recent.map((l) => (
                <tr key={`${l.scope_id}-${l.ip}`}>
                  <td>{nameOf(l) || <><span className="muted">unnamed</span> <Button small onClick={() => setNaming(l)}>Name</Button></>}</td>
                  <td className="mono">{l.ip}</td><td>{l.scope}</td><td>{ago(l.start)}</td><td>{fmt.dt(l.expires)}</td>
                </tr>
              ))}
            </tbody>
          </table>
          {leases && !recent.length && <Empty>No devices have asked for an address yet.</Empty>}
        </div>
      </Card>
      <WhatChanged match={/dhcp|address|device|lease/i} range={range} />
      {naming && <NameDevice lease={naming} onClose={() => setNaming(null)} onSaved={reloadLeases} />}
    </>
  );
}

export default function DHCP() {
  const [tab, setTab] = useHashTab('dhcp', ['overview','scopes','leases','resv'], 'overview');
  const { data: rogue } = useData('/api/dhcp/alerts', { topics: ['dhcp.alerts'] });
  return (
    <Page title="DHCP" sub="Gives every device an address when it joins the network.">
      {(rogue || []).map((r) => (
        <Note key={r.scope} tone="bad" title={`Another DHCP server is answering on ${r.name}`}>
          {r.found.map((f) => f.server).join(', ')} hands out addresses here as well, so devices may get the wrong settings. Switch that device's DHCP off, or turn DHCP off for this network in NopeWarden.
        </Note>
      ))}
      <Tabs value={tab} onChange={setTab} tabs={[{ value: 'overview', label: 'Overview' }, { value: 'scopes', label: 'Address pools' }, { value: 'leases', label: 'Devices' }, { value: 'resv', label: 'Reservations' }]} />
      {tab === 'overview' && <Overview goTab={setTab} />}
      {tab === 'scopes' && <Scopes />}
      {tab === 'leases' && <Leases />}
      {tab === 'resv' && <Reservations />}
    </Page>
  );
}

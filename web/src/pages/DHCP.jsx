import React, { useState } from 'react';
import { del, post, put, useData } from '../api.js';
import { Bar, Button, Card, Empty, Field, Input, Modal, NumberInput, Note, Page, Pill, Select, Tabs, ToggleRow, useAction, fmt } from '../ui.jsx';

const HOURS = [{ value: 3600, label: '1 hour' }, { value: 14400, label: '4 hours' }, { value: 43200, label: '12 hours' }, { value: 86400, label: '1 day' }, { value: 604800, label: '1 week' }];

export function ScopeForm({ initial, ifaces, onClose, onSaved }) {
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
      footer={<><Button onClick={onClose}>Cancel</Button><Button kind="primary" disabled={busy} onClick={async () => { if (await run(() => put('/api/dhcp/scopes', s), 'Saved.')) { onSaved(); onClose(); } }}>Save</Button></>}>
      <div className="formgrid">
        <Field label="Name"><Input value={s.name} onChange={set('name')} /></Field>
        <Field label="Port or VLAN"><Select value={s.iface} onChange={set('iface')} options={[{ value: '', label: 'Choose…' }, ...ifaces.map((i) => i.name)]} /></Field>
      </div>
      <Field label="Who hands out addresses here?">
        <Select value={s.mode} onChange={set('mode')} options={[{ value: 'serve', label: 'Gatehouse' }, { value: 'relay', label: 'Another router, relayed to Gatehouse' }, { value: 'off', label: 'Another device (Gatehouse stays out of it)' }]} />
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
              <div style={{ flex: 1, minWidth: 0 }}>
                <div className="name">{s.name} {!s.enabled && <Pill>Off</Pill>}{s.mode !== 'serve' && <Pill>{s.mode === 'relay' ? 'Relayed' : 'Another device'}</Pill>}</div>
                <div className="meta">{s.iface} · {s.subnet}{s.mode !== 'off' ? ` · ${s.range_start} – ${s.range_end}` : ''}</div>
                {result[s.id] && (
                  <Note tone={result[s.id].verdict === 'ok' ? 'good' : 'warn'} title={result[s.id].verdict === 'conflict' ? 'Another device is handing out addresses here' : result[s.id].verdict === 'none' ? 'Nothing is handing out addresses here' : 'No other servers found'}>
                    {result[s.id].verdict === 'conflict' && `Devices may get an address from ${result[s.id].found.map((f) => f.server).join(', ')} instead of Gatehouse. Turn that server's address handing off, or switch this network to "Another device".`}
                    {result[s.id].verdict === 'none' && 'This network is set to use another device for addresses, but none answered. New devices will not get an address.'}
                    {result[s.id].verdict === 'ok' && 'Only Gatehouse answered on this network.'}
                  </Note>
                )}
                {u && s.mode !== 'off' && <div className="stack" style={{ gap: 'var(--space-1)', marginTop: 'var(--space-2)', maxWidth: '24rem' }}><Bar pct={pct} thin /><span className="small muted">{u.active} of {u.size} addresses in use</span></div>}
              </div>
              <div className="actions"><Button small icon="refresh" disabled={checking === s.id || s.mode === 'relay'} onClick={() => check(s)}>{checking === s.id ? 'Listening…' : 'Check for other servers'}</Button><Button small icon="edit" onClick={() => setEdit(s)}>Edit</Button><Button small kind="danger" icon="trash" onClick={() => window.confirm(`Delete ${s.name}?`) && run(() => del(`/api/dhcp/scopes/${s.id}`)).then(reload)}>Delete</Button></div>
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
            {(data || []).map((l) => (
              <tr key={`${l.scope_id}-${l.ip}`}>
                <td>{l.hostname || <span className="muted">unnamed</span>}</td>
                <td className="mono">{l.ip}</td><td className="mono">{l.mac}</td><td>{l.scope}</td>
                <td>{l.static ? <Pill>Reserved</Pill> : fmt.dt(l.expires)}</td>
                <td className="r"><div className="actions" style={{ justifyContent: 'flex-end' }}>
                  {!l.static && <Button small onClick={() => setResv({ scope_id: l.scope_id, mac: l.mac, ip: l.ip, hostname: l.hostname })}>Reserve</Button>}
                  <Button small kind="danger" onClick={() => run(() => del(`/api/dhcp/leases/${l.scope_id}/${l.ip}`), 'Released.').then(reload)}>Release</Button>
                </div></td>
              </tr>
            ))}
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
    <Modal title="Reserve an address" onClose={onClose} footer={<><Button onClick={onClose}>Cancel</Button><Button kind="primary" disabled={busy} onClick={async () => { if (await run(() => post('/api/dhcp/reservations', r), 'Reserved.')) { onSaved?.(); onClose(); } }}>Reserve</Button></>}>
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
    <Card title="Reserved addresses" actions={<Button small icon="plus" onClick={() => setAdd(true)}>Add reservation</Button>}>
      <div className="list">
        {(data || []).map((r) => <div className="item" key={r.id}><div><div className="name">{r.hostname || r.mac}</div><div className="meta mono">{r.ip} · {r.mac}</div></div><Button small kind="danger" icon="trash" onClick={() => run(() => del(`/api/dhcp/reservations/${r.id}`)).then(reload)}>Remove</Button></div>)}
        {!data?.length && <Empty>No reservations.</Empty>}
      </div>
      {add && <ResvForm initial={{}} scopes={scopes || []} onClose={() => setAdd(false)} onSaved={reload} />}
    </Card>
  );
}

export default function DHCP() {
  const [tab, setTab] = useState('scopes');
  const { data: rogue } = useData('/api/dhcp/alerts', { topics: ['dhcp.alerts'] });
  return (
    <Page title="DHCP" sub="Gives every device an address when it joins the network.">
      {(rogue || []).map((r) => (
        <Note key={r.scope} tone="bad" title={`Another DHCP server is answering on ${r.name}`}>
          {r.found.map((f) => f.server).join(', ')} hands out addresses here as well, so devices may get the wrong settings. Switch that device's DHCP off, or turn DHCP off for this network in Gatehouse.
        </Note>
      ))}
      <Tabs value={tab} onChange={setTab} tabs={[{ value: 'scopes', label: 'Address pools' }, { value: 'leases', label: 'Devices' }, { value: 'resv', label: 'Reservations' }]} />
      {tab === 'scopes' && <Scopes />}
      {tab === 'leases' && <Leases />}
      {tab === 'resv' && <Reservations />}
    </Page>
  );
}

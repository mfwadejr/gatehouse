import React, { useState } from 'react';
import { del, put, useData } from '../api.js';
import NatNotice from '../natnotice.jsx';
import { Button, Card, Empty, Field, Input, Note, Pill, Segmented, Select, ToggleRow, useAction, fmt } from '../ui.jsx';

// One card for UPnP: who may open ports, through which internet connection, and what is open now.
// Nothing is preselected. It cannot be switched on until both choices are made.

function DevicePicker({ listed, onAdd }) {
  const { data: ch } = useData('/api/vpn/choices');
  const [q, setQ] = useState('');
  const [mac, setMac] = useState('');
  const have = new Set(listed.map((d) => d.mac.toLowerCase()));
  const found = (ch?.devices || []).filter((d) => !have.has(d.mac.toLowerCase()) && (!q || `${d.name} ${d.ip} ${d.mac}`.toLowerCase().includes(q.toLowerCase())));
  return (
    <div className="stack">
      <Field label="Add a device" hint="Search by name, address or part of its hardware address (MAC).">
        <Input value={q} onChange={setQ} placeholder="Search devices" aria-label="Search devices" />
      </Field>
      <div className="list" style={{ maxHeight: '12rem', overflow: 'auto' }}>
        {found.slice(0, 40).map((d) => (
          <div className="item" key={d.mac}>
            <div><div className="name"><span className={`dot ${d.online ? '' : 'off'}`} /> {d.name}</div><div className="meta mono">{d.ip || 'not seen yet'} · {d.mac}</div></div>
            <div className="actions"><Button small onClick={() => onAdd({ mac: d.mac, note: d.name || '' })}>Allow</Button></div>
          </div>
        ))}
        {ch && !found.length && <Empty>{q ? 'No matching devices.' : 'Every known device is already listed.'}</Empty>}
      </div>
      <div className="addrow" style={{ gridTemplateColumns: 'minmax(0, 1fr) auto' }}>
        <Field label="Or type the address of a device that has not connected yet"><Input value={mac} onChange={setMac} placeholder="aa:bb:cc:dd:ee:ff" className="in mono" /></Field>
        <Button disabled={!mac.trim()} onClick={() => { onAdd({ mac: mac.trim(), note: '' }); setMac(''); }}>Add</Button>
      </div>
    </div>
  );
}

export default function UPnPCard() {
  const { data, reload } = useData('/api/upnp/card', { topics: ['upnp.maps'], every: 10000 });
  const [run, busy] = useAction();
  const [more, setMore] = useState(false);
  const { data: ch } = useData('/api/vpn/choices');
  if (!data) return null;
  const known = new Map((ch?.devices || []).map((d) => [d.mac.toLowerCase(), d]));
  const st = data.status;
  const base = {
    on: data.on, mode: data.mode, max_lease_hours: st.max_lease_hours,
    networks: data.networks.filter((n) => n.ticked).map((n) => n.id),
    connections: data.connections.filter((c) => c.ticked).map((c) => c.name),
    devices: data.devices,
  };
  const save = (patch, msg) => run(() => put('/api/upnp/card', { ...base, ...patch }), msg).then(reload);
  const tickNet = (id, v) => save({ networks: v ? [...base.networks, id] : base.networks.filter((x) => x !== id), on: data.on && (v || base.networks.length > 1 || data.mode === 'devices') });
  const tickConn = (name, v) => save({ connections: v ? [...base.connections, name] : base.connections.filter((x) => x !== name), on: data.on && (v || base.connections.length > 1) });
  const missing = data.missing || [];
  return (
    <>
      <NatNotice compact />
      <Card title="Let devices open ports by themselves (UPnP)" desc="A game console or media app asks for a port and Gatehouse opens it for that device only, for a limited time. For a known app such as Plex, a port forward above is more reliable.">
        <div className="stack">
          <Field label="How do you want to allow it?">
            <Segmented value={data.mode} onChange={(v) => save({ mode: v, on: false }, undefined)} options={[{ value: 'devices', label: 'Specific devices (safer)' }, { value: 'network', label: 'A whole network (less safe)' }]} />
          </Field>
          {data.mode === 'devices' ? (
            <>
              <p className="small muted">Only the devices you list can open ports, wherever they are on your networks. Everything else is refused.</p>
              <div className="list">
                {data.devices.map((a) => {
                  const d = known.get(a.mac.toLowerCase());
                  return (
                    <div className="item" key={a.mac}>
                      <div><div className="name">{d?.name || a.note || a.mac}</div><div className="meta mono">{a.mac}{d?.ip ? ` · ${d.ip}` : ''}</div></div>
                      <div className="actions"><Button small kind="danger" icon="trash" disabled={busy} onClick={() => save({ devices: data.devices.filter((x) => x.mac !== a.mac), on: data.on && data.devices.length > 1 })}>Remove</Button></div>
                    </div>
                  );
                })}
                {!data.devices.length && <Empty>No devices yet. Add the ones that need it.</Empty>}
              </div>
              <DevicePicker listed={data.devices} onAdd={(d) => save({ devices: [...data.devices, d] })} />
            </>
          ) : (
            <>
              <Note tone="warn">Every device on a ticked network can ask for ports, including ones you have not looked at. A ticked network also covers all of its devices.</Note>
              {data.networks.length === 0 ? <Empty>No networks yet.</Empty> : data.networks.map((n) => (
                <label key={n.id} className="small" style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
                  <input type="checkbox" checked={n.ticked} disabled={busy} onChange={(e) => tickNet(n.id, e.target.checked)} /> {n.name}
                </label>
              ))}
            </>
          )}
          <Field label="Which internet connection?" hint="Ports are opened on the ticked connections only.">
            <div className="stack">
              {data.connections.length === 0 ? <Empty>No internet connection yet.</Empty> : data.connections.map((c) => (
                <label key={c.name} className="small" style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
                  <input type="checkbox" checked={c.ticked} disabled={busy} onChange={(e) => tickConn(c.name, e.target.checked)} /> {c.name}
                </label>
              ))}
            </div>
          </Field>
          <p className="small muted">Safety: a device can only open ports to itself. Ports below 1024 and the ones Gatehouse itself uses are never opened.</p>
          <ToggleRow title="UPnP" desc={data.on ? data.line : missing.length ? `Not on yet. ${missing.join(' ')}` : 'Ready. Switch it on.'}
            checked={data.on} disabled={busy || (!data.on && missing.length > 0)} onChange={(v) => save({ on: v }, v ? 'UPnP is set up. Apply the firewall to start using it.' : 'UPnP is off. Apply the firewall to finish.')} />
        </div>
      </Card>
      {data.on && (
        <Card title="Open now" desc="Ports that devices asked for.">
          <div className="list">
            {st.mappings.map((m) => (
              <div className="item" key={m.id}>
                <div><div className="name">{m.proto.toUpperCase()} port {m.ext_port} → {m.int_addr}:{m.int_port}</div><div className="meta">{m.desc || 'No description'} · closes {fmt.dt(Math.floor(new Date(m.expires).getTime() / 1000))}</div></div>
                <div className="actions"><Button small kind="danger" icon="trash" disabled={busy} onClick={() => run(() => del(`/api/upnp/mappings/${m.id}`)).then(reload)}>Close</Button></div>
              </div>
            ))}
            {!st.mappings.length && <Empty>No ports are open right now.</Empty>}
          </div>
          {st.mappings.length > 0 && <Button small kind="danger" disabled={busy} onClick={() => run(() => del('/api/upnp/mappings')).then(reload)}>Close all</Button>}
        </Card>
      )}
      <details className="more" open={more} onToggle={(e) => setMore(e.currentTarget.open)}>
        <summary>Advanced: how long ports stay open, and recent requests</summary>
        <div className="stack">
          <Field label="Longest a port stays open" hint="Devices renew what they still need.">
            <Select value={String(st.max_lease_hours)} onChange={(v) => save({ max_lease_hours: Number(v) })} options={[{ value: '1', label: '1 hour' }, { value: '6', label: '6 hours' }, { value: '24', label: '1 day' }, { value: '168', label: '7 days' }]} />
          </Field>
          <div className="scroll"><table className="table">
            <thead><tr><th>When</th><th>What</th></tr></thead>
            <tbody>{[...st.log].reverse().slice(0, 50).map((l, i) => <tr key={i}><td>{fmt.ago(l.at)}</td><td>{l.kind === 'deny' ? <Pill tone="warn">Refused</Pill> : null} {l.msg}</td></tr>)}</tbody></table>
            {!st.log.length && <Empty>Nothing yet.</Empty>}
          </div>
        </div>
      </details>
    </>
  );
}

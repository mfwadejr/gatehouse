import React, { useState } from 'react';
import { del, post, put, useData } from '../api.js';
import { CODES, countryName } from '../AddrPicker.jsx';
import { Button, Card, Empty, Field, Input, Modal, Note, Pill, Select, ToggleRow, useAction, useToast, confirmBox } from '../ui.jsx';

// Open a port for an app on a device. Pick the app, pick the device, tick the connection. The
// device is followed by its hardware address, so the forward keeps working if its address changes.

const COUNTRIES = CODES.map((c) => ({ value: c, label: `${countryName(c)} (${c})` })).sort((a, b) => a.label.localeCompare(b.label));

function NewApp({ onSaved, onCancel }) {
  const [name, setName] = useState('');
  const [ports, setPorts] = useState('');
  const [run, busy] = useAction();
  const save = async () => {
    const list = ports.split(/[\s,]+/).filter(Boolean);
    const ok = await run(() => put('/api/fw/portlists', { name: name.trim(), ports: list }));
    if (ok) onSaved(name.trim());
  };
  return (
    <div className="stack" style={{ padding: 12, border: '1px solid var(--color-border, rgba(127,127,127,.3))', borderRadius: 8 }}>
      <strong>Add a new app</strong>
      <Field label="Name"><Input value={name} onChange={setName} placeholder="Game server" /></Field>
      <Field label="Ports" hint="Protocol and port, separated by commas. For example tcp/32400 or udp/27000-27050. Ports below 1024 are fine here but expose more, so use them only if you must.">
        <Input value={ports} onChange={setPorts} className="in mono" placeholder="tcp/32400, udp/32410-32414" />
      </Field>
      <div className="actions"><Button onClick={onCancel}>Cancel</Button><Button kind="primary" onClick={save} disabled={busy || !name.trim() || !ports.trim()}>Save app</Button></div>
    </div>
  );
}

function ForwardForm({ initial, apps, connections, onClose, onSaved, reloadApps }) {
  const [f, setF] = useState({ enabled: true, connections: [], src: [], countries: [], mac: '', ...initial });
  const [adding, setAdding] = useState(false);
  const [q, setQ] = useState('');
  const [srcText, setSrcText] = useState((initial?.src || []).join(', '));
  const [run, busy] = useAction();
  const { data: ch } = useData('/api/vpn/choices');
  const devices = ch?.devices || [];
  const picked = devices.find((d) => d.mac.toLowerCase() === (f.mac || '').toLowerCase());
  const found = devices.filter((d) => !q || `${d.name} ${d.ip} ${d.mac}`.toLowerCase().includes(q.toLowerCase()));
  const app = apps.find((a) => a.name === f.app);
  const tick = (n, v) => setF((x) => ({ ...x, connections: v ? [...x.connections, n] : x.connections.filter((c) => c !== n) }));
  const ready = f.app && f.mac && f.connections.length > 0;
  const save = async () => {
    const src = srcText.split(/[\s,]+/).filter(Boolean);
    const ok = await run(() => put('/api/fw/appforwards', { ...f, src, group: initial?.group || '' }), 'Saved. Apply the firewall to start using it.');
    if (ok) onSaved();
  };
  return (
    <Modal title={initial?.group ? `Edit ${initial.group}` : 'Open a port for an app'} onClose={onClose}
      footer={<><Button onClick={onClose}>Cancel</Button><Button kind="primary" onClick={save} disabled={busy || !ready}>Save</Button></>}>
      <div className="stack">
        <Field label="App" hint={app ? `Opens ${app.ports.join(', ')}.` : 'Choose what you want to reach from outside.'}>
          <Select value={f.app || ''} onChange={(v) => v === '__new' ? setAdding(true) : setF({ ...f, app: v })}
            options={[{ value: '', label: 'Choose an app…' }, ...apps.map((a) => ({ value: a.name, label: a.custom ? `${a.name} (yours)` : a.name })), { value: '__new', label: 'Add a new app…' }]} />
        </Field>
        {adding && <NewApp onCancel={() => setAdding(false)} onSaved={async (n) => { await reloadApps(); setF((x) => ({ ...x, app: n })); setAdding(false); }} />}
        <Field label="Device" hint="Gatehouse follows the device by its hardware address, so this keeps working if its address changes.">
          {picked || f.mac ? (
            <div className="item"><div><div className="name">{picked?.name || initial?.device_name || f.mac}</div><div className="meta mono">{picked?.ip || initial?.ip || ''} · {f.mac}</div></div><div className="actions"><Button small onClick={() => setF({ ...f, mac: '' })}>Change</Button></div></div>
          ) : (
            <div className="stack">
              <Input value={q} onChange={setQ} placeholder="Search by name, address or MAC" aria-label="Search devices" />
              <div className="list" style={{ maxHeight: '11rem', overflow: 'auto' }}>
                {found.slice(0, 40).map((d) => (
                  <div className="item" key={d.mac}>
                    <div><div className="name"><span className={`dot ${d.online ? '' : 'off'}`} /> {d.name}</div><div className="meta mono">{d.ip || 'not seen yet'} · {d.mac}</div></div>
                    <div className="actions"><Button small onClick={() => setF({ ...f, mac: d.mac })}>Choose</Button></div>
                  </div>
                ))}
                {ch && !found.length && <Empty>No matching devices.</Empty>}
              </div>
            </div>
          )}
        </Field>
        <Field label="Through which internet connection?" hint="Tick every connection this should work on.">
          <div className="stack">
            {connections.length === 0 ? <span className="small muted">No internet connection yet.</span> : connections.map((n) => (
              <label key={n} className="small" style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
                <input type="checkbox" checked={f.connections.includes(n)} onChange={(e) => tick(n, e.target.checked)} /> {n}
              </label>
            ))}
          </div>
        </Field>
        <details className="more">
          <summary>Limits (optional): only some people may connect</summary>
          <div className="stack">
            <p className="small muted">With no limits, anyone on the internet can try to connect. Limiting it to a country or a set of addresses is safer.</p>
            <Field label="Only from these countries">
              <div className="stack">
                <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}>
                  {f.countries.map((c) => <Pill key={c}>{countryName(c)} <button type="button" className="linkbtn" aria-label={`Remove ${countryName(c)}`} onClick={() => setF({ ...f, countries: f.countries.filter((x) => x !== c) })}>×</button></Pill>)}
                </div>
                <Select value="" onChange={(v) => v && !f.countries.includes(v) && setF({ ...f, countries: [...f.countries, v] })} options={[{ value: '', label: 'Add a country…' }, ...COUNTRIES]} />
              </div>
            </Field>
            <Field label="Or only from these addresses" hint="Addresses or networks separated by commas, such as 203.0.113.7 or 198.51.100.0/24.">
              <Input value={srcText} onChange={setSrcText} className="in mono" />
            </Field>
          </div>
        </details>
        <Field label="Name" hint="Optional. Leave empty to use the app's name."><Input value={f.name ?? initial?.group ?? ''} onChange={(v) => setF({ ...f, name: v })} /></Field>
        <ToggleRow title="Turned on" checked={f.enabled} onChange={(v) => setF({ ...f, enabled: v })} />
      </div>
    </Modal>
  );
}

// Forwards made before apps existed keep working and are edited the old way.
function LegacyForm({ f, wans, onClose, onSaved }) {
  const [e, setE] = useState({ ...f });
  const [run, busy] = useAction();
  return (
    <Modal title="Edit port" onClose={onClose} footer={<><Button onClick={onClose}>Cancel</Button><Button kind="primary" disabled={busy || !e.name} onClick={async () => { if (await run(() => put('/api/fw/forwards', e), 'Saved. Apply it to start using it.')) onSaved(); }}>Save</Button></>}>
      <Field label="Name"><Input value={e.name} onChange={(v) => setE({ ...e, name: v })} /></Field>
      <div className="formgrid">
        <Field label="Kind"><Select value={e.proto} onChange={(v) => setE({ ...e, proto: v })} options={['tcp', 'udp']} /></Field>
        <Field label="Port from the internet"><Input value={e.ext_port} onChange={(v) => setE({ ...e, ext_port: v })} /></Field>
        <Field label="Device inside"><Input value={e.int_addr} onChange={(v) => setE({ ...e, int_addr: v })} /></Field>
        <Field label="Port on the device" hint="Empty to keep the same"><Input value={e.int_port} onChange={(v) => setE({ ...e, int_port: v })} /></Field>
        <Field label="Which connection"><Select value={e.wan || ''} onChange={(v) => setE({ ...e, wan: v })} options={[{ value: '', label: 'All' }, ...(wans || []).map((w) => w.name)]} /></Field>
      </div>
      <ToggleRow title="Turned on" checked={e.enabled} onChange={(v) => setE({ ...e, enabled: v })} />
    </Modal>
  );
}

export default function Forwards({ reload }) {
  const { data, reload: rf } = useData('/api/fw/appforwards');
  const { data: legacy, reload: rl } = useData('/api/fw/forwards');
  const { data: wans } = useData('/api/net/wans');
  const [edit, setEdit] = useState(null);
  const [old, setOld] = useState(null);
  const [run, busy] = useAction();
  const toast = useToast();
  const refresh = () => { rf(); rl(); reload?.(); };
  const items = data?.forwards || [];
  const fix = (g) => run(() => post('/api/dhcp/reservations', { scope_id: g.reserve_scope, mac: g.mac, ip: g.ip, hostname: g.device_name }), `${g.device_name} will keep the address ${g.ip}.`).then(rf);
  return (
    <Card title="Open a port to the internet" desc="Lets something outside reach an app on one of your devices, like Plex or a game server. Only open what you need." actions={<Button small icon="plus" onClick={() => setEdit({})}>Open a port</Button>}>
      <div className="list">
        {items.map((g) => (
          <div className="item" key={`${g.group}-${g.id || ''}`}>
            <div style={{ flex: 1, minWidth: 0 }}>
              <div className="name">{g.text} {!g.enabled && <Pill>Off</Pill>}</div>
              <div className="meta">{g.countries.length > 0 ? `Only from ${g.countries.map(countryName).join(', ')}. ` : ''}{g.src.length > 0 ? `Only from ${g.src.join(', ')}. ` : ''}{!g.countries.length && !g.src.length ? 'Anyone on the internet can try to connect.' : ''}</div>
              {g.blocked && <div className="small" style={{ color: 'var(--color-status-serious)' }}>{g.blocked}</div>}
              {!g.legacy && g.mac && !g.reserved && g.reserve_scope > 0 && <div className="small">If {g.device_name} gets a different address, this follows it. <button type="button" className="link small" disabled={busy} onClick={() => fix(g)}>Give it a fixed address</button> so it never changes.</div>}
            </div>
            <div className="actions">
              {!g.legacy && <Button small disabled={busy} onClick={() => run(() => post('/api/fw/appforwards/enable', { group: g.group, enabled: !g.enabled }), 'Saved. Apply the firewall to start using it.').then(refresh)}>{g.enabled ? 'Turn off' : 'Turn on'}</Button>}
              <Button small icon="edit" onClick={() => g.legacy ? setOld((legacy || []).find((l) => l.id === g.id)) : setEdit({ group: g.group, name: g.group, app: g.app, mac: g.mac, device_name: g.device_name, ip: g.ip, connections: g.connections, src: g.src, countries: g.countries, enabled: g.enabled })}>Edit</Button>
              <Button small kind="danger" icon="trash" onClick={async () => await confirmBox(`Delete "${g.group}"?`) && run(() => g.legacy ? del(`/api/fw/forwards/${g.id}`) : del(`/api/fw/appforwards/${encodeURIComponent(g.group)}`)).then(refresh)}>Delete</Button>
            </div>
          </div>
        ))}
        {data && !items.length && <Empty>No ports are open to the internet.</Empty>}
      </div>
      {data && data.connections.length === 0 && <Note tone="warn">Add an internet connection first, on the Internet page.</Note>}
      {edit && <ForwardForm initial={edit} apps={data?.apps || []} connections={data?.connections || []} onClose={() => setEdit(null)} onSaved={() => { setEdit(null); refresh(); }} reloadApps={rf} />}
      {old && <LegacyForm f={old} wans={wans?.wans} onClose={() => setOld(null)} onSaved={() => { setOld(null); refresh(); }} />}
    </Card>
  );
}

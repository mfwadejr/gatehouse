import React, { useEffect, useState } from 'react';
import { hashQuery, setHashQuery, useWantedEdit } from '../hashtab.js';
import { del, get, holdAnd, post, put, useData } from '../api.js';
import { SAVED_LATER, SaveChoice } from '../changes.jsx';
import { CODES, countryName } from '../AddrPicker.jsx';
import { PolicyControls, WhenField, whenText } from '../policykit.jsx';
import { Button, Card, Empty, Field, Input, Modal, Note, Pill, Select, ToggleRow, useAction, useToast, Limited , useDelayedDelete } from '../ui.jsx';

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
    <div className="stack" style={{ padding: 'var(--space-3)', border: 'var(--border-width-thin) solid var(--color-border-default)', borderRadius: 'var(--radius-md)' }}>
      <strong>Add a new app</strong>
      <Field label="Name"><Input value={name} onChange={setName} placeholder="Game server" /></Field>
      <Field label="Ports" hint="Protocol and port, separated by commas. For example tcp/32400 or udp/27000-27050. Ports below 1024 are fine here but expose more, so use them only if you must.">
        <Input value={ports} onChange={setPorts} className="in mono" placeholder="tcp/32400, udp/32410-32414" />
      </Field>
      <div className="actions"><Button onClick={onCancel}>Cancel</Button><Button kind="primary" onClick={save} disabled={busy || !name.trim() || !ports.trim()}>Save app</Button></div>
    </div>
  );
}

const keyOf = (p) => `${p.proto}/${p.std || p.port}`;

// Open a port: tap the app, tap the device, done. Nothing is chosen for you except the app's
// usual port, and the screen says so and lets you change it.
export function OpenPortModal({ initial, preset, apps, connections, existing, onClose, onSaved, reloadApps }) {
  const editing = !!initial?.group;
  const [f, setF] = useState(() => {
    const o = {}; const i = {};
    (initial?.ports || []).forEach((p) => { o[keyOf(p)] = p.port; i[keyOf(p)] = p.inside || p.std || p.port; });
    return { enabled: true, connections: [], src: [], countries: [], mac: '', sched: null, outside: o, inside: i, ...preset, ...initial };
  });
  const [adding, setAdding] = useState(false);
  const [more, setMore] = useState(editing);
  const [q, setQ] = useState('');
  const [srcText, setSrcText] = useState((initial?.src || []).join(', '));
  const [result, setResult] = useState(null);
  const [clash, setClash] = useState(null);
  const [typed, setTyped] = useState('');
  const [typedUse, setTypedUse] = useState('');
  const [typedNotes, setTypedNotes] = useState([]);
  const [run, busy] = useAction();
  const toast = useToast();
  const { data: ch } = useData('/api/vpn/choices');
  const devices = ch?.devices || [];
  const picked = devices.find((d) => d.mac.toLowerCase() === (f.mac || '').toLowerCase());
  const devName = picked?.name || f.device_name || preset?.device_name || f.mac;
  const found = devices.filter((d) => !q || `${d.name} ${d.ip} ${d.mac}`.toLowerCase().includes(q.toLowerCase()));
  const app = apps.find((a) => a.name === f.app);
  const tick = (n, v) => setF((x) => ({ ...x, connections: v ? [...x.connections, n] : x.connections.filter((c) => c !== n) }));
  const tried = React.useRef(false);

  const groupName = () => {
    if (editing) return initial.group;
    const nm = (f.name || '').trim();
    if (nm) return nm;
    const taken = new Set((existing || []).map((g) => g.group.toLowerCase()));
    if (!taken.has(f.app.toLowerCase())) return f.app;
    return `${f.app} (${devName})`.slice(0, 24);
  };

  const submit = async (over) => {
    const body = { ...f, ...over, src: srcText.split(/[\s,]+/).filter(Boolean), group: initial?.group || '', name: groupName() };
    if (!body.connections.length && connections.length) body.connections = [connections[0]];
    setClash(null);
    let saved = null;
    try {
      saved = await put('/api/fw/appforwards', body);
    } catch (e) {
      if (e.status === 409 && e.data?.conflict) { setClash(e.data.conflict); setTyped(''); setTypedUse(''); return false; }
      toast(e.message || 'Something went wrong.', 'bad');
      return false;
    }
    const r = await get('/api/fw/appforwards');
    const g = (r?.forwards || []).find((x) => x.group === body.name);
    setResult({ ...(g || { group: body.name, ports: [], device_name: devName, app: f.app }), notes: saved?.notes || [] });
    onSaved?.();
    return true;
  };

  // With the app and device both chosen, open it straight away unless More options is open.
  React.useEffect(() => {
    if (editing || more || tried.current || !f.app || !f.mac || !connections.length) return;
    tried.current = true;
    submit({});
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [f.app, f.mac, more]);

  // Live check of a port as it is typed.
  React.useEffect(() => {
    if (!clash || !typed) { setTypedUse(''); setTypedNotes([]); return undefined; }
    const t = setTimeout(async () => {
      try { const r = await get(`/api/fw/portcheck?proto=${clash.proto}&port=${encodeURIComponent(typed)}&group=${encodeURIComponent(initial?.group || '')}`); setTypedUse(r?.error || (r?.used_by ? `${typed} is already used by ${r.used_by}.` : '')); setTypedNotes(r?.notes || []); } catch { setTypedUse(''); }
    }, 300);
    return () => clearTimeout(t);
  }, [typed, clash, initial?.group]);

  const suggest = async () => {
    try { const r = await get(`/api/fw/portcheck?proto=${clash.proto}&port=${encodeURIComponent(clash.port)}&suggest=1`); if (r?.suggest) setTyped(r.suggest); } catch { /* leave it empty */ }
  };
  const useTyped = () => submit({ outside: { ...f.outside, [`${clash.proto}/${clash.port}`]: typed.trim() } });

  const setPort = (kind, k, v) => setF((x) => ({ ...x, [kind]: { ...x[kind], [k]: v } }));
  const ports = editing ? (initial.ports || []) : (app?.ports || []).map((p) => { const [proto, port] = String(p).split('/'); return { proto, port, std: `${port}` }; });

  if (result) {
    const first = result.ports?.[0];
    return (
      <Modal title="Port open" onClose={onClose} footer={<Button kind="primary" onClick={onClose}>Done</Button>}>
        <div className="stack">
          <p>{first ? `Port ${first.port} was selected for ${result.app} on ${result.device_name}.` : `${result.app} is open on ${result.device_name}.`}</p>
          <p className="small muted">If this is not the correct port, enter it manually.</p>
          {(result.notes || []).map((n, i) => <Note key={i} tone="warn">{n}</Note>)}
          <div className="actions" style={{ justifyContent: 'flex-start' }}>
            <Button onClick={() => { setResult(null); setMore(true); tried.current = true; }}>Change port</Button>
            <Button onClick={async () => { await run(() => del(`/api/fw/appforwards/${encodeURIComponent(result.group)}`), 'Undone.'); onSaved?.(); onClose(); }}>Undo</Button>
          </div>
          <p className="small muted">It goes live in a moment after you save.</p>
        </div>
      </Modal>
    );
  }

  const ready = f.app && f.mac && (f.connections.length > 0 || connections.length > 0);
  return (
    <Modal title={editing ? `Edit ${initial.group}` : 'Open a port'} onClose={onClose}
      footer={<><Button onClick={onClose}>Cancel</Button>{(more || editing) && !clash && <Button kind="primary" onClick={() => submit({})} disabled={busy || !ready}>Save</Button>}</>}>
      <div className="stack">
        {clash && (
          <div className="stack" style={{ padding: 'var(--space-3)', border: 'var(--border-width-thin) solid var(--color-status-serious)', borderRadius: 'var(--radius-md)' }}>
            <strong>{clash.port} is already used by {clash.used_by}.</strong>
            <Field label="Enter the outside port for this one">
              <Input value={typed} onChange={setTyped} className="in mono" placeholder="Type a port" />
            </Field>
            {typedUse && <span className="small errtext">{typedUse}</span>}
            {typedNotes.map((n, i) => <span key={i} className="small muted">{n}</span>)}
            <div className="actions" style={{ justifyContent: 'flex-start' }}>
              <Button small onClick={suggest}>Suggest one</Button>
              <Button kind="primary" onClick={useTyped} disabled={busy || !typed.trim() || !!typedUse}>Open it on this port</Button>
            </div>
          </div>
        )}
        {!f.app ? (
          <Field label="What do you want to reach from outside?">
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: 'var(--space-2)' }}>
              {apps.map((a) => <Button key={a.name} onClick={() => setF({ ...f, app: a.name })}>{a.name}{a.custom ? ' (yours)' : ''}</Button>)}
              <Button onClick={() => setAdding(true)}>Other…</Button>
            </div>
          </Field>
        ) : (
          <div className="item"><div><div className="name">{f.app}</div><div className="meta">{app ? `Usual port${app.ports.length > 1 ? 's' : ''}: ${app.ports.join(', ')}` : ''}</div></div>{!editing && <div className="actions"><Button small onClick={() => { tried.current = false; setF({ ...f, app: '' }); }}>Change</Button></div>}</div>
        )}
        {adding && <NewApp onCancel={() => setAdding(false)} onSaved={async (n) => { await reloadApps(); setF((x) => ({ ...x, app: n })); setAdding(false); }} />}
        {f.app && (picked || f.mac ? (
          <div className="item"><div><div className="name">{devName}</div><div className="meta mono">{picked?.ip || f.ip || ''}</div></div>{!editing && !preset?.mac && <div className="actions"><Button small onClick={() => { tried.current = false; setF({ ...f, mac: '' }); }}>Change</Button></div>}</div>
        ) : (
          <Field label="Which device?">
            <div className="stack">
              <Input value={q} onChange={setQ} placeholder="Search by name or address" aria-label="Search devices" />
              <div className="list" style={{ maxHeight: '11rem', overflow: 'auto' }}>
                {found.slice(0, 40).map((d) => (
                  <div className="item" key={d.mac}>
                    <div><div className="name"><span className={`dot ${d.online ? '' : 'off'}`} /> {d.name}</div><div className="meta mono">{d.ip || 'not seen yet'}</div></div>
                    <div className="actions"><Button small onClick={() => setF({ ...f, mac: d.mac })}>Choose</Button></div>
                  </div>
                ))}
                {ch && !found.length && <Empty>No matching devices.</Empty>}
              </div>
            </div>
          </Field>
        ))}
        {f.app && f.mac && !editing && !more && !clash && <p className="small muted">Opening…</p>}
        {f.app && f.mac && !more && !editing && <button type="button" className="link small" onClick={() => { setMore(true); tried.current = true; }}>More options</button>}
        {(more || editing) && f.app && f.mac && (
          <div className="stack">
            {connections.length > 1 && (
              <Field label="Through which internet connection?">
                <div className="stack">
                  {connections.map((n) => (
                    <label key={n} className="small" style={{ display: 'flex', gap: 'var(--space-2)', alignItems: 'center' }}>
                      <input type="checkbox" checked={(f.connections.length ? f.connections : [connections[0]]).includes(n)} onChange={(e) => tick(n, e.target.checked)} /> {n}
                    </label>
                  ))}
                </div>
              </Field>
            )}
            {ports.map((p) => {
              const k = keyOf(p);
              return (
                <div className="formgrid" key={k}>
                  <Field label={`Outside port (${p.proto})`} hint={`Usual: ${p.std || p.port}`}><Input value={f.outside[k] ?? (editing ? p.port : '')} onChange={(v) => setPort('outside', k, v)} className="in mono" placeholder={String(p.std || p.port)} /></Field>
                  <Field label="Port on the device" hint="Leave as is unless the app listens elsewhere."><Input value={f.inside[k] ?? (editing ? (p.inside || p.std || p.port) : '')} onChange={(v) => setPort('inside', k, v)} className="in mono" placeholder={String(p.std || p.port)} /></Field>
                </div>
              );
            })}
            <p className="small muted">If a port you type is already used by something else, you will be asked for a different one.</p>
            <Field label="Only from these countries (optional)">
              <div className="stack">
                <div style={{ display: 'flex', flexWrap: 'wrap', gap: 'var(--space-1-5)' }}>
                  {f.countries.map((c) => <Pill key={c}>{countryName(c)} <button type="button" className="linkbtn" aria-label={`Remove ${countryName(c)}`} onClick={() => setF({ ...f, countries: f.countries.filter((x) => x !== c) })}>×</button></Pill>)}
                </div>
                <Select value="" onChange={(v) => v && !f.countries.includes(v) && setF({ ...f, countries: [...f.countries, v] })} options={[{ value: '', label: 'Add a country…' }, ...COUNTRIES]} />
              </div>
            </Field>
            <Field label="Or only from these addresses (optional)" hint="Such as 203.0.113.7 or 198.51.100.0/24. With no limits, anyone on the internet can try to connect.">
              <Input value={srcText} onChange={setSrcText} className="in mono" />
            </Field>
            <Field label="Name" hint="Optional. Up to 24 characters."><Input value={f.name ?? (editing ? initial.group : '')} onChange={(v) => setF({ ...f, name: v })} /></Field>
            <WhenField value={f.sched} onChange={(v) => setF({ ...f, sched: v })} hint="Always, or only during certain hours. Outside them the port is closed." />
            <ToggleRow title="Turned on" checked={f.enabled} onChange={(v) => setF({ ...f, enabled: v })} />
          </div>
        )}
      </div>
    </Modal>
  );
}

// Forwards made before apps existed keep working and are edited the old way.
function LegacyForm({ f, wans, onClose, onSaved }) {
  const [e, setE] = useState({ ...f });
  const [run, busy] = useAction();
  return (
    <Modal title="Edit port" onClose={onClose} footer={<><Button onClick={onClose}>Cancel</Button><SaveChoice busy={busy} disabled={!e.name} onSave={async (later) => { if (await run(() => (later ? holdAnd(() => put('/api/fw/forwards', e)) : put('/api/fw/forwards', e)), later ? SAVED_LATER : 'Saved.')) onSaved(); }} /></>}>
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
  const delayedDelete = useDelayedDelete();
  const { data, reload: rf } = useData('/api/fw/appforwards');
  const { data: legacy, reload: rl } = useData('/api/fw/forwards');
  const { data: wans } = useData('/api/net/wans');
  const [edit, setEdit] = useState(() => (hashQuery('new') ? {} : null));
  useEffect(() => { if (hashQuery('new')) setHashQuery('new', ''); }, []);
  const [old, setOld] = useState(null);
  const [run, busy] = useAction();
  const toast = useToast();
  const refresh = () => { rf(); rl(); reload?.(); };
  const items = data?.forwards || [];
  useWantedEdit(data && items, (g, w) => String(g.legacy ? g.id : g.group) === w, (g) => (g.legacy ? setOld((legacy || []).find((l) => l.id === g.id)) : setEdit({ group: g.group, name: g.group, ports: g.ports, app: g.app, mac: g.mac, device_name: g.device_name, ip: g.ip, connections: g.connections, src: g.src, countries: g.countries, enabled: g.enabled, sched: g.sched })));
  const fix = (g) => run(() => post('/api/dhcp/reservations', { scope_id: g.reserve_scope, mac: g.mac, ip: g.ip, hostname: g.device_name }), `${g.device_name} will keep the address ${g.ip}.`).then(rf);
  return (
    <Card title="Open a port to the internet" desc="Let the internet reach an app on one of your devices." actions={<Button small icon="plus" onClick={() => setEdit({})}>Open a port</Button>}>
      <div className="list">
        {<Limited items={items} noun="forwards" render={(g) => (
          <div className="item" key={`${g.group}-${g.id || ''}`}>
            <div className="fill">
              <div className="name">{g.text} {!g.enabled && <Pill>Off</Pill>}{whenText(g.paused_until) && <Pill tone="warn">{whenText(g.paused_until)}</Pill>}</div>
              {g.ports?.some((p) => p.inside) && <div className="small muted">{g.ports.map((p) => `${p.proto} ${p.port} → ${p.inside || p.std || p.port}`).join(', ')}</div>}
              <div className="meta">{g.countries.length > 0 ? `Only from ${g.countries.map(countryName).join(', ')}. ` : ''}{g.src.length > 0 ? `Only from ${g.src.join(', ')}. ` : ''}{!g.countries.length && !g.src.length ? 'Anyone on the internet can try to connect.' : ''}</div>
              {g.blocked && <div className="small errtext">{g.blocked}</div>}
              {!g.legacy && g.mac && !g.reserved && g.reserve_scope > 0 && <div className="small">If {g.device_name} gets a different address, this follows it. <button type="button" className="link small" disabled={busy} onClick={() => fix(g)}>Give it a fixed address</button> so it never changes.</div>}
            </div>
            <div className="actions">
              <PolicyControls name={g.group} enabled={g.enabled} pausedUntil={g.paused_until} busy={busy}
                onPause={g.legacy ? undefined : (m) => run(() => post('/api/fw/appforwards/pause', { group: g.group, minutes: m }), m === 0 ? 'Saved. Apply the firewall to resume it.' : 'Saved. Apply the firewall to start the pause.').then(refresh)}
                onToggle={g.legacy ? undefined : () => run(() => post('/api/fw/appforwards/enable', { group: g.group, enabled: !g.enabled }), 'Saved. Apply the firewall to start using it.').then(refresh)}
                onEdit={() => g.legacy ? setOld((legacy || []).find((l) => l.id === g.id)) : setEdit({ group: g.group, name: g.group, ports: g.ports, app: g.app, mac: g.mac, device_name: g.device_name, ip: g.ip, connections: g.connections, src: g.src, countries: g.countries, enabled: g.enabled, sched: g.sched })}
                onRemove={() => delayedDelete(`The port forward "${g.group}"`, () => (g.legacy ? del(`/api/fw/forwards/${g.id}`) : del(`/api/fw/appforwards/${encodeURIComponent(g.group)}`)), refresh)} />
            </div>
          </div>
        )} />}
        {data && !items.length && <Empty>No ports are open to the internet.</Empty>}
      </div>
      {data && data.connections.length === 0 && <Note tone="warn">Add an internet connection first, on the Internet page.</Note>}
      {edit && <OpenPortModal initial={edit.group ? edit : null} apps={data?.apps || []} connections={data?.connections || []} existing={items} onClose={() => setEdit(null)} onSaved={refresh} reloadApps={rf} />}
      {old && <LegacyForm f={old} wans={wans?.wans} onClose={() => setOld(null)} onSaved={() => { setOld(null); refresh(); }} />}
    </Card>
  );
}

import React, { useMemo, useState } from 'react';
import { del, post, put, useData } from '../api.js';
import ApplyBar from '../ApplyBar.jsx';
import { Button, Card, Empty, Field, Input, Modal, NumberInput, Note, Page, Pill, Problems, Select, Tabs, ToggleRow, useAction, fmt } from '../ui.jsx';

function ZoneForm({ initial, presets, ifaces, taken, onClose, onSaved }) {
  const [z, setZ] = useState({ kind: 'trusted', name: '', ifaces: [], internet: true, management: true, intra_allow: true, gateway: true, upnp: false, ssh: false, dns_force: true, description: '', ...initial });
  const [run, busy] = useAction();
  const set = (k) => (v) => setZ((x) => ({ ...x, [k]: v }));
  const pick = (p) => setZ((x) => ({ ...x, kind: p.kind, internet: p.internet, intra_allow: p.intra_allow, management: p.management }));
  return (
    <Modal title={z.id ? `Edit ${z.name}` : 'Add a network'} onClose={onClose}
      footer={<><Button onClick={onClose}>Cancel</Button><Button kind="primary" disabled={busy || !z.name} onClick={async () => { if (await run(() => put('/api/fw/zones', z), 'Saved. Apply it to start using it.')) { onSaved(); onClose(); } }}>Save</Button></>}>
      <Field label="Name"><Input value={z.name} onChange={set('name')} placeholder="Guests" /></Field>
      <div className="field"><span className="lab">What kind of network is it?</span>
        <div className="stack">
          {presets.map((p) => (
            <button type="button" key={p.kind} className={`preset ${z.kind === p.kind ? 'on' : ''}`} onClick={() => pick(p)} aria-pressed={z.kind === p.kind}>
              <span className="t">{p.title}</span><span className="d">{p.summary}</span>
            </button>
          ))}
        </div>
      </div>
      <div className="field"><span className="lab">Which ports or VLANs belong to it?</span>
        {ifaces.length === 0 && <span className="hint">Add a port on the Internet page first.</span>}
        {ifaces.map((i) => {
          const other = taken[i.name] && taken[i.name] !== z.name;
          return (
            <label key={i.id} className="inline small">
              <input type="checkbox" checked={z.ifaces.includes(i.name)} disabled={!!other}
                onChange={(e) => set('ifaces')(e.target.checked ? [...z.ifaces, i.name] : z.ifaces.filter((n) => n !== i.name))} />
              <span>{i.name} <span className="muted">{i.ipv4}{other ? ` · used by ${taken[i.name]}` : ''}</span></span>
            </label>
          );
        })}
      </div>
      <ToggleRow title="Can reach the internet" checked={z.internet} onChange={set('internet')} />
      <ToggleRow title="Devices here can talk to each other" checked={z.intra_allow} onChange={set('intra_allow')} />
      <ToggleRow title="Can manage Gatehouse" desc="Allows the console from this network." checked={z.management} onChange={set('management')} />
      <ToggleRow title="Gatehouse is the gateway" desc="Off if another router on this network does that job." checked={z.gateway} onChange={set('gateway')} />
      <ToggleRow title="Make devices here use the enforced DNS server" desc="Catches gadgets with a DNS server built in, such as smart TVs and cameras. Turn off for a network that needs its own DNS." checked={z.dns_force} onChange={set('dns_force')} />
      <ToggleRow title="Allow remote login (SSH)" checked={z.ssh} onChange={set('ssh')} />
      <ToggleRow title="Let devices open ports automatically (UPnP)" desc="Convenient for games and cameras, but widens exposure. Off is safer." checked={z.upnp} onChange={set('upnp')} />
    </Modal>
  );
}

function Networks({ ov, presets, reload }) {
  const { data: ifs } = useData('/api/net/interfaces');
  const [edit, setEdit] = useState(null);
  const [run] = useAction();
  const zones = (ov?.zones || []).filter((z) => z.kind !== 'wan');
  const taken = {};
  zones.forEach((z) => z.ifaces?.forEach((n) => { taken[n] = z.name; }));
  const kindTitle = (k) => presets.find((p) => p.kind === k)?.title || k;
  return (
    <>
      {ov?.problems && zones.length > 0 && <Problems list={ov.problems} />}
      <Card title="Your networks" desc="Each network has a type. The type decides what it can reach, in plain terms." actions={<Button small icon="plus" onClick={() => setEdit({})}>Add network</Button>}>
        {zones.length === 0 && <Empty>No networks yet. Add your first one so devices are protected.</Empty>}
        <div className="list">
          {zones.map((z) => (
            <div className="item" key={z.id}>
              <div>
                <div className="name">{z.name} <Pill>{kindTitle(z.kind)}</Pill>{z.management && <Pill tone="good">Can manage Gatehouse</Pill>}</div>
                <div className="meta">{z.ifaces?.length ? z.ifaces.join(', ') : 'no ports yet'}</div>
              </div>
              <div className="actions"><Button small icon="edit" onClick={() => setEdit(z)}>Edit</Button>
                <Button small kind="danger" icon="trash" onClick={() => window.confirm(`Remove ${z.name}?`) && run(() => del(`/api/fw/zones/${z.id}`)).then(reload)}>Remove</Button></div>
            </div>
          ))}
        </div>
      </Card>
      {ov?.summary && (
        <Card title="In plain words" desc="What this firewall does right now, once applied.">
          <ul className="stack" style={{ margin: 0, paddingLeft: '1.2rem' }}>{ov.summary.map((s, i) => <li key={i}>{s}</li>)}</ul>
        </Card>
      )}
      {edit && <ZoneForm initial={edit} presets={presets} ifaces={ifs || []} taken={taken} onClose={() => setEdit(null)} onSaved={reload} />}
    </>
  );
}

function Matrix({ ov }) {
  const zones = ov?.zones || [];
  const cell = (a, b) => {
    if (a.kind === 'wan') return ['deny', 'Blocked'];
    if (a.name === b.name) return a.intra_allow ? ['allow', 'Open'] : ['deny', 'Blocked'];
    if (b.kind === 'wan') return a.internet ? ['allow', 'Open'] : ['deny', 'Blocked'];
    if (a.kind === 'trusted') return ['allow', 'Open'];
    return ['deny', 'Blocked'];
  };
  if (zones.length < 2) return null;
  return (
    <Card title="Who can reach whom" desc="Traffic starting in the network on the left, going to the network along the top. Exceptions you add are not shown here.">
      <div className="scroll">
        <div className="matrix" style={{ gridTemplateColumns: `repeat(${zones.length + 1}, minmax(5rem, 1fr))` }}>
          <div className="cell hd">From ↓ To →</div>
          {zones.map((z) => <div className="cell hd" key={z.name}>{z.name}</div>)}
          {zones.map((a) => (
            <React.Fragment key={a.name}>
              <div className="cell hd">{a.name}</div>
              {zones.map((b) => { const [c, t] = cell(a, b); return <div key={b.name} className={`cell ${c}`}>{t}</div>; })}
            </React.Fragment>
          ))}
        </div>
      </div>
    </Card>
  );
}

function RuleForm({ initial, zoneNames, onClose, onSaved }) {
  const [r, setR] = useState({ name: '', from: zoneNames[0] || '', to: zoneNames[1] || '', src: '', dst: '', service: 'any', action: 'accept', limit: 0, log: false, enabled: true, ...initial });
  const { data: p } = useData('/api/fw/presets');
  const [run, busy] = useAction();
  const set = (k) => (v) => setR((x) => ({ ...x, [k]: v }));
  const all = [...zoneNames, 'self'];
  return (
    <Modal title={r.id ? 'Edit exception' : 'Add an exception'} onClose={onClose}
      footer={<><Button onClick={onClose}>Cancel</Button><Button kind="primary" disabled={busy || !r.name} onClick={async () => { if (await run(() => put('/api/fw/rules', r), 'Saved. Apply it to start using it.')) { onSaved(); onClose(); } }}>Save</Button></>}>
      <p className="sec small">Exceptions are checked before the network types, top to bottom. The first match wins.</p>
      <Field label="Name"><Input value={r.name} onChange={set('name')} placeholder="Guests can print" /></Field>
      <div className="formgrid">
        <Field label="From network"><Select value={r.from} onChange={set('from')} options={zoneNames} /></Field>
        <Field label="To"><Select value={r.to} onChange={set('to')} options={[...all.map((z) => ({ value: z, label: z === 'self' ? 'Gatehouse itself' : z })), { value: 'Internet', label: 'Internet' }].filter((o, i, a) => a.findIndex((x) => x.value === o.value) === i)} /></Field>
        <Field label="Only from this device" hint="Address or network. Empty for any."><Input value={r.src} onChange={set('src')} /></Field>
        <Field label="Only to this device" hint="Address or network. Empty for any."><Input value={r.dst} onChange={set('dst')} /></Field>
        <Field label="What for"><Select value={r.service} onChange={set('service')} options={['any', ...(p?.services || [])]} /></Field>
        <Field label="Then"><Select value={r.action} onChange={set('action')} options={[{ value: 'accept', label: 'Allow' }, { value: 'drop', label: 'Block silently' }, { value: 'reject', label: 'Block and tell the sender' }]} /></Field>
        <Field label="Limit new connections per second" hint="0 for no limit"><NumberInput value={r.limit} onChange={set('limit')} /></Field>
      </div>
      <ToggleRow title="Keep a log of matches" checked={r.log} onChange={set('log')} />
      <ToggleRow title="Turned on" checked={r.enabled} onChange={set('enabled')} />
    </Modal>
  );
}

function Exceptions({ ov, reload }) {
  const { data: rules, reload: rr } = useData('/api/fw/rules');
  const [edit, setEdit] = useState(null);
  const [run] = useAction();
  const names = (ov?.zones || []).map((z) => z.name);
  const refresh = () => { rr(); reload(); };
  const move = (i, d) => {
    const ids = rules.map((r) => r.id);
    const j = i + d;
    if (j < 0 || j >= ids.length) return;
    [ids[i], ids[j]] = [ids[j], ids[i]];
    run(() => post('/api/fw/rules/order', { order: ids })).then(refresh);
  };
  return (
    <Card title="Exceptions" desc="Special cases on top of your network types, such as letting guests use a printer." actions={<Button small icon="plus" disabled={names.length < 1} onClick={() => setEdit({})}>Add exception</Button>}>
      <Note>The rule that keeps you able to manage Gatehouse is always first. It cannot be changed or removed, so you cannot lock yourself out.</Note>
      <div className="list">
        {(rules || []).map((r, i) => (
          <div className="item" key={r.id}>
            <div>
              <div className="name">{r.name} {!r.enabled && <Pill>Off</Pill>}</div>
              <div className="meta">{r.from} → {r.to === 'self' ? 'Gatehouse' : r.to}{r.src ? ` · from ${r.src}` : ''}{r.dst ? ` · to ${r.dst}` : ''} · {r.service} · {r.action === 'accept' ? 'allow' : r.action === 'drop' ? 'block' : 'reject'}{r.limit ? ` · max ${r.limit}/s` : ''}</div>
            </div>
            <div className="actions">
              <Button small aria-label="Move up" onClick={() => move(i, -1)} disabled={i === 0}>↑</Button>
              <Button small aria-label="Move down" onClick={() => move(i, 1)} disabled={i === rules.length - 1}>↓</Button>
              <Button small icon="edit" onClick={() => setEdit(r)}>Edit</Button>
              <Button small kind="danger" icon="trash" onClick={() => window.confirm(`Delete "${r.name}"?`) && run(() => del(`/api/fw/rules/${r.id}`)).then(refresh)}>Delete</Button>
            </div>
          </div>
        ))}
        {!rules?.length && <Empty>No exceptions. Network types are all you need for most setups.</Empty>}
      </div>
      {edit && <RuleForm initial={edit} zoneNames={names} onClose={() => setEdit(null)} onSaved={refresh} />}
    </Card>
  );
}

function Forwards({ reload }) {
  const { data, reload: rf } = useData('/api/fw/forwards');
  const { data: wans } = useData('/api/net/wans');
  const [edit, setEdit] = useState(null);
  const [run, busy] = useAction();
  const refresh = () => { rf(); reload(); };
  return (
    <Card title="Open a port to the internet" desc="Lets something outside reach a device inside, like a game server. Only do this for what you need." actions={<Button small icon="plus" onClick={() => setEdit({ proto: 'tcp', enabled: true, wan: '' })}>Add</Button>}>
      <div className="list">
        {(data || []).map((f) => (
          <div className="item" key={f.id}>
            <div><div className="name">{f.name} {!f.enabled && <Pill>Off</Pill>}</div><div className="meta">{f.proto.toUpperCase()} port {f.ext_port} → {f.int_addr}:{f.int_port || f.ext_port}{f.wan ? ` via ${f.wan}` : ''}</div></div>
            <div className="actions"><Button small icon="edit" onClick={() => setEdit(f)}>Edit</Button><Button small kind="danger" icon="trash" onClick={() => window.confirm(`Delete "${f.name}"?`) && run(() => del(`/api/fw/forwards/${f.id}`)).then(refresh)}>Delete</Button></div>
          </div>
        ))}
        {!data?.length && <Empty>No ports are open to the internet.</Empty>}
      </div>
      {edit && (
        <Modal title={edit.id ? 'Edit port' : 'Open a port'} onClose={() => setEdit(null)} footer={<><Button onClick={() => setEdit(null)}>Cancel</Button><Button kind="primary" disabled={busy || !edit.name} onClick={async () => { if (await run(() => put('/api/fw/forwards', edit), 'Saved. Apply it to start using it.')) { refresh(); setEdit(null); } }}>Save</Button></>}>
          <Field label="Name"><Input value={edit.name} onChange={(v) => setEdit({ ...edit, name: v })} placeholder="Game server" /></Field>
          <div className="formgrid">
            <Field label="Kind"><Select value={edit.proto} onChange={(v) => setEdit({ ...edit, proto: v })} options={['tcp', 'udp']} /></Field>
            <Field label="Port from the internet" hint="Or a range like 27000-27010"><Input value={edit.ext_port} onChange={(v) => setEdit({ ...edit, ext_port: v })} /></Field>
            <Field label="Device inside"><Input value={edit.int_addr} onChange={(v) => setEdit({ ...edit, int_addr: v })} placeholder="192.168.1.50" /></Field>
            <Field label="Port on the device" hint="Empty to keep the same"><Input value={edit.int_port} onChange={(v) => setEdit({ ...edit, int_port: v })} /></Field>
            <Field label="Which connection"><Select value={edit.wan || ''} onChange={(v) => setEdit({ ...edit, wan: v })} options={[{ value: '', label: 'All' }, ...(wans?.wans || []).map((w) => w.name)]} /></Field>
          </div>
          <ToggleRow title="Turned on" checked={edit.enabled} onChange={(v) => setEdit({ ...edit, enabled: v })} />
        </Modal>
      )}
    </Card>
  );
}

function DnsTarget({ s, save }) {
  const [mode, setMode] = useState(s.dns_target ? 'custom' : 'self');
  const [addr, setAddr] = useState(s.dns_target || '');
  const choose = (v) => { setMode(v); if (v === 'self') { setAddr(''); if (s.dns_target) save(''); } };
  return (
    <div className="stack">
      <Field label="Send forced DNS to">
        <Select value={mode} onChange={choose} options={[{ value: 'self', label: 'Gatehouse (recommended)' }, { value: 'custom', label: 'A different DNS server' }]} />
      </Field>
      {mode === 'custom' && (
        <Field label="DNS server address" hint="For example your Pi-hole. That server's own lookups are not redirected. Networks without internet access still use Gatehouse.">
          <Input value={addr} onChange={setAddr} onBlur={() => addr.trim() && addr.trim() !== s.dns_target && save(addr.trim())} placeholder="192.168.1.2" />
        </Field>
      )}
    </div>
  );
}

function Protection({ ov, reload }) {
  const s = ov?.settings;
  const [run] = useAction();
  if (!s) return null;
  const save = (k) => (v) => run(() => put('/api/fw/settings', { ...s, [k]: v })).then(reload);
  return (
    <Card title="Protection" desc="Extra safeguards that apply to every network.">
      <ToggleRow title="Force every device to use one DNS server" desc="Devices with their own DNS server built in (smart TVs, cameras, IoT gadgets) are quietly redirected, so your filters cannot be bypassed. You can exclude a network when you edit it." checked={s.dns_redirect} onChange={save('dns_redirect')} />
      {s.dns_redirect && <DnsTarget s={s} save={save('dns_target')} />}
      <ToggleRow title="Block encrypted DNS to other servers" desc="Stops devices bypassing your filters with DNS-over-TLS." checked={s.block_dot} onChange={save('block_dot')} />
      <ToggleRow title="Ask for a code before changing the firewall" desc="Requires your authenticator code to apply network changes." checked={s.step_up} onChange={save('step_up')} />
      <ToggleRow title="Log blocked traffic" desc="Useful when troubleshooting. Uses more disk space." checked={s.log_drops} onChange={save('log_drops')} />
    </Card>
  );
}

function History() {
  const { data } = useData('/api/fw/history', { topics: ['fw.status'] });
  return (
    <Card title="Change history" desc="Every time the firewall was changed, and whether you kept it.">
      <div className="scroll"><table className="table">
        <thead><tr><th>#</th><th>When</th><th>Who</th><th>What</th><th>Result</th></tr></thead>
        <tbody>{(data || []).map((h) => (
          <tr key={h.version}><td>{h.version}</td><td>{fmt.dt(h.at)}</td><td>{h.author}</td><td>{h.summary}</td>
            <td><Pill tone={h.status === 'confirmed' ? 'good' : h.status === 'pending' ? 'warn' : ''}>{h.status === 'confirmed' ? 'Kept' : h.status === 'pending' ? 'Waiting' : h.status === 'superseded' ? 'Replaced' : 'Undone'}</Pill></td></tr>
        ))}</tbody></table>
        {!data?.length && <Empty>No changes have been applied yet.</Empty>}
      </div>
    </Card>
  );
}

export default function Firewall({ me }) {
  const { data: ov, reload } = useData('/api/fw/overview', { topics: ['fw.status'] });
  const { data: pr } = useData('/api/fw/presets');
  const [tab, setTab] = useState('networks');
  const presets = pr?.presets || [];
  return (
    <Page title="Firewall" sub="Decide what each network may reach. Changes only take effect when you apply them.">
      <Tabs value={tab} onChange={setTab} tabs={[{ value: 'networks', label: 'Networks' }, { value: 'exceptions', label: 'Exceptions' }, { value: 'ports', label: 'Open ports' }, { value: 'protection', label: 'Protection' }, { value: 'history', label: 'History' }]} />
      {tab === 'networks' && <><Networks ov={ov} presets={presets} reload={reload} /><Matrix ov={ov} /></>}
      {tab === 'exceptions' && <Exceptions ov={ov} reload={reload} />}
      {tab === 'ports' && <Forwards reload={reload} />}
      {tab === 'protection' && <Protection ov={ov} reload={reload} />}
      {tab === 'history' && <History />}
      <ApplyBar kind="fw" label="the firewall" status={ov?.status} me={me} onChanged={reload} />
    </Page>
  );
}

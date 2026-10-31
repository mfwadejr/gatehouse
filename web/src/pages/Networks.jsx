import React, { useState } from 'react';
import { del, post, put, useData } from '../api.js';
import { confirmRemoval } from '../removal.js';
import ApplyBar from '../ApplyBar.jsx';
import AddNetwork from '../AddNetwork.jsx';
import { ScopeForm } from './DHCP.jsx';
import { usePortWatch } from './Internet.jsx';
import { Button, Card, Empty, Field, Input, Modal, Note, Page, Pill, Select, ToggleRow, useAction, useToast, confirmBox, usePortNames } from '../ui.jsx';

// The one place for your own networks: what they are, where they plug in, who is on them, and how
// they get addresses. Adding one is one form. The internet connection itself is on the Internet page.

function PortRow({ p, onSaved }) {
  const [editing, setEditing] = useState(false);
  const [name, setName] = useState(p.label || '');
  const [run, busy] = useAction();
  const save = async (v) => {
    const ok = await run(() => put('/api/net/ports/label', { name: p.name, label: v }), v ? 'Name saved.' : 'Name cleared.');
    if (ok !== false) { setEditing(false); onSaved && onSaved(); }
  };
  return (
    <div className="item">
      <div style={{ flex: 1, minWidth: 0 }}>
        <div className="name">{p.label || p.name} {p.usb && <Pill>USB</Pill>}</div>
        {p.label && <div className="meta mono">{p.name}</div>}
        <div className="meta mono">{p.mac}</div>
        <div className="meta">{p.addrs?.length ? <>Address <span className="mono">{p.addrs.join(', ')}</span></> : 'No address'}</div>
        {editing && (
          <div className="inline" style={{ marginTop: 'var(--space-2)' }}>
            <Input value={name} onChange={setName} placeholder="For example: T-Mobile gateway" aria-label={`Name for ${p.name}`} maxLength={30}
              onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); save(name); } }} />
            <Button small kind="primary" disabled={busy} onClick={() => save(name)}>Save</Button>
            {p.label && <Button small disabled={busy} onClick={() => save('')}>Use the original name</Button>}
            <Button small onClick={() => { setEditing(false); setName(p.label || ''); }}>Cancel</Button>
          </div>
        )}
      </div>
      <div className="inline">
        {!editing && <Button small icon="edit" onClick={() => setEditing(true)}>Rename</Button>}
        <Pill tone={p.link ? 'good' : ''}>{p.wifi ? (p.link ? 'Wi-Fi connected' : 'Wi-Fi not connected') : p.link ? `Connected${p.speed > 0 ? ` · ${p.speed} Mb/s` : ''}` : 'No cable'}</Pill>
      </div>
    </div>
  );
}

function EditNetwork({ n, zones, onClose, onSaved }) {
  const [f, setF] = useState({ ...n.iface });
  const [run, busy] = useAction();
  const set = (k) => (v) => setF((x) => ({ ...x, [k]: v }));
  const { data: sug } = useData('/api/networks/suggest');
  const [port, setPort] = useState(n.iface.kind === 'vlan' ? n.iface.parent : n.iface.name);
  const [vlan, setVlan] = useState(String(n.iface.vlan || ''));
  const isBridge = n.iface.kind === 'bridge';
  const { data: allPorts } = useData('/api/networks/suggest?all=1');
  const [mem, setMem] = useState(n.iface.members || []);
  const first = n.iface.members || [];
  const dropped = first.filter((m) => !mem.includes(m));
  const added = mem.filter((m) => !first.includes(m));
  const tick = (name, on) => setMem((x) => (on ? [...x.filter((m) => m !== name), name] : x.filter((m) => m !== name)));
  const why = (p) => {
    if (mem.includes(p.name) || first.includes(p.name)) return '';
    if (p.use === 'internet') return 'used for the internet';
    if (p.use === 'bridge') return 'part of another network';
    if (p.use === 'network') return 'has a network of its own';
    return '';
  };
  const plain = n.iface.kind === 'phys';
  const [extra, setExtra] = useState([]);
  const tickExtra = (name, on) => setExtra((x) => (on ? [...x.filter((m) => m !== name), name] : x.filter((m) => m !== name)));
  const addPorts = async () => {
    const ok = await run(() => post('/api/networks/add-ports', { id: n.iface.id, ports: extra }), 'Saved. Apply it to start using it.');
    if (ok) { onSaved(); onClose(); }
  };
  const movable = !isBridge;
  const moved = movable && (port !== (n.iface.kind === 'vlan' ? n.iface.parent : n.iface.name) || Number(vlan || 0) !== (n.iface.vlan || 0));
  const save = async () => {
    const ok = await run(async () => {
      await put('/api/net/interfaces', isBridge ? { ...f, members: mem } : f);
      if (moved) await post('/api/networks/move', { id: n.iface.id, port, vlan: Number(vlan || 0) });
    }, 'Saved. Apply it to start using it.');
    if (ok) { onSaved(); onClose(); }
  };
  return (
    <Modal title={`Edit ${n.title}`} onClose={onClose}
      footer={<><Button onClick={onClose}>Cancel</Button><Button kind="primary" onClick={save} disabled={busy || (isBridge && mem.length === 0)}>Save</Button></>}>
      <div className="stack">
        <Field label="Address of this router on the network" hint="With the size, like 192.168.10.1/24. Devices here use it as their gateway.">
          <Input value={f.ipv4 || ''} onChange={set('ipv4')} className="in mono" />
        </Field>
        <Field label="What it can reach" hint="The firewall network this belongs to. Change what each network can reach on the Firewall page.">
          <Select value={String(f.zone_id || 0)} onChange={(v) => set('zone_id')(Number(v))} options={[{ value: '0', label: 'Not assigned yet' }, ...zones.map((z) => ({ value: String(z.id), label: z.name }))]} />
        </Field>
        <ToggleRow title="In use" desc="Turn off to switch the network off without deleting it." checked={f.enabled} onChange={set('enabled')} />
        {isBridge && (
          <Field label="Ports on this network" hint="Tick the ports that act as a switch for this network. Devices plugged into any ticked port share the same addresses. Nothing changes until you apply.">
            <div className="stack">
              {((allPorts && allPorts.ports) || []).map((p) => {
                const reason = why(p);
                const on = mem.includes(p.name);
                return (
                  <label key={p.name} className="small" style={{ display: 'flex', gap: 8, alignItems: 'center', opacity: reason ? 0.6 : 1 }}>
                    <input type="checkbox" checked={on} disabled={!!reason} onChange={(e) => tick(p.name, e.target.checked)} />
                    <span className="mono">{p.name}</span>
                    <span className="muted">{p.wifi ? 'Wi-Fi card' : p.link ? `cable connected${p.speed > 0 ? `, ${p.speed} Mb/s` : ''}` : 'no cable'}{reason ? ` · ${reason}` : on && !first.includes(p.name) ? ' · will join' : ''}</span>
                  </label>
                );
              })}
              {mem.length === 0 && <Note tone="warn">A network needs at least one port.</Note>}
              {(dropped.length > 0 || added.length > 0) && <Note tone="warn">{dropped.length ? `Taking ${dropped.join(', ')} out cuts off devices plugged into ${dropped.length === 1 ? 'it' : 'them'}. ` : ''}If you manage Gatehouse through one of these ports, keep another way in until you have applied and checked it. Gatehouse puts the change back by itself if you cannot confirm it.</Note>}
            </div>
          </Field>
        )}
        {plain && (
          <Field label="Add more ports to this network" hint="Devices on these ports share this network's addresses, like extra ports on a switch. Nothing changes until you apply.">
            <div className="stack">
              {((allPorts && allPorts.ports) || []).filter((p) => !p.wifi && p.name !== n.iface.name).map((p) => {
                const reason = why(p);
                return (
                  <label key={p.name} className="small" style={{ display: 'flex', gap: 8, alignItems: 'center', opacity: reason ? 0.6 : 1 }}>
                    <input type="checkbox" checked={extra.includes(p.name)} disabled={!!reason} onChange={(e) => tickExtra(p.name, e.target.checked)} />
                    <span className="mono">{p.name}</span>
                    <span className="muted">{p.link ? `cable connected${p.speed > 0 ? `, ${p.speed} Mb/s` : ''}` : 'no cable'}{reason ? ` · ${reason}` : ''}</span>
                  </label>
                );
              })}
              {extra.length > 0 && <Note tone="warn">Gatehouse briefly restarts this network while it changes. If you manage Gatehouse through it, keep another way in until you have applied and checked it. Gatehouse puts the change back by itself if you cannot confirm it.</Note>}
              <div><Button small disabled={busy || extra.length === 0} onClick={addPorts}>Add {extra.length ? extra.join(', ') : 'ports'} to this network</Button></div>
            </div>
          </Field>
        )}
        <details className="more">
          <summary>More options</summary>
          <div className="stack">
            {movable && (
              <>
                <Field label="Port" hint="The physical port this network is plugged into.">
                  <Select value={port} onChange={setPort} options={[...new Set([port, ...((sug && sug.ports) || []).map((p) => p.name)])].map((x) => ({ value: x, label: x }))} />
                </Field>
                <Field label="VLAN number" hint="Leave empty to use the whole port. Your switch must pass the number you choose.">
                  <Input value={vlan} onChange={(v) => setVlan(v.replace(/\D/g, ''))} className="in mono" />
                </Field>
                {moved && <Note tone="warn">Moving a network to another port or VLAN cuts devices off until they are on the new one. If you manage Gatehouse from this network, keep another way in until you have applied and checked it.</Note>}
              </>
            )}
            <p className="small muted">
              {f.kind === 'vlan' ? <>This network is a VLAN: number {f.vlan} on port {f.parent}. Your switch must pass that number.</> : f.kind === 'bridge' ? <>This network spreads over several ports ({(f.members || []).join(', ')}), carried by {f.name}.</> : <>This network uses the whole port {f.name}. To add another network to the same port, add a network and choose this port: Gatehouse then uses a VLAN number for it.</>}
            </p>
          </div>
        </details>
      </div>
    </Modal>
  );
}

// Sensible starting values for a network that has no DHCP yet, taken from its own address.
function newScope(n) {
  const m = /^(\d+)\.(\d+)\.(\d+)\.(\d+)\/(\d+)$/.exec(n.iface.ipv4 || '');
  if (!m) return { name: n.title, iface: n.iface.name, mode: 'serve' };
  const base = `${m[1]}.${m[2]}.${m[3]}`;
  return { name: n.title, iface: n.iface.name, mode: 'serve', subnet: `${base}.0/${m[5]}`, range_start: `${base}.100`, range_end: `${base}.200`, router: `${base}.${m[4]}`, dns: `${base}.${m[4]}` };
}

function dhcpText(s) {
  if (!s) return 'DHCP not set up';
  if (!s.enabled) return 'DHCP off';
  if (s.mode === 'off') return 'Addresses come from another device';
  if (s.mode === 'relay') return `Addresses relayed to ${s.relay_addr}`;
  return `DHCP ${s.range_start} – ${s.range_end}`;
}

export default function Networks({ me }) {
  const { data: ifs, reload: ri } = useData('/api/net/interfaces', { topics: ['net.status'] });
  const { data: zones } = useData('/api/fw/zones', { topics: ['fw.status'] });
  const { data: scopes, reload: rsc } = useData('/api/dhcp/scopes', { topics: ['dhcp.leases'] });
  const { data: dev } = useData('/api/devices', { topics: ['dhcp.leases'], every: 15000 });
  const { data: st, reload: rs } = useData('/api/net/status', { topics: ['net.status'], every: 4000 });
  const { data: fwo, reload: rfw } = useData('/api/fw/overview', { topics: ['fw.status'] });
  const { data: alerts } = useData('/api/dhcp/alerts', { topics: ['dhcp.alerts'] });
  const { ports, reload: rp } = usePortWatch(false);
  const pn = usePortNames();
  const [adding, setAdding] = useState(false);
  const [edit, setEdit] = useState(null);
  const [dhcp, setDhcp] = useState(null);
  const toast = useToast();
  const [run] = useAction();
  const all = () => { ri(); rs(); rsc(); rfw(); };
  const zoneName = (id) => (zones || []).find((z) => z.id === id)?.name || '';
  const stat = st?.status;

  const nets = (ifs || []).map((i) => {
    const zn = zoneName(i.zone_id);
    const scope = (scopes || []).find((s) => s.iface === i.name);
    const members = (dev?.devices || []).filter((d) => zn && d.network === zn);
    return { iface: i, title: zn || i.name, zone: zn, scope, online: members.filter((d) => d.online).length, total: members.length };
  });

  return (
    <Page title="Networks" sub="Your own networks: where they plug in, who is on them, and how they get addresses."
      actions={<Button kind="primary" icon="plus" onClick={() => setAdding(true)}>Add a network</Button>}>
      {!stat?.helper && stat && <Note tone="warn" title="Network changes are unavailable">The network helper is not running, so networks can be viewed but not changed.</Note>}
      {(alerts || []).map((r) => (
        <Note key={r.scope} tone="bad" title={`Another DHCP server is answering on ${r.name}`}>
          {r.found.map((f) => f.server).join(', ')} hands out addresses here as well, so devices may get the wrong settings. Switch that device's DHCP off, or turn DHCP off for this network.
        </Note>
      ))}
      <Card title="Your networks" desc="Each one is separate. What it can reach is decided on the Firewall page.">
        {!ifs ? null : !nets.length ? (
          <Empty>
            <div className="stack" style={{ alignItems: 'center' }}>
              <span>No networks yet. Add one and devices can join it.</span>
              <Button kind="primary" icon="plus" onClick={() => setAdding(true)}>Add a network</Button>
            </div>
          </Empty>
        ) : (
          <div className="list">
            {nets.map((n) => (
              <div className="item" key={n.iface.id}>
                <div style={{ flex: 1, minWidth: 0 }}>
                  <div className="name">{n.title} {!n.iface.enabled && <Pill>Off</Pill>}{!n.zone && <Pill tone="warn">Not assigned to a firewall network</Pill>}</div>
                  <div className="meta">{n.iface.ipv4 || 'no address'} · {n.iface.kind === 'vlan' ? `VLAN ${n.iface.vlan} on ${n.iface.parent}` : `port ${pn(n.iface.name)}`}</div>
                  <div className="meta">{dhcpText(n.scope)}{n.zone ? ` · ${n.online} device${n.online === 1 ? '' : 's'} online${n.total > n.online ? `, ${n.total} known` : ''}` : ''}</div>
                </div>
                <div className="actions">
                  <Button small onClick={() => setDhcp(n.scope || newScope(n))}>DHCP</Button>
                  <Button small icon="edit" onClick={() => setEdit(n)}>Edit</Button>
                  <Button small kind="danger" icon="trash" onClick={async () => { if (await confirmRemoval(`/api/net/interfaces/${n.iface.id}/removal`, `Remove ${n.title}? Devices on it will lose their connection.`)) run(() => del(`/api/net/interfaces/${n.iface.id}`)).then(all); }}>Remove</Button>
                </div>
              </div>
            ))}
          </div>
        )}
      </Card>

      <Card title="Network ports on this device" desc="Live status and addresses. Give a port a name you will recognise, such as your internet gateway or a spare.">
        <div className="list">
          {ports.map((p) => <PortRow key={p.name} p={p} onSaved={rp} />)}
          {!ports.length && <Empty>No network ports found.</Empty>}
        </div>
      </Card>

      {adding && <AddNetwork onClose={() => setAdding(false)} onCreated={(n) => { setAdding(false); toast(`${n} added. Review and apply it below.`); all(); }} />}
      {edit && <EditNetwork n={edit} zones={zones || []} onClose={() => setEdit(null)} onSaved={all} />}
      {dhcp && <ScopeForm initial={dhcp} ifaces={ifs || []} onClose={() => setDhcp(null)} onSaved={all} />}
      <ApplyBar kind="net" label="your network settings" status={stat} me={me} onChanged={all} />
      {!stat?.dirty && !stat?.pending && fwo?.status && <ApplyBar kind="fw" label="the firewall" status={fwo.status} me={me} onChanged={rfw} />}
    </Page>
  );
}

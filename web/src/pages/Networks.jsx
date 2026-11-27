import { WhatChanged } from '../whatchanged.jsx';
import GuestPortal from './GuestPortal.jsx';
import React, { useState } from 'react';
import TrafficPanel from '../TrafficPanel.jsx';
import { del, holdAnd, post, put, useData } from '../api.js';
import { NetworkLayout } from '../netlayout.jsx';
import { SAVED_LATER, SaveChoice } from '../changes.jsx';
import { confirmRemoval } from '../removal.js';
import AddNetwork from '../AddNetwork.jsx';
import NetworkEditor from '../NetworkEditor.jsx';
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
      <div className="fill">
        <div className="name">{p.label || p.name} {p.usb && <Pill>USB</Pill>}</div>
        {p.label && <div className="meta mono">{p.name}</div>}
        <div className="meta mono">{p.mac}</div>
        <div className="meta">{p.addrs?.length ? <>Address <span className="mono">{p.addrs.join(', ')}</span></> : 'No address'}</div>
        {editing && (
          <div className="inline mt2">
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
        <Pill tone={p.link ? 'good' : ''}>{p.link ? `Connected${p.speed > 0 ? ` · ${p.speed} Mb/s` : ''}` : 'No cable'}</Pill>
      </div>
    </div>
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
  const [traffic, setTraffic] = useState('');
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
      {ifs && <NetworkLayout nets={nets} zones={zones} scopes={scopes} onOpen={(n) => setEdit(n)} />}
      <WhatChanged match={/network|bridge|port|vlan|dhcp|wan|vpn|tunnel|link/i} page="networks" />
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
                <div className="fill">
                  <div className="name">{n.title} {!n.iface.enabled && <Pill>Off</Pill>}{!n.zone && <Pill tone="warn">Not assigned to a firewall network</Pill>}</div>
                  <div className="meta">{n.iface.ipv4 || 'no address'} · {n.iface.kind === 'vlan' ? `VLAN ${n.iface.vlan} on ${pn(n.iface.parent)}` : `port ${pn(n.iface.name)}`}</div>
                  <div className="meta">{dhcpText(n.scope)}{n.zone ? ` · ${n.online} device${n.online === 1 ? '' : 's'} online${n.total > n.online ? `, ${n.total} known` : ''}` : ''}</div>
                </div>
                <div className="actions">
                  {n.zone && <Button small onClick={() => setTraffic(traffic === n.zone ? '' : n.zone)}>{traffic === n.zone ? 'Hide traffic' : 'Traffic'}</Button>}
                  <Button small onClick={() => setDhcp(n.scope || newScope(n))}>DHCP</Button>
                  <Button small icon="edit" onClick={() => setEdit(n)}>Edit</Button>
                  <Button small kind="danger" icon="trash" onClick={async () => { if (await confirmRemoval(`/api/net/interfaces/${n.iface.id}/removal`, `Remove ${n.title}? Devices on it will lose their connection.`)) run(() => del(`/api/net/interfaces/${n.iface.id}`)).then(all); }}>Remove</Button>
                </div>
              </div>
            ))}
          </div>
        )}
      </Card>

      {traffic && <TrafficPanel kind="net" id={traffic} title={`${traffic} traffic`} sub="What the devices on this network are downloading and uploading, all its ports together." />}

      <GuestPortal />

      <Card title="Network ports on this device" desc="Live status and addresses. Give a port a name you will recognise, such as your internet gateway or a spare.">
        <div className="list">
          {ports.map((p) => <PortRow key={p.name} p={p} onSaved={rp} />)}
          {!ports.length && <Empty>No network ports found.</Empty>}
        </div>
      </Card>

      {adding && <AddNetwork onClose={() => setAdding(false)} onCreated={(n, later) => { setAdding(false); toast(later ? `${n} saved for later. It waits on the Review page.` : `${n} added. It goes live in a moment.`); all(); }} />}
      {edit && <NetworkEditor zone={(zones || []).find((z) => z.id === edit.iface.zone_id) || null} iface={edit.iface} scope={edit.scope || null} zones={zones || []} onClose={() => setEdit(null)} onSaved={all} />}
      {dhcp && <ScopeForm initial={dhcp} ifaces={ifs || []} onClose={() => setDhcp(null)} onSaved={all} />}
    </Page>
  );
}

import React, { useState } from 'react';
import { holdAnd, post, put, useData } from './api.js';
import { SAVED_LATER, SaveChoice } from './changes.jsx';
import { Button, Field, Input, Modal, Note, Segmented, Select, ToggleRow, useAction, usePortNames } from './ui.jsx';

const LEASES = [{ value: '3600', label: '1 hour' }, { value: '7200', label: '2 hours' }, { value: '86400', label: '1 day' }, { value: '604800', label: '1 week' }];

// The one place to change a network. It is opened from Networks and from Firewall, Networks, and holds
// everything the person chose when adding it: its name and type, where it plugs in, its address, who
// hands out addresses, what it may reach and how it is reached. Each setting is saved to the place it
// belongs (the firewall network, the port or VLAN, the DHCP address pool), so those pages stay in step.
//   zone  the firewall network (a row from /api/fw/zones), or null when the port has none yet
//   iface the port, VLAN or bridge (a row from /api/net/interfaces)
//   scope its DHCP address pool, or null
export default function NetworkEditor({ zone, iface, scope, zones, onClose, onSaved }) {
  const pn = usePortNames();
  const { data: pr } = useData('/api/fw/presets');
  const presets = pr?.presets || [];
  const { data: sug } = useData('/api/networks/suggest');
  const { data: allPorts } = useData('/api/networks/suggest?all=1');
  const [run, busy] = useAction();
  const [z, setZ] = useState(zone ? { ...zone } : null);
  const [f, setF] = useState({ ...iface });
  const [sc, setSc] = useState(scope ? { ...scope } : null);
  const setZk = (k) => (v) => setZ((x) => ({ ...x, [k]: v }));
  const setFk = (k) => (v) => setF((x) => ({ ...x, [k]: v }));
  const setSk = (k) => (v) => setSc((x) => ({ ...x, [k]: v }));
  const pick = (p) => setZ((x) => ({ ...x, kind: p.kind, internet: p.internet, intra_allow: p.intra_allow, management: p.management }));

  const isBridge = iface.kind === 'bridge';
  const plain = iface.kind === 'phys';
  const movable = !isBridge;
  const home = iface.kind === 'vlan' ? iface.parent : iface.name;
  const [port, setPort] = useState(home);
  const [vlan, setVlan] = useState(String(iface.vlan || ''));
  const moved = movable && (port !== home || Number(vlan || 0) !== (iface.vlan || 0));
  const [mem, setMem] = useState(iface.members || []);
  const first = iface.members || [];
  const dropped = first.filter((m) => !mem.includes(m));
  const added = mem.filter((m) => !first.includes(m));
  const [extra, setExtra] = useState([]);
  const tick = (name, on) => setMem((x) => (on ? [...x.filter((m) => m !== name), name] : x.filter((m) => m !== name)));
  const tickExtra = (name, on) => setExtra((x) => (on ? [...x.filter((m) => m !== name), name] : x.filter((m) => m !== name)));
  const why = (p) => {
    if (mem.includes(p.name) || first.includes(p.name)) return '';
    if (p.use === 'internet') return 'used for the internet';
    if (p.use === 'bridge') return 'part of another network';
    if (p.use === 'network') return 'has a network of its own';
    return '';
  };
  const portRows = (list, checked, onTick) => (list || []).map((p) => {
    const reason = why(p);
    return (
      <label key={p.name} className="small" style={{ display: 'flex', gap: 'var(--space-2)', alignItems: 'center', opacity: reason ? 0.6 : 1 }}>
        <input type="checkbox" checked={checked(p.name)} disabled={!!reason} onChange={(e) => onTick(p.name, e.target.checked)} />
        <span className="mono">{pn(p.name)}</span>
        <span className="muted">{p.link ? `cable connected${p.speed > 0 ? `, ${p.speed} Mb/s` : ''}` : 'no cable'}{reason ? ` · ${reason}` : ''}</span>
      </label>
    );
  });
  const title = z?.name || iface.name;
  const needsPort = z && z.management && z.kind !== 'wan' && z.kind !== 'vpn' && !(z.ifaces || []).length;
  const bad = !!(z && !z.name.trim()) || (isBridge && mem.length === 0);

  const addPorts = async () => {
    const ok = await run(() => post('/api/networks/add-ports', { id: iface.id, ports: extra }), 'Saved. It goes live in a moment.');
    if (ok) { onSaved(); onClose(); }
  };
  const save = async (later) => {
    const go = async () => {
      // The name and type first: the rest follows the new name.
      if (z) await put('/api/fw/zones', z);
      await put('/api/net/interfaces', isBridge ? { ...f, members: mem } : f);
      if (moved) await post('/api/networks/move', { id: iface.id, port, vlan: Number(vlan || 0) });
      if (sc && scope) await put('/api/dhcp/scopes', { ...sc, name: z ? z.name : sc.name });
    };
    const ok = await run(later ? () => holdAnd(go) : go, later ? SAVED_LATER : 'Saved.');
    if (ok) { onSaved(); onClose(); }
  };

  return (
    <Modal title={`Edit ${title}`} onClose={onClose}
      footer={<><Button onClick={onClose}>Cancel</Button><SaveChoice onSave={save} busy={busy} disabled={bad || needsPort} /></>}>
      <div className="stack">
        {z && <Field label="Name" hint="Rules, address pools, blocks and graphs follow a new name.">
          <Input value={z.name} onChange={setZk('name')} placeholder="Guests" />
        </Field>}
        {z && (
          <div className="field"><span className="lab">What kind of network is it?</span>
            <div className="stack">
              {presets.map((p) => (
                <button type="button" key={p.kind} className={`preset ${z.kind === p.kind ? 'on' : ''}`} onClick={() => pick(p)} aria-pressed={z.kind === p.kind}>
                  <span className="t">{p.title}</span><span className="d">{p.summary}</span>
                </button>
              ))}
            </div>
            {z.kind !== zone.kind && <span className="hint">The switches below were set to match {presets.find((p) => p.kind === z.kind)?.title || 'the new type'}. Change any of them before you save.</span>}
          </div>
        )}
        {!z && (
          <Field label="What it can reach" hint="The firewall network this belongs to. Change what each network can reach on the Firewall page.">
            <Select value={String(f.zone_id || 0)} onChange={(v) => setFk('zone_id')(Number(v))} options={[{ value: '0', label: 'Not assigned yet' }, ...(zones || []).map((x) => ({ value: String(x.id), label: x.name }))]} />
          </Field>
        )}

        <Field label="Address of this router on the network" hint="With the size, like 192.168.10.1/24. Devices here use it as their gateway.">
          <Input value={f.ipv4 || ''} onChange={setFk('ipv4')} className="in mono" />
        </Field>

        {sc ? (
          <>
            <Field label="Give devices addresses (DHCP)" hint={sc.mode === 'serve' && sc.range_start ? `Addresses ${sc.range_start} to ${sc.range_end}. Change the range with the DHCP button on the Networks page.` : undefined}>
              <Segmented value={sc.mode} onChange={setSk('mode')} options={[{ value: 'serve', label: 'NopeWarden does' }, { value: 'off', label: 'Off' }, { value: 'relay', label: 'Pass on to another server' }]} />
            </Field>
            {sc.mode === 'relay' && (
              <Field label="DHCP server to pass requests to" hint="Its address, for example 10.0.0.5.">
                <Input value={sc.relay_addr || ''} onChange={setSk('relay_addr')} className="in mono" />
              </Field>
            )}
            {sc.mode === 'serve' && (
              <Field label="How long a device keeps its address">
                <Segmented value={String(sc.lease_seconds || 43200)} onChange={(v) => setSk('lease_seconds')(Number(v))} options={LEASES.some((l) => l.value === String(sc.lease_seconds || 43200)) ? LEASES : [...LEASES, { value: String(sc.lease_seconds || 43200), label: `${Math.round((sc.lease_seconds || 43200) / 3600)} hours` }]} />
              </Field>
            )}
          </>
        ) : (
          <Note>DHCP is not set up for this network yet. Use the DHCP button on the Networks page to set it up.</Note>
        )}

        {z && <ToggleRow title="Can reach the internet" checked={z.internet} onChange={setZk('internet')} />}
        {z && <ToggleRow title="Devices here can talk to each other" desc={z.kind === 'guest' ? 'Off keeps guests from seeing each other. Turn on only if they need to, for example to cast to a TV.' : 'Off keeps each device to itself.'} checked={z.intra_allow} onChange={setZk('intra_allow')} />}
        {z && <ToggleRow title="Can manage NopeWarden" desc="Allows the console from this network. Keep at least one network on, so you can always get back in; NopeWarden refuses a change that would leave none." checked={z.management} onChange={setZk('management')} />}
        {needsPort && <p className="small errtext">A network that can manage NopeWarden needs a port. Turn Can manage NopeWarden off, or give it one.</p>}
        <ToggleRow title="In use" desc="Turn off to switch the network off without deleting it." checked={f.enabled} onChange={setFk('enabled')} />
        <Field label="IPv6">
          <Segmented value={f.pd ? 'auto' : 'off'} onChange={(v) => setFk('pd')(v === 'auto')} options={[{ value: 'off', label: 'Off' }, { value: 'auto', label: 'Automatic' }]} />
        </Field>

        {isBridge && (
          <Field label="Ports on this network" hint="Tick the ports that act as a switch for this network. Devices plugged into any ticked port share the same addresses. Nothing changes until you apply.">
            <div className="stack">
              {portRows((allPorts && allPorts.ports) || [], (n) => mem.includes(n), tick)}
              {mem.length === 0 && <Note tone="warn">A network needs at least one port.</Note>}
              {(dropped.length > 0 || added.length > 0) && <Note tone="warn">{dropped.length ? `Taking ${dropped.map(pn).join(', ')} out cuts off devices plugged into ${dropped.length === 1 ? 'it' : 'them'}. ` : ''}If you manage NopeWarden through one of these ports, keep another way in until you have applied and checked it.</Note>}
            </div>
          </Field>
        )}
        {plain && (
          <Field label="Add more ports to this network" hint="Devices on these ports share this network's addresses, like extra ports on a switch. Nothing changes until you apply.">
            <div className="stack">
              {portRows(((allPorts && allPorts.ports) || []).filter((p) => p.name !== iface.name), (n) => extra.includes(n), tickExtra)}
              {extra.length > 0 && <Note tone="warn">NopeWarden briefly restarts this network while it changes. If you manage NopeWarden through it, keep another way in until you have applied and checked it.</Note>}
              <div><Button small disabled={busy || extra.length === 0} onClick={addPorts}>Add {extra.length ? extra.map(pn).join(', ') : 'ports'} to this network</Button></div>
            </div>
          </Field>
        )}

        <details className="more">
          <summary>More options</summary>
          <div className="stack">
            {movable && (
              <>
                <Field label="Port" hint="The physical port this network is plugged into.">
                  <Select value={port} onChange={setPort} options={[...new Set([port, ...((sug && sug.ports) || []).map((p) => p.name)])].map((x) => ({ value: x, label: pn(x) }))} />
                </Field>
                <Field label="VLAN number" hint="Leave empty to use the whole port. Your switch must pass the number you choose.">
                  <Input value={vlan} onChange={(v) => setVlan(v.replace(/\D/g, ''))} className="in mono" />
                </Field>
                {moved && <Note tone="warn">Moving a network to another port or VLAN cuts devices off until they are on the new one. If you manage NopeWarden from this network, keep another way in until you have applied and checked it.</Note>}
              </>
            )}
            <p className="small muted">
              {iface.kind === 'vlan' ? <>This network is a VLAN: number {iface.vlan} on port {pn(iface.parent)}. Your switch must pass that number.</> : isBridge ? <>This network spreads over several ports ({(iface.members || []).map(pn).join(', ')}).</> : <>This network uses the whole port {pn(iface.name)}.</>}
            </p>
            {z && (
              <>
                <ToggleRow title="NopeWarden is the gateway" desc="Off if another router on this network does that job." checked={z.gateway} onChange={setZk('gateway')} />
                <ToggleRow title="Make devices here use the enforced DNS server" desc="Catches gadgets with a DNS server built in, such as smart TVs and cameras. Turn off for a network that needs its own DNS. A different server for this network, and exceptions for single devices, are under Protection." checked={z.dns_force} onChange={setZk('dns_force')} />
                <ToggleRow title="Allow remote login (SSH)" checked={z.ssh} onChange={setZk('ssh')} />
                <p className="small muted">Letting devices open ports by themselves (UPnP) is set in one place: Firewall, Open ports.</p>
              </>
            )}
          </div>
        </details>
      </div>
    </Modal>
  );
}

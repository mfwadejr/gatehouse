import React, { useEffect, useRef, useState } from 'react';
import { del, put, useData } from '../api.js';
import ApplyBar from '../ApplyBar.jsx';
import WanGroups, { Traffic } from './WanGroups.jsx';
import { Button, Card, Empty, Field, Input, Modal, NumberInput, Note, Page, Pill, Segmented, Select, ToggleRow, useAction, useToast, fmt } from '../ui.jsx';

const TYPES = [
  { value: 'dhcp', label: 'Automatic' },
  { value: 'pppoe', label: 'Username and password' },
  { value: 'static', label: 'Fixed address' },
];

// Watches the network ports and reports the one whose cable was just plugged in.
export function usePortWatch(active) {
  const { data } = useData('/api/net/ports', { every: active ? 1500 : 5000, enabled: true });
  const base = useRef(null);
  const [found, setFound] = useState('');
  useEffect(() => {
    if (!active) { base.current = null; setFound(''); return; }
    if (!data) return;
    if (!base.current) { base.current = Object.fromEntries(data.map((p) => [p.name, p.link])); return; }
    const hit = data.find((p) => p.link && base.current[p.name] === false);
    if (hit) setFound(hit.name);
  }, [data, active]);
  return { ports: data || [], found };
}

export function PortPicker({ value, onChange, ports, found, exclude = [] }) {
  const options = ports.filter((p) => !exclude.includes(p.name));
  if (!options.length) return <Input value={value} onChange={onChange} placeholder="eth0" />;
  return (
    <Select value={value} onChange={onChange}
      options={[{ value: '', label: 'Choose a port…' }, ...options.map((p) => ({ value: p.name, label: `${p.name} — ${p.wifi ? 'Wi-Fi card' : p.link ? 'cable connected' : 'no cable'}${p.speed && p.speed > 0 ? `, ${p.speed} Mb/s` : ''}${p.usb ? ', USB adapter' : ''}${found === p.name ? ' (just plugged in)' : ''}` }))]} />
  );
}

function WanForm({ initial, ports, found, onClose, onSaved, wans }) {
  const [w, setW] = useState({ type: 'dhcp', vlan: 0, v6_mode: 'slaac', enabled: true, priority: (wans?.length || 0) + 1, weight: 1, ...initial });
  const [adv, setAdv] = useState(false);
  const [run, busy] = useAction();
  const set = (k) => (v) => setW((x) => ({ ...x, [k]: v }));
  const save = async () => {
    const ok = await run(() => put('/api/net/wans', w), 'Saved. Apply it to start using it.');
    if (ok) { onSaved(); onClose(); }
  };
  return (
    <Modal title={w.id ? `Edit ${w.name}` : 'Add an internet connection'} onClose={onClose}
      footer={<><Button onClick={onClose}>Cancel</Button><Button kind="primary" onClick={save} disabled={busy}>Save</Button></>}>
      <Field label="Name" hint="For example the name of your provider."><Input value={w.name} onChange={set('name')} placeholder="My provider" /></Field>
      <Field label="Which port is the internet cable plugged into?"><PortPicker value={w.iface} onChange={set('iface')} ports={ports} found={found} /></Field>
      <Field label="How does your provider connect you?">
        <Segmented value={w.type} onChange={set('type')} options={TYPES} />
      </Field>
      {w.type === 'dhcp' && <p className="sec small">Most home and office connections work this way. Nothing else to enter.</p>}
      {w.type === 'pppoe' && (
        <div className="formgrid">
          <Field label="Username"><Input value={w.pppoe_user} onChange={set('pppoe_user')} autoComplete="off" /></Field>
          <Field label="Password" hint={w.has_pass ? 'Leave empty to keep the saved password.' : undefined}><Input type="password" value={w.pppoe_pass} onChange={set('pppoe_pass')} autoComplete="new-password" /></Field>
        </div>
      )}
      {w.type === 'static' && (
        <div className="formgrid">
          <Field label="Address" hint="With the prefix, like 203.0.113.10/29"><Input value={w.addr} onChange={set('addr')} /></Field>
          <Field label="Gateway"><Input value={w.gateway} onChange={set('gateway')} /></Field>
          <Field label="DNS servers" hint="Optional, comma separated"><Input value={w.dns} onChange={set('dns')} /></Field>
        </div>
      )}
      <ToggleRow title="Let devices open ports on this connection (UPnP)" desc="For games and media apps. Also needs to be switched on for a network under Firewall." checked={!!w.upnp} onChange={set('upnp')} />
      <button className="link small" onClick={() => setAdv(!adv)}>{adv ? 'Hide' : 'Show'} advanced options</button>
      {adv && (
        <div className="formgrid">
          <Field label="VLAN number" hint="Only if your provider requires one. 0 for none."><NumberInput value={w.vlan} onChange={set('vlan')} /></Field>
          <Field label="IPv6"><Select value={w.v6_mode} onChange={set('v6_mode')} options={[{ value: 'none', label: 'Off' }, { value: 'slaac', label: 'Automatic' }, { value: 'pd', label: 'Automatic with a prefix for my networks' }, { value: 'static', label: 'Fixed address' }]} /></Field>
          {w.v6_mode === 'static' && <><Field label="IPv6 address"><Input value={w.v6_addr} onChange={set('v6_addr')} /></Field><Field label="IPv6 gateway"><Input value={w.v6_gateway} onChange={set('v6_gateway')} /></Field></>}
          <Field label="Check by" hint="How Gatehouse knows this connection works."><Select value={w.check_kind} onChange={set('check_kind')} options={[{ value: 'icmp', label: 'Ping' }, { value: 'tcp', label: 'Open a connection' }]} /></Field>
          <Field label="Check against" hint={w.check_kind === 'tcp' ? 'host:port' : 'An address that is almost always up'}><Input value={w.check_target} onChange={set('check_target')} placeholder="1.1.1.1" /></Field>
          <Field label="Order" hint="1 is used first."><NumberInput value={w.priority} onChange={set('priority')} /></Field>
          <Field label="Share of traffic" hint="Used when sharing load."><NumberInput value={w.weight} onChange={set('weight')} /></Field>
          <Field label="Packet size (MTU)" hint="0 for automatic"><NumberInput value={w.mtu} onChange={set('mtu')} /></Field>
        </div>
      )}
    </Modal>
  );
}

function QuickSetup({ ports, found, onDone }) {
  const [step, setStep] = useState(0);
  const [wan, setWan] = useState({ name: 'Internet', iface: '', type: 'dhcp', pppoe_user: '', pppoe_pass: '', addr: '', gateway: '' });
  const [lan, setLan] = useState({ iface: '', cidr: '192.168.1.1/24' });
  const [run, busy] = useAction();
  const sw = (k) => (v) => setWan((x) => ({ ...x, [k]: v }));
  useEffect(() => { if (found && step === 0 && !wan.iface) sw('iface')(found); /* eslint-disable-next-line */ }, [found]);
  useEffect(() => { if (step === 1 && !lan.iface) { const c = ports.find((p) => p.name !== wan.iface && p.link) || ports.find((p) => p.name !== wan.iface); if (c) setLan((x) => ({ ...x, iface: c.name })); } /* eslint-disable-next-line */ }, [step]);

  const finish = async () => {
    const [ip, bits] = lan.cidr.split('/');
    const o = ip.split('.').map(Number);
    const net = `${o[0]}.${o[1]}.${o[2]}.0/${bits}`;
    const ok = await run(async () => {
      await put('/api/net/wans', { ...wan, priority: 1, enabled: true });
      await put('/api/net/interfaces', { name: lan.iface, kind: 'phys', ipv4: lan.cidr, enabled: true });
      await put('/api/fw/zones', { name: 'Home', kind: 'trusted', ifaces: [lan.iface], internet: true, management: true, gateway: true, intra_allow: true });
      await put('/api/dhcp/scopes', { name: 'Home', iface: lan.iface, mode: 'serve', subnet: net, range_start: `${o[0]}.${o[1]}.${o[2]}.100`, range_end: `${o[0]}.${o[1]}.${o[2]}.200`, router: ip, dns: ip, domain: 'lan', ntp: '', lease_seconds: 43200, relay_addr: '', enabled: true });
      return true;
    }, 'Everything is set up. Review it and apply.');
    if (ok) onDone();
  };

  const wanOK = wan.iface && wan.name && (wan.type !== 'pppoe' || (wan.pppoe_user && wan.pppoe_pass)) && (wan.type !== 'static' || (wan.addr && wan.gateway));
  const lanOK = lan.iface && lan.iface !== wan.iface && /^\d+\.\d+\.\d+\.\d+\/\d+$/.test(lan.cidr);
  return (
    <Card title={step === 0 ? 'Step 1 of 2 · Connect to the internet' : 'Step 2 of 2 · Your own network'} desc="Gatehouse does the rest: addressing, DNS, and a safe firewall.">
      {step === 0 ? (
        <>
          <Note tone="good" title="Plug the internet cable in now">Plug the cable from your modem or provider into the port you want to use. Gatehouse will notice which port it is.</Note>
          <Field label="Port"><PortPicker value={wan.iface} onChange={sw('iface')} ports={ports} found={found} /></Field>
          <Field label="How does your provider connect you?"><Segmented value={wan.type} onChange={sw('type')} options={TYPES} /></Field>
          {wan.type === 'pppoe' && <div className="formgrid"><Field label="Username"><Input value={wan.pppoe_user} onChange={sw('pppoe_user')} autoComplete="off" /></Field><Field label="Password"><Input type="password" value={wan.pppoe_pass} onChange={sw('pppoe_pass')} autoComplete="new-password" /></Field></div>}
          {wan.type === 'static' && <div className="formgrid"><Field label="Address" hint="Like 203.0.113.10/29"><Input value={wan.addr} onChange={sw('addr')} /></Field><Field label="Gateway"><Input value={wan.gateway} onChange={sw('gateway')} /></Field></div>}
          <div className="actions"><Button kind="primary" disabled={!wanOK} onClick={() => setStep(1)}>Next</Button></div>
        </>
      ) : (
        <>
          <Field label="Which port connects to your switch or devices?"><PortPicker value={lan.iface} onChange={(v) => setLan((x) => ({ ...x, iface: v }))} ports={ports} found="" exclude={[wan.iface]} /></Field>
          <Field label="Address of this device on your network" hint="The usual 192.168.1.1/24 is fine unless it clashes with another network you use."><Input value={lan.cidr} onChange={(v) => setLan((x) => ({ ...x, cidr: v }))} /></Field>
          <p className="sec small">This creates a trusted home network that can manage Gatehouse, hands out addresses from .100 to .200 automatically, and protects it from the internet.</p>
          <div className="actions"><Button onClick={() => setStep(0)}>Back</Button><Button kind="primary" disabled={!lanOK || busy} onClick={finish}>Set everything up</Button></div>
        </>
      )}
    </Card>
  );
}

function linkTone(l) { if (!l) return ['', 'Not checked yet']; if (!l.up) return ['bad', 'Down']; return ['good', l.active ? 'Working' : 'Standby']; }

export default function Internet({ me }) {
  const { data: wd, reload: rw } = useData('/api/net/wans', { topics: ['net.status'] });
  const { data: ifs, reload: ri } = useData('/api/net/interfaces', { topics: ['net.status'] });
  const { data: st, reload: rs } = useData('/api/net/status', { topics: ['net.status'], every: 4000 });
  const { data: zones } = useData('/api/fw/zones', { topics: ['fw.status'] });
  const { data: fwo, reload: rfw } = useData('/api/fw/overview', { topics: ['fw.status'] });
  const [edit, setEdit] = useState(null);
  const [run] = useAction();
  const wans = wd?.wans || [];
  const isNew = wd && ifs && wans.length === 0 && ifs.length === 0;
  const { ports, found } = usePortWatch(isNew || !!edit);
  const all = () => { rw(); ri(); rs(); };
  const links = st?.links || {};
  const stat = st?.status;

  return (
    <Page title="Internet" sub="How this device connects to your internet provider. Your own networks are on the Networks page.">
      {wd && ifs && isNew && <QuickSetup ports={ports} found={found} onDone={all} />}
      {!stat?.helper && stat && <Note tone="warn" title="Network changes are unavailable">The network helper is not running, so connections can be viewed but not changed.</Note>}

      {wans.length > 0 || !isNew ? (
        <Card title="Internet connections" actions={<Button icon="plus" onClick={() => setEdit({})}>Add connection</Button>}
          desc={wans.length > 1 ? 'Choose how these share the work, or send certain devices through specific ones, in Connection groups below.' : undefined}>
          {wans.length === 0 ? <Empty>No connection yet.</Empty> : (
            <div className="list">
              {wans.map((w) => {
                const [tone, text] = linkTone(links[w.name]);
                return (
                  <div className="item" key={w.id}>
                    <div>
                      <div className="name">{w.name} {!w.enabled && <Pill>Off</Pill>}</div>
                      <div className="meta">{w.iface}{w.vlan ? ` · VLAN ${w.vlan}` : ''} · {TYPES.find((t) => t.value === w.type)?.label}{links[w.name]?.gateway ? ` · gateway ${links[w.name].gateway}` : ''}{links[w.name]?.rtt ? ` · ${fmt.d1(links[w.name].rtt / 1e6)} ms` : ''}</div>
                    </div>
                    <div className="actions">
                      <Pill tone={tone}>{text}</Pill>
                      <Button small icon="edit" onClick={() => setEdit(w)}>Edit</Button>
                      <Button small kind="danger" icon="trash" onClick={() => { if (window.confirm(`Remove ${w.name}?`)) run(() => del(`/api/net/wans/${w.id}`)).then(all); }}>Remove</Button>
                    </div>
                  </div>
                );
              })}
            </div>
          )}
        </Card>
      ) : null}

      <WanGroups wans={wans} links={links} onChanged={all} />
      <Traffic wans={wans} links={links} />

      {edit && <WanForm initial={edit} wans={wans} ports={ports} found={found} onClose={() => setEdit(null)} onSaved={all} />}
      <ApplyBar kind="net" label="your network settings" status={stat} me={me} onChanged={all} />
      {!stat?.dirty && !stat?.pending && fwo?.status && <ApplyBar kind="fw" label="the firewall" status={fwo.status} me={me} onChanged={rfw} />}
    </Page>
  );
}

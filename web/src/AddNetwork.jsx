import React, { useEffect, useRef, useState } from 'react';
import { get, post } from './api.js';
import { Button, Field, Input, Modal, Note, Segmented, Select, ToggleRow, useAction } from './ui.jsx';

// One form for a new network: the port, its address, DHCP and what it may reach. Everything has a
// safe default, so the usual case is a name and a port.
export default function AddNetwork({ onClose, onCreated }) {
  const [run, busy] = useAction();
  const [sug, setSug] = useState(null);
  const [f, setF] = useState({ name: '', port: '', vlan: 0, address: '', dhcp: 'server', relay: '', internet: true, isolate: true, lease: '1d', ipv6: 'off' });
  const [chk, setChk] = useState(null);
  const [chkFailed, setChkFailed] = useState(false);
  const [sugFailed, setSugFailed] = useState(false);
  const set = (k) => (v) => setF((x) => ({ ...x, [k]: v }));
  const timer = useRef(null);

  useEffect(() => {
    get('/api/networks/suggest').then((s) => {
      setSug(s);
      const free = s.ports.find((p) => !p.use);
      setF((x) => ({ ...x, address: s.address || '', port: (free || s.ports[0])?.name || '', vlan: free ? 0 : s.vlan }));
      setChk({ ok: true, router: s.address, summary: s.summary });
    }).catch(() => { setSug({ ports: [] }); setSugFailed(true); });
  }, []);

  // A live summary of what the address means, as it is typed.
  useEffect(() => {
    if (!f.address) return undefined;
    clearTimeout(timer.current);
    timer.current = setTimeout(() => {
      get(`/api/networks/check?address=${encodeURIComponent(f.address)}`).then((c) => { setChk(c); setChkFailed(false); }).catch(() => { setChk(null); setChkFailed(true); });
    }, 250);
    return () => clearTimeout(timer.current);
  }, [f.address]);

  // What the network is for sets the two switches below it; changing a switch by hand leaves none selected.
  const purposes = { own: { internet: true, isolate: false }, guest: { internet: true, isolate: true }, iot: { internet: false, isolate: true } };
  const purpose = Object.keys(purposes).find((k) => purposes[k].internet === f.internet && purposes[k].isolate === f.isolate) || '';
  const port = sug?.ports?.find((p) => p.name === f.port);
  const shared = !!port?.use; // the port already has something on it, so this network rides on a VLAN
  useEffect(() => {
    if (!sug) return;
    setF((x) => ({ ...x, vlan: shared ? (x.vlan || sug.vlan) : 0 }));
  }, [shared]); // eslint-disable-line react-hooks/exhaustive-deps

  const save = async () => {
    const body = { ...f, vlan: Number(f.vlan) || 0 };
    const r = await run(() => post('/api/networks/add', body));
    if (r) onCreated?.(f.name);
  };

  const portOptions = (sug?.ports || []).map((p) => ({
    value: p.name,
    label: `${p.name}${p.link ? ' · cable connected' : ''}${p.use === 'internet' ? ' · used for the internet' : p.use === 'network' ? ' · already has a network' : p.use === 'bridge' ? ' · part of a bridge' : ''}`,
  }));

  return (
    <Modal title="Add a network" onClose={onClose}
      footer={<><Button onClick={onClose}>Cancel</Button><Button kind="primary" onClick={save} disabled={busy || !f.name.trim() || !f.port || chk?.ok === false}>Add network</Button></>}>
      <div className="stack">
        {sugFailed && <Note tone="warn" title="Couldn't load suggestions">Enter the port and address by hand, or close this window and try again.</Note>}
        {sug?.first && <Note tone="good" title="This is your first network">It will also be allowed to manage this device, so you can always reach this console from it. You can change that later under Firewall.</Note>}
        <Field label="Name" hint="For example Home, Cameras, Guests or Office.">
          <Input value={f.name} onChange={set('name')} maxLength={40} autoComplete="off" />
        </Field>
        <Field label="What is this network for?" hint={purpose === 'own' ? 'Your own computers and phones. They can reach the internet and your other networks.' : purpose === 'guest' ? 'Visitors. Internet only; they cannot reach your other networks.' : purpose === 'iot' ? 'Cameras and smart-home gadgets. No internet and no other networks.' : 'Custom: set by the two switches further down.'}>
          <Segmented value={purpose} onChange={(k) => setF((x) => ({ ...x, ...purposes[k] }))} options={[{ value: 'own', label: 'My own devices' }, { value: 'guest', label: 'Guests' }, { value: 'iot', label: 'Cameras and smart-home' }]} />
        </Field>
        <Field label="Port" hint={shared ? 'This port already has a network, so the new one shares it using a VLAN. Your switch must pass that VLAN number.' : 'Plug the switch or devices for this network into this port.'}>
          <Select value={f.port} onChange={set('port')} options={portOptions.length ? portOptions : [{ value: '', label: 'No ports found' }]} />
        </Field>
        {shared && (
          <Field label="VLAN number" hint="Picked for you. Only change it if your switch already uses a number.">
            <Input value={f.vlan} onChange={set('vlan')} inputMode="numeric" style={{ maxWidth: '8rem' }} />
          </Field>
        )}
        <Field label="Address of this router on the network" hint={chk?.ok === false ? undefined : chk?.summary?.text}>
          <Input value={f.address} onChange={set('address')} className="in mono" placeholder="192.168.10.1/24" />
        </Field>
        {chk?.ok === false && <Note tone="warn">{chk.problem}</Note>}
        {chkFailed && <Note tone="warn" title="Couldn't check this address just now">Gatehouse checks it again when you add the network, and tells you then if there is a problem.</Note>}

        <Field label="Give devices addresses (DHCP)">
          <Segmented value={f.dhcp} onChange={set('dhcp')} options={[{ value: 'server', label: 'Gatehouse does' }, { value: 'off', label: 'Off' }, { value: 'relay', label: 'Pass on to another server' }]} />
        </Field>
        {f.dhcp === 'relay' && (
          <Field label="DHCP server to pass requests to" hint="Its address, for example 10.0.0.5.">
            <Input value={f.relay} onChange={set('relay')} className="in mono" />
          </Field>
        )}
        {f.dhcp === 'server' && (
          <Field label="How long a device keeps its address">
            <Segmented value={f.lease} onChange={set('lease')} options={[{ value: '1h', label: '1 hour' }, { value: '1d', label: '1 day' }, { value: '1w', label: '1 week' }]} />
          </Field>
        )}

        <ToggleRow title="Allow internet access" desc="Devices on this network can reach the internet." checked={f.internet} onChange={set('internet')} />
        <ToggleRow title="Keep this network separate" desc={f.isolate ? 'It cannot reach your other networks, and they cannot reach it.' : 'It and your other networks can reach each other.'} checked={f.isolate} onChange={set('isolate')} />
        <Field label="IPv6">
          <Segmented value={f.ipv6} onChange={set('ipv6')} options={[{ value: 'off', label: 'Off' }, { value: 'auto', label: 'Automatic' }]} />
        </Field>
        <Note>Gatehouse checks that an address is free before it hands it out, and warns you if another DHCP server answers on this network.</Note>
      </div>
    </Modal>
  );
}

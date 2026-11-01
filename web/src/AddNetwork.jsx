import React, { useEffect, useRef, useState } from 'react';
import { get, post } from './api.js';
import { Button, Field, Input, Modal, Note, Segmented, Select, ToggleRow, useAction } from './ui.jsx';

// One form for a new network: the port, its address, DHCP and what it may reach. Everything has a
// safe default, so the usual case is a name and a port.
export default function AddNetwork({ onClose, onCreated }) {
  const [run, busy] = useAction();
  const [sug, setSug] = useState(null);
  const [f, setF] = useState({ name: '', port: '', vlan: 0, address: '', dhcp: 'server', relay: '', kind: 'private', internet: true, intra_allow: true, no_address: false, lease: '1d', ipv6: 'off' });
  const [asVlan, setAsVlan] = useState(false);
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

  // The kind of network is the firewall's own kind. It sets the two switches below; Custom lets you set them.
  const KINDS = [
    { value: 'trusted', label: 'Trusted', internet: true, intra: true, hint: 'Your own computers and phones. They can reach the internet and your other networks. Nothing else can start a connection to them.' },
    { value: 'private', label: 'Private', internet: true, intra: true, hint: 'Internet only. Cannot reach your other networks. Devices on it can talk to each other.' },
    { value: 'guest', label: 'Guest', internet: true, intra: false, hint: 'Visitors. Internet only, and their devices cannot see each other or your other networks.' },
    { value: 'isolated', label: 'Isolated', internet: false, intra: true, hint: 'Cameras and smart-home gadgets. No internet and no other networks.' },
    { value: 'custom', label: 'Custom', internet: true, intra: true, hint: 'Starts with nothing allowed between networks. Set the switches below, then write your own rules.' },
  ];
  const kind = KINDS.find((k) => k.value === f.kind) || KINDS[1];
  const chooseKind = (v) => { const k = KINDS.find((x) => x.value === v); setF((x) => ({ ...x, kind: v, internet: k.internet, intra_allow: k.intra })); };
  const port = sug?.ports?.find((p) => p.name === f.port);
  const shared = !!port?.use; // the port already has something on it, so this network has to ride on a VLAN
  const vlanOn = asVlan || shared;
  useEffect(() => {
    if (!sug) return;
    setF((x) => ({ ...x, vlan: vlanOn ? (Number(x.vlan) || sug.vlan) : 0, no_address: vlanOn ? x.no_address : false }));
  }, [vlanOn]); // eslint-disable-line react-hooks/exhaustive-deps

  const save = async () => {
    const body = { ...f, vlan: vlanOn ? Number(f.vlan) || 0 : 0 };
    const r = await run(() => post('/api/networks/add', body));
    if (r) onCreated?.(f.name);
  };

  const portOptions = (sug?.ports || []).map((p) => ({
    value: p.name,
    label: `${p.name}${p.link ? ' · cable connected' : ''}${p.use === 'internet' ? ' · used for the internet' : p.use === 'network' ? ' · already has a network' : p.use === 'bridge' ? ' · part of a bridge' : ''}`,
  }));

  return (
    <Modal title="Add a network" onClose={onClose}
      footer={<><Button onClick={onClose}>Cancel</Button><Button kind="primary" onClick={save} disabled={busy || !f.name.trim() || !f.port || (!f.no_address && chk?.ok === false)}>Add network</Button></>}>
      <div className="stack">
        {sugFailed && <Note tone="warn" title="Couldn't load suggestions">Enter the port and address by hand, or close this window and try again.</Note>}
        {sug?.first && <Note tone="good" title="This is your first network">It will also be allowed to manage this device, so you can always reach this console from it. You can change that later under Firewall.</Note>}
        <Field label="Name" hint="For example Home, Cameras, Guests or Office.">
          <Input value={f.name} onChange={set('name')} maxLength={40} autoComplete="off" />
        </Field>
        <Field label="What kind of network is it?" hint={kind.hint}>
          <Segmented value={f.kind} onChange={chooseKind} options={KINDS.map((k) => ({ value: k.value, label: k.label }))} />
        </Field>
        <Field label="Port" hint={shared ? 'This port already has a network, so the new one shares it using a VLAN. Your switch must pass that VLAN number.' : 'Plug the switch or devices for this network into this port.'}>
          <Select value={f.port} onChange={set('port')} options={portOptions.length ? portOptions : [{ value: '', label: 'No ports found' }]} />
        </Field>
        <Field label="How is it carried on the port?" hint={vlanOn ? 'As a VLAN: tagged traffic, so several networks can share one cable to a managed switch or access point.' : 'The whole port belongs to this network.'}>
          <Segmented value={vlanOn ? 'vlan' : 'port'} onChange={(v) => setAsVlan(v === 'vlan')} options={[{ value: 'port', label: 'The whole port', disabled: shared }, { value: 'vlan', label: 'A VLAN on the port' }]} />
        </Field>
        {vlanOn && (
          <Field label="VLAN number" hint="Picked for you. Only change it if your switch already uses a number.">
            <Input value={f.vlan} onChange={set('vlan')} inputMode="numeric" style={{ maxWidth: '8rem' }} />
          </Field>
        )}
        {vlanOn && <ToggleRow title="No address, no DHCP" desc="For a link to a switch or access point that gives out the addresses itself. Gatehouse makes the VLAN and its firewall network, and nothing else." checked={f.no_address} onChange={(v) => setF((x) => ({ ...x, no_address: v }))} />}
        {!f.no_address && (
          <Field label="Address of this router on the network" hint={chk?.ok === false ? undefined : chk?.summary?.text}>
            <Input value={f.address} onChange={set('address')} className="in mono" placeholder="192.168.10.1/24" />
          </Field>
        )}
        {!f.no_address && chk?.ok === false && <Note tone="warn">{chk.problem}</Note>}
        {!f.no_address && chkFailed && <Note tone="warn" title="Couldn't check this address just now">Gatehouse checks it again when you add the network, and tells you then if there is a problem.</Note>}

        {!f.no_address && <Field label="Give devices addresses (DHCP)">
          <Segmented value={f.dhcp} onChange={set('dhcp')} options={[{ value: 'server', label: 'Gatehouse does' }, { value: 'off', label: 'Off' }, { value: 'relay', label: 'Pass on to another server' }]} />
        </Field>}
        {!f.no_address && f.dhcp === 'relay' && (
          <Field label="DHCP server to pass requests to" hint="Its address, for example 10.0.0.5.">
            <Input value={f.relay} onChange={set('relay')} className="in mono" />
          </Field>
        )}
        {!f.no_address && f.dhcp === 'server' && (
          <Field label="How long a device keeps its address">
            <Segmented value={f.lease} onChange={set('lease')} options={[{ value: '1h', label: '1 hour' }, { value: '1d', label: '1 day' }, { value: '1w', label: '1 week' }]} />
          </Field>
        )}

        {f.kind === 'custom' && <ToggleRow title="Allow internet access" desc="Devices on this network can reach the internet." checked={f.internet} onChange={set('internet')} />}
        <ToggleRow title="Devices here can talk to each other" desc={f.kind === 'guest' ? 'Off keeps guests from seeing each other. Turn on only if they need to, for example to cast to a TV.' : 'Off keeps each device to itself.'} checked={f.intra_allow} onChange={set('intra_allow')} />
        {!f.no_address && <Field label="IPv6">
          <Segmented value={f.ipv6} onChange={set('ipv6')} options={[{ value: 'off', label: 'Off' }, { value: 'auto', label: 'Automatic' }]} />
        </Field>}
        <Note>Gatehouse checks that an address is free before it hands it out, and warns you if another DHCP server answers on this network.</Note>
      </div>
    </Modal>
  );
}

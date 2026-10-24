import React, { useState } from 'react';
import { del, get, put, useData } from '../api.js';
import ApplyBar from '../ApplyBar.jsx';
import { Button, Card, Empty, Field, Input, Modal, Note, Page, Pill, Segmented, Select, ToggleRow, useAction } from '../ui.jsx';

const COUNTRIES = {
  US: 'United States', CA: 'Canada', GB: 'United Kingdom', IE: 'Ireland', AU: 'Australia', NZ: 'New Zealand', DE: 'Germany', FR: 'France',
  ES: 'Spain', IT: 'Italy', NL: 'Netherlands', BE: 'Belgium', CH: 'Switzerland', AT: 'Austria', SE: 'Sweden', NO: 'Norway', DK: 'Denmark',
  FI: 'Finland', PL: 'Poland', PT: 'Portugal', CZ: 'Czechia', JP: 'Japan', KR: 'South Korea', SG: 'Singapore', IN: 'India', BR: 'Brazil',
  MX: 'Mexico', ZA: 'South Africa', AE: 'United Arab Emirates', IL: 'Israel',
};

const MODES = [
  { value: 'client', label: 'Join a Wi-Fi network' },
  { value: 'ap', label: 'Broadcast a Wi-Fi network' },
];

// A passphrase people can read out and type: lowercase words are long, this is four groups of letters.
function makePassword() {
  const abc = 'abcdefghjkmnpqrstuvwxyz23456789';
  const b = crypto.getRandomValues(new Uint8Array(16));
  const part = (o) => Array.from(b.slice(o, o + 4), (x) => abc[x % abc.length]).join('');
  return `${part(0)}-${part(4)}-${part(8)}-${part(12)}`;
}

function bars(dbm) { return dbm > -55 ? 'Excellent' : dbm > -67 ? 'Good' : dbm > -78 ? 'Fair' : 'Weak'; }

// What the card is doing right now, kept apart from what it was set up to do.
function Live({ c }) {
  const sv = c.saved, lv = c.live || {};
  const ap = sv.mode === 'ap';
  const addrs = (lv.addrs || []).join(', ');
  let tone = '', label;
  if (!sv.enabled) label = 'Switched off';
  else if (lv.linked) { tone = 'good'; label = ap ? 'Broadcasting' : 'Connected'; }
  else label = ap ? 'Starting the network…' : 'Not connected yet';
  return (
    <div className="stack">
      <div className="wg-head">
        <Pill tone={tone}>{label}</Pill>
        <span className="name">{sv.ssid}</span>
        <span className="small muted">
          {ap ? `broadcasting on ${sv.band} GHz` : 'joins this network for internet'} · {sv.country}
          {!ap && lv.linked && lv.signal ? ` · signal ${bars(lv.signal).toLowerCase()}` : ''}
        </span>
      </div>
      <div className="small">{addrs ? <>Address <span className="mono">{addrs}</span></> : <span className="muted">{lv.linked ? 'Waiting for an address…' : 'No address yet.'}</span>}</div>
      {!lv.linked && sv.enabled && <p className="small muted">If you just saved, apply the change first. If it stays like this, check the password and that the network is in range.</p>}
    </div>
  );
}

function Setup({ card, country, wans, ifs, onClose, onSaved }) {
  const s = card.saved;
  const canAP = card.caps.ap || card.caps.bands.length === 0; // unknown capabilities: let the driver decide
  const [f, setF] = useState({
    iface: card.iface, mode: s?.mode || 'client', ssid: s?.ssid || '', psk: '', country: s?.country || country || 'US',
    band: s?.band || (card.caps.bands.includes('5') ? '5' : '2.4'), channel: s?.channel || 0, hidden: !!s?.hidden, wpa3: !!s?.wpa3, enabled: true,
  });
  const [where, setWhere] = useState(ifs.find((i) => i.kind === 'bridge' && i.members?.length) ? 'bridge' : 'own');
  const bridges = ifs.filter((i) => i.kind === 'bridge');
  const [bridge, setBridge] = useState(bridges[0]?.name || '');
  const [cidr, setCidr] = useState(() => {
    const used = new Set(ifs.map((i) => (i.ipv4 || '').split('.')[2]));
    for (let n = 2; n < 250; n++) if (!used.has(String(n))) return `192.168.${n}.1/24`;
    return '192.168.2.1/24';
  });
  const [found, setFound] = useState(null);
  const [run, busy] = useAction();
  const set = (k) => (v) => setF((x) => ({ ...x, [k]: v }));
  const ap = f.mode === 'ap';

  const scan = async () => {
    const r = await run(() => get(`/api/net/wifi/${card.iface}/scan`));
    if (r) setFound(r);
  };

  const save = async () => {
    const ok = await run(async () => {
      if (!ap) {
        if (!wans.some((w) => w.iface === card.iface)) {
          await put('/api/net/wans', { name: `Wi-Fi ${f.ssid}`.slice(0, 32), iface: card.iface, type: 'dhcp', v6_mode: 'slaac', vlan: 0, enabled: true, priority: wans.length + 1, weight: 1 });
        }
      } else if (where === 'bridge') {
        const b = bridges.find((x) => x.name === bridge);
        if (!b) throw new Error('Choose which network the Wi-Fi joins.');
        if (!b.members.includes(card.iface)) await put('/api/net/interfaces', { ...b, members: [...b.members, card.iface] });
      } else if (!ifs.some((i) => i.name === card.iface)) {
        const [ip, bits] = cidr.split('/');
        const o = ip.split('.').map(Number);
        const net = `${o[0]}.${o[1]}.${o[2]}.0/${bits}`;
        await put('/api/net/interfaces', { name: card.iface, kind: 'phys', ipv4: cidr, enabled: true });
        await put('/api/fw/zones', { name: 'Wi-Fi', kind: 'trusted', ifaces: [card.iface], internet: true, management: false, gateway: true, intra_allow: true });
        await put('/api/dhcp/scopes', { name: 'Wi-Fi', iface: card.iface, mode: 'serve', subnet: net, range_start: `${o[0]}.${o[1]}.${o[2]}.100`, range_end: `${o[0]}.${o[1]}.${o[2]}.200`, router: ip, dns: ip, domain: 'lan', ntp: '', lease_seconds: 43200, relay_addr: '', enabled: true });
      }
      await put('/api/net/wifi', f);
      return true;
    }, 'Saved. Apply it to start using it.');
    if (ok) { onSaved(); onClose(); }
  };

  const ready = f.ssid && f.country && (s?.has_psk || f.psk || (!ap)) && (!ap || s?.has_psk || f.psk) && (!ap || (where === 'bridge' ? bridge : /^\d+\.\d+\.\d+\.\d+\/\d+$/.test(cidr)));
  return (
    <Modal title={`Wi-Fi card ${card.iface}`} onClose={onClose}
      footer={<><Button onClick={onClose}>Cancel</Button><Button kind="primary" onClick={save} disabled={busy || !ready}>Save</Button></>}>
      <Field label="What should this card do?">
        <Segmented value={f.mode} onChange={set('mode')} options={MODES.map((m) => ({ ...m, disabled: m.value === 'ap' && !canAP }))} />
      </Field>
      {!canAP && <Note tone="warn" title="This card cannot broadcast a network">Many cards can only join networks. You can still use it as an internet connection.</Note>}
      <p className="sec small">{ap ? 'Phones, laptops and other devices will connect to this device over the air, as they would to a Wi-Fi router.' : 'Use another Wi-Fi network, such as a hotspot, as an internet connection for this device. It appears under Internet and can be a backup for a cable.'}</p>

      {!ap && (
        <>
          <div className="actions"><Button onClick={scan} disabled={busy}>Look for networks</Button></div>
          {found && (
            <div className="stack">
              {found.length === 0 && <Empty>No networks found.</Empty>}
              {found.slice(0, 12).map((n) => (
                <button key={n.ssid} type="button" className={`wg-member${f.ssid === n.ssid ? ' on' : ''}`} onClick={() => { setF((x) => ({ ...x, ssid: n.ssid, wpa3: n.security === 'wpa3' })); setTimeout(() => document.getElementById('wifi-psk')?.focus(), 0); }}>
                  <span className="name grow">{n.ssid}</span>
                  <span className="small muted">{n.security === 'open' ? 'Open' : 'Password'} · {bars(n.signal)}</span>
                </button>
              ))}
            </div>
          )}
        </>
      )}
      <Field label="Network name"><Input value={f.ssid} onChange={set('ssid')} placeholder={ap ? 'Gatehouse' : 'Name of the network to join'} autoComplete="off" /></Field>
      <Field label="Password" hint={s?.has_psk ? 'Leave empty to keep the saved password.' : ap ? 'At least 8 characters.' : 'Leave empty for an open network.'}>
        <div className="actions" style={{ flexWrap: 'nowrap' }}>
          <Input id="wifi-psk" type={ap ? 'text' : 'password'} value={f.psk} onChange={set('psk')} autoComplete="new-password" />
          {ap && <Button onClick={() => set('psk')(makePassword())}>Make one</Button>}
        </div>
      </Field>
      <Field label="Country this device is in" hint="Sets which channels and power levels are legal.">
        <Select value={f.country} onChange={set('country')} options={[...(COUNTRIES[f.country] ? [] : [{ value: f.country, label: f.country }]), ...Object.entries(COUNTRIES).map(([v, l]) => ({ value: v, label: l }))]} />
      </Field>
      {ap && (
        <>
          <Field label="Speed band" hint="5 GHz is faster and less crowded; 2.4 GHz reaches farther.">
            <Segmented value={f.band} onChange={set('band')} options={[{ value: '5', label: '5 GHz' }, { value: '2.4', label: '2.4 GHz' }].filter((o) => !card.caps.bands.length || card.caps.bands.includes(o.value))} />
          </Field>
          <Field label="Where do the devices on this Wi-Fi connect?">
            <Segmented value={where} onChange={setWhere} options={[{ value: 'own', label: 'Its own network' }, ...(bridges.length ? [{ value: 'bridge', label: 'Join an existing network' }] : [])]} />
          </Field>
          {where === 'own' ? (
            <Field label="Address of this device on the Wi-Fi network" hint="Gatehouse will also hand out addresses and protect it with the firewall."><Input value={cidr} onChange={setCidr} /></Field>
          ) : (
            <Field label="Network"><Select value={bridge} onChange={setBridge} options={bridges.map((b) => ({ value: b.name, label: `${b.name} (${(b.members || []).join(', ')})` }))} /></Field>
          )}
          <ToggleRow title="Allow WPA3" desc="Newer, stronger security. Older devices still connect with WPA2." checked={f.wpa3} onChange={set('wpa3')} />
        </>
      )}
      <ToggleRow title="Hide the network name" desc={ap ? 'Devices must type the name to join. This adds little real security.' : 'The network does not announce its name.'} checked={f.hidden} onChange={set('hidden')} />
    </Modal>
  );
}

export default function Wifi({ me }) {
  const { data, reload } = useData('/api/net/wifi', { topics: ['net.status'], every: 5000 });
  const { data: wd, reload: rw } = useData('/api/net/wans', { topics: ['net.status'] });
  const { data: ifs, reload: ri } = useData('/api/net/interfaces', { topics: ['net.status'] });
  const { data: st, reload: rs } = useData('/api/net/status', { topics: ['net.status'], every: 5000 });
  const [edit, setEdit] = useState(null);
  const [forget, setForget] = useState(null);
  const [run] = useAction();
  const cards = data?.cards || [];
  const all = () => { reload(); rw(); ri(); rs(); };

  const remove = async (c) => {
    await run(async () => {
      await del(`/api/net/wifi/${c.iface}`);
      for (const w of (wd?.wans || []).filter((x) => x.iface === c.iface)) await del(`/api/net/wans/${w.id}`);
      for (const i of (ifs || []).filter((x) => x.name === c.iface)) await del(`/api/net/interfaces/${i.id}`);
      return true;
    }, c.saved?.mode === 'ap' ? 'Removed. Apply it to take effect.' : 'Forgotten. Apply it to take effect.');
    all();
  };

  return (
    <Page title="Wi-Fi" sub="Use a wireless card to join another network for internet, or to broadcast a network of your own.">
      {data && cards.length === 0 && (
        <Card><Empty>No wireless card found in this device. A USB Wi-Fi adapter also works; plug it in and it will appear here.</Empty></Card>
      )}
      {cards.map((c) => (
        <Card key={c.iface} title={c.iface} desc={`${c.usb ? 'USB adapter' : 'Built-in card'}${c.driver ? ` · ${c.driver}` : ''}${c.caps.bands.length ? ` · ${c.caps.bands.map((b) => `${b} GHz`).join(' and ')}` : ''}`}
          actions={<>
            <Button icon="edit" small onClick={() => setEdit(c)}>{c.saved ? 'Edit' : 'Set up'}</Button>
            {c.saved && (c.saved.mode === 'ap'
              ? <Button icon="trash" small onClick={() => remove(c)}>Remove</Button>
              : <Button icon="trash" small onClick={() => setForget(c)}>Forget network</Button>)}
          </>}>
          {!c.saved ? <p className="sec">Not set up. Nothing is broadcast and nothing is joined until you choose.</p> : <Live c={c} />}
          {!c.caps.ap && c.caps.bands.length > 0 && <p className="small muted">This card can join networks but cannot broadcast one.</p>}
        </Card>
      ))}
      {forget && (
        <Modal title={`Forget ${forget.saved.ssid}?`} onClose={() => setForget(null)}
          footer={<><Button onClick={() => setForget(null)}>Keep it</Button><Button kind="primary" onClick={async () => { const c = forget; setForget(null); await remove(c); }}>Forget network</Button></>}>
          <p>This card will stop using <b>{forget.saved.ssid}</b>. The saved password is deleted, and so is the internet connection that used this network. The card itself stays, ready to join a different network with Set up.</p>
          <p className="small muted">Nothing changes until you apply it.</p>
        </Modal>
      )}
      {edit && <Setup card={edit} country={data?.country} wans={wd?.wans || []} ifs={ifs || []} onClose={() => setEdit(null)} onSaved={all} />}
      <ApplyBar kind="net" label="your network settings" status={st?.status} me={me} onChanged={all} />
    </Page>
  );
}

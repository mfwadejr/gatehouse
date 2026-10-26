import React, { useState } from 'react';
import { del, get, post, put, useData } from '../api.js';
import ApplyBar from '../ApplyBar.jsx';
import { Button, Card, Empty, Field, Input, Modal, Note, Page, Pill, Segmented, Select, ToggleRow, useAction } from '../ui.jsx';

export const COUNTRIES = {
  US: 'United States', CA: 'Canada', GB: 'United Kingdom', IE: 'Ireland', AU: 'Australia', NZ: 'New Zealand', DE: 'Germany', FR: 'France',
  ES: 'Spain', IT: 'Italy', NL: 'Netherlands', BE: 'Belgium', CH: 'Switzerland', AT: 'Austria', SE: 'Sweden', NO: 'Norway', DK: 'Denmark',
  FI: 'Finland', PL: 'Poland', PT: 'Portugal', CZ: 'Czechia', JP: 'Japan', KR: 'South Korea', SG: 'Singapore', IN: 'India', BR: 'Brazil',
  MX: 'Mexico', ZA: 'South Africa', AE: 'United Arab Emirates', IL: 'Israel',
};

// Who uses a Wi-Fi network decides its firewall network: what it may reach and whether it gets the internet.
const WHO = {
  own: { name: 'Wi-Fi', kind: 'trusted', internet: true, intra: true },
  guest: { name: 'Guest Wi-Fi', kind: 'guest', internet: true, intra: false },
  iot: { name: 'Smart-home Wi-Fi', kind: 'isolated', internet: false, intra: true },
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

const STD = { n: 'Wi-Fi 4', ac: 'Wi-Fi 5', ax: 'Wi-Fi 6', be: 'Wi-Fi 7' };

// What a card can do, in words. A band the card lists but may not broadcast on says so.
function bandsText(c) {
  const caps = c.caps;
  const parts = (caps.listed?.length ? caps.listed : caps.bands).map((b) => {
    if (!caps.ap) return `${b} GHz`;
    if (b === '6') return '6 GHz (join only for now)';
    return caps.ap_bands.includes(b) ? `${b} GHz` : `${b} GHz (join only until a country allows it)`;
  });
  return parts.join(' and ') + (caps.std && STD[caps.std] ? ` · ${STD[caps.std]}` : '');
}

function bars(dbm) { return dbm > -55 ? 'Excellent' : dbm > -67 ? 'Good' : dbm > -78 ? 'Fair' : 'Weak'; }

// A card that broadcasts does not ask for an address: it carries a network, and the router's
// address lives on that network (or on the bridge the card belongs to).
function Serves({ lv, enabled }) {
  const sv = lv.serves;
  if (!enabled) return null;
  if (!sv) {
    return <div className="small" style={{ color: 'var(--color-status-warning)' }}>This card is broadcasting, but no network is attached to it, so devices that join get no address. Add it to a network under Networks, or put it in a bridge.</div>;
  }
  const name = sv.zone || sv.name;
  const addr = sv.addr ? sv.addr.replace(/\/\d+$/, '') : '';
  return (
    <div className="small">
      Carries the network <b>{name}</b>{sv.via ? <> through the bridge <span className="mono">{sv.via}</span></> : null}
      {addr ? <> · router address <span className="mono">{addr}</span></> : null}
    </div>
  );
}

// What the card says it can do next to what a test found. Only a test can say a band really works.
function CardCheck({ c, onDone }) {
  const [run, busy] = useAction();
  const listed = c.caps.listed?.length ? c.caps.listed : c.caps.bands;
  if (!listed.length) return null;
  const ck = c.check;
  const go = async () => { if (await run(() => post(`/api/net/wifi/${c.iface}/check`, {}), 'Card checked.')) onDone(); };
  return (
    <div className="stack">
      <table className="tbl small">
        <thead><tr><th>Band</th><th>Reported by the card</th><th>Tested</th></tr></thead>
        <tbody>
          {listed.map((b) => {
            const heard = ck?.bands?.[b]?.heard;
            return (
              <tr key={b}>
                <td>{b} GHz</td>
                <td>{c.caps.ap_bands?.includes(b) ? 'Join and broadcast' : 'Join'}</td>
                <td>{!ck ? <span className="muted">Not tested yet</span> : heard > 0 ? <Pill tone="good">Heard {heard} network{heard === 1 ? '' : 's'}</Pill> : <Pill tone="warn">Heard nothing</Pill>}</td>
              </tr>
            );
          })}
        </tbody>
      </table>
      {ck && listed.some((b) => !ck.bands?.[b]?.heard) && <p className="small muted">"Heard nothing" can mean there is simply nothing on that band nearby, or that the card does not really work there. Run the check again somewhere with more networks in range, or in a different place.</p>}
      <div className="inline">
        <Button small disabled={busy} onClick={go}>{ck ? 'Check again' : 'Check this card'}</Button>
        <span className="small muted">{ck ? `Last checked ${new Date(ck.at * 1000).toLocaleString()}.` : 'Listens on every band the card reports, for a few seconds.'} A card that is broadcasting cannot listen at the same time. Broadcasting itself is not tested here.</span>
      </div>
    </div>
  );
}

// What the card is doing right now, kept apart from what it was set up to do.
function Live({ c }) {
  const sv = c.saved, lv = c.live || {};
  const ap = sv.mode === 'ap';
  // Show the IPv4 address on the status line; IPv6 addresses stay out of sight until asked for.
  const all = lv.addrs || [];
  const v4 = all.filter((x) => !x.includes(':'));
  const v6 = all.filter((x) => x.includes(':'));
  const addrs = v4.join(', ');
  let tone = '', label;
  if (!sv.enabled) label = 'Switched off';
  else if (ap && lv.linked && c.health && !c.health.ok && !c.health.reason?.includes('coming back')) { tone = 'bad'; label = 'Not responding'; }
  else if (ap && lv.linked && c.health && !c.health.ok) { tone = 'warn'; label = 'Restarting…'; }
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
      {ap && sv.enabled && c.health && !c.health.ok && (
        <p className="small" style={{ color: 'var(--color-status-serious)' }}>
          The card says it is broadcasting, but {c.health.reason || 'it is not answering'}.
          {c.health.action ? ` Gatehouse ${c.health.action}.` : ' Gatehouse restarts it by itself if this lasts a minute.'}
        </p>
      )}
      {ap && sv.enabled && c.health?.ok && <div className="small muted">{c.health.stations} device{c.health.stations === 1 ? '' : 's'} connected{c.health.recoveries ? ` · restarted ${c.health.recoveries} time${c.health.recoveries === 1 ? '' : 's'} in the last hour` : ''}</div>}
      {ap && lv.linked && lv.channel > 0 && (
        <div className="small">
          Actually using channel <b>{lv.channel}</b> on {lv.band} GHz{lv.band !== sv.band ? <> <Pill tone="warn">not the {sv.band} GHz you asked for</Pill></> : null}
          {sv.channel > 0 && lv.channel !== sv.channel ? <> <Pill tone="warn">you chose channel {sv.channel}</Pill></> : null}
        </div>
      )}
      {ap ? <Serves lv={lv} enabled={sv.enabled} /> : (
        <div className="small">{addrs ? <>Address <span className="mono">{addrs}</span></> : <span className="muted">{lv.linked ? 'Waiting for an address…' : 'No address yet.'}</span>}</div>
      )}
      {v6.length > 0 && (
        <details className="small">
          <summary className="muted">{v6.length} IPv6 address{v6.length === 1 ? '' : 'es'}</summary>
          <div className="mono" style={{ overflowWrap: 'anywhere' }}>{v6.join(', ')}</div>
        </details>
      )}
      {!lv.linked && sv.enabled && <p className="small muted">If you just saved, apply the change first. If it stays like this, check the password and that the network is in range.</p>}
    </div>
  );
}

// The bands a card can broadcast on in a country. The driver's list is for the country in force
// now; for any other country only the hardware's bands are known, so they are all offered and the
// card is checked again once that country is applied.
function apBandsFor(card, cc) {
  const caps = card.caps;
  if (!caps.listed?.length) return ['5', '2.4'];
  if (card.reg && cc === card.reg) return caps.ap_bands;
  return caps.listed.filter((b) => b !== '6');
}

function Setup({ card, country, wans, ifs, onClose, onSaved }) {
  const s = card.saved;
  const canAP = card.caps.ap || card.caps.bands.length === 0; // unknown capabilities: let the driver decide
  const [f, setF] = useState({
    iface: card.iface, mode: s?.mode || 'client', ssid: s?.ssid || '', psk: '', country: s?.country || country || 'US',
    band: s?.band || (apBandsFor(card, country || 'US').includes('5') ? '5' : '2.4'), channel: s?.channel || 0, hidden: !!s?.hidden, wpa3: !!s?.wpa3, enabled: true,
  });
  // IPv6 on a joined network defaults to Off, as on most gateways; most hotspots and guest networks gain nothing from it.
  const [v6, setV6] = useState({ mode: 'none', addr: '', gateway: '' });
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
  const apBands = apBandsFor(card, f.country);
  const chans = f.country === card.reg ? (card.caps.channels || []).filter((c) => c.band === f.band && c.Usable !== false && !c.disabled && !c.no_ir && !c.radar) : [];

  const scan = async () => {
    const r = await run(() => get(`/api/net/wifi/${card.iface}/scan`));
    if (r) setFound(r);
  };

  const save = async () => {
    const ok = await run(async () => {
      if (!ap) {
        if (!wans.some((w) => w.iface === card.iface)) {
          await put('/api/net/wans', { name: `Wi-Fi ${f.ssid}`.slice(0, 32), iface: card.iface, type: 'dhcp', v6_mode: v6.mode, v6_addr: v6.mode === 'static' ? v6.addr : '', v6_gateway: v6.mode === 'static' ? v6.gateway : '', vlan: 0, enabled: true, priority: wans.length + 1, weight: 1 });
        }
      } else if (where === 'bridge') {
        const b = bridges.find((x) => x.name === bridge);
        if (!b) throw new Error('Choose which network the Wi-Fi joins.');
        if (!b.members.includes(card.iface)) await put('/api/net/interfaces', { ...b, members: [...b.members, card.iface] });
      } else if (!ifs.some((i) => i.name === card.iface)) {
        const [ip, bits] = cidr.split('/');
        const o = ip.split('.').map(Number);
        const net = `${o[0]}.${o[1]}.${o[2]}.0/${bits}`;
        const z = WHO[where] || WHO.own;
        await put('/api/net/interfaces', { name: card.iface, kind: 'phys', ipv4: cidr, enabled: true });
        await put('/api/fw/zones', { name: z.name, kind: z.kind, ifaces: [card.iface], internet: z.internet, management: false, gateway: true, intra_allow: z.intra });
        await put('/api/dhcp/scopes', { name: z.name, iface: card.iface, mode: 'serve', subnet: net, range_start: `${o[0]}.${o[1]}.${o[2]}.100`, range_end: `${o[0]}.${o[1]}.${o[2]}.200`, router: ip, dns: ip, domain: 'lan', ntp: '', lease_seconds: z.kind === 'guest' ? 3600 * 4 : 43200, relay_addr: '', enabled: true });
      }
      await put('/api/net/wifi', ap && apBands.length && !apBands.includes(f.band) ? { ...f, band: apBands[0], channel: 0 } : f);
      return true;
    }, 'Saved. Apply it to start using it.');
    if (ok) { onSaved(); onClose(); }
  };

  const v6ok = ap || wans.some((w) => w.iface === card.iface) || v6.mode !== 'static' || (v6.addr && v6.gateway);
  const ready = v6ok && f.ssid && f.country && (s?.has_psk || f.psk || (!ap)) && (!ap || s?.has_psk || f.psk) && (!ap || (where === 'bridge' ? bridge : /^\d+\.\d+\.\d+\.\d+\/\d+$/.test(cidr)));
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
      {!ap && !wans.some((w) => w.iface === card.iface) && (
        <Field label="IPv6" hint="Off is right for most hotspots and guest networks. Change it later under Internet.">
          <Select value={v6.mode} onChange={(m) => setV6((x) => ({ ...x, mode: m }))} options={[{ value: 'none', label: 'Off' }, { value: 'slaac', label: 'Automatic' }, { value: 'static', label: 'Fixed address' }]} />
        </Field>
      )}
      {!ap && !wans.some((w) => w.iface === card.iface) && v6.mode === 'static' && (
        <>
          <Field label="IPv6 address"><Input value={v6.addr} onChange={(x) => setV6((v) => ({ ...v, addr: x }))} placeholder="2001:db8::2/64" /></Field>
          <Field label="IPv6 gateway"><Input value={v6.gateway} onChange={(x) => setV6((v) => ({ ...v, gateway: x }))} placeholder="2001:db8::1" /></Field>
        </>
      )}
      <Field label="Country this device is in" hint="Sets which channels and power levels are legal.">
        <Select value={f.country} onChange={set('country')} options={[...(COUNTRIES[f.country] ? [] : [{ value: f.country, label: f.country }]), ...Object.entries(COUNTRIES).map(([v, l]) => ({ value: v, label: l }))]} />
      </Field>
      {ap && (
        <>
          {apBands.length === 0 ? (
            <Note tone="warn" title="This card cannot broadcast here">It may not start a network on any band in {f.country}. Choose another country, or use another card.</Note>
          ) : (
            <>
              <Field label="Speed band" hint="5 GHz is faster and less crowded; 2.4 GHz reaches farther.">
                <Segmented value={apBands.includes(f.band) ? f.band : apBands[0]} onChange={(v) => setF((x) => ({ ...x, band: v, channel: 0 }))} options={[{ value: '5', label: '5 GHz' }, { value: '2.4', label: '2.4 GHz' }].filter((o) => apBands.includes(o.value))} />
              </Field>
              {chans.length > 0 && (
                <Field label="Channel" hint={`${f.country === card.reg ? `Channels this card may use in ${card.reg}.` : `This list is for ${card.reg || 'the country now in force'}; it is read again once ${f.country} is applied.`} Automatic is the best choice for most people.`}>
                  <Select value={String(f.channel || 0)} onChange={(v) => set('channel')(Number(v))} options={[{ value: '0', label: 'Automatic' }, ...chans.map((c) => ({ value: String(c.num), label: `Channel ${c.num} (${c.mhz} MHz)` }))]} />
                </Field>
              )}
            </>
          )}
          <Field label="Who will use this Wi-Fi?" hint={where === 'bridge' ? 'Devices join the same network as your wired ones.' : where === 'guest' ? 'Visitors: internet only, they cannot reach your other networks.' : where === 'iot' ? 'Cameras and smart-home gadgets: no internet and no other networks.' : 'Your own phones and laptops: their own address range, able to reach your other networks.'}>
            <Segmented value={where} onChange={setWhere} options={[{ value: 'own', label: 'My own devices' }, ...(bridges.length ? [{ value: 'bridge', label: 'Same network as wired' }] : []), { value: 'guest', label: 'Guests' }, { value: 'iot', label: 'Smart-home gadgets' }]} />
          </Field>
          {where !== 'bridge' ? (
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

  // Switching a card off keeps all its settings; it only stops it from broadcasting or joining.
  const toggle = async (c) => {
    await run(() => put('/api/net/wifi', { ...c.saved, psk: '', enabled: !c.saved.enabled }), c.saved.enabled ? 'Switched off. Apply it to take effect.' : 'Switched on. Apply it to take effect.');
    all();
  };

  const [rep, setRep] = useState(null);
  const report = async (c) => {
    const r = await run(() => get(`/api/net/wifi/${c.iface}/report`));
    if (r) setRep({ iface: c.iface, text: r.report });
  };
  const keepAwake = async (on) => { await run(() => put('/api/net/wifi/keepawake', { on }), on ? 'The card is kept awake.' : 'Power saving is back to the default.'); reload(); };

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
        <Card key={c.iface} title={c.iface} desc={`${c.usb ? 'USB adapter' : 'Built-in card'}${c.driver ? ` · ${c.driver}` : ''}${c.caps.bands.length ? ` · ${bandsText(c)}` : ''}`}
          actions={<>
            <Button icon="edit" small onClick={() => setEdit(c)}>{c.saved ? 'Edit' : 'Set up'}</Button>
            {c.saved && c.saved.mode === 'ap' && <Button small onClick={() => report(c)}>Wi-Fi report</Button>}
            {c.saved && <Button small onClick={() => toggle(c)}>{c.saved.enabled ? 'Switch off' : 'Switch on'}</Button>}
            {c.saved && (c.saved.mode === 'ap'
              ? <Button icon="trash" small onClick={() => remove(c)}>Remove</Button>
              : <Button icon="trash" small onClick={() => setForget(c)}>Forget network</Button>)}
          </>}>
          {!c.saved ? <p className="sec">Not set up. Nothing is broadcast and nothing is joined until you choose.</p> : <Live c={c} />}
          <CardCheck c={c} onDone={all} />
          {!c.caps.ap && c.caps.bands.length > 0 && <p className="small muted">This card can join networks but cannot broadcast one.</p>}
        </Card>
      ))}
      {cards.some((c) => c.saved?.mode === 'ap') && (
        <Card title="Keep the card awake" desc="Some cards, Realtek ones in particular, can stop answering after a while when they save power. This turns power saving off for broadcasting cards. If something looks wrong with Wi-Fi after changing it, turn it off again.">
          <ToggleRow title="Turn off power saving" checked={!!data?.keep_awake} onChange={keepAwake} />
          <p className="small muted">Gatehouse also checks every 20 seconds that a broadcasting card is really serving. If it stops for a minute, Gatehouse restarts the Wi-Fi service, then reloads the card's driver, and writes each step to the Logs.</p>
        </Card>
      )}
      {rep && (
        <Modal title={`Wi-Fi report: ${rep.iface}`} onClose={() => setRep(null)}
          footer={<><Button onClick={() => navigator.clipboard?.writeText(rep.text)}>Copy</Button><Button kind="primary" onClick={() => setRep(null)}>Close</Button></>}>
          <p className="small muted">What the card, the access point and the kernel have said recently. Copy it if you need to ask for help.</p>
          <pre className="mono small" style={{ maxHeight: '55vh', overflow: 'auto', whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>{rep.text}</pre>
        </Modal>
      )}
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

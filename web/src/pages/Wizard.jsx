import React, { useEffect, useRef, useState } from 'react';
import { get, post } from '../api.js';
import { Button, Field, Icon, Input, Note, Pill, Segmented, Select, ToggleRow, enterToPrimary } from '../ui.jsx';
import { COUNTRIES } from './Wifi.jsx';

const baseAddr = (cidr) => (cidr || '').split('/')[0];

function Frame({ title, step, of = 3, children }) {
  const ref = useRef(null);
  useEffect(() => {
    const on = (e) => enterToPrimary(e, ref.current);
    document.addEventListener('keydown', on);
    return () => document.removeEventListener('keydown', on);
  }, []);
  return (
    <main className="auth">
      <section className="card" ref={ref} style={{ width: 'min(100%, 40rem)' }}>
        <div className="brand" style={{ padding: 0 }}><Icon name="logo" size={24} /><span>Gatehouse</span></div>
        {step && <div className="small muted">Step {step} of {of}</div>}
        <h1>{title}</h1>
        {children}
      </section>
    </main>
  );
}

function PortPick({ ports, value, onChange, taken = [], here }) {
  return (
    <div className="stack">
      {ports.map((p) => {
        const off = taken.includes(p.name);
        return (
          <button type="button" key={p.name} disabled={off} className={`preset ${value === p.name ? 'on' : ''}`} aria-pressed={value === p.name} onClick={() => onChange(p.name)}>
            <span className="t">
              {p.name}{p.usb ? ' (USB adapter)' : ''}{p.wifi ? ' (Wi-Fi)' : ''}{' '}
              {p.link ? <Pill tone="good">cable connected{p.speed && p.speed > 0 ? `, ${p.speed} Mb/s` : ''}</Pill> : <Pill>no cable</Pill>}
              {here === p.name ? <Pill tone="info">you are connected here</Pill> : null}
            </span>
            <span className="d mono">{p.mac}{off ? ' · used for something else' : ''}</span>
          </button>
        );
      })}
    </div>
  );
}

const empty = (lan) => ({ lan: { port: lan, mode: 'router', address: '', addressing: 'dhcp', static: '', gateway: '', dns: '' }, wan: null, bridge: [] });

// The first-run wizard: how this device is used on the network. Everything here can be changed later.
export default function Wizard({ data, me, onDone }) {
  const wired = (data.ports || []).filter((p) => !p.wifi);
  const wifiCards = (data.ports || []).filter((p) => p.wifi);
  const [found, setFound] = useState(null);
  const [scanning, setScanning] = useState(false);
  const [step, setStep] = useState('mode');
  const [net, setNet] = useState(() => {
    // The installer already asked which port is the internet and which is your network.
    const ins = data.install || {};
    const has = (name) => wired.some((p) => p.name === name);
    const n = empty((has(ins.lan_port) && ins.lan_port) || data.here || (wired.find((p) => p.link) || wired[0])?.name || '');
    n.lan.address = data.suggested_lan || '192.168.1.1/24';
    if (has(ins.wan_port) && ins.wan_port !== n.lan.port) n.wan = { type: 'dhcp', addr: '', gateway: '', dns: '', pppoe_user: '', pppoe_pass: '', port: ins.wan_port };
    return n;
  });
  const [host, setHost] = useState(data.hostname || 'gatehouse');
  const [zone, setZone] = useState(data.timezone || 'UTC');
  const [mfa, setMfa] = useState(false);
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState(null);
  const setLan = (k) => (v) => setNet((m) => ({ ...m, lan: { ...m.lan, [k]: v } }));
  const setWan = (k) => (v) => setNet((m) => ({ ...m, wan: { ...m.wan, [k]: v } }));
  const lan = net.lan;
  const wan = net.wan;
  const router = lan.mode === 'router';
  const order = router ? ['mode', 'internet', 'network', 'about'] : ['mode', 'network', 'about'];
  const sn = order.indexOf(step) + 1;

  // Arrived over the LAN cable itself while becoming the router: the connection will move. Say so plainly.
  const hereIsLan = data.here && data.here === lan.port;

  const okLan = router ? /^\d+\.\d+\.\d+\.\d+\/\d+$/.test(lan.address) : lan.addressing === 'dhcp' || (/\//.test(lan.static) && lan.gateway);
  const okWan = !wan || (wan.wifi ? !!(wan.port && wan.wifi.ssid.trim() && (!wan.wifi.psk || wan.wifi.psk.length >= 8) && wan.wifi.country) : wan.port && (wan.type === 'dhcp' || (wan.type === 'pppoe' && wan.pppoe_user && wan.pppoe_pass) || (wan.type === 'static' && wan.addr && wan.gateway)));
  const ready = lan.port && okLan && okWan;
  const others = wired.filter((p) => p.name !== lan.port && p.name !== wan?.port);

  const finish = async (skip) => {
    setErr(''); setBusy(true);
    try {
      const r = await post('/api/setup/wizard', skip ? { skip: true } : { network: net, hostname: host.trim(), timezone: zone, require_mfa: mfa });
      if (skip) onDone(); else setResult(r);
    } catch (e) { setErr(e.message); }
    finally { setBusy(false); }
  };

  if (result) {
    const addr = result.address;
    const url = addr ? `https://${addr}/` : '';
    return <Applied url={url} mode={result.mode} user={me.user.username} onDone={onDone} />;
  }

  if (step === 'mode') {
    return (
      <Frame title="How will Gatehouse be used?" step={sn} of={order.length}>
        <p className="sec">You can change any of this later. Nothing is switched on until you finish.</p>
        <div className="stack">
          <button type="button" className={`preset ${router ? 'on' : ''}`} aria-pressed={router} onClick={() => setLan('mode')('router')}>
            <span className="t">Gatehouse is my router</span>
            <span className="d">It hands out addresses to your devices (DHCP), runs DNS, and connects you to the internet.</span>
          </button>
          <button type="button" className={`preset ${!router ? 'on' : ''}`} aria-pressed={!router} onClick={() => setNet((m) => ({ ...m, lan: { ...m.lan, mode: 'behind' }, wan: null }))}>
            <span className="t">Gatehouse sits behind my existing router</span>
            <span className="d">Another router stays in charge of addresses and the internet. Gatehouse adds DNS filtering and a firewall for devices on this network.</span>
          </button>
        </div>
        <div className="actions"><Button onClick={() => finish(true)} disabled={busy}>Skip for now</Button><Button kind="primary" onClick={() => setStep(router ? 'internet' : 'network')}>Next</Button></div>
        {err && <Note tone="bad">{err}</Note>}
      </Frame>
    );
  }

  if (step === 'internet') {
    const cand = wired.filter((p) => p.name !== data.here);
    const viaWifi = !!wan?.wifi;
    const blank = { type: 'dhcp', addr: '', gateway: '', dns: '', pppoe_user: '', pppoe_pass: '' };
    const pick = (p) => setNet((m) => ({
      ...m,
      wan: { ...blank, ...(m.wan && !m.wan.wifi ? m.wan : {}), port: p },
      bridge: m.bridge.filter((b) => b !== p),
      lan: m.lan.port === p ? { ...m.lan, port: data.here || '' } : m.lan,
    }));
    const useWifi = () => setNet((m) => ({ ...m, wan: { ...blank, port: wifiCards[0].name, wifi: { ssid: '', psk: '', country: data.country || 'US' } } }));
    const setJoin = (k) => (v) => setNet((m) => ({ ...m, wan: { ...m.wan, wifi: { ...m.wan.wifi, [k]: v } } }));
    const scan = async () => {
      setScanning(true); setErr('');
      try { setFound(await get(`/api/net/wifi/${wan.port}/scan`)); } catch (e) { setErr(e.message || 'The card could not look for networks.'); setFound(null); }
      finally { setScanning(false); }
    };
    return (
      <Frame title="Your internet connection" step={sn} of={order.length}>
        {wifiCards.length > 0 && (
          <Field label="How does the internet reach this device?">
            <Segmented value={viaWifi ? 'wifi' : 'cable'} onChange={(v) => (v === 'wifi' ? useWifi() : setNet((m) => ({ ...m, wan: null })))} options={[{ value: 'cable', label: 'A cable from my modem' }, { value: 'wifi', label: 'A Wi-Fi network (hotspot)' }]} />
          </Field>
        )}
        {viaWifi ? (
          <>
            <p className="sec">This device joins another Wi-Fi network, such as a mobile hotspot, and uses it as the internet.</p>
            {wifiCards.length > 1 && (
              <Field label="Wi-Fi card"><Select value={wan.port} onChange={(v) => { setNet((m) => ({ ...m, wan: { ...m.wan, port: v } })); setFound(null); }} options={wifiCards.map((c) => ({ value: c.name, label: `${c.name}${c.usb ? ' (USB adapter)' : ''}` }))} /></Field>
            )}
            <div className="actions"><Button onClick={scan} disabled={scanning}>{scanning ? 'Looking…' : 'Look for networks'}</Button></div>
            {found && (
              <div className="stack">
                {found.length === 0 && <p className="small muted">No networks found. Move closer, or type the name below.</p>}
                {found.slice(0, 10).map((n) => (
                  <button type="button" key={n.ssid} className={`preset ${wan.wifi.ssid === n.ssid ? 'on' : ''}`} aria-pressed={wan.wifi.ssid === n.ssid} onClick={() => setJoin('ssid')(n.ssid)}>
                    <span className="t">{n.ssid}</span><span className="d">{n.security === 'open' ? 'Open network' : 'Needs a password'}</span>
                  </button>
                ))}
              </div>
            )}
            <Field label="Network name"><Input value={wan.wifi.ssid} onChange={setJoin('ssid')} autoComplete="off" /></Field>
            <Field label="Password" hint="Leave empty for an open network. A Wi-Fi password is 8 to 63 characters."><Input type="password" value={wan.wifi.psk} onChange={setJoin('psk')} autoComplete="off" /></Field>
            <Field label="Country this device is in" hint="Sets which Wi-Fi channels are legal.">
              <Select value={wan.wifi.country} onChange={setJoin('country')} options={[...(COUNTRIES[wan.wifi.country] ? [] : [{ value: wan.wifi.country, label: wan.wifi.country }]), ...Object.entries(COUNTRIES).map(([v, l]) => ({ value: v, label: l }))]} />
            </Field>
            <p className="small muted">Joining happens when you finish. If the name or password is wrong, change it under Wi-Fi once you are in.</p>
          </>
        ) : (
          <>
            <p className="sec">Which port does the cable from your modem or provider plug into?</p>
            {data.here && <p className="small muted">You are connected through {data.here} right now, so it is kept for your own network.</p>}
            {cand.length ? (
              <PortPick ports={cand} value={wan?.port || ''} onChange={pick} />
            ) : (
              <Note tone="warn" title="This machine has only one wired port">It cannot be both your internet and your network. Add a USB network adapter{wifiCards.length ? ', or choose a Wi-Fi network above' : ''}.</Note>
            )}
            {wan?.port && (
              <>
                <Field label="How does your provider connect you?">
                  <Select value={wan.type} onChange={setWan('type')} options={[{ value: 'dhcp', label: 'Automatically (most cable and fibre)' }, { value: 'pppoe', label: 'User name and password (PPPoE)' }, { value: 'static', label: 'A fixed address' }]} />
                </Field>
                {wan.type === 'pppoe' && (
                  <div className="formgrid">
                    <Field label="User name"><Input value={wan.pppoe_user} onChange={setWan('pppoe_user')} autoComplete="off" /></Field>
                    <Field label="Password"><Input type="password" value={wan.pppoe_pass} onChange={setWan('pppoe_pass')} autoComplete="off" /></Field>
                  </div>
                )}
                {wan.type === 'static' && (
                  <div className="formgrid">
                    <Field label="Address" hint="For example 203.0.113.10/24"><Input className="mono" value={wan.addr} onChange={setWan('addr')} /></Field>
                    <Field label="Gateway"><Input className="mono" value={wan.gateway} onChange={setWan('gateway')} /></Field>
                    <Field label="DNS servers (optional)"><Input className="mono" value={wan.dns} onChange={setWan('dns')} /></Field>
                  </div>
                )}
              </>
            )}
          </>
        )}
        <div className="actions">
          <Button onClick={() => setStep('mode')}>Back</Button>
          <Button onClick={() => { setNet((m) => ({ ...m, wan: null })); setStep('network'); }}>Not now</Button>
          <Button kind="primary" disabled={!wan?.port || !okWan} onClick={() => setStep('network')}>Next</Button>
        </div>
        {err && <Note tone="bad">{err}</Note>}
      </Frame>
    );
  }

  if (step === 'network') {
    return (
      <Frame title="Your network" step={sn} of={order.length}>
        <h2 className="h3">Which port is your LAN?</h2>
        <p className="small muted">The port your computers and switch connect to.{data.here ? ` You are using ${data.here} right now.` : ''}</p>
        <PortPick ports={wired} value={lan.port} here={data.here} taken={[wan?.port].filter(Boolean)}
          onChange={(p) => setNet((m) => ({ ...m, lan: { ...m.lan, port: p }, wan: m.wan?.port === p ? { ...m.wan, port: '' } : m.wan, bridge: m.bridge.filter((b) => b !== p) }))} />
        {router ? (
          <>
            <Field label="Address of this device on your network" hint="Gatehouse picked a range that does not clash with the network you are on now.">
              <Input className="mono" value={lan.address} onChange={setLan('address')} placeholder="192.168.1.1/24" />
            </Field>
            {wifiCards.some((c) => c.name !== wan?.port) && (
              <ToggleRow title="I will also use Wi-Fi on this network" desc="Lets a Wi-Fi card share this network with your wired devices. You set up the Wi-Fi itself later, under Wi-Fi." checked={!!net.wifi_share} onChange={(v) => setNet((m) => ({ ...m, wifi_share: v }))} />
            )}
            <ToggleRow title="Give devices their addresses (DHCP)" desc={lan.no_dhcp ? 'Off. Devices need an address set by hand, or another server on this network must hand them out.' : 'Recommended. Devices that connect get an address, the gateway and DNS automatically.'} checked={!lan.no_dhcp} onChange={(v) => setLan('no_dhcp')(!v)} />
          </>
        ) : (
          <>
            <Field label="Address of this device">
              <Select value={lan.addressing} onChange={setLan('addressing')} options={[{ value: 'dhcp', label: 'Automatic, from my router (DHCP)' }, { value: 'static', label: 'A fixed address I choose' }]} />
            </Field>
            {lan.addressing === 'static' && (
              <div className="formgrid">
                <Field label="Address" hint="For example 192.168.0.50/24"><Input className="mono" value={lan.static} onChange={setLan('static')} /></Field>
                <Field label="Your router's address"><Input className="mono" value={lan.gateway} onChange={setLan('gateway')} placeholder="192.168.0.1" /></Field>
                <Field label="DNS server (optional)"><Input className="mono" value={lan.dns} onChange={setLan('dns')} /></Field>
              </div>
            )}
          </>
        )}
        {others.length > 0 && (
          <>
            <h2 className="h3">Other ports</h2>
            <p className="small muted">Switched off unless you choose. Joining the LAN makes a port work like another socket on a switch.</p>
            <div className="portlist">
              {others.map((p) => {
                const joined = net.bridge.includes(p.name);
                return (
                  <div className="portrow" key={p.name}>
                    <div className="pid"><span className="name mono">{p.name}</span><span className="small muted">{p.usb ? 'USB adapter' : 'Built-in port'}</span></div>
                    <div className="pstat"><span className={`dot ${p.link ? '' : 'off'}`} />{p.link ? 'Cable connected' : 'No cable'}</div>
                    <Segmented value={joined ? 'bridge' : 'unused'} onChange={(v) => setNet((m) => ({ ...m, bridge: v === 'bridge' ? [...m.bridge.filter((b) => b !== p.name), p.name] : m.bridge.filter((b) => b !== p.name) }))}
                      options={[{ value: 'unused', label: 'Not used' }, { value: 'bridge', label: 'Join the LAN' }]} />
                  </div>
                );
              })}
            </div>
          </>
        )}
                <div className="actions"><Button onClick={() => setStep(router ? 'internet' : 'mode')}>Back</Button><Button kind="primary" disabled={!ready} onClick={() => setStep('about')}>Next</Button></div>
      </Frame>
    );
  }

  return (
    <Frame title="About this device" step={sn} of={order.length}>
      <Field label="Name of this device" hint="Shown on your network, for example gatehouse"><Input value={host} onChange={setHost} /></Field>
      <Field label="Time zone"><Select value={zone} onChange={setZone} options={data.timezones || ['UTC']} /></Field>
      <ToggleRow title="Require two-step sign-in" desc="Optional. Asks for a code from an authenticator app each time you sign in. You can change this later in Settings." checked={mfa} onChange={setMfa} />
      {router && hereIsLan && !(data.install?.address && lan.address === data.install.address) && <Note tone="warn" title="Your connection will move">Gatehouse becomes your router on this port. After you finish, unplug and replug the cable on this computer so it gets a new address. The next page shows where to go.</Note>}
      {err && <Note tone="bad">{err}</Note>}
      <div className="actions"><Button onClick={() => setStep('network')}>Back</Button><Button kind="primary" disabled={busy || !host} onClick={() => finish(false)}>Finish setup</Button></div>
    </Frame>
  );
}

// After the wizard: the change moves this connection, so wait for the console to answer at its new address.
function Applied({ url, mode, user, onDone }) {
  const [up, setUp] = useState(false);
  useEffect(() => {
    if (!url) return undefined;
    let live = true;
    const probe = async () => {
      try { await fetch(`${url}api/health`, { mode: 'no-cors', cache: 'no-store' }); if (live) { setUp(true); window.location.href = url; } } catch { /* not up yet */ }
    };
    const t = setInterval(probe, 3000);
    return () => { live = false; clearInterval(t); };
  }, [url]);
  return (
    <Frame title="Setting up your network…">
      <p className="sec">{mode === 'router' ? 'Gatehouse is becoming your router and will start handing out addresses.' : 'Gatehouse is applying your settings.'}</p>
      {url ? (
        <>
          <Note tone="good" title="Unplug and replug the cable on this computer">That makes it pick up a new address. This page opens Gatehouse by itself as soon as it is ready{up ? '' : '.'} Sign in as <strong>{user}</strong>.</Note>
          <div className="actions"><a className="btn primary" href={url}>Open {url.replace('https://', '').replace('/', '')} now</a></div>
        </>
      ) : (
        <>
          <Note tone="good" title="Done">Gatehouse gets its address from your router. The address is shown on this machine&apos;s screen. Open it and sign in as <strong>{user}</strong>.</Note>
          <div className="actions"><Button kind="primary" onClick={onDone}>Continue</Button></div>
        </>
      )}
    </Frame>
  );
}

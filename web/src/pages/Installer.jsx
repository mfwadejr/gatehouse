import React, { useEffect, useRef, useState } from 'react';
import { get, post, setInstallCode } from '../api.js';
import { Button, Card, Field, Icon, Input, Note, Pill, Segmented, Select, Bar, ToggleRow, enterToPrimary } from '../ui.jsx';

const gb = (n) => `${Math.round(n / 1e9)} GB`;

function Shell({ title, children, step }) {
  const ref = useRef(null);
  // Enter does what the green button does, even when the pointer is resting on it and nothing is focused.
  useEffect(() => {
    const on = (e) => enterToPrimary(e, ref.current);
    document.addEventListener('keydown', on);
    return () => document.removeEventListener('keydown', on);
  }, []);
  return (
    <main className="auth">
      <section className="card" ref={ref} style={{ width: 'min(100%, 40rem)' }}>
        <div className="brand" style={{ padding: 0 }}><Icon name="logo" size={24} /><span>Gatehouse installer</span></div>
        {step && <div className="small muted">Step {step} of 5</div>}
        <h1>{title}</h1>
        {children}
      </section>
    </main>
  );
}

function pwHint(p) {
  if (!p) return 'At least 12 characters. Several unrelated words in a row works well.';
  if (p.length < 12) return `${p.length} of 12 characters. ${12 - p.length} more needed.`;
  return 'Long enough.';
}

const baseAddr = (cidr) => (cidr || '').split('/')[0];

// What to do after the restart, given how the network was set up.
function afterInstall(f) {
  const n = f.network;
  const open = (a) => <>Open <strong>https://{a}</strong> and sign in as <strong>{f.username}</strong>.</>;
  if (n.lan.mode === 'router') {
    return <>Gatehouse will start handing out addresses on your network. Unplug and replug the cable on your computer so it gets a new one, then {open(baseAddr(n.lan.address)).props.children}</>;
  }
  if (n.lan.addressing === 'static') return open(baseAddr(n.lan.static));
  return <>Gatehouse will get its address from your router. The address appears on this machine&apos;s screen after it starts. Open it and sign in as <strong>{f.username}</strong>.</>;
}

function PortPick({ ports, value, onChange, taken, recent }) {
  return (
    <div className="stack">
      {ports.map((p) => {
        const off = taken.includes(p.name);
        return (
          <button type="button" key={p.name} disabled={off} className={`preset ${value === p.name ? 'on' : ''}`} aria-pressed={value === p.name} onClick={() => onChange(p.name)}>
            <span className="t">{p.name}{p.usb ? ' (USB adapter)' : ''} {p.link ? <Pill tone="good">cable connected{p.speed ? `, ${p.speed} Mb/s` : ''}</Pill> : <Pill>no cable</Pill>}{recent[p.name] ? <Pill tone="info">just plugged in</Pill> : null}</span>
            <span className="d mono">{p.mac}{off ? ' · used for something else' : ''}</span>
          </button>
        );
      })}
    </div>
  );
}

function NetworkStep({ f, setF, onBack, onNext }) {
  const [info, setInfo] = useState(null);
  const [recent, setRecent] = useState({});
  const prev = useRef({});
  const n = f.network;
  const setNet = (fn) => setF((x) => ({ ...x, network: fn(x.network) }));
  const setLan = (k) => (v) => setNet((m) => ({ ...m, lan: { ...m.lan, [k]: v } }));
  const setWan = (k) => (v) => setNet((m) => ({ ...m, wan: { ...m.wan, [k]: v } }));

  useEffect(() => {
    let live = true;
    const tick = async () => {
      try {
        const d = await get('/api/install/network');
        if (!live) return;
        const fresh = {};
        d.ports.forEach((p) => { if (p.link && prev.current[p.name] === false) fresh[p.name] = true; prev.current[p.name] = p.link; });
        if (Object.keys(fresh).length) { setRecent((r) => ({ ...r, ...fresh })); setTimeout(() => setRecent((r) => { const c = { ...r }; Object.keys(fresh).forEach((k) => delete c[k]); return c; }), 6000); }
        setInfo(d);
        setNet((m) => (m.lan.mode === 'router' && !m.lan.address ? { ...m, lan: { ...m.lan, address: d.suggested_lan } } : m));
      } catch { /* the page may briefly lose the network */ }
    };
    tick();
    const t = setInterval(tick, 2000);
    return () => { live = false; clearInterval(t); };
  }, []);

  const ports = info?.ports || [];
  const lanPort = n.lan.port;
  const wanPort = n.wan?.port || '';
  const used = [lanPort, wanPort, ...n.bridge].filter(Boolean);
  const others = ports.filter((p) => p.name !== lanPort && p.name !== wanPort);

  const pickMode = (mode) => setNet((m) => ({ ...m, lan: { ...m.lan, mode, address: mode === 'router' ? (m.lan.address || info?.suggested_lan || '') : m.lan.address }, wan: mode === 'behind' ? null : m.wan }));
  const lan = n.lan;
  const wan = n.wan;
  const okLan = lan.mode === 'router' ? /^\d+\.\d+\.\d+\.\d+\/\d+$/.test(lan.address)
    : lan.mode === 'behind' ? (lan.addressing === 'dhcp' || (/\//.test(lan.static) && lan.gateway)) : false;
  const okWan = !wan || (wan.port && (wan.type === 'dhcp' || (wan.type === 'pppoe' && wan.pppoe_user && wan.pppoe_pass) || (wan.type === 'static' && wan.addr && wan.gateway)));
  const ready = lanPort && okLan && okWan;

  return (
    <Shell title="Connect your network" step={3}>
      <p className="sec">Plug your <strong>LAN cable</strong> into a port. It lights up below. The internet cable is optional, and you can add it now or later.</p>
      {!ports.length && <Note tone="warn">No wired network port found yet. If you use a USB network adapter, plug it in.</Note>}

      <h2 className="h3">1. Which port is your LAN?</h2>
      <PortPick ports={ports} value={lanPort} onChange={(p) => setNet((m) => ({ ...m, lan: { ...m.lan, port: p }, wan: m.wan?.port === p ? { ...m.wan, port: '' } : m.wan, bridge: m.bridge.filter((b) => b !== p) }))} taken={[wanPort].filter(Boolean)} recent={recent} />

      {lanPort && (
        <>
          <h2 className="h3">2. How will Gatehouse be used?</h2>
          <div className="stack">
            <button type="button" className={`preset ${lan.mode === 'router' ? 'on' : ''}`} aria-pressed={lan.mode === 'router'} onClick={() => pickMode('router')}>
              <span className="t">Gatehouse is my router</span>
              <span className="d">It hands out addresses to your devices (DHCP), runs DNS, and connects you to the internet.</span>
            </button>
            <button type="button" className={`preset ${lan.mode === 'behind' ? 'on' : ''}`} aria-pressed={lan.mode === 'behind'} onClick={() => pickMode('behind')}>
              <span className="t">Gatehouse sits behind my existing router</span>
              <span className="d">Another router stays in charge of addresses and the internet. Gatehouse adds DNS filtering and a firewall for devices on this network.</span>
            </button>
          </div>
          {lan.mode === 'router' && (
            <Field label="Address of this device on your network" hint="Gatehouse picked a range that does not clash with the network you are on now. Devices get addresses from the same range.">
              <Input className="mono" value={lan.address} onChange={setLan('address')} placeholder="192.168.1.1/24" />
            </Field>
          )}
          {lan.mode === 'behind' && (
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
        </>
      )}

      {lan.mode === 'router' && (
        <>
          <h2 className="h3">3. Internet connection <span className="muted small">(optional)</span></h2>
          <p className="small muted">Connect the cable from your modem or provider to another port. Skip this to add it later under Internet.</p>
          {!wan ? (
            <Button onClick={() => setNet((m) => ({ ...m, wan: { port: '', type: 'dhcp', addr: '', gateway: '', dns: '', pppoe_user: '', pppoe_pass: '' } }))}>Set up internet now</Button>
          ) : (
            <>
              <PortPick ports={ports} value={wan.port} onChange={(p) => setNet((m) => ({ ...m, wan: { ...m.wan, port: p }, bridge: m.bridge.filter((b) => b !== p) }))} taken={[lanPort]} recent={recent} />
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
              <div className="actions"><Button small onClick={() => setNet((m) => ({ ...m, wan: null }))}>Not now</Button></div>
            </>
          )}
        </>
      )}

      {lanPort && others.length > 0 && (
        <>
          <h2 className="h3">Other ports</h2>
          <p className="small muted">These ports are switched off unless you choose. Joining the LAN makes a port work like another socket on a switch, on the same network as your LAN port. You can set any of them up differently later.</p>
          <div className="portlist">
            {others.map((p) => {
              const joined = n.bridge.includes(p.name);
              return (
                <div className="portrow" key={p.name}>
                  <div className="pid">
                    <span className="name mono">{p.name}</span>
                    <span className="small muted">{p.usb ? 'USB adapter' : 'Built-in port'}{p.mac ? ` · ${p.mac}` : ''}</span>
                  </div>
                  <div className="pstat"><span className={`dot ${p.link ? '' : 'off'}`} />{p.link ? `Cable connected${p.speed ? `, ${p.speed} Mb/s` : ''}` : 'No cable'}</div>
                  <Segmented value={joined ? 'bridge' : 'unused'} onChange={(v) => setNet((m) => ({ ...m, bridge: v === 'bridge' ? [...m.bridge.filter((b) => b !== p.name), p.name] : m.bridge.filter((b) => b !== p.name) }))}
                    options={[{ value: 'unused', label: 'Not used' }, { value: 'bridge', label: 'Join the LAN' }]} />
                </div>
              );
            })}
          </div>
        </>
      )}
      <div className="actions"><Button onClick={onBack}>Back</Button><Button kind="primary" disabled={!ready} onClick={onNext}>Next</Button></div>
    </Shell>
  );
}

export default function Installer({ info }) {
  const [step, setStep] = useState(0);
  const [code, setCode] = useState('');
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState(false);
  const [disks, setDisks] = useState([]);
  const [zones, setZones] = useState(['UTC']);
  const [f, setF] = useState({ disk: '', confirm: '', hostname: 'gatehouse', username: 'admin', password: '', again: '', timezone: 'UTC', require_mfa: false, network: { lan: { port: '', mode: '', address: '', addressing: 'dhcp', static: '', gateway: '', dns: '' }, wan: null, bridge: [] } });
  const [status, setStatus] = useState(null);
  const [restarting, setRestarting] = useState(0);
  const poll = useRef(null);
  const set = (k) => (v) => setF((x) => ({ ...x, [k]: v }));

  const unlock = async () => {
    setErr(''); setBusy(true); setInstallCode(code);
    try {
      await post('/api/install/unlock');
      setDisks(await get('/api/install/disks'));
      const z = await get('/api/install/timezones'); setZones(z);
      try { const tz = Intl.DateTimeFormat().resolvedOptions().timeZone; if (z.includes(tz)) set('timezone')(tz); } catch { /* ignore */ }
      setStep(1);
    } catch (e) { setErr(e.message); setInstallCode(''); }
    finally { setBusy(false); }
  };

  const start = async () => {
    setErr(''); setBusy(true);
    try {
      const { again, ...body } = f;
      await post('/api/install/start', body);
      setStep(5);
    } catch (e) { setErr(e.message); }
    finally { setBusy(false); }
  };

  useEffect(() => {
    if (step !== 5) return undefined;
    const tick = async () => { try { const s = await get('/api/install/status'); setStatus(s); if (s.state === 'done' || s.state === 'error') clearInterval(poll.current); } catch { /* the page may briefly lose the network */ } };
    tick(); poll.current = setInterval(tick, 1500);
    return () => clearInterval(poll.current);
  }, [step]);

  if (restarting) return <Restarting f={f} since={restarting} />;
  const disk = disks.find((d) => d.path === f.disk);
  const pwOK = f.password.length >= 12 && f.password === f.again;

  if (step === 0) {
    return (
      <Shell title="Welcome">
        <p className="sec">This will install Gatehouse onto a disk in this machine. To make sure it is really you, type the code shown on the screen of the machine you are installing on.</p>
        <form className="stack" onSubmit={(e) => { e.preventDefault(); unlock(); }}>
          <Field label="Installer code"><Input className="codebox" value={code} onChange={setCode} placeholder="0000-0000" inputMode="numeric" autoComplete="off" autoFocus /></Field>
          {err && <Note tone="bad">{err}</Note>}
          <Button kind="primary" disabled={busy || !code} onClick={unlock}>Continue</Button>
        </form>
      </Shell>
    );
  }
  if (step === 1) {
    return (
      <Shell title="Where should Gatehouse be installed?" step={1}>
        <Note tone="warn" title="Everything on the chosen disk will be erased.">Pick the internal disk, not the USB stick you started from. The installer will not let you choose the stick.</Note>
        <div className="stack">
          {disks.map((d) => (
            <button type="button" key={d.path} disabled={d.in_use} className={`preset ${f.disk === d.path ? 'on' : ''}`} onClick={() => setF((x) => ({ ...x, disk: d.path, confirm: '' }))} aria-pressed={f.disk === d.path}>
              <span className="t">{d.model || d.path} · {gb(d.size)}</span>
              <span className="d mono">{d.path}{d.transport ? ` · ${d.transport}` : ''}{d.removable ? ' · removable' : ''}{d.partitions ? ` · ${d.partitions} existing partition${d.partitions > 1 ? 's' : ''}` : ' · empty'}{d.in_use ? ' · this is the installer USB stick' : ''}</span>
            </button>
          ))}
          {!disks.length && <Note tone="bad">No suitable disk was found. A disk must be at least 4 GB.</Note>}
        </div>
        <div className="actions"><Button kind="primary" disabled={!f.disk} onClick={() => setStep(2)}>Next</Button></div>
      </Shell>
    );
  }
  if (step === 2) {
    return (
      <Shell title="About this device" step={2}>
        <Field label="Name of this device" hint="Shown on your network, for example gatehouse"><Input value={f.hostname} onChange={set('hostname')} /></Field>
        <Field label="Time zone"><Select value={f.timezone} onChange={set('timezone')} options={zones} /></Field>
        <Field label="Administrator name"><Input value={f.username} onChange={set('username')} autoComplete="username" /></Field>
        <Field label="Password" hint={pwHint(f.password)}><Input type="password" value={f.password} onChange={set('password')} autoComplete="new-password" /></Field>
        <Field label="Password again" hint={f.again && f.again !== f.password ? 'These do not match yet.' : undefined}><Input type="password" value={f.again} onChange={set('again')} autoComplete="new-password" /></Field>
        <ToggleRow title="Require two-step sign-in" desc="Optional. Asks for a code from an authenticator app each time you sign in, and sets it up the first time. You can turn this on or off later in Settings." checked={f.require_mfa} onChange={set('require_mfa')} />
        {!pwOK && f.password.length > 0 && (
          <Note tone="warn" title={f.password.length < 12 ? `The password is too short: ${f.password.length} of the 12 characters needed` : 'The two passwords are different'}>
            {f.password.length < 12
              ? `Add ${12 - f.password.length} more character${12 - f.password.length > 1 ? 's' : ''}. Several unrelated words in a row is easy to remember and long enough.`
              : 'Type the same password in both boxes.'}
          </Note>
        )}
        <div className="actions"><Button onClick={() => setStep(1)}>Back</Button><Button kind="primary" disabled={!pwOK || !f.hostname || !f.username} onClick={() => setStep(3)}>Next</Button></div>
      </Shell>
    );
  }
  if (step === 3) {
    return <NetworkStep f={f} setF={setF} onBack={() => setStep(2)} onNext={() => setStep(4)} />;
  }
  if (step === 4) {
    return (
      <Shell title="Ready to install" step={4}>
        <Note tone="bad" title="This erases the disk">{disk?.model || f.disk} ({gb(disk?.size || 0)}) at <span className="mono">{f.disk}</span> will be wiped, including anything stored on it.</Note>
        <Field label={`Type ${f.disk} to confirm`}><Input value={f.confirm} onChange={set('confirm')} autoComplete="off" className="mono" /></Field>
        {err && <Note tone="bad">{err}</Note>}
        <div className="actions"><Button onClick={() => setStep(3)}>Back</Button><Button kind="primary" disabled={busy || f.confirm !== f.disk} onClick={start}>Erase and install</Button></div>
      </Shell>
    );
  }
  const done = status?.steps?.filter((s) => s.state === 'done').length || 0;
  const total = status?.steps?.length || 1;
  return (
    <Shell title={status?.state === 'done' ? 'Installed' : status?.state === 'error' ? 'Something went wrong' : 'Installing…'} step={5}>
      {status && <Bar pct={status.state === 'done' ? 100 : (100 * done) / total} />}
      <div className="list">
        {(status?.steps || []).map((s) => (
          <div className="item" key={s.name}><span>{s.name}</span><span className={s.state === 'error' ? '' : 'muted'}>{s.state === 'done' ? '✓' : s.state === 'running' ? 'working…' : s.state === 'error' ? 'failed' : ''}</span></div>
        ))}
      </div>
      {status?.state === 'error' && <><Note tone="bad">{status.error}</Note><pre className="card mono" style={{ margin: 0, whiteSpace: 'pre-wrap', maxHeight: '16rem', overflow: 'auto' }}>{status.log}</pre><div className="actions"><Button onClick={() => setStep(4)}>Try again</Button></div></>}
      {status?.state === 'done' && (
        <>
          <Note tone="good" title="Gatehouse is installed.">Click Restart, then take the USB stick out when the screen goes blank. {afterInstall(f)}</Note>
          <div className="actions"><Button kind="primary" onClick={async () => { try { await post('/api/install/reboot'); } catch { /* going down */ } setRestarting(Date.now()); }}>Restart now</Button></div>
        </>
      )}
    </Shell>
  );
}

const BOOT_SECONDS = 75; // about how long a restart takes before the console answers

// The address Gatehouse will have after the restart, when it is known in advance.
function targetAddress(f) {
  const n = f.network;
  if (n.lan.mode === 'router') return baseAddr(n.lan.address);
  if (n.lan.addressing === 'static') return baseAddr(n.lan.static);
  return '';
}

// Shown after Restart: counts the minute the machine needs, tells the person to pull the stick,
// and sends the browser to the new console as soon as it answers.
function Restarting({ f, since }) {
  const [now, setNow] = useState(Date.now());
  const target = targetAddress(f);
  const url = target ? `https://${target}/` : '';
  useEffect(() => { const t = setInterval(() => setNow(Date.now()), 500); return () => clearInterval(t); }, []);
  useEffect(() => {
    if (!url) return undefined;
    let live = true;
    const probe = async () => {
      try { await fetch(`${url}api/health`, { mode: 'no-cors', cache: 'no-store' }); if (live) window.location.href = url; } catch { /* not up yet, or the certificate is not accepted yet */ }
    };
    const t = setInterval(probe, 3000);
    return () => { live = false; clearInterval(t); };
  }, [url]);
  const s = Math.max(0, (now - since) / 1000);
  const left = Math.max(0, Math.ceil(BOOT_SECONDS - s));
  const phase = s < 10 ? 'Restarting this machine…' : left > 0 ? 'Starting Gatehouse…' : 'Still starting. This can take a little longer the first time.';
  return (
    <Shell title="Restarting">
      <Bar pct={Math.min(95, (100 * s) / BOOT_SECONDS)} />
      <p className="sec"><strong>{phase}</strong>{left > 0 ? ` About ${left} second${left === 1 ? '' : 's'} to go.` : ''}</p>
      {s < 40 && <Note tone="warn" title="Take the USB stick out now">If it stays in, the machine may start the installer again instead of Gatehouse.</Note>}
      {url ? (
        <>
          <p className="sec">This page opens Gatehouse by itself as soon as it is ready. If your network gives out addresses through Gatehouse, unplug and replug the cable on this computer so it gets a new one. Your browser will warn about the certificate the first time. That is expected: choose to continue.</p>
          <div className="actions"><a className="btn primary" href={url}>Open {url.replace('https://', '').replace('/', '')} now</a></div>
        </>
      ) : (
        <p className="sec">Gatehouse gets its address from your router. When it has started, the address appears on this machine&apos;s screen. Open it and sign in as <strong>{f.username}</strong>.</p>
      )}
    </Shell>
  );
}

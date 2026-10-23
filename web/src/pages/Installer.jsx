import React, { useEffect, useRef, useState } from 'react';
import { get, post, setInstallCode } from '../api.js';
import { Button, Card, Field, Icon, Input, Note, Select, Bar, ToggleRow } from '../ui.jsx';

const gb = (n) => `${Math.round(n / 1e9)} GB`;

function Shell({ title, children, step }) {
  return (
    <main className="auth">
      <section className="card" style={{ width: 'min(100%, 40rem)' }}>
        <div className="brand" style={{ padding: 0 }}><Icon name="logo" size={24} /><span>Gatehouse installer</span></div>
        {step && <div className="small muted">Step {step} of 4</div>}
        <h1>{title}</h1>
        {children}
      </section>
    </main>
  );
}

export default function Installer({ info }) {
  const [step, setStep] = useState(0);
  const [code, setCode] = useState('');
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState(false);
  const [disks, setDisks] = useState([]);
  const [zones, setZones] = useState(['UTC']);
  const [f, setF] = useState({ disk: '', confirm: '', hostname: 'gatehouse', username: 'admin', password: '', again: '', timezone: 'UTC', require_mfa: false });
  const [status, setStatus] = useState(null);
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
      setStep(4);
    } catch (e) { setErr(e.message); }
    finally { setBusy(false); }
  };

  useEffect(() => {
    if (step !== 4) return undefined;
    const tick = async () => { try { const s = await get('/api/install/status'); setStatus(s); if (s.state === 'done' || s.state === 'error') clearInterval(poll.current); } catch { /* the page may briefly lose the network */ } };
    tick(); poll.current = setInterval(tick, 1500);
    return () => clearInterval(poll.current);
  }, [step]);

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
        <Field label="Password" hint="At least 12 characters. A few random words works well."><Input type="password" value={f.password} onChange={set('password')} autoComplete="new-password" /></Field>
        <Field label="Password again" hint={f.again && f.again !== f.password ? 'These do not match.' : undefined}><Input type="password" value={f.again} onChange={set('again')} autoComplete="new-password" /></Field>
        <ToggleRow title="Require two-step sign-in" desc="Optional. Asks for a code from an authenticator app each time you sign in, and sets it up the first time. You can turn this on or off later in Settings." checked={f.require_mfa} onChange={set('require_mfa')} />
        <div className="actions"><Button onClick={() => setStep(1)}>Back</Button><Button kind="primary" disabled={!pwOK || !f.hostname || !f.username} onClick={() => setStep(3)}>Next</Button></div>
      </Shell>
    );
  }
  if (step === 3) {
    return (
      <Shell title="Ready to install" step={3}>
        <Note tone="bad" title="This erases the disk">{disk?.model || f.disk} ({gb(disk?.size || 0)}) at <span className="mono">{f.disk}</span> will be wiped, including anything stored on it.</Note>
        <Field label={`Type ${f.disk} to confirm`}><Input value={f.confirm} onChange={set('confirm')} autoComplete="off" className="mono" /></Field>
        {err && <Note tone="bad">{err}</Note>}
        <div className="actions"><Button onClick={() => setStep(2)}>Back</Button><Button kind="primary" disabled={busy || f.confirm !== f.disk} onClick={start}>Erase and install</Button></div>
      </Shell>
    );
  }
  const done = status?.steps?.filter((s) => s.state === 'done').length || 0;
  const total = status?.steps?.length || 1;
  return (
    <Shell title={status?.state === 'done' ? 'Installed' : status?.state === 'error' ? 'Something went wrong' : 'Installing…'} step={4}>
      {status && <Bar pct={status.state === 'done' ? 100 : (100 * done) / total} />}
      <div className="list">
        {(status?.steps || []).map((s) => (
          <div className="item" key={s.name}><span>{s.name}</span><span className={s.state === 'error' ? '' : 'muted'}>{s.state === 'done' ? '✓' : s.state === 'running' ? 'working…' : s.state === 'error' ? 'failed' : ''}</span></div>
        ))}
      </div>
      {status?.state === 'error' && <><Note tone="bad">{status.error}</Note><pre className="card mono" style={{ margin: 0, whiteSpace: 'pre-wrap', maxHeight: '16rem', overflow: 'auto' }}>{status.log}</pre><div className="actions"><Button onClick={() => setStep(3)}>Try again</Button></div></>}
      {status?.state === 'done' && (
        <>
          <Note tone="good" title="Gatehouse is installed.">Remove the USB stick, then restart. After it starts, open <strong>https://{f.hostname}.lan</strong> or the address shown on its screen and sign in as <strong>{f.username}</strong>.</Note>
          <div className="actions"><Button kind="primary" onClick={async () => { try { await post('/api/install/reboot'); } catch { /* going down */ } }}>Restart now</Button></div>
        </>
      )}
    </Shell>
  );
}

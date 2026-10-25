import React, { useEffect, useRef, useState } from 'react';
import { get, post, setInstallCode } from '../api.js';
import { Button, Field, Icon, Input, Note, Bar, enterToPrimary } from '../ui.jsx';

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
        {step && <div className="small muted">Step {step} of 2</div>}
        <h1>{title}</h1>
        {children}
      </section>
    </main>
  );
}

// The installer asks only for the disk. The administrator account, networks and the rest are set up in
// the browser the first time Gatehouse starts, so there is nothing to write down or carry anywhere.
export default function Installer() {
  const [step, setStep] = useState(0);
  const [code, setCodeRaw] = useState('');
  // Show the code as 0000-0000: the dash appears by itself after the fourth digit.
  const setCode = (v) => {
    const d = String(v).replace(/\D/g, '').slice(0, 8);
    setCodeRaw(d.length > 4 ? `${d.slice(0, 4)}-${d.slice(4)}` : d.length === 4 && String(v).length >= 4 && !String(v).endsWith('-') && v.length > code.length ? `${d}-` : d);
  };
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState(false);
  const [disks, setDisks] = useState([]);
  const [f, setF] = useState({ disk: '', confirm: '' });
  const [status, setStatus] = useState(null);
  const [restarting, setRestarting] = useState(0);
  const poll = useRef(null);
  const set = (k) => (v) => setF((x) => ({ ...x, [k]: v }));

  const unlock = async () => {
    setErr(''); setBusy(true); setInstallCode(code);
    try {
      await post('/api/install/unlock');
      setDisks(await get('/api/install/disks'));
      setStep(1);
    } catch (e) { setErr(e.message); setInstallCode(''); }
    finally { setBusy(false); }
  };

  const start = async () => {
    setErr(''); setBusy(true);
    try {
      await post('/api/install/start', f);
      setStep(3);
    } catch (e) { setErr(e.message); }
    finally { setBusy(false); }
  };

  useEffect(() => {
    if (step !== 3) return undefined;
    const tick = async () => { try { const s = await get('/api/install/status'); setStatus(s); if (s.state === 'done' || s.state === 'error') clearInterval(poll.current); } catch { /* the page may briefly lose the network */ } };
    tick(); poll.current = setInterval(tick, 1500);
    return () => clearInterval(poll.current);
  }, [step]);

  if (restarting) return <Restarting since={restarting} />;
  const disk = disks.find((d) => d.path === f.disk);

  if (step === 0) {
    return (
      <Shell title="Welcome">
        <p className="sec">This will install Gatehouse onto a disk in this machine. To make sure it is really you, type the code shown on the screen of the machine you are installing on.</p>
        <form className="stack" onSubmit={(e) => { e.preventDefault(); unlock(); }}>
          <Field label="Installer code"><Input className="codebox" value={code} onChange={setCode} placeholder="0000-0000" inputMode="numeric" autoComplete="off" autoFocus /></Field>
          {err && <Note tone="bad">{err}</Note>}
          <Button kind="primary" type="submit" disabled={busy || !code}>Continue</Button>
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
            <button type="button" key={d.path} disabled={d.in_use} className={`preset ${f.disk === d.path ? 'on' : ''}`} onClick={() => setF({ disk: d.path, confirm: '' })} aria-pressed={f.disk === d.path}>
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
      <Shell title="Ready to install" step={2}>
        <Note tone="bad" title="This erases the disk">{disk?.model || f.disk} ({gb(disk?.size || 0)}) at <span className="mono">{f.disk}</span> will be wiped, including anything stored on it.</Note>
        <Field label={`Type ${f.disk} to confirm`}><Input value={f.confirm} onChange={set('confirm')} autoComplete="off" className="mono" /></Field>
        {err && <Note tone="bad">{err}</Note>}
        <div className="actions"><Button onClick={() => setStep(1)}>Back</Button><Button kind="primary" disabled={busy || f.confirm !== f.disk} onClick={start}>Erase and install</Button></div>
      </Shell>
    );
  }
  const done = status?.steps?.filter((s) => s.state === 'done').length || 0;
  const total = status?.steps?.length || 1;
  return (
    <Shell title={status?.state === 'done' ? 'Installed' : status?.state === 'error' ? 'Something went wrong' : 'Installing…'}>
      {status && <Bar pct={status.state === 'done' ? 100 : (100 * done) / total} />}
      <div className="list">
        {(status?.steps || []).map((s) => (
          <div className="item" key={s.name}><span>{s.name}</span><span className={s.state === 'error' ? '' : 'muted'}>{s.state === 'done' ? '✓' : s.state === 'running' ? 'working…' : s.state === 'error' ? 'failed' : ''}</span></div>
        ))}
      </div>
      {status?.state === 'error' && <><Note tone="bad">{status.error}</Note><pre className="card mono" style={{ margin: 0, whiteSpace: 'pre-wrap', maxHeight: '16rem', overflow: 'auto' }}>{status.log}</pre><div className="actions"><Button onClick={() => setStep(2)}>Try again</Button></div></>}
      {status?.state === 'done' && (
        <>
          <Note tone="good" title="Gatehouse is installed.">Click Restart, then take the USB stick out when the screen goes blank. When it has started, the machine&apos;s screen shows an address. Open it in a browser to create your administrator account and finish setup.</Note>
          <div className="actions"><Button kind="primary" onClick={async () => { try { await post('/api/install/reboot'); } catch { /* going down */ } setRestarting(Date.now()); }}>Restart now</Button></div>
        </>
      )}
    </Shell>
  );
}

const BOOT_SECONDS = 75; // about how long a restart takes before the machine shows its address

// Shown after Restart: counts the minute the machine needs and tells the person to pull the stick.
function Restarting({ since }) {
  const [now, setNow] = useState(Date.now());
  useEffect(() => { const t = setInterval(() => setNow(Date.now()), 500); return () => clearInterval(t); }, []);
  const s = Math.max(0, (now - since) / 1000);
  const left = Math.max(0, Math.ceil(BOOT_SECONDS - s));
  const phase = s < 10 ? 'Restarting this machine…' : left > 0 ? 'Starting Gatehouse…' : 'Still starting. This can take a little longer the first time.';
  return (
    <Shell title="Restarting">
      <Bar pct={Math.min(95, (100 * s) / BOOT_SECONDS)} />
      <p className="sec"><strong>{phase}</strong>{left > 0 ? ` About ${left} second${left === 1 ? '' : 's'} to go.` : ''}</p>
      {s < 40 && <Note tone="warn" title="Take the USB stick out now">If it stays in, the machine may start the installer again instead of Gatehouse.</Note>}
      <p className="sec">When it has started, this machine&apos;s screen shows an address like <span className="mono">https://192.168.1.20</span>. Open it from a computer on the same network to create your administrator account. Your browser will warn about the certificate the first time. That is expected: choose to continue.</p>
    </Shell>
  );
}

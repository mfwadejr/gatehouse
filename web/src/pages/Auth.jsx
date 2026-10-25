import React, { useEffect, useState } from 'react';
import qrcode from 'qrcode-generator';
import { get, post, setCsrf } from '../api.js';
import { Button, Field, Icon, Input, Note } from '../ui.jsx';

function Frame({ title, children }) {
  return (
    <main className="auth">
      <section className="card">
        <div className="brand" style={{ padding: 0 }}><Icon name="logo" size={24} /><span>Gatehouse</span></div>
        <h1>{title}</h1>
        {children}
      </section>
    </main>
  );
}

function submitOnEnter(fn) { return (e) => { e.preventDefault(); fn(); }; }

function pwHint(p) {
  if (!p) return 'At least 12 characters. Several unrelated words in a row works well.';
  if (p.length < 12) return `${p.length} of 12 characters. ${12 - p.length} more needed.`;
  return 'Long enough.';
}

// The first visit to a new device: whoever opens it from the local network creates the administrator
// account. There is no code and no default login. Once it exists this page never appears again.
export function Setup({ onDone }) {
  const [f, setF] = useState({ username: 'admin', password: '', again: '' });
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState(false);
  const set = (k) => (v) => setF((x) => ({ ...x, [k]: v }));
  const go = async () => {
    setErr('');
    if (f.password.length < 12) { setErr(`The password is too short: ${f.password.length} of the 12 characters needed. Several unrelated words in a row is easy to remember and long enough.`); return; }
    if (f.password !== f.again) { setErr('The two passwords do not match.'); return; }
    setBusy(true);
    try { await post('/api/setup', { username: f.username, password: f.password }); onDone(f.username, f.password); }
    catch (e) { setErr(e.message); }
    finally { setBusy(false); }
  };
  return (
    <Frame title="Welcome">
      <p className="sec">Create your administrator account. You will use it to sign in to this device.</p>
      <form className="stack" onSubmit={submitOnEnter(go)}>
        <Field label="Administrator name"><Input value={f.username} onChange={set('username')} autoComplete="username" autoFocus /></Field>
        <Field label="Password" hint={pwHint(f.password)}><Input type="password" value={f.password} onChange={set('password')} autoComplete="new-password" /></Field>
        <Field label="Password again" hint={f.again && f.again !== f.password ? 'These do not match yet.' : undefined}><Input type="password" value={f.again} onChange={set('again')} autoComplete="new-password" /></Field>
        {err && <Note tone="bad">{err}</Note>}
        <Button kind="primary" type="submit" disabled={busy}>Create account</Button>
      </form>
    </Frame>
  );
}

export function Login({ onSession, prefill }) {
  const [u, setU] = useState(prefill?.u || '');
  const [p, setP] = useState(prefill?.p || '');
  const [ticket, setTicket] = useState('');
  const [code, setCode] = useState('');
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState(false);
  useEffect(() => { if (prefill) go(); /* eslint-disable-next-line */ }, []);
  async function go() {
    setErr(''); setBusy(true);
    try {
      if (ticket) {
        const r = await post('/api/auth/mfa', { ticket, code: code.trim() });
        setCsrf(r.csrf); onSession();
      } else {
        const r = await post('/api/auth/login', { username: u, password: p });
        if (r.status === 'mfa') setTicket(r.ticket); else { setCsrf(r.csrf); onSession(); }
      }
    } catch (e) { setErr(e.message); }
    finally { setBusy(false); }
  }
  return (
    <Frame title={ticket ? 'Enter your code' : 'Sign in'}>
      <form className="stack" onSubmit={submitOnEnter(go)}>
        {!ticket ? (
          <>
            <Field label="Name"><Input value={u} onChange={setU} autoComplete="username" autoFocus /></Field>
            <Field label="Password"><Input type="password" value={p} onChange={setP} autoComplete="current-password" /></Field>
          </>
        ) : (
          <Field label="Code from your authenticator app" hint="Lost your phone? Type one of your recovery codes instead.">
            <Input className="codebox" value={code} onChange={setCode} autoComplete="one-time-code" inputMode="numeric" autoFocus />
          </Field>
        )}
        {err && <Note tone="bad">{err}</Note>}
        <Button kind="primary" type="submit" disabled={busy}>{ticket ? 'Verify' : 'Sign in'}</Button>
      </form>
    </Frame>
  );
}

function Qr({ text }) {
  const [svg, setSvg] = useState('');
  useEffect(() => {
    const q = qrcode(0, 'M'); q.addData(text); q.make();
    setSvg(q.createSvgTag({ cellSize: 4, margin: 0, scalable: true }));
  }, [text]);
  return <div className="qr" style={{ width: '14rem' }} role="img" aria-label="QR code for your authenticator app" dangerouslySetInnerHTML={{ __html: svg }} />;
}

export function Enrol({ onDone }) {
  const [info, setInfo] = useState(null);
  const [code, setCode] = useState('');
  const [codes, setCodes] = useState(null);
  const [err, setErr] = useState('');
  useEffect(() => { post('/api/auth/enrol/begin').then(setInfo).catch((e) => setErr(e.message)); }, []);
  const confirm = async () => {
    setErr('');
    try { const r = await post('/api/auth/enrol/confirm', { code: code.trim() }); setCodes(r.recovery_codes || r.codes || []); }
    catch (e) { setErr(e.message); }
  };
  if (codes) {
    return (
      <Frame title="Save your recovery codes">
        <p className="sec">If you lose your phone, each of these codes lets you sign in once. Keep them somewhere safe. They will not be shown again.</p>
        <pre className="card mono" style={{ margin: 0 }}>{codes.join('\n')}</pre>
        <Button kind="primary" onClick={onDone}>I have saved them</Button>
      </Frame>
    );
  }
  return (
    <Frame title="Turn on two-step sign-in">
      <p className="sec">Scan this with an authenticator app (Google Authenticator, Authy, 1Password and similar), then type the 6-digit code it shows.</p>
      {info && <Qr text={info.uri} />}
      {info && <details><summary className="small muted">Can't scan? Enter this key by hand</summary><p className="mono" style={{ wordBreak: 'break-all' }}>{info.secret}</p></details>}
      <form className="stack" onSubmit={submitOnEnter(confirm)}>
        <Field label="6-digit code"><Input className="codebox" value={code} onChange={setCode} inputMode="numeric" autoComplete="one-time-code" /></Field>
        {err && <Note tone="bad">{err}</Note>}
        <Button kind="primary" type="submit" disabled={!info}>Turn on</Button>
      </form>
    </Frame>
  );
}

export async function loadSetup() { return get('/api/setup'); }

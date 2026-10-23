import React, { useEffect, useState } from 'react';
import { del, post, put, useData } from '../api.js';
import { Button, Card, Empty, Field, Input, Modal, Note, Page, Pill, Segmented, ToggleRow, useAction, fmt } from '../ui.jsx';
import { Enrol } from './Auth.jsx';

function Password() {
  const [f, setF] = useState({ current: '', next: '', again: '' });
  const [run, busy] = useAction();
  const mismatch = f.next && f.again && f.next !== f.again;
  return (
    <Card title="Password" desc="Use at least 12 characters. A few random words is easy to remember and hard to guess.">
      <div className="formgrid">
        <Field label="Current password"><Input type="password" value={f.current} onChange={(v) => setF({ ...f, current: v })} autoComplete="current-password" /></Field>
        <Field label="New password"><Input type="password" value={f.next} onChange={(v) => setF({ ...f, next: v })} autoComplete="new-password" /></Field>
        <Field label="New password again" hint={mismatch ? 'These do not match.' : undefined}><Input type="password" value={f.again} onChange={(v) => setF({ ...f, again: v })} autoComplete="new-password" /></Field>
      </div>
      <div className="actions"><Button kind="primary" disabled={busy || !f.current || !f.next || mismatch} onClick={async () => { if (await run(() => post('/api/auth/password', { current: f.current, new: f.next }), 'Password changed. Other sessions were signed out.')) setF({ current: '', next: '', again: '' }); }}>Change password</Button></div>
    </Card>
  );
}

function Sessions() {
  const { data, reload } = useData('/api/auth/sessions');
  const [run] = useAction();
  return (
    <Card title="Where you are signed in" actions={<Button small onClick={() => run(() => post('/api/auth/sessions/revoke-others'), 'Signed out everywhere else.').then(reload)}>Sign out everywhere else</Button>}>
      <div className="list">
        {(data || []).map((s) => (
          <div className="item" key={s.id}>
            <div><div className="name">{s.ip} {s.current && <Pill tone="good">This browser</Pill>}</div><div className="meta">{(s.ua || '').slice(0, 80)} · active {fmt.ago(s.last_seen)}</div></div>
            {!s.current && <Button small kind="danger" onClick={() => run(() => del(`/api/auth/sessions/${s.id}`)).then(reload)}>Sign out</Button>}
          </div>
        ))}
        {!data?.length && <Empty>No other sessions.</Empty>}
      </div>
    </Card>
  );
}

function Appearance() {
  const [theme, setTheme] = useState(() => { try { return localStorage.getItem('gh.theme') || 'dark'; } catch { return 'dark'; } });
  useEffect(() => { document.documentElement.dataset.theme = theme; try { localStorage.setItem('gh.theme', theme); } catch { /* ignore */ } }, [theme]);
  return (
    <Card title="Appearance">
      <Segmented value={theme} onChange={setTheme} options={[{ value: 'dark', label: 'Dark' }, { value: 'light', label: 'Light' }]} />
    </Card>
  );
}

function TwoStep({ me, data, reload, boot }) {
  const [run, busy] = useAction();
  const [turningOn, setTurningOn] = useState(false);
  const [turningOff, setTurningOff] = useState(false);
  const [f, setF] = useState({ password: '', code: '' });
  const on = me.user.totp_enabled;
  const required = !!data?.mfa_required;
  if (turningOn) return <Enrol onDone={() => { setTurningOn(false); reload(); boot?.(); }} />;
  return (
    <Card title="Two-step sign-in" desc="A code from an authenticator app on your phone, asked for each time you sign in. Optional, and you can turn it off again.">
      <div className="inline"><Pill tone={on ? 'good' : 'warn'}>{on ? 'On' : 'Off'}</Pill>
        {on && <span className="sec small">{data ? `${data.recovery_left} recovery code${data.recovery_left === 1 ? '' : 's'} left` : ''}</span>}</div>
      {on && data && data.recovery_left <= 2 && <Note tone="warn">You are running low on recovery codes.</Note>}
      <div className="actions">
        {!on && <Button kind="primary" onClick={() => setTurningOn(true)}>Turn on</Button>}
        {on && <Button onClick={() => setTurningOn(true)}>Set up a new phone</Button>}
        {on && <Button kind="danger" disabled={required} onClick={() => setTurningOff(true)}>Turn off</Button>}
      </div>
      {on && required && <Note>Two-step sign-in is required for every account, so it cannot be turned off. An administrator can change that below.</Note>}
      {me.user.role === 'admin' && (
        <ToggleRow title="Require two-step sign-in for every account" desc={on ? 'Everyone will be asked to set it up the next time they sign in.' : 'Turn it on for your own account first.'}
          checked={required} disabled={!on && !required} onChange={(v) => run(() => put('/api/auth/policy', { require_mfa: v })).then(reload)} />
      )}
      {turningOff && (
        <Modal title="Turn off two-step sign-in" onClose={() => setTurningOff(false)}
          footer={<><Button onClick={() => setTurningOff(false)}>Cancel</Button><Button kind="danger" disabled={busy || !f.password || !f.code}
            onClick={async () => { if (await run(() => post('/api/auth/mfa/disable', { password: f.password, code: f.code.trim() }), 'Two-step sign-in is off.')) { setTurningOff(false); setF({ password: '', code: '' }); reload(); boot?.(); } }}>Turn off</Button></>}>
          <p className="sec">Confirm it is you. Enter your password and a current code from your phone (or a recovery code).</p>
          <Field label="Password"><Input type="password" value={f.password} onChange={(v) => setF({ ...f, password: v })} autoComplete="current-password" /></Field>
          <Field label="Code"><Input className="codebox" value={f.code} onChange={(v) => setF({ ...f, code: v })} inputMode="numeric" autoComplete="one-time-code" /></Field>
        </Modal>
      )}
    </Card>
  );
}

export default function Settings({ me, reloadMe }) {
  const { data, reload } = useData('/api/auth/me', { every: 60000 });
  return (
    <Page title="Settings" sub="Your account and how you sign in.">
      <TwoStep me={me} data={data} reload={reload} boot={reloadMe} />
      <Password />
      <Sessions />
      <Appearance />
    </Page>
  );
}

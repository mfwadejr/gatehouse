import React, { useEffect, useState } from 'react';
import { del, get, post, put, useData } from '../api.js';
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

function ConsolePort() {
  const { data, reload } = useData('/api/system/console-port', { every: 5000 });
  const [run, busy] = useAction();
  const [port, setPort] = useState('');
  const [left, setLeft] = useState(0);
  const pend = data?.pending;
  useEffect(() => {
    if (!pend) { setLeft(0); return undefined; }
    const tick = () => setLeft(Math.max(0, pend.deadline - Math.floor(Date.now() / 1000)));
    tick();
    const t = setInterval(tick, 1000);
    return () => clearInterval(t);
  }, [pend?.deadline]);
  if (!data) return null;
  const url = (p) => `https://${window.location.hostname}${p === 443 ? '' : `:${p}`}/`;
  const start = async () => {
    const n = parseInt(port, 10);
    const r = await run(() => post('/api/system/console-port', { port: n }));
    if (r) { setPort(''); reload(); window.open(url(r.port), '_blank', 'noopener'); }
  };
  const onNew = pend && data.here === pend.port;
  return (
    <Card title="Console address" desc={`This console answers on port ${data.port}. Change it if something else needs port 443.`}>
      {pend ? (
        <div className="stack">
          {onNew ? (
            <Note tone="good" title={`You reached the console on port ${pend.port}`}>It works. Keep this port? The old one (port {data.port}) stops answering a moment after you confirm. {left > 0 && `${left} seconds left.`}</Note>
          ) : (
            <Note tone="warn" title={`Trying port ${pend.port}`}>Open the new address, sign in if asked, and press Keep this port there. If you do nothing, the change is undone after {left} seconds and nothing is lost.</Note>
          )}
          <div className="actions">
            {onNew
              ? <Button kind="primary" disabled={busy} onClick={() => run(() => post('/api/system/console-port/confirm', {}), 'Saved.').then(reload)}>Keep this port</Button>
              : <Button kind="primary" onClick={() => window.open(url(pend.port), '_blank', 'noopener')}>Open {url(pend.port)}</Button>}
            <Button disabled={busy} onClick={() => run(() => post('/api/system/console-port/cancel', {}), 'Undone.').then(reload)}>Cancel the change</Button>
          </div>
        </div>
      ) : (
        <div className="stack">
          <Field label="New port" hint="A number from 1 to 65535, like 8443. The old port keeps working until you confirm the new one. Ports used by DNS, DHCP, a VPN or a port forward are refused.">
            <div className="inline"><Input value={port} onChange={setPort} placeholder="8443" inputMode="numeric" style={{ maxWidth: '8rem' }} /><Button kind="primary" disabled={busy || !port} onClick={start}>Try this port</Button></div>
          </Field>
          <p className="small muted">Forgot the port? The machine's own screen always shows the address, and the recovery screen (Alt+F2) can put it back to 443.</p>
        </div>
      )}
    </Card>
  );
}

function Updates({ me }) {
  const { data, reload } = useData('/api/system/update', { every: 15000 });
  const [run, busy] = useAction();
  const [asking, setAsking] = useState(false);
  const [code, setCode] = useState('');
  const [job, setJob] = useState(null); // { to, since } while an update is installing
  const [repo, setRepo] = useState(null);
  const [outcome, setOutcome] = useState(null);

  // While installing, the console restarts, so errors are expected; keep asking until it answers.
  useEffect(() => {
    if (!job) return undefined;
    const t = setInterval(async () => {
      try {
        const d = await get('/api/system/update');
        const r = d.result;
        if (r && r.at >= job.since && d.state === 'idle') {
          setOutcome(r);
          setJob(null);
          reload();
        }
      } catch { /* the console is restarting */ }
    }, 3000);
    return () => clearInterval(t);
  }, [job, reload]);

  if (!data) return null;
  const go = async () => {
    const r = await run(() => post('/api/system/update/install', { code }));
    if (r) { setAsking(false); setCode(''); setOutcome(null); setJob({ to: r.to, since: Math.floor(Date.now() / 1000) - 5 }); }
  };
  const start = () => { if (me?.user?.totp_enabled) setAsking(true); else go(); };
  const l = data.latest;
  return (
    <Card title="Updates" desc="Gatehouse updates itself from signed releases. Nothing is installed until you press Update now, and your settings are kept."
      actions={<Button small disabled={busy || !!job || !data.key_set} onClick={() => run(() => post('/api/system/update/check', {}), 'Checked.').then(reload)}>Check now</Button>}>
      <div className="stack">
        <div className="inline"><span>You are running version <b>{data.current}</b>.</span>{data.checked_at > 0 && <span className="small muted">Checked {fmt.ago(data.checked_at)}</span>}</div>
        {!data.key_set && <Note tone="warn" title="Updates are not set up in this build">This copy of Gatehouse has no update signing key, so it cannot install updates. Build a new installer once with a key (docs/UPDATES.md); after that, updates need no installer.</Note>}
        {data.key_set && data.error && <Note tone="warn" title="Could not look for updates">{data.error}</Note>}
        {job && <Note tone="good" title={`Installing ${job.to}`}>This takes about a minute. The console restarts and this page reconnects by itself. If anything goes wrong the old version is put back automatically.</Note>}
        {!job && outcome && (outcome.ok
          ? <Note tone="good" title={`Updated to ${outcome.to}`}>Everything is running the new version.</Note>
          : <Note tone="bad" title="The update did not work and was undone">Version {outcome.to} could not start, so {outcome.from} was put back. {outcome.error}</Note>)}
        {!job && !outcome && data.result && !data.result.ok && <Note tone="warn" title="The last update was undone">{data.result.error}</Note>}
        {!job && data.available && l && (
          <Note tone="good" title={`Version ${l.version} is available`}>
            {l.notes && <div style={{ whiteSpace: 'pre-wrap', margin: '.25rem 0 .5rem' }}>{l.notes}</div>}
            <Button kind="primary" disabled={busy || data.state !== 'idle'} onClick={start}>Update now</Button>
          </Note>
        )}
        {!job && data.key_set && !data.available && !data.error && data.checked_at > 0 && <p className="muted">You have the newest version.</p>}
        {data.needs_reboot && <Note tone="warn" title="A restart is needed">Operating system updates were installed that need a restart to finish. Restart the machine when it suits you.</Note>}
        <ToggleRow title="Install Debian security updates automatically" desc="Fixes for the operating system underneath Gatehouse. Off by default; when on, the machine checks daily." checked={data.os_security}
          onChange={(v) => run(() => put('/api/system/update/os-security', { enabled: v })).then(reload)} />
        <Field label="Where releases come from" hint="The GitHub page that publishes Gatehouse releases, as owner/name. Only releases signed with this build's key are ever installed.">
          <div className="inline"><Input value={repo ?? data.repo} onChange={setRepo} style={{ maxWidth: '18rem' }} />
            <Button disabled={busy || repo === null || repo === data.repo} onClick={() => run(() => put('/api/system/update/settings', { repo }), 'Saved.').then(() => { setRepo(null); reload(); })}>Save</Button></div>
        </Field>
      </div>
      {asking && (
        <Modal title="Install the update" onClose={() => setAsking(false)}
          footer={<><Button onClick={() => setAsking(false)}>Cancel</Button><Button kind="primary" disabled={busy || code.length < 6} onClick={go}>Install</Button></>}>
          <Field label="Code from your authenticator app"><Input value={code} onChange={setCode} inputMode="numeric" autoComplete="one-time-code" autoFocus /></Field>
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
      {me?.user?.role === 'admin' && <Updates me={me} />}
      {me?.user?.role === 'admin' && <ConsolePort />}
      <Password />
      <Sessions />
      <Appearance />
    </Page>
  );
}

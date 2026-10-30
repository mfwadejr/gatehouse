import React, { useEffect, useState } from 'react';
import { del, get, post, put, useData } from '../api.js';
import { Guard, Button, Card, Empty, Field, Form, Input, Modal, Note, Page, Pill, Segmented, Select, ToggleRow, useAction, fmt, useStepUp, stepReady, StepUp } from '../ui.jsx';
import { Enrol } from './Auth.jsx';
import Backups from './Backups.jsx';
import Changes from './Changes.jsx';
import Syslog from './Syslog.jsx';
import FlowExport from './FlowExport.jsx';
import Snmp from './Snmp.jsx';
import GuestPortal from './GuestPortal.jsx';
import BridgeMode from './BridgeMode.jsx';
import HighAvailability from './HighAvailability.jsx';
import TrafficLabels from './TrafficLabels.jsx';
import RemoteLogin from './RemoteLogin.jsx';
import Alerts from './Alerts.jsx';

function Password() {
  const [f, setF] = useState({ current: '', next: '', again: '' });
  const [run, busy] = useAction();
  const mismatch = f.next && f.again && f.next !== f.again;
  return (
    <Card enter title="Password" desc="Use at least 12 characters. A few random words is easy to remember and hard to guess.">
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
    <Card title="Two-factor sign-in" desc="A code from an authenticator app on your phone, asked for each time you sign in. Optional, and you can turn it off again.">
      <div className="inline"><Pill tone={on ? 'good' : 'warn'}>{on ? 'On' : 'Off'}</Pill>
        {on && <span className="sec small">{data ? `${data.recovery_left} recovery code${data.recovery_left === 1 ? '' : 's'} left` : ''}</span>}</div>
      {on && data && data.recovery_left <= 2 && <Note tone="warn">You are running low on recovery codes.</Note>}
      <div className="actions">
        {!on && <Button kind="primary" onClick={() => setTurningOn(true)}>Turn on</Button>}
        {on && <Button onClick={() => setTurningOn(true)}>Set up a new phone</Button>}
        {on && <Button kind="danger" disabled={required} onClick={() => setTurningOff(true)}>Turn off</Button>}
      </div>
      {on && required && <Note>Two-factor sign-in is required for every account, so it cannot be turned off. An administrator can change that below.</Note>}
      {me.user.role === 'admin' && (
        <ToggleRow title="Require two-factor sign-in for every account" desc={on ? 'Everyone will be asked to set it up the next time they sign in.' : 'Turn it on for your own account first.'}
          checked={required} disabled={!on && !required} onChange={(v) => run(() => put('/api/auth/policy', { require_mfa: v })).then(reload)} />
      )}
      {turningOff && (
        <Modal title="Turn off two-factor sign-in" onClose={() => setTurningOff(false)}
          footer={<><Button onClick={() => setTurningOff(false)}>Cancel</Button><Button kind="danger" disabled={busy || !f.password || !f.code}
            onClick={async () => { if (await run(() => post('/api/auth/mfa/disable', { password: f.password, code: f.code.trim() }), 'Two-factor sign-in is off.')) { setTurningOff(false); setF({ password: '', code: '' }); reload(); boot?.(); } }}>Turn off</Button></>}>
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
    <Card enter title="Console address" desc={`This console answers on port ${data.port}. Change it if something else needs port 443.`}>
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
            <div className="inline"><Input value={port} onChange={setPort} aria-label="New console port" placeholder="8443" inputMode="numeric" style={{ maxWidth: '8rem' }} /><Button kind="primary" disabled={busy || !port} onClick={start}>Try this port</Button></div>
          </Field>
          <p className="small muted">Forgot the port? The machine's own screen always shows the address, and the recovery screen (Alt+F2) can put it back to 443.</p>
        </div>
      )}
    </Card>
  );
}

const POWER = {
  reboot: { title: 'Restart this machine', verb: 'Restart', warn: 'The network is down for about a minute while it restarts. This page reconnects by itself.', waiting: 'Restarting…' },
  poweroff: { title: 'Shut down this machine', verb: 'Shut down', warn: 'The network goes down and stays down until someone switches the machine back on.', waiting: 'Shutting down…' },
  'restart-gatehouse': { title: 'Restart Gatehouse only', verb: 'Restart Gatehouse', warn: 'The console, DNS and DHCP are down for a few seconds. The machine itself keeps running.', waiting: 'Restarting Gatehouse…' },
};

// Waits until the machine stops answering and then answers again, then reloads the page.
function useReconnect() {
  const [waiting, setWaiting] = useState(false);
  useEffect(() => {
    if (!waiting) return undefined;
    let down = false;
    const started = Date.now();
    const t = setInterval(async () => {
      try {
        const r = await fetch('/api/health', { cache: 'no-store' });
        if (r.ok && (down || Date.now() - started > 45000)) { clearInterval(t); window.location.reload(); }
      } catch { down = true; }
    }, 3000);
    return () => clearInterval(t);
  }, [waiting]);
  return [waiting, setWaiting];
}

function PowerModal({ action, me, onClose }) {
  const meta = POWER[action];
  const [code, setCode] = useState('');
  const [run, busy] = useAction();
  const [waiting, setWaiting] = useReconnect();
  const needCode = useStepUp().everyday;
  const go = async () => { if (await run(() => post('/api/system/power', { action, code }))) setWaiting(true); };
  if (waiting) {
    return (
      <Modal title={meta.waiting} onClose={() => {}} footer={null}>
        <p>{action === 'poweroff' ? 'The machine is shutting down. You can close this page.' : 'Waiting for the machine to come back. This page reloads by itself when it does.'}</p>
      </Modal>
    );
  }
  return (
    <Modal title={meta.title} onClose={onClose}
      footer={<><Button onClick={onClose}>Cancel</Button><Button kind="primary" disabled={busy || (needCode && code.length < 6)} onClick={go}>{meta.verb}</Button></>}>
      <p>{meta.warn}</p>
      {needCode && <Field label="Code from your authenticator app"><Input value={code} onChange={setCode} inputMode="numeric" autoComplete="one-time-code" autoFocus /></Field>}
    </Modal>
  );
}

function PowerCard({ me }) {
  const [action, setAction] = useState(null);
  return (
    <Card title="Power" desc="Restart or shut down this machine, or restart just the Gatehouse programs. Asks for your authenticator code if you use one.">
      <div className="actions">
        <Button onClick={() => setAction('reboot')}>Restart this machine</Button>
        <Button onClick={() => setAction('restart-gatehouse')}>Restart Gatehouse only</Button>
        <Button onClick={() => setAction('poweroff')}>Shut down</Button>
      </div>
      {action && <PowerModal action={action} me={me} onClose={() => setAction(null)} />}
    </Card>
  );
}

const RESET = {
  network: {
    title: 'Reset network settings',
    verb: 'Reset network settings',
    lines: ['Networks, firewall rules, port forwards, DHCP, DNS names and lists, VPNs, and Wi-Fi go back to a clean start.', 'Your accounts, sign-in settings, update settings and the console port stay.'],
  },
  factory: {
    title: 'Factory reset',
    verb: 'Erase everything',
    lines: ['Everything is erased: all settings, accounts, logs and history. The setup wizard opens again.', 'Gatehouse itself and the installed system stay as they are.'],
  },
};

function ResetModal({ level, me, onClose }) {
  const meta = RESET[level];
  const [code, setCode] = useState('');
  const [word, setWord] = useState('');
  const [run, busy] = useAction();
  const [waiting, setWaiting] = useReconnect();
  const su = useStepUp();
  const [pw, setPw] = useState('');
  const go = async () => { if (await run(() => post('/api/system/reset', { level, code, password: pw, confirm: word }))) setWaiting(true); };
  if (waiting) {
    return (
      <Modal title="Resetting…" onClose={() => {}} footer={null}>
        <p>The machine is going back to its starting settings. This page reloads by itself when it answers again.</p>
        <p className="small muted">If it does not come back, the address to use is shown on the machine's own screen. Wired ports ask for an address automatically.</p>
      </Modal>
    );
  }
  return (
    <Modal title={meta.title} onClose={onClose}
      footer={<><Button onClick={onClose}>Cancel</Button><Button kind="danger" disabled={busy || !stepReady(su, code, pw) || (level === 'factory' && word.toLowerCase() !== 'reset')} onClick={go}>{meta.verb}</Button></>}>
      {meta.lines.map((l) => <p key={l}>{l}</p>)}
      <p>A copy of the settings is saved on the machine first. Download one from the Backups card above to keep it somewhere safe.</p>
      {level === 'factory' && <Field label="Type the word reset to confirm"><Input value={word} onChange={setWord} autoFocus /></Field>}
      <StepUp su={su} code={code} setCode={setCode} password={pw} setPassword={setPw} />
    </Modal>
  );
}

function ResetCard({ me }) {
  const [level, setLevel] = useState(null);
  return (
    <Card title="Reset to default" desc="Start over. A copy of your settings is always saved first, and you can download it. Asks for your authenticator code if you use one, or your password if you do not.">
      <div className="actions">
        <Button onClick={() => setLevel('network')}>Reset network settings…</Button>
        <Button kind="danger" onClick={() => setLevel('factory')}>Factory reset…</Button>
      </div>
      {level && <ResetModal level={level} me={me} onClose={() => setLevel(null)} />}
    </Card>
  );
}

const hourLabel = (h) => `${h === 0 ? 12 : h > 12 ? h - 12 : h}:00 ${h < 12 ? 'AM' : 'PM'}`;

function UpdateSteps({ steps, rollingBack }) {
  const mark = { done: '✓', active: '●', pending: '○' };
  return (
    <ol className="usteps" aria-label="Update progress">
      {steps.map((st) => (
        <li key={st.id} className={st.state}><span aria-hidden="true">{mark[st.state]}</span> {st.label}{st.state === 'active' && rollingBack ? ' (undoing)' : ''}</li>
      ))}
    </ol>
  );
}

function Updates({ me }) {
  const { data, reload } = useData('/api/system/update', { every: 15000 });
  const [run, busy] = useAction();
  const [asking, setAsking] = useState(false);
  const [code, setCode] = useState('');
  const [job, setJob] = useState(null); // { since } while an update is going on
  const [live, setLive] = useState(null); // fresher status while something is happening
  const [lost, setLost] = useState(false);
  const [repo, setRepo] = useState(null);
  const [outcome, setOutcome] = useState(null);
  const [restart, setRestart] = useState(false);
  const d = live || data;

  // An update that is already going on (the page was opened or reloaded in the middle of it).
  useEffect(() => {
    if (!job && data && (data.working || data.state !== 'idle')) setJob({ since: 0 });
  }, [data, job]);

  // While installing the console restarts, so errors are expected; keep asking until it answers.
  useEffect(() => {
    if (!job && !data?.checking) return undefined;
    let lastOk = Date.now();
    const t = setInterval(async () => {
      try {
        const x = await get('/api/system/update');
        lastOk = Date.now();
        setLost(false);
        setLive(x);
        if (job) {
          const r = x.result;
          if (!x.working && x.state === 'idle' && (x.run_error || (r && r.at >= job.since))) {
            setOutcome({ result: r && r.at >= job.since ? r : null, error: x.run_error });
            setJob(null);
            setLive(null);
            reload();
          }
        } else if (!x.checking) {
          setLive(null);
          reload();
        }
      } catch {
        if (Date.now() - lastOk > 180000) setLost(true);
      }
    }, 3000);
    return () => clearInterval(t);
  }, [job, data?.checking, reload]);

  if (!d) return null;
  const go = async () => {
    const r = await run(() => post('/api/system/update/install', { code }));
    if (r) { setAsking(false); setCode(''); setOutcome(null); setJob({ since: Math.floor(Date.now() / 1000) - 5 }); }
  };
  const everyday = useStepUp().everyday;
  const start = () => { if (everyday) setAsking(true); else go(); };
  const save = (patch) => run(() => put('/api/system/update/settings', patch)).then(reload);
  const l = d.latest;
  const steps = d.steps || [];
  const working = !!job;
  const res = outcome?.result;
  return (
    <Card title="Updates" desc="Gatehouse and the system underneath it update together, from one button. Nothing is installed until you press Update now, unless you choose Automatic below. Your settings are kept."
      actions={<Button small disabled={busy || working || d.checking || !d.key_set} onClick={() => run(() => post('/api/system/update/check', {})).then(() => { reload(); })}>Check now</Button>}>
      <div className="stack">
        <div className="inline"><span>You are running version <b>{d.current}</b>.</span>{d.checked_at > 0 && <span className="small muted">Checked {fmt.ago(d.checked_at)}</span>}</div>
        {!d.key_set && <Note tone="warn" title="Updates are not set up in this build">This copy of Gatehouse has no update signing key, so it cannot install updates. Build a new installer once with a key (docs/UPDATES.md); after that, updates need no installer.</Note>}
        {d.key_set && d.error && <Note tone="warn" title="Could not look for Gatehouse updates">{d.error}</Note>}
        {d.key_set && d.system_error && <Note tone="warn" title="Could not look at the system updates">{d.system_error}</Note>}
        {d.pair?.phase === 'failed' && d.pair.error && <Note tone="warn" title="The update of both units stopped">{d.pair.error}</Note>}
        {d.checking && !working && <p className="muted">Looking at what the system has waiting…</p>}
        {working && (
          <Note tone="good" title="Updating">
            {steps.length > 0 && <UpdateSteps steps={steps} rollingBack={d.rolling_back} />}
            <p className="small" style={{ margin: '.5rem 0 0' }}>{d.rolling_back ? 'Something went wrong, so the previous version is being put back.' : 'This takes a few minutes. The console restarts near the end and this page reconnects by itself. If anything goes wrong the old Gatehouse is put back automatically.'}</p>
            {lost && <p className="small" style={{ margin: '.5rem 0 0' }}><b>This page cannot reach the machine.</b> Look at the machine's own screen: it shows what is happening.</p>}
          </Note>
        )}
        {!working && outcome && (outcome.error
          ? <Note tone="bad" title="The update could not start">{outcome.error}</Note>
          : res?.ok
            ? <Note tone="good" title={res.from === res.to ? 'System updates installed' : `Updated to ${res.to}`}>
                {res.from === res.to ? 'Gatehouse was already the newest version.' : 'Everything is running the new version.'}
                {res.system && res.system.packages > 0 && ` ${res.system.packages} system packages were updated.`}
                {res.system?.skipped && ` The system updates were skipped: ${res.system.skipped}`}
              </Note>
            : <Note tone="bad" title={res?.rolled_back ? 'The update did not work and was undone' : 'The update did not work'}>{res?.error}</Note>)}
        {!working && !outcome && d.result && !d.result.ok && <Note tone="warn" title={d.result.rolled_back ? 'The last update was undone' : 'The last update did not work'}>{d.result.error}</Note>}
        {!working && d.available && (
          <Note tone="good" title="Updates are ready">
            <ul className="ulist">
              {d.gatehouse_new && l && <li><b>Gatehouse {l.version}</b>{l.notes && <div style={{ whiteSpace: 'pre-wrap' }} className="small">{l.notes}</div>}</li>}
              {d.system_count > 0 && <li><b>{d.system_count} system update{d.system_count === 1 ? '' : 's'}</b>{d.system_names?.length > 0 && <span className="small muted"> ({d.system_names.slice(0, 6).join(', ')}{d.system_count > 6 ? ', …' : ''})</span>}</li>}
            </ul>
            <Button kind="primary" disabled={busy || d.state !== 'idle'} onClick={start}>Update now</Button>
          </Note>
        )}
        {!working && d.key_set && !d.available && !d.error && !d.system_error && !d.checking && d.checked_at > 0 && <p className="muted">Everything is up to date.</p>}
        {d.needs_reboot && !working && <Note tone="warn" title="A restart is needed">The system was updated and needs a restart to finish. The network is down for about a minute while it restarts; pick a moment that suits you.<div style={{ marginTop: '.5rem' }}><Button onClick={() => setRestart(true)}>Restart now</Button></div></Note>}
        <Field label="Updating" hint="Off: only when you press Check now. Notify: Gatehouse looks twice a day and tells you when something is ready. Automatic: it installs by itself in the hour you choose below.">
          <Segmented value={d.mode} onChange={(v) => save({ mode: v })} options={[{ value: 'off', label: 'Off' }, { value: 'notify', label: 'Notify' }, { value: 'auto', label: 'Automatic' }]} />
        </Field>
        {d.mode === 'auto' && (
          <Field label="Start automatic updates at" hint="Gatehouse restarts for about a minute while it updates, so pick a quiet hour (the machine's own clock).">
            <Select value={String(d.window)} onChange={(v) => save({ window: Number(v) })} options={Array.from({ length: 24 }, (_, h) => ({ value: String(h), label: hourLabel(h) }))} style={{ maxWidth: '10rem' }} />
          </Field>
        )}
        <Field label="Where releases come from" hint="The GitHub page that publishes Gatehouse releases, as owner/name. Only releases signed with this build's key are ever installed.">
          <Form className="inline" onSubmit={() => run(() => put('/api/system/update/settings', { repo }), 'Saved.').then(() => { setRepo(null); reload(); })}><Input value={repo ?? d.repo} onChange={setRepo} style={{ maxWidth: '18rem' }} />
            <Button type="submit" disabled={busy || repo === null || repo === d.repo}>Save</Button></Form>
        </Field>
      </div>
      {restart && <PowerModal action="reboot" me={me} onClose={() => setRestart(false)} />}
      {asking && (
        <Modal title="Install the updates" onClose={() => setAsking(false)}
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
      {me?.user?.role === 'admin' && <Guard what="Updates"><Updates me={me} /></Guard>}
      {me?.user?.role === 'admin' && <Guard what="PowerCard"><PowerCard me={me} /></Guard>}
      {me?.user?.role === 'admin' && <Guard what="ConsolePort"><ConsolePort /></Guard>}
      {me?.user?.role === 'admin' && <Guard what="Alerts"><Alerts /></Guard>}
      {me?.user?.role === 'admin' && <Guard what="RemoteLogin"><RemoteLogin me={me} /></Guard>}
      {me?.user?.role === 'admin' && (
        <Card title="My network" desc="A printable summary for a handover or your records: networks, addresses, devices, open ports and how to get back in. It never contains passwords or keys.">
          <a className="btn" href="/api/summary/print" target="_blank" rel="noreferrer">Open the printable page</a>
        </Card>
      )}
      {me?.user?.role === 'admin' && <Guard what="Changes"><Changes me={me} /></Guard>}
      {me?.user?.role === 'admin' && <details className="more"><summary>Advanced</summary><Guard what="High availability"><HighAvailability me={me} /></Guard><Guard what="Bridge mode"><BridgeMode /></Guard><Guard what="Traffic labels"><TrafficLabels /></Guard><Guard what="Syslog"><Syslog /></Guard><Guard what="Guest sign-in page"><GuestPortal /></Guard><Guard what="Flow export"><FlowExport /></Guard><Guard what="SNMP"><Snmp /></Guard></details>}
      {me?.user?.role === 'admin' && <Guard what="Backups"><Backups me={me} /></Guard>}
      {me?.user?.role === 'admin' && <Guard what="ResetCard"><ResetCard me={me} /></Guard>}
      <Password />
      <Sessions />
      <Appearance />
    </Page>
  );
}

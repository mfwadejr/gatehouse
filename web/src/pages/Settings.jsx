import GetStarted from './GetStarted.jsx';
import MapLocation from './MapLocation.jsx';
import { useTheme } from '../theme.js';
import React, { useEffect, useState } from 'react';
import { del, get, post, put, useData } from '../api.js';
import { Guard, Button, Card, Empty, Field, Form, Input, Modal, Note, Page, Pill, Segmented, Select, Tabs, ToggleRow, useAction, useCheck, CheckLine, fmt, useStepUp, useToast, DangerStep } from '../ui.jsx';
import { Enrol } from './Auth.jsx';
import Backups from './Backups.jsx';
import Changes from './Changes.jsx';
import Syslog from './Syslog.jsx';
import FlowExport from './FlowExport.jsx';
import Snmp from './Snmp.jsx';
import Storage from './Storage.jsx';
import GuestPortal from './GuestPortal.jsx';
import BridgeMode from './BridgeMode.jsx';
import DeviceMode from './DeviceMode.jsx';
import MgmtPort from './MgmtPort.jsx';
import HighAvailability from './HighAvailability.jsx';
import RemoteLogin from './RemoteLogin.jsx';
import Alerts from './Alerts.jsx';
import { PowerModal, useReconnect } from './PowerModal.jsx';

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

// A browser string such as "Mozilla/5.0 (X11; Linux x86_64) ... Chrome/120" in words people use.
function browserName(ua) {
  const u = ua || '';
  if (!u) return 'Unknown device';
  const app = /Edg\//.test(u) ? 'Edge' : /OPR\/|Opera/.test(u) ? 'Opera' : /Firefox\//.test(u) ? 'Firefox' : /HeadlessChrome/.test(u) ? 'A test browser' : /Chrome\//.test(u) ? 'Chrome' : /Safari\//.test(u) ? 'Safari' : null;
  const os = /iPhone|iPad/.test(u) ? 'iPhone or iPad' : /Android/.test(u) ? 'Android' : /Mac OS X|Macintosh/.test(u) ? 'a Mac' : /Windows/.test(u) ? 'Windows' : /Linux|X11/.test(u) ? 'Linux' : '';
  if (app) return os ? `${app} on ${os}` : app;
  if (/^Go-http-client/.test(u)) return 'A script (Go client)';
  if (/curl/i.test(u)) return 'A script (curl)';
  return u.slice(0, 40);
}

function Sessions() {
  const { data, reload } = useData('/api/auth/sessions');
  const [run] = useAction();
  return (
    <Card title="Where you are signed in" desc="Browsers and devices currently signed in to this console." actions={<Button small onClick={() => run(() => post('/api/auth/sessions/revoke-others'), 'Signed out everywhere else.').then(reload)}>Sign out everywhere else</Button>}>
      <div className="list">
        {(data || []).map((s) => (
          <div className="item" key={s.id}>
            <div><div className="name">{s.ip} {s.current && <Pill tone="good">This browser</Pill>}</div><div className="meta">{browserName(s.ua)} · active {fmt.ago(s.last_seen)}</div></div>
            {!s.current && <Button small kind="danger" onClick={() => run(() => del(`/api/auth/sessions/${s.id}`)).then(reload)}>Sign out</Button>}
          </div>
        ))}
        {!data?.length && <Empty>No other sessions.</Empty>}
      </div>
    </Card>
  );
}

function Appearance() {
  const [theme, setTheme] = useTheme();
  return (
    <Card title="Appearance" desc="How the console looks on this browser.">
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


function PowerCard({ me }) {
  const [action, setAction] = useState(null);
  return (
    <Card title="Power" desc="Restart or shut down this machine, or restart just the NopeWarden programs. Asks for your authenticator code if you use one.">
      <div className="actions">
        <Button onClick={() => setAction('reboot')}>Restart this machine</Button>
        <Button onClick={() => setAction('restart-nopewarden')}>Restart NopeWarden only</Button>
        <Button onClick={() => setAction('poweroff')}>Shut down</Button>
      </div>
      {action && <PowerModal action={action} me={me} onClose={() => setAction(null)} />}
    </Card>
  );
}

const RESET = {
  defaults: {
    title: 'Restore default settings',
    verb: 'Restore default settings',
    lines: ['Your preferences go back to how a new machine starts: backup schedule, alert choices, how long logs are kept, update checks and what the console shows.'],
    stays: 'Networks, firewall rules, DHCP, DNS, VPNs, accounts, sign-in settings and all logs stay exactly as they are.',
  },
  network: {
    title: 'Reset network settings',
    verb: 'Reset network settings',
    lines: ['Networks, firewall rules, port forwards, DHCP, DNS names and lists, and VPNs go back to a clean start.'],
    stays: 'Your accounts, sign-in settings, update settings and the console port stay.',
  },
  factory: {
    title: 'Factory reset',
    verb: 'Erase everything',
    lines: ['Everything is erased: all settings, accounts, logs and history. The setup wizard opens again.'],
    stays: 'NopeWarden itself and the installed system stay as they are.',
  },
};

function ResetModal({ level, me, onClose }) {
  const meta = RESET[level];
  const [code, setCode] = useState('');
  const [word, setWord] = useState('');
  const [run, busy] = useAction();
  const [waiting, setWaiting] = useReconnect();
  const su = useStepUp();
  const toast = useToast();
  const [pw, setPw] = useState('');
  const ready = pw.length > 0 && (su.danger !== 'both' || code.trim().length >= 6) && (level !== 'factory' || word.toLowerCase() === 'reset');
  const go = async () => {
    if (!(await run(() => post('/api/system/reset', { level, code, password: pw, confirm: word })))) return;
    if (level === 'defaults') { toast('Settings were restored to their defaults.', 'good'); onClose(); return; }
    setWaiting(true);
  };
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
      footer={<><Button onClick={onClose}>Cancel</Button><Button kind="danger" disabled={busy || !ready} onClick={go}>{meta.verb}</Button></>}>
      {meta.lines.map((l) => <p key={l}>{l}</p>)}
      <p><strong>What stays:</strong> {meta.stays}</p>
      <p>A copy of the settings is saved on the machine first. Download one from the Backups card above to keep it somewhere safe.</p>
      {level === 'factory' && <Field label="Type the word reset to confirm"><Input value={word} onChange={setWord} /></Field>}
      <DangerStep su={su} code={code} setCode={setCode} pw={pw} setPw={setPw} />
    </Modal>
  );
}

function ResetCard({ me }) {
  const [level, setLevel] = useState(null);
  const row = (cls, level, title, text, label) => (
    <div className={`dz-card ${cls}`}>
      <div><h3>{title}</h3><p>{text}</p></div>
      <Button kind={level === 'factory' ? 'danger' : undefined} onClick={() => setLevel(level)}>{label}</Button>
    </div>
  );
  return (
    <section className="dz">
      <h2>Danger zone</h2>
      <p className="desc">These change a lot and ask for your password every time, plus your authenticator code if you use one. A copy of your settings is always saved first, so you can undo it.</p>
      {row('dz-1', 'defaults', 'Restore default settings', 'Only your preferences go back to the defaults. Networks, accounts and logs stay.', 'Restore default settings…')}
      {row('dz-2', 'network', 'Reset network settings', 'Networks, firewall, DHCP, DNS and VPNs start clean. Accounts and update settings stay.', 'Reset network settings…')}
      {row('dz-3', 'factory', 'Factory reset', 'Everything is erased and the setup wizard opens again.', 'Factory reset…')}
      {level && <ResetModal level={level} me={me} onClose={() => setLevel(null)} />}
    </section>
  );
}


const TABS = [
  { value: 'general', label: 'General' },
  { value: 'account', label: 'Account & sign-in' },
  { value: 'alerts', label: 'Alerts & logs' },
  { value: 'backup', label: 'Backup & recovery' },
  { value: 'advanced', label: 'Advanced' },
];
const TAB_KEY = 'gh.settings.tab';
const readTab = () => {
  const seg = window.location.hash.replace(/^#\/?/, '').split('?')[0].split('/')[1];
  if (TABS.some((t) => t.value === seg)) return seg;
  try { const v = localStorage.getItem(TAB_KEY); if (TABS.some((t) => t.value === v)) return v; } catch { /* ignore */ }
  return 'general';
};

export default function Settings({ me, reloadMe }) {
  const { data, reload } = useData('/api/auth/me', { every: 60000 });
  const admin = me?.user?.role === 'admin';
  const [tab, setTab] = useState(readTab);
  // Search and links can open a tab with #/settings/<tab>.
  useEffect(() => {
    const on = () => { if (window.location.hash.replace(/^#\/?/, '').split('?')[0].split('/')[0] === 'settings') setTab(readTab()); };
    window.addEventListener('hashchange', on);
    return () => window.removeEventListener('hashchange', on);
  }, []);
  const choose = (v) => {
    setTab(v);
    try { localStorage.setItem(TAB_KEY, v); } catch { /* ignore */ }
    window.history.replaceState(null, '', `#/settings/${v}`);
  };
  return (
    <Page title="Settings" sub="Your account and how this box is run.">
      {admin && <Tabs value={tab} onChange={choose} tabs={TABS} />}
      {admin && tab === 'general' && (
        <>
          <Guard what="Get started"><GetStarted /></Guard>
          <Guard what="Device mode"><DeviceMode /></Guard>
          <Guard what="Management port"><MgmtPort /></Guard>
          <Guard what="PowerCard"><PowerCard me={me} /></Guard>
          <Guard what="ConsolePort"><ConsolePort /></Guard>
          <Guard what="Map location"><MapLocation /></Guard>
        </>
      )}
      {(!admin || tab === 'account') && (
        <>
          <TwoStep me={me} data={data} reload={reload} boot={reloadMe} />
          <Password />
          <Sessions />
          {admin && <Guard what="RemoteLogin"><RemoteLogin me={me} /></Guard>}
        </>
      )}
      {admin && tab === 'alerts' && (
        <>
          <Guard what="Alerts"><Alerts /></Guard>
          <Guard what="Syslog"><Syslog /></Guard>
          <Guard what="Flow export"><FlowExport /></Guard>
          <Guard what="SNMP"><Snmp /></Guard>
          <Guard what="Storage"><Storage /></Guard>
        </>
      )}
      {admin && tab === 'backup' && (
        <>
          <Guard what="Backups"><Backups me={me} /></Guard>
          <Guard what="Changes"><Changes me={me} /></Guard>
          <Guard what="ResetCard"><ResetCard me={me} /></Guard>
        </>
      )}
      {admin && tab === 'advanced' && (
        <>
          <div id="set-ha"><Guard what="High availability"><HighAvailability me={me} /></Guard></div>
          <Guard what="Bridge mode"><BridgeMode /></Guard>
          <Guard what="Guest sign-in page"><GuestPortal /></Guard>
        </>
      )}
      {(!admin || tab === 'general') && <Appearance />}
    </Page>
  );
}

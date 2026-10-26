import React, { useRef, useState } from 'react';
import { del, post, put, saveFile, sendFile, useData } from '../api.js';
import { Button, Card, Empty, Field, Input, Modal, Note, Pill, Select, useAction, fmt, confirmBox } from '../ui.jsx';

// Backups of the settings: made on a schedule, checked when they are made, and checkable again by
// trying a restore without changing anything. Nothing here touches the network until a restore is
// confirmed, and a restore saves a backup of the current settings first.

const HOURS = Array.from({ length: 24 }, (_, i) => ({ value: String(i), label: `${String(i).padStart(2, '0')}:00` }));
const bytes = (n) => (n > 1048576 ? `${(n / 1048576).toFixed(1)} MB` : `${Math.max(1, Math.round(n / 1024))} KB`);

const LABELS = [
  ['networks', 'Networks'], ['zones', 'Firewall networks'], ['rules', 'Firewall rules'], ['scopes', 'DHCP scopes'],
  ['reservations', 'Reserved addresses'], ['vpn_devices', 'VPN devices'], ['wifi', 'Wi-Fi cards'], ['devices', 'Named devices'], ['users', 'Accounts'],
];

function statusPill(b) {
  if (!b.report) return <Pill tone="">Not checked</Pill>;
  if (!b.report.ok) return <Pill tone="bad">Failed its check</Pill>;
  if (b.hand_checked) return <Pill tone="good">Checked by hand</Pill>;
  return <Pill tone="good">Passed its check</Pill>;
}

function CheckModal({ b, now, onClose, onChanged }) {
  const [run, busy] = useAction();
  const [pw, setPw] = useState('');
  const [rep, setRep] = useState(b.report);
  const [cur, setCur] = useState(now);
  const [done, setDone] = useState(!!b.hand_checked);
  const check = async () => {
    const r = await run(() => post(`/api/backups/${b.name}/check`, { password: pw }));
    if (r) { setRep(r.report); setCur(r.now); onChanged(); }
  };
  return (
    <Modal title="Check this backup" onClose={onClose} footer={<Button onClick={onClose}>Close</Button>}>
      <div className="stack">
        <p className="small muted">This opens the backup the way a restore would and looks at it. Nothing on this machine is changed.</p>
        {b.protected && <Field label="Password for this backup"><Input type="password" value={pw} onChange={setPw} /></Field>}
        <Button disabled={busy} onClick={check}>{rep ? 'Check again' : 'Run the check'}</Button>
        {rep && (
          <>
            <div className="list">
              {rep.checks.map((c) => (
                <div className="item" key={c.name}><div><div className="name">{c.name} <Pill tone={c.ok ? 'good' : 'bad'}>{c.ok ? 'Passed' : 'Failed'}</Pill></div><div className="small muted">{c.detail}</div></div></div>
              ))}
            </div>
            {rep.ok && (
              <>
                <h3 style={{ fontSize: 'var(--font-size-base)', fontWeight: 'var(--font-weight-semibold)' }}>What a restore would bring back</h3>
                <table className="tbl small">
                  <thead><tr><th /><th>In this backup</th><th>Here now</th></tr></thead>
                  <tbody>{LABELS.map(([k, l]) => (<tr key={k}><td>{l}</td><td>{rep.contents[k]}</td><td className={cur && cur[k] !== rep.contents[k] ? '' : 'muted'}>{cur ? cur[k] : ''}</td></tr>))}</tbody>
                </table>
                {done
                  ? <Note tone="good">Checked by hand{b.hand_by ? ` by ${b.hand_by}` : ''}{b.hand_checked ? ` on ${fmt.dt(b.hand_checked)}` : ''}.</Note>
                  : <Button disabled={busy} onClick={async () => { if (await run(() => post(`/api/backups/${b.name}/hand-check`, {}), 'Marked as checked by hand.')) { setDone(true); onChanged(); } }}>I looked at this and it is what I expect</Button>}
              </>
            )}
          </>
        )}
      </div>
    </Modal>
  );
}

function RestoreModal({ b, me, onClose, onDone }) {
  const [run, busy] = useAction();
  const [word, setWord] = useState('');
  const [code, setCode] = useState('');
  const [pw, setPw] = useState('');
  const [res, setRes] = useState(null);
  const needCode = !!me?.user?.totp_enabled;
  if (res) {
    return (
      <Modal title="Settings restored" onClose={() => { onDone(); onClose(); }} footer={<Button kind="primary" onClick={() => { onDone(); onClose(); }}>Close</Button>}>
        <p>The settings from this backup are now saved. They are not live yet: go to the pending changes bar, review them, and apply them. If something goes wrong, Gatehouse puts the previous settings back by itself.</p>
        <p className="small muted">A backup of what was here before is in the list ({res.safety}). Accounts were taken from {res.accounts_from}.{res.key_changed ? ' This backup came from a different machine, so its sign-in accounts replaced the ones here. Sign in with the accounts from the backup.' : ''}</p>
      </Modal>
    );
  }
  return (
    <Modal title="Restore this backup?" onClose={onClose}
      footer={<><Button onClick={onClose}>Cancel</Button><Button kind="danger" disabled={busy || word.toLowerCase() !== 'restore' || (needCode && code.length < 6)} onClick={async () => { const r = await run(() => post(`/api/backups/${b.name}/restore`, { confirm: word.toLowerCase(), code, password: pw })); if (r) setRes(r); }}>Restore</Button></>}>
      <div className="stack">
        <p>Your networks, firewall, DHCP, DNS, VPN and Wi-Fi settings are replaced with the ones in the backup made {fmt.dt(b.created)}. Nothing changes on the network until you apply it.</p>
        <p className="small muted">Your current settings are saved as a backup first, so you can come back. Sign-in accounts stay as they are, unless the backup came from a different machine.</p>
        {b.protected && <Field label="Password for this backup"><Input type="password" value={pw} onChange={setPw} /></Field>}
        <Field label="Type the word restore to confirm"><Input value={word} onChange={setWord} autoFocus /></Field>
        {needCode && <Field label="Code from your authenticator app"><Input value={code} onChange={setCode} inputMode="numeric" autoComplete="one-time-code" /></Field>}
      </div>
    </Modal>
  );
}

function ProtectedDownload({ b, onClose }) {
  const [run, busy] = useAction();
  const [pw, setPw] = useState('');
  return (
    <Modal title="Download with a password" onClose={onClose}
      footer={<><Button onClick={onClose}>Cancel</Button><Button kind="primary" disabled={busy || pw.length < 8} onClick={async () => { if (await run(() => saveFile('POST', `/api/backups/${b.name}/download`, { password: pw }, b.name.replace(/\.ghbk$/, '') + '-protected.ghbk'))) onClose(); }}>Download</Button></>}>
      <p>A backup holds the key to your saved Wi-Fi, internet and VPN passwords. A password keeps it safe if the file is stored somewhere shared. There is no way to recover a lost password, so keep it somewhere safe.</p>
      <Field label="Password (at least 8 characters)"><Input type="password" value={pw} onChange={setPw} autoFocus /></Field>
    </Modal>
  );
}

export default function Backups({ me }) {
  const { data, reload } = useData('/api/backups', { every: 30000 });
  const [run, busy] = useAction();
  const [check, setCheck] = useState(null);
  const [restore, setRestore] = useState(null);
  const [pdl, setPdl] = useState(null);
  const [cfg, setCfg] = useState(null);
  const [upPw, setUpPw] = useState('');
  const file = useRef(null);
  if (!data) return null;
  const c = cfg || data.config;
  const list = data.backups || [];
  const lastGood = list.find((b) => b.report && b.report.ok);
  const handNow = list.find((b) => b.hand_checked);
  const stale = !handNow || Date.now() / 1000 - handNow.hand_checked > 90 * 86400;
  const save = async (patch) => { const n = { ...c, ...patch }; setCfg(n); if (await run(() => put('/api/backups/config', { ...n, hour: Number(n.hour), keep: Number(n.keep) }), 'Saved.')) { setCfg(null); reload(); } };
  return (
    <Card title="Backups" desc="Copies of your settings, made on a schedule and checked as they are made. A backup is only worth having if it restores, so you can also check one without changing anything."
      actions={<Button small kind="primary" disabled={busy} onClick={async () => { if (await run(() => post('/api/backups', {}), 'Backup made.')) reload(); }}>Back up now</Button>}>
      <div className="stack">
        <div className="inline" style={{ alignItems: 'flex-end' }}>
          <Field label="Make a backup"><Select value={c.schedule} onChange={(v) => save({ schedule: v })} options={[{ value: 'daily', label: 'Every day' }, { value: 'weekly', label: 'Every week' }, { value: 'off', label: 'Only when I ask' }]} /></Field>
          {c.schedule !== 'off' && <Field label="At"><Select value={String(c.hour)} onChange={(v) => save({ hour: Number(v) })} options={HOURS} /></Field>}
          <Field label="Keep the newest"><Select value={String(c.keep)} onChange={(v) => save({ keep: Number(v) })} options={[3, 5, 7, 10, 14, 30, 60].map((n) => ({ value: String(n), label: String(n) }))} /></Field>
        </div>
        {!lastGood && <Note tone="warn" title="No good backup yet">Press Back up now to make one.</Note>}
        {lastGood && stale && <Note tone="" title="Try a restore check">No backup has been checked by hand in the last 90 days. Open Check on a backup, look at what is inside, and confirm it. That is how you know it will work on the day you need it.</Note>}
        {!list.length ? <Empty>No backups yet.</Empty> : (
          <div className="list">
            {list.map((b) => (
              <div className="item" key={b.name} style={{ alignItems: 'flex-start' }}>
                <div>
                  <div className="name">{fmt.dt(b.created)} {statusPill(b)}{b.protected && <Pill tone="">Password</Pill>}</div>
                  <div className="small muted">{b.kind} · {bytes(b.size)}{b.report?.ok ? ` · ${b.report.contents.rules} rules, ${b.report.contents.networks} networks` : ''}</div>
                </div>
                <div className="actions">
                  <Button small onClick={() => setCheck(b)}>Check</Button>
                  <Button small kind="danger" onClick={() => setRestore(b)}>Restore…</Button>
                  <Button small onClick={() => saveFile('GET', `/api/backups/${b.name}/download`, undefined, b.name)}>Download</Button>
                  {!b.protected && <Button small onClick={() => setPdl(b)}>Download with password</Button>}
                  <Button small icon="trash" onClick={async () => { if (await confirmBox('Delete this backup?')) { if (await run(() => del(`/api/backups/${b.name}`), 'Deleted.')) reload(); } }}>Delete</Button>
                </div>
              </div>
            ))}
          </div>
        )}
        <div className="inline" style={{ alignItems: 'flex-end' }}>
          <Field label="Use a backup from somewhere else" hint="Choose a backup file. It is added to the list, checked, and not restored until you say so.">
            <input ref={file} type="file" accept=".ghbk,.db" />
          </Field>
          <Field label="Its password, if it has one"><Input type="password" value={upPw} onChange={setUpPw} /></Field>
          <Button disabled={busy} onClick={async () => {
            const f = file.current?.files?.[0];
            if (!f) return;
            if (await run(() => sendFile('/api/backups/upload', f, upPw ? { 'X-Backup-Password': upPw } : {}), 'Added to the list.')) { file.current.value = ''; setUpPw(''); reload(); }
          }}>Add it</Button>
        </div>
        <p className="small muted">To keep a copy somewhere else, use Download and save the file on another computer or drive. Backups made on this machine are lost with it.</p>
      </div>
      {check && <CheckModal b={check} now={data.now} onClose={() => setCheck(null)} onChanged={reload} />}
      {restore && <RestoreModal b={restore} me={me} onClose={() => setRestore(null)} onDone={reload} />}
      {pdl && <ProtectedDownload b={pdl} onClose={() => setPdl(null)} />}
    </Card>
  );
}

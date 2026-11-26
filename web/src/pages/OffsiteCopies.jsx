import React, { useState } from 'react';
import { post, put, useData } from '../api.js';
import { Button, Empty, Field, Input, Note, Pill, Select, ToggleRow, useAction, fmt } from '../ui.jsx';

// Copies of each backup kept somewhere other than this machine, encrypted with a password of the
// person's own. Passwords and keys typed here are sent once and never shown again.

const KINDS = {
  s3: 'Cloud storage (Amazon S3, Backblaze B2, Wasabi, Cloudflare R2 and others)',
  webdav: 'Nextcloud, ownCloud, Synology or another WebDAV server',
  email: 'Email, as an attachment',
};

function blank(kind) {
  const id = `n${Math.random().toString(16).slice(2, 8)}`;
  const base = { id, kind, label: '', enabled: false };
  if (kind === 's3') return { ...base, s3: { endpoint: '', region: '', bucket: '', prefix: 'nopewarden', access_key: '', secret: '', path_style: false } };
  if (kind === 'webdav') return { ...base, dav: { url: '', dir: 'NopeWarden', user: '', pass: '' } };
  return { ...base, mail: { to: '' } };
}

function When({ st }) {
  if (!st || !st.at) return <span className="small muted">Nothing sent yet.</span>;
  if (st.ok) return <span className="small muted">Last copy sent {fmt.dt(st.at)} ({st.name}).</span>;
  return <span className="small bad-text">Last try failed {fmt.dt(st.at)}: {st.error}. It will try again within the hour.</span>;
}

function Dest({ d, st, set, remove, test, busy }) {
  const s3 = (k) => (v) => set({ ...d, s3: { ...d.s3, [k]: v } });
  const dav = (k) => (v) => set({ ...d, dav: { ...d.dav, [k]: v } });
  return (
    <div className="item" style={{ alignItems: 'flex-start', display: 'block' }}>
      <div className="stack">
        <div className="name">{d.label || 'New place'} <Pill tone="">{d.kind === 's3' ? 'Cloud storage' : d.kind === 'webdav' ? 'WebDAV' : 'Email'}</Pill></div>
        <Field label="A name for this place"><Input value={d.label} onChange={(v) => set({ ...d, label: v })} placeholder="For example, Backblaze or Home NAS" /></Field>
        {d.kind === 's3' && (
          <div className="inline" style={{ alignItems: 'flex-start' }}>
            <Field label="Bucket"><Input value={d.s3.bucket} onChange={s3('bucket')} /></Field>
            <Field label="Folder in the bucket" hint="Optional."><Input value={d.s3.prefix} onChange={s3('prefix')} /></Field>
            <Field label="Server address" hint="Leave empty for Amazon S3. Others give you one, like https://s3.us-west-002.backblazeb2.com."><Input value={d.s3.endpoint} onChange={s3('endpoint')} placeholder="https://" /></Field>
            <Field label="Region" hint="Empty is fine for most."><Input value={d.s3.region} onChange={s3('region')} placeholder="us-east-1" /></Field>
            <Field label="Access key"><Input value={d.s3.access_key} onChange={s3('access_key')} autoComplete="off" /></Field>
            <Field label="Secret key" hint={d.s3.secret_set ? 'Already saved. Type a new one only to change it.' : ''}><Input type="password" value={d.s3.secret} onChange={s3('secret')} autoComplete="new-password" /></Field>
          </div>
        )}
        {d.kind === 's3' && <ToggleRow title="Put the bucket name in the path" desc="Needed by most services other than Amazon. Turn it on if a test says the bucket is not found." checked={!!d.s3.path_style} onChange={s3('path_style')} />}
        {d.kind === 'webdav' && (
          <div className="inline" style={{ alignItems: 'flex-start' }}>
            <Field label="Address" hint="The WebDAV address, like https://cloud.example.com/remote.php/dav/files/you"><Input value={d.dav.url} onChange={dav('url')} placeholder="https://" /></Field>
            <Field label="Folder" hint="Made for you if it is missing."><Input value={d.dav.dir} onChange={dav('dir')} /></Field>
            <Field label="User name"><Input value={d.dav.user} onChange={dav('user')} autoComplete="off" /></Field>
            <Field label="Password" hint={d.dav.pass_set ? 'Already saved. Type a new one only to change it.' : 'For Nextcloud, make an app password in its security settings.'}><Input type="password" value={d.dav.pass} onChange={dav('pass')} autoComplete="new-password" /></Field>
          </div>
        )}
        {d.kind === 'email' && (
          <Field label="Send to" hint="Leave empty to use your alerts address. Uses the mail server from Alerts. Backups over 15 MB are too big to send this way.">
            <Input value={d.mail.to} onChange={(v) => set({ ...d, mail: { to: v } })} placeholder="you@example.com" />
          </Field>
        )}
        <ToggleRow title="Send a copy here after every backup" checked={d.enabled} onChange={(v) => set({ ...d, enabled: v })} />
        <When st={st} />
        <div className="actions">
          <Button small disabled={busy} onClick={() => test(d)}>Save and test</Button>
          <Button small icon="trash" onClick={remove}>Remove</Button>
        </div>
      </div>
    </div>
  );
}

export default function OffsiteCopies() {
  const { data, reload } = useData('/api/backups/offsite', { every: 30000 });
  const [run, busy] = useAction();
  const [draft, setDraft] = useState(null);
  const [pw, setPw] = useState('');
  const [msg, setMsg] = useState({});
  if (!data) return null;
  const c = draft || data.config;
  const status = data.status || {};
  const edit = (patch) => setDraft({ ...c, ...patch });
  const body = () => ({ ...c, password: pw, keep: Number(c.keep), remind_days: Number(c.remind_days) });
  const save = async () => {
    const r = await run(() => put('/api/backups/offsite', body()), 'Saved.');
    if (r !== false) { setDraft(null); setPw(''); reload(); }
    return r !== false;
  };
  const test = async (d) => {
    if (!(await save())) return;
    const r = await run(() => post(`/api/backups/offsite/${d.id}/test`, {}));
    setMsg((m) => ({ ...m, [d.id]: r?.message || '' }));
  };
  return (
    <div className="stack">
      <h3 style={{ fontSize: 'var(--font-size-base)', fontWeight: 'var(--font-weight-semibold)' }}>Copies off this machine</h3>
      <p className="small muted">A backup on this machine is lost with it. Here you can have a copy sent somewhere else after every backup. Copies are scrambled with a password of your own before they leave, so the place that keeps them cannot read them.</p>
      <div className="inline" style={{ alignItems: 'flex-start' }}>
        <Field label="Password for the copies" hint={data.config.password_set ? 'A password is set. Type a new one only to change it; older copies keep the old one.' : 'At least 8 characters.'}>
          <Input type="password" value={pw} onChange={(v) => { setPw(v); if (!draft) setDraft(c); }} autoComplete="new-password" />
        </Field>
        <Field label="Keep this many copies in each place" hint="Older copies there are deleted.">
          <div style={{ width: '9rem' }}><Select value={String(c.keep)} onChange={(v) => edit({ keep: Number(v) })} options={[3, 7, 14, 30, 60].map((n) => ({ value: String(n), label: String(n) }))} /></div>
        </Field>
      </div>
      <Note tone="warn" title="Write this password down somewhere safe">It is the only thing that opens a copy, and it is saved on this machine too, so if this machine is lost, this machine cannot remind you. To restore a copy, download it from where it is kept and use Use a backup from somewhere else, with this password.</Note>
      {!c.dests.length && <Empty>No places yet. Add one below.</Empty>}
      {c.dests.map((d) => (
        <React.Fragment key={d.id}>
          <Dest d={d} st={status[d.id]} busy={busy} test={test}
            set={(n) => edit({ dests: c.dests.map((x) => (x.id === d.id ? n : x)) })}
            remove={() => edit({ dests: c.dests.filter((x) => x.id !== d.id) })} />
          {msg[d.id] && <Note tone="good">{msg[d.id]}</Note>}
        </React.Fragment>
      ))}
      <div className="inline" style={{ alignItems: 'flex-end' }}>
        <Field label="Add a place">
          <Select value="" onChange={(k) => k && edit({ dests: [...c.dests, blank(k)] })} options={[{ value: '', label: 'Choose…' }, ...Object.entries(KINDS).map(([value, label]) => ({ value, label }))]} />
        </Field>
        <Button kind="primary" disabled={busy || !draft} onClick={save}>Save</Button>
        <Button disabled={busy || !data.summary.enabled || !!draft} onClick={async () => { const r = await run(() => post('/api/backups/offsite/send', {}), 'Backup made and sent.'); if (r !== false) reload(); }}>Back up and send now</Button>
      </div>
      <ToggleRow title="Remind me to download a backup" desc="A reminder on the Overview when no backup has been downloaded to another computer for a while. Turn this off if copies go off this machine automatically." checked={!!c.remind_on} onChange={(v) => edit({ remind_on: v })} />
    </div>
  );
}

import React, { useEffect, useState } from 'react';
import { post, put, useData } from '../api.js';
import { Button, Card, Field, Input, Note, Select, ToggleRow, useAction, useToast } from '../ui.jsx';

// Alerts in plain words, by email or by ntfy (a free push-notification service you can also host
// yourself). Each kind can be switched off, quiet hours hold messages until morning, and a test
// button proves the channel works.

export default function Alerts() {
  const { data, reload } = useData('/api/alerts');
  const { data: ch } = useData('/api/vpn/choices');
  const [f, setF] = useState(null);
  const [run, busy] = useAction();
  const toast = useToast();
  useEffect(() => {
    if (data && !f) setF({ email: { ...data.email, new_pass: '' }, ntfy: { ...data.ntfy, new_token: '' }, on: data.on, quiet: data.quiet, watch: data.watch });
  }, [data, f]);
  if (!f) return <Card title="Alerts"><span className="muted">Loading…</span></Card>;
  const set = (k, v) => setF((x) => ({ ...x, [k]: v }));
  const em = (k) => (v) => set('email', { ...f.email, [k]: v });
  const nt = (k) => (v) => set('ntfy', { ...f.ntfy, [k]: v });
  const save = () => run(async () => { const d = await put('/api/alerts', f); setF({ email: { ...d.email, new_pass: '' }, ntfy: { ...d.ntfy, new_token: '' }, on: d.on, quiet: d.quiet, watch: d.watch }); reload(); return d; }, 'Alert settings saved.');
  const test = async (channel) => {
    if (!(await save())) return;
    await run(() => post('/api/alerts/test', { channel }), channel === 'email' ? 'Test email sent. Check your inbox.' : 'Test message sent. Check your phone.');
  };
  const devices = ch?.devices || [];
  const watched = new Set(f.watch.map((m) => m.toLowerCase()));
  const anyChannel = f.email.enabled || f.ntfy.enabled;
  return (
    <Card title="Alerts" desc="Get a message when something needs your attention. Choose where to send it, then which things to tell you about.">
      <div className="stack">
        <ToggleRow title="Push message with ntfy" desc="Free, no account needed. Install the ntfy app on your phone and subscribe to your topic name. Pick a long random topic, because anyone who knows it can read your alerts." checked={f.ntfy.enabled} onChange={nt('enabled')} />
        {f.ntfy.enabled && (
          <div className="formgrid">
            <Field label="Topic"><Input value={f.ntfy.topic} onChange={nt('topic')} placeholder="gatehouse-home-7f3k29x" className="in mono" /></Field>
            <Field label="Server" hint="Leave as it is unless you run your own."><Input value={f.ntfy.server} onChange={nt('server')} className="in mono" /></Field>
            <Field label="Access token (optional)" hint={data.ntfy.has_token ? 'A token is saved. Type a new one to replace it.' : 'Only if your server needs a login.'}><Input type="password" value={f.ntfy.new_token} onChange={nt('new_token')} autoComplete="off" /></Field>
            <div style={{ alignSelf: 'end' }}><Button onClick={() => test('ntfy')} disabled={busy}>Send a test</Button></div>
          </div>
        )}
        <ToggleRow title="Email" desc="Uses your own mail account. Gmail and similar need an app password, not your normal one." checked={f.email.enabled} onChange={em('enabled')} />
        {f.email.enabled && (
          <div className="formgrid">
            <Field label="Mail server"><Input value={f.email.host} onChange={em('host')} placeholder="smtp.example.com" className="in mono" /></Field>
            <Field label="Port"><Input value={String(f.email.port || '')} onChange={(v) => em('port')(Number(v.replace(/\D/g, '')))} className="in mono" /></Field>
            <Field label="Connection"><Select value={f.email.security} onChange={em('security')} options={[{ value: 'starttls', label: 'Secure (STARTTLS, usually port 587)' }, { value: 'tls', label: 'Secure (TLS, usually port 465)' }, { value: 'none', label: 'Not secure (no login allowed)' }]} /></Field>
            <Field label="Login name"><Input value={f.email.user} onChange={em('user')} autoComplete="off" /></Field>
            <Field label="Password" hint={data.email.has_pass ? 'A password is saved. Type a new one to replace it.' : ''}><Input type="password" value={f.email.new_pass} onChange={em('new_pass')} autoComplete="new-password" /></Field>
            <Field label="Send from"><Input value={f.email.from} onChange={em('from')} placeholder="gatehouse@example.com" /></Field>
            <Field label="Send to"><Input value={f.email.to} onChange={em('to')} placeholder="you@example.com" /></Field>
            <div style={{ alignSelf: 'end' }}><Button onClick={() => test('email')} disabled={busy}>Send a test</Button></div>
          </div>
        )}
        {!anyChannel && <Note>Switch on at least one way to be told, or no alerts are sent.</Note>}
        <strong>Tell me when</strong>
        {data.kinds.map((k) => (
          <ToggleRow key={k.id} title={k.label} desc={k.desc} checked={!!f.on[k.id]} onChange={(v) => set('on', { ...f.on, [k.id]: v })} />
        ))}
        {f.on.device_offline && (
          <Field label="Devices to watch" hint="Tick the ones you want to hear about, such as a camera or a server.">
            <div className="stack" style={{ maxHeight: '12rem', overflow: 'auto' }}>
              {devices.map((d) => (
                <label key={d.mac} className="small" style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
                  <input type="checkbox" checked={watched.has(d.mac.toLowerCase())} onChange={(e) => set('watch', e.target.checked ? [...f.watch, d.mac] : f.watch.filter((m) => m.toLowerCase() !== d.mac.toLowerCase()))} /> {d.name} <span className="muted mono">{d.ip}</span>
                </label>
              ))}
              {!devices.length && <span className="small muted">No devices seen yet.</span>}
            </div>
          </Field>
        )}
        <ToggleRow title="Quiet hours" desc={f.quiet.enabled ? `Nothing is sent between ${f.quiet.from} and ${f.quiet.to}. What happened is sent as one message afterwards.${data.held ? ` ${data.held} waiting now.` : ''}` : 'Off. Alerts are sent at once, day or night.'} checked={f.quiet.enabled} onChange={(v) => set('quiet', { ...f.quiet, enabled: v })} />
        {f.quiet.enabled && (
          <div className="formgrid">
            <Field label="From"><Input type="time" value={f.quiet.from} onChange={(v) => set('quiet', { ...f.quiet, from: v })} /></Field>
            <Field label="Until"><Input type="time" value={f.quiet.to} onChange={(v) => set('quiet', { ...f.quiet, to: v })} /></Field>
          </div>
        )}
        <div className="actions"><Button kind="primary" onClick={save} disabled={busy}>Save</Button></div>
      </div>
    </Card>
  );
}

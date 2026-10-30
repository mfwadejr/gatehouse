import React, { useEffect, useState } from 'react';
import { put, useData } from '../api.js';
import { Button, Card, Field, Input, Note, Pill, Select, useAction } from '../ui.jsx';

// "Reach my home from anywhere": three steps in order, each showing whether it is done. The web name
// keeps pointing at this router when your provider changes its address; the VPN and the phone are
// the existing VPN wizard.
export default function RemoteAccess({ onAddVPN, hasDevice }) {
  const { data, reload } = useData('/api/remote', { every: 20000 });
  const [f, setF] = useState(null);
  const [run, busy] = useAction();
  useEffect(() => { if (data && !f) setF({ ...data.config, secret: '' }); }, [data, f]);
  if (!f) return null;
  const set = (k) => (v) => setF((x) => ({ ...x, [k]: v }));
  const named = !!data.config.provider && !!data.config.host;
  const save = async () => {
    const r = await run(() => put('/api/remote', f));
    if (r) { reload(); setF({ ...f, secret: '' }); }
  };
  const step = (n, done, title, children) => (
    <div className="item" style={{ display: 'block' }}>
      <div className="name">{n}. {title} {done ? <Pill tone="good">Done</Pill> : <Pill>To do</Pill>}</div>
      <div className="stack" style={{ marginTop: 'var(--space-2)' }}>{children}</div>
    </div>
  );
  return (
    <Card title="Reach my home from anywhere" desc="Three steps: a web name for your home, a VPN on this router, and your phone.">
      {data.blocked.length > 0 && (
        <Note tone="warn" title="This may not work on your connection">{data.blocked.join(', ')} is shared with other customers by your provider (or sits behind another router), so nothing can reach it from outside. See the Internet page for the options.</Note>
      )}
      <div className="list">
        {step(1, named, 'A web name that always finds your home', (
          <>
            <span className="small muted">Your provider changes your home's address now and then. A free web name from DuckDNS, No-IP or similar follows it. Gatehouse updates it every 10 minutes.</span>
            <div className="formgrid">
              <Field label="Service"><Select value={f.provider} onChange={set('provider')} options={[{ value: '', label: 'Not set up' }, { value: 'duckdns', label: 'DuckDNS' }, { value: 'dyndns2', label: 'No-IP, Dynu or similar' }]} /></Field>
              {f.provider && <Field label="Web name"><Input value={f.host} onChange={set('host')} placeholder={f.provider === 'duckdns' ? 'myhome' : 'myhome.ddns.net'} /></Field>}
              {f.provider === 'dyndns2' && <><Field label="Update server"><Input value={f.server} onChange={set('server')} placeholder="dynupdate.no-ip.com" /></Field><Field label="User name"><Input value={f.user} onChange={set('user')} /></Field></>}
              {f.provider && <Field label={f.provider === 'duckdns' ? 'Token' : 'Password'} hint={data.has_secret ? 'Saved. Type a new one to replace it.' : 'Kept sealed; never shown again.'}><Input type="password" value={f.secret} onChange={set('secret')} autoComplete="new-password" /></Field>}
            </div>
            {f.provider && <div><Button kind="primary" disabled={busy} onClick={save}>Save and update now</Button></div>}
            {named && <span className="small muted">{data.status.error ? `Problem: ${data.status.error}` : data.status.last_ok ? `Last updated ${new Date(data.status.last_ok * 1000).toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' })}.` : 'Not updated yet.'}</span>}
          </>
        ))}
        {step(2, data.vpn_server, 'A VPN on this router', (
          data.vpn_server ? <span className="small muted">Ready. Phones connect to {data.endpoint || 'your web name'}.</span>
            : <div><Button onClick={onAddVPN}>Add a VPN</Button> <span className="small muted">Choose "Reach home from anywhere".</span></div>
        ))}
        {step(3, !!hasDevice, 'Add your phone', <span className="small muted">On your VPN below, choose Add a device, then scan the code with the WireGuard app.</span>)}
      </div>
    </Card>
  );
}

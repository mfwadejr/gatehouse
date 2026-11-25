import React, { useEffect, useState } from 'react';
import { del, post, put, useData } from '../api.js';
import { Button, Card, Field, Note, ToggleRow, useAction } from '../ui.jsx';

// Encrypted DNS for your devices: phones and laptops can ask this router over DNS-over-TLS or
// DNS-over-HTTPS. Off by default, offered only on the networks chosen, never on an internet port.
export default function EncryptedDnsServer() {
  const { data, reload } = useData('/api/dns/encrypted-server', { every: 10000 });
  const [f, setF] = useState(null);
  const [show, setShow] = useState(false);
  const [pem, setPem] = useState({ cert: '', key: '' });
  const [run, busy] = useAction();
  useEffect(() => { if (data && !f) setF({ on: !!data.config.on, ifaces: data.config.ifaces || [] }); }, [data, f]);
  if (!f) return null;
  const flip = (n) => setF((x) => ({ ...x, ifaces: x.ifaces.includes(n) ? x.ifaces.filter((d) => d !== n) : [...x.ifaces, n] }));
  const save = () => run(async () => { await put('/api/dns/encrypted-server', f); setF(null); reload(); }, 'Saved.');
  const nets = data.networks || [];
  const cert = data.cert || {};
  const chosen = nets.filter((n) => f.ifaces.includes(n.iface) && n.allowed);
  const live = data.config.on;
  return (
    <Card title="Encrypted DNS for your devices" desc="Let your phones and laptops ask this firewall over an encrypted connection, so nobody on the way can read their lookups, and your lists and logs still apply. Off by default, and never offered to the internet.">
      <div className="stack">
        <ToggleRow title="Offer encrypted DNS" checked={f.on} onChange={(v) => setF((x) => ({ ...x, on: v }))} />
        {f.on && (
          <Field label="Which networks may use it" hint="Pick the networks your devices are on. A port that faces the internet can never be chosen.">
            <div className="stack">
              {nets.map((n) => (
                <label key={n.iface} style={{ display: 'flex', gap: '.6rem', alignItems: 'center', opacity: n.allowed ? 1 : 0.55 }}>
                  <input type="checkbox" disabled={!n.allowed} checked={f.ifaces.includes(n.iface)} onChange={() => flip(n.iface)} />
                  <span>{n.name} <span className="muted small">({n.ip}){n.allowed ? '' : ` — ${n.why}`}</span></span>
                </label>
              ))}
              {!nets.length && <span className="small muted">You have no networks to choose from yet.</span>}
            </div>
          </Field>
        )}
        <div className="row"><Button kind="primary" disabled={busy} onClick={save}>Save</Button></div>
        {live && (
          <div className="stack">
            <span className="small">{data.error ? `Problem: ${data.error}` : ''}{data.dot_error ? ` Port ${data.dot_port} could not be opened: ${data.dot_error}.` : ''}{data.doh_error ? ` Port ${data.doh_port} could not be opened: ${data.doh_error}.` : ''}</span>
            <div className="small muted">Enter these on a device (use the address of the network it is on):</div>
            {chosen.length === 0 && <div className="small muted">Save a network above to see its addresses.</div>}
            {chosen.map((n) => (
              <div key={n.iface} className="small">
                <strong>{n.name}</strong>: private DNS name or address <code>{n.ip}</code> (port {data.dot_port}, DNS-over-TLS) or <code>https://{n.ip}:{data.doh_port}{data.doh_path}</code> (DNS-over-HTTPS)
              </div>
            ))}
          </div>
        )}
        <Field label="Certificate" hint={cert.kind === 'uploaded'
          ? 'You uploaded this certificate, so devices that trust its issuer will accept it.'
          : 'NopeWarden made its own certificate. Devices will only accept it if you install it on them, so most phones will refuse it. To use Android’s Private DNS or avoid warnings, upload a certificate your devices already trust.'}>
          <div className="small">
            {cert.kind === 'uploaded' ? 'Uploaded' : 'Made by NopeWarden'}{cert.expires ? `, valid until ${cert.expires}` : ''}{cert.names?.length ? `, for ${cert.names.slice(0, 4).join(', ')}` : ''}
          </div>
          <div className="row" style={{ marginTop: '.4rem' }}>
            <Button small onClick={() => setShow((s) => !s)}>{show ? 'Cancel' : 'Upload a certificate'}</Button>
            {cert.kind === 'uploaded' && <Button small kind="danger" disabled={busy} onClick={() => run(async () => { await del('/api/dns/encrypted-server/cert'); reload(); }, 'Back to the certificate NopeWarden made.')}>Use NopeWarden’s own</Button>}
          </div>
        </Field>
        {show && (
          <div className="stack">
            <Field label="Certificate (PEM)"><textarea className="in" rows={5} value={pem.cert} onChange={(e) => setPem({ ...pem, cert: e.target.value })} placeholder="-----BEGIN CERTIFICATE-----" /></Field>
            <Field label="Private key (PEM)"><textarea className="in" rows={5} value={pem.key} onChange={(e) => setPem({ ...pem, key: e.target.value })} placeholder="-----BEGIN PRIVATE KEY-----" /></Field>
            <div className="row"><Button kind="primary" disabled={busy || !pem.cert || !pem.key} onClick={() => run(async () => { await post('/api/dns/encrypted-server/cert', pem); setPem({ cert: '', key: '' }); setShow(false); reload(); }, 'Certificate saved.')}>Save certificate</Button></div>
          </div>
        )}
        <Note>This is different from Encrypted DNS above, which is how this firewall asks the internet. Here, your devices ask the firewall. Changes apply at once and are undone if the firewall cannot take them.</Note>
      </div>
    </Card>
  );
}

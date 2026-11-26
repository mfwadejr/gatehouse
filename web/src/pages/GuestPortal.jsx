import React, { useEffect, useRef, useState } from 'react';
import { post, put, useData } from '../api.js';
import { PORTAL_BLUE, PORTAL_COLOURS } from '../portalcolor.js';
import { Button, Card, Field, Input, Note, Select, ToggleRow, useAction } from '../ui.jsx';

// A sign-in page for guests: on the networks you pick, a device sees your page first and gets
// internet only after it taps Connect (and ticks your terms, if you wrote some). Off by default.
// It lives on Networks because it belongs to the guest network. The phone beside the settings shows
// the real page as the server would send it, for the settings on screen, before anything is saved.
const TIMES = [[1, '1 hour'], [2, '2 hours'], [4, '4 hours'], [8, '8 hours'], [12, '12 hours'], [24, '24 hours'], [168, '7 days']];

function Phone({ f }) {
  const [key, setKey] = useState('');
  const [which, setWhich] = useState('page');
  const timer = useRef(null);
  useEffect(() => {
    clearTimeout(timer.current);
    timer.current = setTimeout(() => {
      post('/api/portal/preview', { ...f, on: true, hours: Number(f.hours) || 4 }).then((r) => setKey(r.key)).catch(() => {});
    }, 250);
    return () => clearTimeout(timer.current);
  }, [f.title, f.terms, f.hours, f.welcome, f.accent, f.button, f.logo]); // eslint-disable-line react-hooks/exhaustive-deps
  return (
    <div className="gp-side">
      <div className="gp-phone" role="img" aria-label="Preview of the guest sign-in page on a phone">
        {key && <iframe title="Guest page preview" sandbox="allow-scripts" src={`/api/portal/preview/${key}/${which}`} />}
      </div>
      <div className="row" style={{ justifyContent: 'center', marginTop: '.6rem' }}>
        <Button small kind={which === 'page' ? 'primary' : undefined} onClick={() => setWhich('page')}>Sign-in page</Button>
        <Button small kind={which === 'done' ? 'primary' : undefined} onClick={() => setWhich('done')}>After Connect</Button>
      </div>
      <div className="small muted" style={{ textAlign: 'center' }}>Not saved until you press Save.</div>
    </div>
  );
}

export default function GuestPortal() {
  const { data, reload } = useData('/api/portal', { every: 10000 });
  const [f, setF] = useState(null);
  const [run, busy] = useAction();
  useEffect(() => { if (data && !f) setF({ ...data.config, ifaces: data.config.ifaces || [], welcome: data.config.welcome || '', accent: data.config.accent || '', button: data.config.button || '', logo: data.config.logo || '' }); }, [data, f]);
  if (!f) return null;
  const set = (k) => (v) => setF((x) => ({ ...x, [k]: v }));
  const flip = (n) => setF((x) => ({ ...x, ifaces: x.ifaces.includes(n) ? x.ifaces.filter((d) => d !== n) : [...x.ifaces, n] }));
  const save = () => run(async () => { const { logoError, ...body } = f;
    await put('/api/portal', { ...body, hours: Number(f.hours) }); setF(null); reload(); }, 'Saved.');
  const nets = data.networks || [];
  const times = TIMES.some(([h]) => h === Number(f.hours)) ? TIMES : [...TIMES, [Number(f.hours), `${f.hours} hours`]].sort((a, b) => a[0] - b[0]);
  const pickLogo = (e) => {
    const file = e.target.files?.[0];
    if (!file) return;
    if (file.size > 100 * 1024) { setF((x) => ({ ...x, logoError: 'That picture is over 100 KB. Choose a smaller one.' })); return; }
    const r = new FileReader();
    r.onload = () => setF((x) => ({ ...x, logo: String(r.result), logoError: '' }));
    r.readAsDataURL(file);
  };
  return (
    <Card title="Guest sign-in page" desc="Guests on the networks you choose see a page first and reach the internet only after they tap Connect. Off by default.">
      <div className="gp-wrap">
        <div className="stack gp-form">
          <ToggleRow title="Use a sign-in page" checked={f.on} onChange={set('on')} />
          {f.on && (
            <>
              <Field label="Which networks" hint="Pick your guest network. A network that manages NopeWarden, or that faces the internet, can never be chosen, so you cannot lock yourself out.">
                <div className="stack">
                  {nets.map((n) => (
                    <label key={n.iface} style={{ display: 'flex', gap: '.6rem', alignItems: 'center', opacity: n.allowed ? 1 : 0.55 }}>
                      <input type="checkbox" disabled={!n.allowed} checked={f.ifaces.includes(n.iface)} onChange={() => flip(n.iface)} />
                      <span>{n.name} <span className="muted small">({n.cidr}){n.allowed ? '' : ` — ${n.why}`}</span></span>
                    </label>
                  ))}
                  {!nets.length && <span className="small muted">You have no other networks yet. Add one with Add a network above.</span>}
                </div>
              </Field>
              <Field label="Title"><Input value={f.title} onChange={set('title')} placeholder="Welcome" /></Field>
              <Field label="Welcome line (optional)" hint="One short line under the title, for example the name of the place."><Input value={f.welcome} onChange={set('welcome')} maxLength={200} /></Field>
              <Field label="Terms (optional)" hint="If you write something here, guests must tick “I agree to these terms” before Connect works. Leave it empty for a plain Connect button. Leave a blank line between paragraphs.">
                <textarea className="in" rows={5} value={f.terms || ''} onChange={(e) => set('terms')(e.target.value)} />
              </Field>
              <Field label="Button wording" hint="Connect if you leave it empty."><Input value={f.button} onChange={set('button')} maxLength={30} placeholder="Connect" /></Field>
              <Field label="Button colour">
                <div className="row">
                  {PORTAL_COLOURS.map(([n, c]) => (
                    <button key={c} type="button" title={n} aria-label={n} aria-pressed={(f.accent || PORTAL_BLUE).toLowerCase() === c} onClick={() => set('accent')(c === PORTAL_BLUE ? '' : c)} style={{ background: c, width: '1.8rem', height: '1.8rem', borderRadius: '50%', border: (f.accent || PORTAL_BLUE).toLowerCase() === c ? '3px solid var(--color-text-primary)' : '1px solid var(--color-border-strong)', cursor: 'pointer' }} />
                  ))}
                </div>
              </Field>
              <Field label="Logo (optional)" hint="A small PNG, JPEG, GIF or WebP picture, up to 100 KB. It is stored on this device and shown at the top of the page.">
                <div className="row">
                  <input type="file" accept="image/png,image/jpeg,image/gif,image/webp" onChange={pickLogo} />
                  {f.logo && <Button small onClick={() => set('logo')('')}>Remove the logo</Button>}
                </div>
                {f.logoError && <span className="small" style={{ color: 'var(--color-status-warning)' }}>{f.logoError}</span>}
              </Field>
              <Field label="Stay signed in for" hint="After this time a guest taps Connect again. It is remembered by the device, so a new address from the network does not ask again.">
                <Select value={String(f.hours)} onChange={(v) => set('hours')(Number(v))} options={times.map(([h, l]) => ({ value: String(h), label: l }))} />
              </Field>
              <span className="small muted">{data.error ? `Problem: ${data.error}` : `${data.signed_in} ${data.signed_in === 1 ? 'device is' : 'devices are'} signed in right now.`}</span>
            </>
          )}
          <div className="row">
            <Button kind="primary" disabled={busy} onClick={save}>Save</Button>
            {data.signed_in > 0 && <Button disabled={busy} onClick={() => run(async () => { await post('/api/portal/clear', {}); reload(); }, 'Everyone was signed out.')}>Sign everyone out</Button>}
          </div>
          {f.on && <Note>Guests on these networks get internet over IPv4 only. A device that only opens secure (https) pages may not show the page by itself; most phones and laptops notice the sign-in page on their own and open it. The page is plain http, so do not use it to collect secrets. Phones that use a private Wi-Fi address may be asked to sign in again on each network.</Note>}
          {f.on && <Note>Guests cannot reach each other through NopeWarden. Two guests on the same Wi-Fi or switch talk directly, which a firewall never sees, so turn on <b>client isolation</b> (sometimes called guest mode or AP isolation) on your access point. A guest network built from several of this device's own ports keeps them apart by itself.</Note>}
        </div>
        {f.on && <Phone f={f} />}
      </div>
    </Card>
  );
}

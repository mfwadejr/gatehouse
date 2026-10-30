import React, { useEffect, useState } from 'react';
import { post, put, useData } from '../api.js';
import { Button, Card, Field, Input, NumberInput, Note, ToggleRow, useAction } from '../ui.jsx';

// A sign-in page for guests: on the networks you pick, a device sees your page first and gets
// internet only after it taps Connect. Off by default.
export default function GuestPortal() {
  const { data, reload } = useData('/api/portal', { every: 10000 });
  const [f, setF] = useState(null);
  const [run, busy] = useAction();
  useEffect(() => { if (data && !f) setF({ ...data.config, ifaces: data.config.ifaces || [] }); }, [data, f]);
  if (!f) return null;
  const set = (k) => (v) => setF((x) => ({ ...x, [k]: v }));
  const flip = (n) => setF((x) => ({ ...x, ifaces: x.ifaces.includes(n) ? x.ifaces.filter((d) => d !== n) : [...x.ifaces, n] }));
  const save = () => run(async () => { await put('/api/portal', { ...f, hours: Number(f.hours) }); setF(null); reload(); }, 'Saved.');
  const nets = data.networks || [];
  return (
    <Card title="Guest sign-in page" desc="Guests on the networks you choose see a page first and reach the internet only after they tap Connect. Off by default.">
      <div className="stack">
        <ToggleRow title="Use a sign-in page" checked={f.on} onChange={set('on')} />
        {f.on && (
          <>
            <Field label="Which networks" hint="Pick your guest network. A network that manages Gatehouse, or that faces the internet, can never be chosen, so you cannot lock yourself out.">
              <div className="stack">
                {nets.map((n) => (
                  <label key={n.iface} style={{ display: 'flex', gap: '.6rem', alignItems: 'center', opacity: n.allowed ? 1 : 0.55 }}>
                    <input type="checkbox" disabled={!n.allowed} checked={f.ifaces.includes(n.iface)} onChange={() => flip(n.iface)} />
                    <span>{n.name} <span className="muted small">({n.cidr}){n.allowed ? '' : ` — ${n.why}`}</span></span>
                  </label>
                ))}
                {!nets.length && <span className="small muted">You have no other networks yet. Add one on the Networks page first.</span>}
              </div>
            </Field>
            <Field label="Title"><Input value={f.title} onChange={set('title')} placeholder="Welcome" /></Field>
            <Field label="Terms (optional)" hint="If you write something here, guests must agree to it. Leave empty for a plain Connect button. Blank line between paragraphs.">
              <textarea className="in" rows={5} value={f.terms || ''} onChange={(e) => set('terms')(e.target.value)} />
            </Field>
            <Field label="Stay signed in for (hours)"><NumberInput value={f.hours} min={1} max={720} onChange={set('hours')} /></Field>
            <span className="small muted">{data.error ? `Problem: ${data.error}` : `${data.signed_in} ${data.signed_in === 1 ? 'device is' : 'devices are'} signed in right now.`}</span>
          </>
        )}
        <div className="row">
          <Button kind="primary" disabled={busy} onClick={save}>Save</Button>
          {data.signed_in > 0 && <Button disabled={busy} onClick={() => run(async () => { await post('/api/portal/clear', {}); reload(); }, 'Everyone was signed out.')}>Sign everyone out</Button>}
        </div>
        <Note>Guests on these networks get internet over IPv4 only. A device that only opens secure (https) pages may not show the page by itself; most phones and laptops notice the sign-in page on their own and open it.</Note>
      </div>
    </Card>
  );
}

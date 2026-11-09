import React, { useEffect, useState } from 'react';
import { get, post, put, useData } from '../api.js';
import { Button, Card, Field, Note, Pill, Segmented, ToggleRow, useAction, fmt } from '../ui.jsx';

// How names are looked up: by asking encrypted forwarders (the default), or by resolving directly with
// Unbound, which starts at the internet's root servers and checks DNSSEC signatures itself.
export default function ResolverCard() {
  const { data, reload } = useData('/api/dns/resolver', { every: 10000 });
  const [run, busy] = useAction();
  const [tests, setTests] = useState(null);
  const [step, setStep] = useState('');
  // While a change is under way the program can take up to a minute. Ask it once a second what it is
  // doing, so the card says so instead of sitting silent.
  useEffect(() => {
    if (!busy) { setStep(''); return undefined; }
    let live = true;
    const ask = () => get('/api/dns/resolver').then((r) => { if (live && r && r.step) setStep(r.step); }).catch(() => {});
    const t = setInterval(ask, 1000);
    return () => { live = false; clearInterval(t); };
  }, [busy]);
  if (!data) return null;
  const direct = data.mode === 'direct';
  const choose = (mode) => mode !== data.mode && run(() => put('/api/dns/resolver', { mode, dnssec: data.dnssec }), mode === 'direct' ? 'Now resolving directly.' : 'Now using your encrypted forwarders.').then(() => { setTests(null); reload(); });
  const sec = (v) => run(() => put('/api/dns/resolver', { mode: data.mode, dnssec: v }), v ? 'Signatures are checked.' : 'Signatures are not checked.').then(reload);
  const test = async () => { const r = await run(() => post('/api/dns/resolver/test')); if (r) setTests(r.tests); };
  return (
    <Card title="How names are looked up" desc="Your devices ask NopeWarden. When it does not already know a name, it either asks a forwarder you trust, or finds the answer itself.">
      <Field label="Look names up by" hint={direct
        ? 'NopeWarden asks the internet’s root servers itself (using Unbound on this machine), so no single company sees every name your devices look up. Unbound checks DNSSEC signatures so forged answers are refused.'
        : 'NopeWarden asks the encrypted forwarders listed below, one company at a time. Fast and simple.'}>
        <Segmented value={data.mode} onChange={choose} options={[{ value: 'forward', label: 'Encrypted forwarders', disabled: busy }, { value: 'direct', label: 'Resolve directly', disabled: busy }]} />
      </Field>
      {busy && (
        <div className="inline small" role="status" aria-live="polite">
          <span className="spinner" aria-hidden="true" />
          <span><b>Working on it.</b> {step || 'Starting…'} <span className="muted">This can take up to a minute; nothing else needs doing.</span></span>
        </div>
      )}
      {!data.installed && direct === false && <p className="small muted">{data.why}</p>}
      {direct && (
        <div className="stack">
          <div className="inline">
            <Pill tone={data.active === 'direct' ? 'good' : 'warn'}>{data.active === 'direct' ? 'Using Unbound' : 'Using your forwarders for now'}</Pill>
            {data.version && <span className="small muted">Unbound {data.version}</span>}
            {data.last_probe > 0 && <span className="small muted">checked {fmt.ago(data.last_probe)}</span>}
          </div>
          {data.fallback && (
            <Note tone="warn" title="Names are being looked up through your forwarders">
              {data.reason} NopeWarden switched on its own so nothing stopped working{data.since ? ` (${fmt.ago(data.since)})` : ''}. It switches back by itself once Unbound has answered well a few times.
            </Note>
          )}
          <ToggleRow title="Check DNSSEC signatures" desc={data.dnssec ? 'On. If a site’s signatures do not match, the lookup is refused and the log says why in plain words.' : 'Off. Answers are accepted without checking their signatures.'} checked={data.dnssec} onChange={sec} />
          <div className="actions"><Button disabled={busy} onClick={test}>Test it</Button></div>
          {tests && (
            <ul className="stack small" style={{ margin: 0, paddingLeft: 0, listStyle: 'none' }}>
              {tests.map((t) => <li key={t.name}><Pill tone={t.ok ? 'good' : 'bad'}>{t.ok ? 'Pass' : 'Fail'}</Pill> <b>{t.name}</b> <span className="muted">{t.detail}</span></li>)}
            </ul>
          )}
          <p className="small muted" style={{ margin: 0 }}>Your own names (Own names, DHCP hostnames) are always answered here first, so they never leave the machine. The first lookup of a new name can take a moment longer, because Unbound asks several servers in turn; repeats are answered from memory.</p>
        </div>
      )}
    </Card>
  );
}

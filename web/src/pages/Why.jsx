import React, { useState } from 'react';
import { post, useData } from '../api.js';
import { Button, Card, Field, Input, Modal, Note, Pill, Select, useAction } from '../ui.jsx';

// "Why can't it reach that?" Pick a device and a website or address. Gatehouse goes through
// everything that can stop it and says which thing did.

const tone = { blocks: 'bad', ok: 'good', unknown: 'warn' };
const word = { blocks: 'Blocks it', ok: 'Fine', unknown: 'Could not check' };

export function WhyBody({ device: fixed, target: t0 = '', onFixed }) {
  const { data } = useData('/api/devices');
  const [device, setDevice] = useState(fixed ? String(fixed) : '');
  const [target, setTarget] = useState(t0);
  const [port, setPort] = useState('');
  const [res, setRes] = useState(null);
  const [run, busy] = useAction();
  const list = (data?.devices || data || []).filter?.((d) => d.id) || [];
  const ask = () => run(async () => setRes(await post('/api/diag/why', { device: Number(device), target, port })));
  const fix = (f) => run(async () => {
    if (f.kind === 'allow-domain') await post('/api/dns/rules', { domain: f.domain, action: 'allow', note: 'Allowed from the connection tester' });
    else if (f.kind === 'resume-device') await post(`/api/devices/${f.id}/pause`, { minutes: 0 });
    else if (f.kind === 'goto') { window.location.hash = `#/${f.page}`; return; }
    onFixed?.();
    setRes(await post('/api/diag/why', { device: Number(device), target, port }));
  }, 'Done. Checked again below.');
  return (
    <div className="stack">
      {!fixed && (
        <Field label="Which device?">
          <Select value={device} onChange={setDevice} options={[{ value: '', label: 'Choose a device…' }, ...list.map((d) => ({ value: String(d.id), label: d.name || d.hostname || d.mac }))]} />
        </Field>
      )}
      <div className="formgrid">
        <Field label="Website or address" hint="For example netflix.com or 93.184.216.34."><Input value={target} onChange={setTarget} placeholder="example.com" /></Field>
        <Field label="Port (optional)" hint="443 for websites."><Input value={port} onChange={setPort} inputMode="numeric" placeholder="443" /></Field>
      </div>
      <div><Button kind="primary" disabled={busy || !device || !target.trim()} onClick={ask}>Find out why</Button></div>
      {res && (
        <div className="stack">
          <Note tone={res.verdict === 'blocked' ? 'bad' : res.verdict === 'open' ? 'good' : 'warn'} title={res.verdict === 'blocked' ? `${res.device} cannot reach ${res.target}` : res.verdict === 'open' ? 'Gatehouse is not blocking it' : 'No clear answer'}>{res.summary}</Note>
          <div className="list">
            {res.findings.map((f, i) => (
              <div className="item" key={i}>
                <div><div className="name">{f.title} <Pill tone={tone[f.state]}>{word[f.state]}</Pill></div><div className="meta">{f.text}</div></div>
                {f.fix && <Button small kind="primary" disabled={busy} onClick={() => fix(f.fix)}>{f.fix.label}</Button>}
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}

export default function Why() {
  return <Card title="Why can't it reach that?" desc="Pick a device and a website. Gatehouse checks pauses, filtering and the firewall, and tells you which one is in the way."><WhyBody /></Card>;
}

export function WhyModal({ device, onClose }) {
  return <Modal title="Why can't it reach that?" onClose={onClose} footer={<Button onClick={onClose}>Close</Button>}><WhyBody device={device} /></Modal>;
}

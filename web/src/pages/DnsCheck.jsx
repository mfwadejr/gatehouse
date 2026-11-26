import React, { useEffect, useState } from 'react';
import { get, post, useData } from '../api.js';
import { Button, Card, Empty, Field, Note, Pill, Select, useAction, fmt } from '../ui.jsx';

// The DNS check: does a device use NopeWarden's DNS, or does it also talk to a DNS server on its own?
// Each destination says how it was recognised, so "confirmed" and "probably" are never mixed up.

const LEVEL = { known: ['good', 'On the known list'], confirmed: ['good', 'Confirmed DNS server'], probably: ['warn', 'Probably a DNS server'], port: ['warn', 'DNS port'], unknown: ['', 'Unknown'] };

export function DnsCheckBody({ device: fixed }) {
  const { data: dev } = useData('/api/devices');
  const list = (dev?.devices || dev || []).filter?.((d) => d.id && d.ip) || [];
  const [ip, setIp] = useState(fixed || '');
  const { data, reload } = useData(`/api/diag/dnscheck?ip=${encodeURIComponent(ip)}`, { enabled: !!ip });
  const [run, busy] = useAction();
  const probe = (addr) => run(() => post('/api/diag/dnscheck/probe', { addr }), 'Checked.').then(reload);
  return (
    <div className="stack">
      {!fixed && (
        <Field label="Which device?">
          <Select value={ip} onChange={setIp} options={[{ value: '', label: 'Choose a device…' }, ...list.map((d) => ({ value: d.ip, label: `${d.name || d.hostname || d.mac} (${d.ip})` }))]} />
        </Field>
      )}
      {ip && !data && <p className="small muted">Checking the last 24 hours…</p>}
      {data && (
        <div className="stack">
          <Note tone={data.tone === 'good' ? 'good' : data.tone === 'warn' ? 'warn' : 'info'} title={data.verdict}>{data.detail}</Note>
          <p className="small muted">{fmt.n(data.lookups)} lookups through NopeWarden in the last {data.hours} hours{data.blocked ? `, ${fmt.n(data.blocked)} blocked` : ''}.</p>
          {!data.servers?.length ? <Empty>No connections to a DNS server other than NopeWarden were seen.</Empty> : (
            <div className="list">
              {data.servers.map((s) => {
                const [tone, label] = LEVEL[s.level] || LEVEL.unknown;
                return (
                  <div className="item" key={`${s.addr}${s.port}${s.proto}`}>
                    <div>
                      <div className="name">{s.service || s.name || s.addr} <Pill tone={tone}>{label}</Pill></div>
                      <div className="meta">{s.addr}, port {s.port}. {s.allowed ? `${fmt.n(s.allowed)} got through` : ''}{s.allowed && s.stopped ? ', ' : ''}{s.stopped ? `${fmt.n(s.stopped)} stopped` : ''}{s.every ? `, ${s.every}` : ''}. Last {fmt.ago(s.last)}.</div>
                      <div className="meta">{s.why}</div>
                      {s.probe && <div className="meta">{s.probe.detail}</div>}
                    </div>
                    {s.level !== 'confirmed' && s.level !== 'known' && <Button small disabled={busy} onClick={() => probe(s.addr)}>Check this address</Button>}
                  </div>
                );
              })}
            </div>
          )}
          <p className="small muted">"Check this address" sends one DNS question to that server over HTTPS and TLS and reads the name on its certificate. NopeWarden never does this by itself, because it connects to someone else's server.</p>
          <details><summary className="small">What this check cannot see</summary><ul className="small muted">{(data.limits || []).map((l) => <li key={l}>{l}</li>)}</ul></details>
          {!data.block_dot && <p className="small muted">Encrypted DNS on port 853 is not being stopped. Forced DNS (DNS, Servers) can stop it.</p>}
        </div>
      )}
    </div>
  );
}

export default function DnsCheck() {
  return <Card title="DNS check" desc="See whether a device uses NopeWarden's DNS or also talks to a DNS server on its own, which skips your block lists."><DnsCheckBody /></Card>;
}

// A short line for a device's panel: only said when the device goes around NopeWarden's DNS.
export function SkippingDns({ ip }) {
  const [data, setData] = useState(null);
  // Only administrators can run the check; for anyone else the line just stays out of the way.
  useEffect(() => { if (ip) get(`/api/diag/dnscheck?ip=${encodeURIComponent(ip)}`).then(setData, () => setData(null)); }, [ip]);
  if (!data || !data.servers?.length || !(data.verdict.startsWith('Also') || data.verdict.startsWith('Tried'))) return null;
  return (
    <Note tone={data.verdict.startsWith('Also') ? 'warn' : 'info'} title="Skipping your DNS">
      {data.detail} <a className="link" href="#/diagnostics/tools">Open the DNS check</a>
    </Note>
  );
}

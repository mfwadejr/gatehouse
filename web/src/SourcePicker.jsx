import React, { useMemo, useState } from 'react';
import { useData } from './api.js';
import { Button, Field, Input } from './ui.jsx';

// Chooses what a rule applies to: whole networks, individual devices, or an address range.
// value is a list of { kind, value, label }; onChange gets the new list.
export default function SourcePicker({ value, onChange }) {
  const { data: ch } = useData('/api/vpn/choices');
  const [q, setQ] = useState('');
  const [range, setRange] = useState('');
  const sel = useMemo(() => new Set(value.map((u) => `${u.kind}|${u.value}`)), [value]);
  const extra = value.filter((u) => u.kind === 'cidr').map((u) => u.value);
  const toggle = (kind, v, label = '') => {
    const k = `${kind}|${v}`;
    onChange(sel.has(k) ? value.filter((u) => `${u.kind}|${u.value}` !== k) : [...value, { kind, value: v, label }]);
  };
  const devs = useMemo(() => (ch?.devices || []).filter((d) => !q || `${d.name} ${d.ip} ${d.mac}`.toLowerCase().includes(q.toLowerCase())), [ch, q]);
  const addRange = () => {
    const r = range.trim();
    if (r && !sel.has(`cidr|${r}`)) onChange([...value, { kind: 'cidr', value: r, label: '' }]);
    setRange('');
  };
  return (
    <div className="stack">
      <Field label="Whole networks">
        <div className="stack" style={{ gap: 'var(--space-1)' }}>
          {(ch?.networks || []).map((n) => (
            <label key={n.name} className="inline"><input type="checkbox" checked={sel.has(`network|${n.name}`)} onChange={() => toggle('network', n.name)} /> <span className="name">{n.name}</span> <span className="small muted mono">{n.subnets.join(', ')}</span></label>
          ))}
          {ch && !ch.networks.length && <span className="muted small">No networks yet.</span>}
        </div>
      </Field>
      <Field label="Individual devices" hint="Gatehouse keeps the device's address fixed so it stays in the group.">
        <Input value={q} onChange={setQ} placeholder="Search devices" aria-label="Search devices" />
        <div className="stack" style={{ gap: 'var(--space-1)', maxHeight: '12rem', overflow: 'auto', marginTop: 'var(--space-2)' }}>
          {devs.map((d) => (
            <label key={d.mac} className="inline"><input type="checkbox" checked={sel.has(`device|${d.mac}`)} onChange={() => toggle('device', d.mac, d.name)} /> <span className={`dot ${d.online ? '' : 'off'}`} /> <span className="name">{d.name}</span> <span className="small muted mono">{d.ip || 'not seen yet'}</span></label>
          ))}
          {ch && !devs.length && <span className="muted small">No matching devices.</span>}
        </div>
      </Field>
      <Field label="Address or range" hint="For anything else, like 192.168.1.50 or 192.168.20.0/24.">
        <div className="inline">
          <Input value={range} onChange={setRange} aria-label="Address or range" placeholder="192.168.1.50" onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); addRange(); } }} />
          <Button onClick={addRange}>Add</Button>
        </div>
        {extra.map((r) => (
          <label key={r} className="inline"><input type="checkbox" checked onChange={() => toggle('cidr', r)} /> <span className="mono">{r}</span></label>
        ))}
      </Field>
    </div>
  );
}

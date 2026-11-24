import React, { useState } from 'react';
import { holdAnd, post, put, useData } from '../api.js';
import { SAVED_LATER, SaveChoice } from '../changes.jsx';
import { Button, Card, Field, Note, Pill, useAction, useToast, usePortNames } from '../ui.jsx';

// Who may manage this device, and a check that you still can. It lives with the management port,
// because it limits the same doors: the console, remote login and ping on that port.

// Addresses and ranges are written as plain text, separated by commas, spaces or new lines.
export const parseRanges = (text) => String(text || '').split(/[\s,]+/).filter(Boolean);

// One editor for a list of address ranges, shared by this card and Bridge mode. Beside the text box it
// offers the ranges this device already knows about, so nobody has to type them: a VPN, the network the
// management port is on, and the address you are connecting from.
export function RangeField({ label, hint, value, onChange, picks = [], placeholder = '192.168.1.0/24, 10.8.0.0/24' }) {
  const have = new Set(parseRanges(value));
  const add = (ranges) => {
    const next = parseRanges(value);
    ranges.forEach((r) => { if (!next.includes(r)) next.push(r); });
    onChange(next.join(', '));
  };
  const free = picks.filter((p) => p.ranges.some((r) => !have.has(r)));
  return (
    <Field label={label} hint={hint}>
      <div className="stack gap1">
        <textarea className="in mono" rows={2} value={value} onChange={(e) => onChange(e.target.value)} spellCheck={false} placeholder={placeholder} aria-label={label} />
        {free.length > 0 && (
          <div className="inline" style={{ flexWrap: 'wrap' }}>
            <span className="small muted">Add:</span>
            {free.map((p) => <Button key={p.label} small onClick={() => add(p.ranges)}>{p.label}</Button>)}
          </div>
        )}
      </div>
    </Field>
  );
}

// The ranges worth offering as one-press additions, from what GET /api/fw/protected knows.
export function knownRanges(data) {
  if (!data) return [];
  const out = [];
  (data.vpns || []).forEach((v) => out.push({ label: `${v.name} (${v.ranges.join(', ')})`, ranges: v.ranges }));
  if ((data.port_net || []).length) out.push({ label: `Management port network (${data.port_net.join(', ')})`, ranges: data.port_net });
  if (data.you?.ip) out.push({ label: `My address (${data.you.ip})`, ranges: [data.you.ip] });
  return out;
}

export default function ManageAccess() {
  const pn = usePortNames();
  const { data, reload } = useData('/api/fw/protected', { topics: ['fw.status'] });
  const [run, busy] = useAction();
  const toast = useToast();
  const [edit, setEdit] = useState(false);
  const [nets, setNets] = useState([]);
  const [vpns, setVpns] = useState([]);
  const [text, setText] = useState('');
  const [res, setRes] = useState(null);
  if (!data) return null;
  const allow = data.allow || {};
  const limited = (allow.resolved || []).length > 0;
  const start = () => { setNets(allow.networks || []); setVpns(allow.vpns || []); setText((allow.cidrs || []).join(', ')); setEdit(true); };
  const flip = (list, set) => (n) => set(list.includes(n) ? list.filter((y) => y !== n) : [...list, n]);
  const key = (x) => [...(x || [])].sort().join(',');
  const save = async (later) => {
    const cidrs = parseRanges(text);
    if (key(nets) === key(allow.networks) && key(vpns) === key(allow.vpns) && key(cidrs) === key(allow.cidrs)) { toast('No changes. These are already saved.'); setEdit(false); return; }
    const body = { networks: nets, vpns, cidrs };
    if (await run(() => (later ? holdAnd(() => put('/api/fw/management-access', body)) : put('/api/fw/management-access', body)), later ? SAVED_LATER : 'Saved.')) { setEdit(false); reload(); }
  };
  const test = async () => { const r = await run(() => post('/api/fw/access-check', {})); if (r) setRes(r); };
  const parts = [...(allow.networks || []), ...(allow.vpns || []), ...(allow.cidrs || [])];
  return (
    <Card title="Who may manage this device" desc="Limit who can open the console, sign in over remote login or ping this device on the management port. Leave it empty and anyone on a management network may."
      actions={<div className="inline"><Button small onClick={test} disabled={busy}>Test my access</Button><Button small onClick={edit ? () => setEdit(false) : start}>{limited ? 'Change' : 'Limit who may manage'}</Button></div>}>
      <div className="inline">
        <Pill tone={limited ? 'good' : ''}>{limited ? 'Limited' : 'Anyone on a management network'}</Pill>
        <span>{limited ? `Only ${parts.join(', ')} may manage.${(allow.networks || []).length + (allow.vpns || []).length ? ` That is ${(allow.resolved || []).join(', ')}.` : ''}` : 'No limit is set. The networks marked as able to manage this device may.'}</span>
      </div>
      {edit && (
        <div className="stack mt3">
          <Note title="You cannot lock yourself out here">If what you pick does not include the address you are using now, NopeWarden refuses to save it. You are connecting from {data.you.ip}{data.you.iface ? ` (${pn(data.you.iface)})` : ''}.</Note>
          {(data.networks || []).length > 0 && (
            <Field label="Your networks">
              <div className="stack gap1">
                {data.networks.map((n) => (
                  <label key={n.name} className="inline"><input type="checkbox" checked={nets.includes(n.name)} onChange={() => flip(nets, setNets)(n.name)} /> <span className="name">{n.name}</span> <span className="small muted mono">{n.subnets.join(', ')}</span></label>
                ))}
              </div>
            </Field>
          )}
          {(data.vpns || []).length > 0 && (
            <Field label="Your VPNs" hint="Chosen by name, so the limit follows the VPN if its range changes.">
              <div className="stack gap1">
                {data.vpns.map((v) => (
                  <label key={v.name} className="inline"><input type="checkbox" checked={vpns.includes(v.name)} onChange={() => flip(vpns, setVpns)(v.name)} /> <span className="name">{v.name}</span> <span className="small muted mono">{v.ranges.join(', ')}</span></label>
                ))}
              </div>
            </Field>
          )}
          <RangeField label="Other addresses or ranges" hint="Separate with commas, spaces or new lines, like 192.168.9.0/24 or 10.8.0.5. A single address is fine."
            value={text} onChange={setText} picks={knownRanges({ port_net: data.port_net, you: data.you })} />
          <div className="actions"><SaveChoice busy={busy} onSave={save} /><Button onClick={() => setEdit(false)}>Cancel</Button></div>
        </div>
      )}
      {res && (
        <div className="stack mt3">
          <Note tone={res.ok ? 'good' : 'bad'} title={res.ok ? 'You can still manage this device' : 'Something needs attention'}>
            <ul>{res.checks.map((c, i) => <li key={i}>{c.ok ? '✓' : '✗'} {c.text}</li>)}</ul>
          </Note>
        </div>
      )}
    </Card>
  );
}

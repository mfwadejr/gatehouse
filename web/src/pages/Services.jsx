import React, { useMemo, useState } from 'react';
import { del, holdAnd, put, useData } from '../api.js';
import { SAVED_LATER, SaveChoice } from '../changes.jsx';
import { Button, Card, Empty, Field, Input, Modal, PageOrPart, Paged, Select, useAction, useUndoDelete } from '../ui.jsx';

// Services: the one place to see what NopeWarden knows by name and to make your own. A service is a
// name for one or more ports. Rules pick services by searching for the name or a port.

const PROTOS = [
  { value: 'tcp', label: 'TCP' },
  { value: 'udp', label: 'UDP' },
  { value: 'both', label: 'TCP and UDP' },
  { value: 'ping', label: 'ICMP (ping)' },
];

// portProblem explains in plain words what is wrong with a port or range typed in the box, or
// returns '' when it is fine. The server checks again when the service is saved.
export function portProblem(text) {
  const t = text.trim();
  if (!t) return 'Type a port number first.';
  if (/[a-z]/i.test(t) && /\d/.test(t)) return 'Just the number here. Choose TCP or UDP in the box on the left.';
  if (!/^\d+(\s*-\s*\d+)?$/.test(t)) return 'Use a number like 443, or a range like 27000-27050.';
  const [lo, hi] = t.split('-').map((x) => parseInt(x, 10));
  if (lo < 1 || lo > 65535 || (hi !== undefined && (hi < 1 || hi > 65535))) return 'Ports go from 1 to 65535.';
  if (hi !== undefined && hi < lo) return 'A range runs from the smaller number to the larger one.';
  return '';
}

// countPorts is how many ports an entry covers, for the Uses column.
function entryUses(e, known) {
  if (e === 'ping') return 'ping';
  const k = known[e];
  if (k) return `${k.length} ${k.length === 1 ? 'entry' : 'entries'}`;
  const m = /^(?:tcp|udp)\/(\d+)(?:-(\d+))?$/.exec(e);
  if (!m) return '';
  const n = m[2] ? parseInt(m[2], 10) - parseInt(m[1], 10) + 1 : 1;
  return `${n} ${n === 1 ? 'port' : 'ports'}`;
}

function ServiceForm({ initial, builtin, onClose, onSaved }) {
  const [name, setName] = useState(initial?.name || '');
  const [entries, setEntries] = useState(() => (initial?.ports || []).map((x) => x.toLowerCase()));
  const [proto, setProto] = useState('tcp');
  const [port, setPort] = useState('');
  const [err, setErr] = useState('');
  const [mode, setMode] = useState(''); // '', 'name' or 'paste'
  const [extra, setExtra] = useState('');
  const [extraErr, setExtraErr] = useState('');
  const [run, busy] = useAction();
  const known = useMemo(() => Object.fromEntries((builtin || []).map((b) => [b.name.toLowerCase(), b.ports])), [builtin]);
  const addEntries = (list) => setEntries((cur) => [...cur, ...list.filter((x, i) => !cur.includes(x) && list.indexOf(x) === i)]);
  const add = () => {
    if (proto === 'ping') { addEntries(['ping']); setErr(''); return; }
    const problem = portProblem(port);
    if (problem) { setErr(problem); return; }
    const p = port.replace(/\s+/g, '');
    addEntries(proto === 'both' ? [`tcp/${p}`, `udp/${p}`] : [`${proto}/${p}`]);
    setPort(''); setErr('');
  };
  const addExtra = () => {
    const words = extra.split(/[\s,]+/).filter(Boolean).map((x) => x.toLowerCase());
    const bad = words.filter((w) => !(w in known) && w !== 'ping' && !/^(tcp|udp)\/\d+(-\d+)?$/.test(w));
    if (!words.length) { setExtraErr('Type or paste at least one entry.'); return; }
    if (bad.length) { setExtraErr(`Not understood: ${bad.join(', ')}. Use a name such as https, or tcp/8080 and udp/27000-27050.`); return; }
    const wrong = words.find((w) => /^(tcp|udp)\//.test(w) && portProblem(w.split('/')[1]));
    if (wrong) { setExtraErr(`${wrong}: ${portProblem(wrong.split('/')[1])}`); return; }
    addEntries(words); setExtra(''); setExtraErr(''); setMode('');
  };
  const label = (e) => (e === 'ping' ? ['ICMP', 'ping'] : known[e] ? ['Name', e] : e.split('/'));
  return (
    <Modal title={initial?.id ? `Edit ${initial.name}` : 'New service'} onClose={onClose}
      footer={<><Button onClick={onClose}>Cancel</Button><SaveChoice busy={busy} disabled={!name.trim() || !entries.length} onSave={async (later) => { const body = { id: initial?.id || 0, name: name.trim(), ports: entries }; if (await run(() => (later ? holdAnd(() => put('/api/fw/portlists', body)) : put('/api/fw/portlists', body)), later ? SAVED_LATER : 'Saved.')) { onSaved(); onClose(); } }} /></>}>
      <Field label="Name" hint="What you will search for when you make a rule. If you rename it, open ports and rules that use it follow the new name."><Input value={name} onChange={setName} placeholder="Plex" /></Field>
      <Field label="Ports">
        <div className="svc-add">
          <Select value={proto} onChange={setProto} options={PROTOS} aria-label="Protocol" />
          <Input value={port} onChange={(v) => { setPort(v); setErr(''); }} disabled={proto === 'ping'} placeholder={proto === 'ping' ? 'Ping has no port' : 'Port or range, like 443 or 27000-27050'} aria-label="Port" onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); add(); } }} />
          <Button kind="primary" onClick={add}>Add</Button>
        </div>
        {err && <div className="field-err" role="alert">{err}</div>}
      </Field>
      <div className="hint">Or <button type="button" className="linkbtn" onClick={() => setMode(mode === 'name' ? '' : 'name')}>add a known name</button> such as https, ssh or dns, or <button type="button" className="linkbtn" onClick={() => setMode(mode === 'paste' ? '' : 'paste')}>paste a list</button>.</div>
      {mode && (
        <div className="field">
          {mode === 'paste'
            ? <textarea className="in mono" rows={4} value={extra} onChange={(e) => { setExtra(e.target.value); setExtraErr(''); }} spellCheck={false} placeholder={'tcp/32400\nudp/1900\nudp/27000-27050'} aria-label="Paste a list" />
            : <Input value={extra} onChange={(v) => { setExtra(v); setExtraErr(''); }} placeholder="https, ssh, dns" aria-label="Known name" onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); addExtra(); } }} />}
          {extraErr && <div className="field-err" role="alert">{extraErr}</div>}
          <div className="svc-extra-go"><Button onClick={addExtra}>{mode === 'paste' ? 'Add these' : 'Add name'}</Button></div>
        </div>
      )}
      {entries.length > 0 && (
        <>
          <div className="svc-list">
            <div className="svc-row head"><div>Protocol</div><div>Port</div><div>Uses</div><div /></div>
            {entries.map((e) => { const [pr, pt] = label(e); return (
              <div className="svc-row" key={e}>
                <div><span className="svc-proto mono">{pr.toUpperCase()}</span></div>
                <div>{pt}</div>
                <div className="muted">{entryUses(e, known)}</div>
                <button type="button" className="svc-x" aria-label={`Remove ${e}`} onClick={() => setEntries((cur) => cur.filter((x) => x !== e))}>✕</button>
              </div>
            ); })}
          </div>
          <div className="hint">{entries.length} {entries.length === 1 ? 'entry' : 'entries'}. Click ✕ to remove one.</div>
        </>
      )}
    </Modal>
  );
}

// "Used by 2 rules and 1 open port", from the list the server sends (open ports start with "Open port: ").
function usedText(list) {
  const ports = list.filter((u) => u.startsWith('Open port: ')).length, rules = list.length - ports;
  const bits = [];
  if (rules) bits.push(`${rules} ${rules === 1 ? 'rule' : 'rules'}`);
  if (ports) bits.push(`${ports} open ${ports === 1 ? 'port' : 'ports'}`);
  return `Used by ${bits.join(' and ')}`;
}

export default function Services({ part, goPorts }) {
  const { data, reload } = useData('/api/fw/services');
  const [q, setQ] = useState('');
  const [edit, setEdit] = useState(null);
  const [run] = useAction();
  const undoDelete = useUndoDelete();
  const needle = q.trim().toLowerCase();
  const hit = (x) => !needle || x.name.toLowerCase().includes(needle) || x.ports.some((p) => p.includes(needle));
  const mine = useMemo(() => (data?.custom || []).filter(hit), [data, needle]);
  const known = useMemo(() => (data?.builtin || []).filter(hit), [data, needle]);
  return (
    <PageOrPart part={part} title="Services" sub="Names for ports. Make your own, then pick them by name when you make a firewall rule."
      actions={<Button kind="primary" icon="plus" onClick={() => setEdit({})}>New service</Button>}>
      <Card title="Search" desc="Find a service by name to see which ports it uses.">
        <Input value={q} onChange={setQ} placeholder="Search by name or port number" aria-label="Search services" />
      </Card>
      <Card title="Your services" desc="Made by you. Used in rules by name.">
        <div className="list">
          {mine.map((s) => (
            <div className="item" key={s.id}>
              <div>
                <div className="name">{s.name}</div>
                <div className="meta mono">{s.ports.join(', ')}</div>
                <div className="meta">{s.used_by.length ? <>{usedText(s.used_by)}: {s.used_by.map((u) => u.replace(/^Open port: /, '')).join(', ')}</> : 'Not used yet'}</div>
              </div>
              <div className="actions">
                <Button small icon="edit" onClick={() => setEdit(s)}>Edit</Button>
                <Button small kind="danger" icon="trash" onClick={() => (s.used_by.length ? run(() => del(`/api/fw/portlists/${s.id}`)).then(reload) : undoDelete(`The service "${s.name}"`, () => del(`/api/fw/portlists/${s.id}`), () => put('/api/fw/portlists', { name: s.name, ports: s.ports }), reload))}>Delete</Button>
              </div>
            </div>
          ))}
          {!mine.length && <Empty>{needle ? 'None of your services match.' : 'No services of your own yet. Choose New service to make one.'}</Empty>}
        </div>
      </Card>
      {(data?.open_ports || []).length > 0 && (
        <Card title="Open ports" desc={part ? 'Apps you have opened to the internet. Change them on the Open ports tab.' : 'Apps you have opened to the internet. Change them on the Firewall page, Open ports tab.'}
          actions={part && goPorts ? <Button small onClick={goPorts}>Open ports</Button> : undefined}>
          <div className="list">
            {data.open_ports.map((g) => <div className="item" key={g.group}><div><div className="name">{g.group}</div><div className="meta">{g.text}</div></div></div>)}
          </div>
        </Card>
      )}
      <Card title="Built in" desc="NopeWarden already knows these by name. They cannot be changed.">
        <div className="list">
          <Paged items={known}>{(shownKnown) => shownKnown.map((s) => (
            <div className="item" key={s.name}>
              <div><div className="name">{s.name}</div><div className="meta mono">{s.ports.join(', ')}</div>{s.note && <div className="small muted">{s.note}</div>}</div>
            </div>
          ))}</Paged>
          {!known.length && <Empty>No built-in service matches.</Empty>}
        </div>
      </Card>
      {edit && <ServiceForm initial={edit} builtin={data?.builtin} onClose={() => setEdit(null)} onSaved={reload} />}
    </PageOrPart>
  );
}

import React, { useMemo, useState } from 'react';
import { del, post, put, useData } from '../api.js';
import { CODES, countryName } from '../AddrPicker.jsx';
import { go } from '../App.jsx';
import { Button, Card, Empty, Field, Input, Modal, Note, Page, Pill, Segmented, Select, ToggleRow, useAction, confirmBox } from '../ui.jsx';

const STATE = {
  working: { tone: 'good', label: 'Working' },
  off: { tone: '', label: 'Paused' },
  nobody: { tone: 'warn', label: 'Nobody yet' },
  'target-off': { tone: 'bad', label: 'Not working' },
  'one-connection': { tone: 'warn', label: 'Needs two connections' },
};
const DEST_TYPES = [{ value: 'domain', label: 'Website' }, { value: 'ip', label: 'Address or range' }, { value: 'region', label: 'Country' }];

function RouteForm({ initial, targets, onClose, onSaved }) {
  const { data: ch } = useData('/api/vpn/choices');
  const [run, busy] = useAction();
  const first = targets.find((t) => t.enabled) || targets[0];
  const [f, setF] = useState(() => ({ name: '', enabled: true, target_kind: first?.kind || 'tunnel', target: first?.target || '', kill: true, uses: [], dests: [], ...initial }));
  const [sel, setSel] = useState(() => new Set((initial?.uses || []).map((u) => `${u.kind}|${u.value}`)));
  const [q, setQ] = useState('');
  const [range, setRange] = useState('');
  const [extra, setExtra] = useState(() => (initial?.uses || []).filter((u) => u.kind === 'cidr').map((u) => u.value));
  const [mode, setMode] = useState(initial?.dests?.length ? 'some' : 'all');
  const [dests, setDests] = useState(initial?.dests?.length ? initial.dests : []);
  const [dType, setDType] = useState('domain');
  const [dVal, setDVal] = useState('');
  const set = (k) => (v) => setF((x) => ({ ...x, [k]: v }));
  const tgt = targets.find((t) => t.kind === f.target_kind && t.target === f.target);
  const toggle = (k) => setSel((s) => { const n = new Set(s); if (n.has(k)) n.delete(k); else n.add(k); return n; });
  const devs = useMemo(() => (ch?.devices || []).filter((d) => !q || `${d.name} ${d.ip} ${d.mac}`.toLowerCase().includes(q.toLowerCase())), [ch, q]);
  const addRange = () => { const r = range.trim(); if (!r) return; setExtra((x) => (x.includes(r) ? x : [...x, r])); setSel((s) => new Set(s).add(`cidr|${r}`)); setRange(''); };
  const addDest = () => {
    const v = dVal.trim();
    if (!v) return;
    setDests((d) => (d.some((x) => x.kind === dType && x.value.toLowerCase() === v.toLowerCase()) ? d : [...d, { kind: dType, value: v }]));
    setDVal('');
  };
  const pick = (v) => { const [kind, target] = v.split('|'); setF((x) => ({ ...x, target_kind: kind, target, kill: kind === 'tunnel' ? x.kill : false })); };
  const save = async () => {
    const uses = [...sel].map((k) => { const [kind, ...r] = k.split('|'); const value = r.join('|'); return { kind, value, label: kind === 'device' ? (ch?.devices || []).find((d) => d.mac === value)?.name || '' : '' }; });
    const body = { ...f, uses, dests: mode === 'all' ? [] : dests };
    if (mode === 'some' && !body.dests.length) { await run(() => Promise.reject(new Error('Add at least one website, address or country, or choose Everything.'))); return; }
    const r = await run(() => put('/api/routes', body), 'Saved.');
    if (r) { onSaved(); onClose(); }
  };
  return (
    <Modal title={f.id ? `Edit ${f.name}` : 'Add a route'} onClose={onClose} footer={<><Button onClick={onClose}>Cancel</Button><Button kind="primary" disabled={busy} onClick={save}>Save</Button></>}>
      <div className="stack">
        <Field label="Name"><Input value={f.name} onChange={set('name')} placeholder="Streaming through the VPN" /></Field>
        <Field label="Send the traffic through">
          <Select value={`${f.target_kind}|${f.target}`} onChange={pick}
            options={targets.map((t) => ({ value: `${t.kind}|${t.target}`, label: `${t.kind === 'tunnel' ? 'VPN' : 'Internet'}: ${t.label}${t.enabled ? '' : ' (off)'}` }))} />
        </Field>
        {f.target_kind === 'tunnel' ? (
          <ToggleRow title="Kill switch" desc="If the VPN drops, this traffic stops instead of leaking out through your normal connection." checked={!!f.kill} onChange={set('kill')} />
        ) : (
          <p className="small muted">If this connection is down, the traffic uses your normal connection.{tgt && !tgt.enabled ? ' This connection is switched off right now.' : ''}</p>
        )}
        <Field label="Who uses this route?" hint="Optional. You can save now and choose later.">
          <div className="stack" style={{ gap: 'var(--space-1)' }}>
            {(ch?.networks || []).map((n) => (
              <label key={n.name} className="inline"><input type="checkbox" checked={sel.has(`network|${n.name}`)} onChange={() => toggle(`network|${n.name}`)} /> <span className="name">{n.name}</span> <span className="small muted mono">{n.subnets.join(', ')}</span></label>
            ))}
            {(ch?.groups || []).map((g) => <label key={g} className="inline"><input type="checkbox" checked={sel.has(`group|${g}`)} onChange={() => toggle(`group|${g}`)} /> <span className="name">{g}</span> <span className="small muted">device group</span></label>)}
          </div>
          <Input value={q} onChange={setQ} placeholder="Search devices" aria-label="Search devices" />
          <div className="stack" style={{ gap: 'var(--space-1)', maxHeight: '12rem', overflow: 'auto', marginTop: 'var(--space-2)' }}>
            {devs.map((d) => (
              <label key={d.mac} className="inline"><input type="checkbox" checked={sel.has(`device|${d.mac}`)} onChange={() => toggle(`device|${d.mac}`)} /> <span className={`dot ${d.online ? '' : 'off'}`} /> <span className="name">{d.name}</span> <span className="small muted mono">{d.ip || 'not seen yet'}</span></label>
            ))}
          </div>
          <div className="addrow" style={{ marginTop: 'var(--space-2)' }}>
            <Input value={range} onChange={setRange} aria-label="Address or range" placeholder="Or an address like 192.168.1.50" onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); addRange(); } }} />
            <Button onClick={addRange}>Add</Button>
          </div>
          {extra.map((r) => <label key={r} className="inline"><input type="checkbox" checked={sel.has(`cidr|${r}`)} onChange={() => toggle(`cidr|${r}`)} /> <span className="mono">{r}</span></label>)}
          {sel.size === 0 && <p className="small muted">Nobody yet. The route is saved but does nothing until you choose.</p>}
        </Field>
        <Field label="Where does it apply?">
          <Segmented value={mode} onChange={setMode} options={[{ value: 'all', label: 'Everything' }, { value: 'some', label: 'Only some destinations' }]} />
        </Field>
        {mode === 'some' && (
          <>
            <div className="addrow">
              <Select value={dType} onChange={(v) => { setDType(v); setDVal(''); }} aria-label="Type" options={DEST_TYPES} />
              {dType === 'region'
                ? <Select value={dVal} onChange={(v) => { if (v) setDests((d) => (d.some((x) => x.kind === 'region' && x.value === v) ? d : [...d, { kind: 'region', value: v }])); }} aria-label="Country"
                  options={[{ value: '', label: 'Choose a country…' }, ...CODES.map((c) => ({ value: c, label: countryName(c) })).sort((a, b) => a.label.localeCompare(b.label))]} />
                : <Input value={dVal} onChange={setDVal} aria-label="Destination" placeholder={dType === 'domain' ? 'example.com' : '203.0.113.0/24'} onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); addDest(); } }} />}
              {dType !== 'region' && <Button onClick={addDest}>Add</Button>}
            </div>
            <div className="chips">
              {dests.map((d, i) => <button type="button" key={i} className="chip" onClick={() => setDests((x) => x.filter((_, j) => j !== i))} title="Remove">{d.kind === 'region' ? countryName(d.value) : d.value} ✕</button>)}
            </div>
            <p className="small muted">Websites are found from the lookups your devices make through Gatehouse, so a device that uses its own DNS, or already knew the address, may go the normal way at first. Big sites change addresses often. Apps that use fixed addresses cannot be matched by name. Routes carry IPv4 traffic.</p>
          </>
        )}
      </div>
    </Modal>
  );
}

export default function Routes() {
  const { data, reload } = useData('/api/routes', { topics: ['vpn.status', 'net.status'], every: 15000 });
  const [run] = useAction();
  const [edit, setEdit] = useState(null);
  const routes = data?.routes || [];
  const targets = data?.targets || [];
  const again = () => { reload(); };
  const move = (i, d) => {
    const j = i + d;
    if (j < 0 || j >= routes.length) return;
    const ids = routes.map((r) => r.id);
    [ids[i], ids[j]] = [ids[j], ids[i]];
    run(() => post('/api/routes/order', { order: ids })).then(again);
  };
  const body = (r) => ({ id: r.id, name: r.name, enabled: r.enabled, target_kind: r.target_kind, target: r.target, kill: r.kill, uses: r.uses, dests: r.dests });
  return (
    <Page title="Routes" sub="Send chosen devices or networks through a VPN or one internet connection, for everything or only for some destinations."
      actions={<Button kind="primary" small icon="plus" disabled={!targets.length} onClick={() => setEdit({})}>Add a route</Button>}>
      {!targets.length && data && (
        <Note tone="warn" title="Nothing to route to yet">Add a VPN on the VPN page, or a second internet connection on the Internet page, then come back here.</Note>
      )}
      <Card title="Your routes" desc="The first route that matches is used, so put the more specific ones at the top.">
        <div className="list">
          {routes.map((r, i) => {
            const st = STATE[r.state] || STATE.working;
            return (
              <div className={`item ${r.enabled ? '' : 'off'}`} key={r.id}>
                <div style={{ flex: 1, minWidth: 0 }}>
                  <div className="name">{r.name} <Pill tone={st.tone}>{st.label}</Pill></div>
                  <div className="meta">{r.plain}</div>
                  {(r.warnings || []).map((w, k) => <div className="meta warnline" key={k}>⚠ {w}</div>)}
                </div>
                <div className="actions">
                  <Button small aria-label="Move up" disabled={i === 0} onClick={() => move(i, -1)}>↑</Button>
                  <Button small aria-label="Move down" disabled={i === routes.length - 1} onClick={() => move(i, 1)}>↓</Button>
                  <Button small onClick={() => run(() => put('/api/routes', { ...body(r), enabled: !r.enabled }), r.enabled ? 'Paused.' : 'Turned on.').then(again)}>{r.enabled ? 'Pause' : 'Turn on'}</Button>
                  <Button small icon="edit" onClick={() => setEdit(r)}>Edit</Button>
                  <Button small kind="danger" icon="trash" onClick={async () => await confirmBox(`Remove "${r.name}"?\nThe traffic goes back to using your normal connection.`) && run(() => del(`/api/routes/${r.id}`), 'Removed.').then(again)}>Remove</Button>
                </div>
              </div>
            );
          })}
          {data && !routes.length && <Empty>No routes yet. Add one to send a device, a network or a website through a VPN or a chosen connection.</Empty>}
        </div>
      </Card>
      {(data?.legacy || []).length > 0 && (
        <Card title="Set up on the VPN page" desc="These send everything from the devices below through a VPN. They are changed on the VPN page, and they come after the routes above.">
          <div className="list">
            {data.legacy.map((l) => (
              <div className="item" key={l.tunnel}><div><div className="name">{l.uses}</div><div className="meta">everything through {l.label}</div></div><div className="actions"><Button small onClick={() => go('vpn')}>Open VPN</Button></div></div>
            ))}
          </div>
        </Card>
      )}
      {edit && <RouteForm initial={edit.id ? edit : undefined} targets={targets} onClose={() => setEdit(null)} onSaved={again} />}
    </Page>
  );
}

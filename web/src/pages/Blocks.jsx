import React, { useState } from 'react';
import { del, post, put, useData } from '../api.js';
import { Button, Card, Empty, Field, Input, Modal, Note, Pill, Segmented, Select, ToggleRow, useAction, confirmBox } from '../ui.jsx';

// Block an app, a topic or a website name for everyone, a network, a profile or one device, always
// or on a schedule. Today the blocking works by name. Thorough checking is shown but not on yet.

const DAYS = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];
const STATUS = { blocking: ['Blocking', 'bad'], allowing: ['Allowed', 'good'], paused: ['Paused', 'warn'], off: ['Off', ''], 'outside hours': ['Not in its hours', ''], waiting: ['Waiting', 'warn'] };

function parseSchedule(s) { try { return s ? JSON.parse(s) : { mode: 'always' }; } catch { return { mode: 'always' }; } }

function BlockForm({ initial, data, onClose, onSaved }) {
  const [f, setF] = useState({ kind: 'app', value: '', scope: 'everyone', target: '', allow: false, method: 'fast', enabled: true, ...initial });
  const sc0 = parseSchedule(initial?.schedule);
  const [sc, setSc] = useState({ mode: sc0.mode || 'always', from: sc0.from || '21:00', to: sc0.to || '07:00', days: sc0.days || [1, 2, 3, 4, 5] });
  const [q, setQ] = useState('');
  const [run, busy] = useAction();
  const { data: groups } = useData('/api/dns/groups');
  const { data: ch } = useData('/api/vpn/choices');
  const { data: ov } = useData('/api/fw/overview');
  const networks = (ov?.matrix_zones || []).map((z) => z.name);
  const apps = (data?.apps || []).filter((a) => !q || a.name.toLowerCase().includes(q.toLowerCase()));
  const schedule = sc.mode === 'always' ? '' : JSON.stringify(sc.mode === 'daily' ? { mode: 'daily', from: sc.from, to: sc.to } : { mode: 'weekly', from: sc.from, to: sc.to, days: sc.days });
  const targets = f.scope === 'network' ? networks.map((n) => ({ value: n, label: n }))
    : f.scope === 'profile' ? (groups || []).map((g) => ({ value: String(g.id), label: g.name }))
      : f.scope === 'device' ? (ch?.devices || []).map((d) => ({ value: d.mac.toLowerCase(), label: d.name })) : [];
  const ready = f.value.trim() && (f.scope === 'everyone' || f.target);
  const save = async () => { if (await run(() => put('/api/blocks', { ...f, schedule }), 'Saved.')) { onSaved(); onClose(); } };
  return (
    <Modal title={initial?.id ? 'Edit' : (f.allow ? 'Allow an exception' : 'Block something')} onClose={onClose}
      footer={<><Button onClick={onClose}>Cancel</Button><Button kind="primary" onClick={save} disabled={busy || !ready}>Save</Button></>}>
      <div className="stack">
        <Field label="What">
          <Segmented value={f.kind} onChange={(v) => setF({ ...f, kind: v, value: '' })} options={[{ value: 'app', label: 'An app' }, { value: 'topic', label: 'A topic' }, { value: 'name', label: 'A website' }]} />
        </Field>
        {f.kind === 'app' && (
          <Field label="Which app" hint={f.value ? `Chosen: ${f.value}` : 'Search, then tap one.'}>
            <div className="stack">
              <Input value={q} onChange={setQ} placeholder="Search apps" aria-label="Search apps" />
              <div style={{ display: 'flex', flexWrap: 'wrap', gap: 'var(--space-2)', maxHeight: '10rem', overflow: 'auto' }}>
                {apps.map((a) => <Button key={a.name} small kind={f.value === a.name ? 'primary' : undefined} onClick={() => setF({ ...f, value: a.name })}>{a.name}</Button>)}
              </div>
            </div>
          </Field>
        )}
        {f.kind === 'topic' && (
          <Field label="Which topic" hint="Each topic is a list Gatehouse downloads and keeps up to date.">
            <div className="stack">
              {(data?.topics || []).map((t) => (
                <label key={t.key} className="small" style={{ display: 'flex', gap: 'var(--space-2)', alignItems: 'center' }}>
                  <input type="radio" name="topic" checked={f.value === t.key} onChange={() => setF({ ...f, value: t.key })} /> <span><strong>{t.name}</strong> · {t.descr}</span>
                </label>
              ))}
            </div>
          </Field>
        )}
        {f.kind === 'name' && <Field label="Website name" hint="Such as games.example.com. Blocks it and anything under it."><Input value={f.value} onChange={(v) => setF({ ...f, value: v })} className="in mono" /></Field>}
        <Field label="For whom">
          <Segmented value={f.scope} onChange={(v) => setF({ ...f, scope: v, target: '' })} options={[{ value: 'everyone', label: 'Everyone' }, { value: 'network', label: 'A network' }, { value: 'profile', label: 'A profile' }, { value: 'device', label: 'One device' }]} />
        </Field>
        {f.scope !== 'everyone' && <Select value={f.target} onChange={(v) => setF({ ...f, target: v })} options={[{ value: '', label: 'Choose…' }, ...targets]} />}
        <ToggleRow title="This is an exception" desc="Lets this app, topic or website through for the chosen device, network or profile, even if a block elsewhere says no." checked={f.allow} onChange={(v) => setF({ ...f, allow: v })} />
        <Field label="How">
          <div className="stack">
            <label className="small" style={{ display: 'flex', gap: 'var(--space-2)' }}><input type="radio" checked readOnly /> <span><strong>Option A: By name (fast)</strong>. Gatehouse refuses the app's website names. No slowdown. It can miss an app that uses its own private DNS, or a call already in progress.</span></label>
            <label className="small muted" style={{ display: 'flex', gap: 'var(--space-2)' }}><input type="radio" disabled /> <span><strong>Option B: Thorough</strong>. Looks at the traffic itself. Catches more but can be slower. Not switched on yet.</span></label>
          </div>
        </Field>
        <Field label="When">
          <Segmented value={sc.mode} onChange={(v) => setSc({ ...sc, mode: v })} options={[{ value: 'always', label: 'Always' }, { value: 'daily', label: 'Every day' }, { value: 'weekly', label: 'Chosen days' }]} />
        </Field>
        {sc.mode !== 'always' && (
          <div className="stack">
            <div className="formgrid">
              <Field label="From"><input type="time" className="in" value={sc.from} onChange={(e) => setSc({ ...sc, from: e.target.value })} /></Field>
              <Field label="To"><input type="time" className="in" value={sc.to} onChange={(e) => setSc({ ...sc, to: e.target.value })} /></Field>
            </div>
            {sc.mode === 'weekly' && (
              <div style={{ display: 'flex', gap: 'var(--space-2)', flexWrap: 'wrap' }}>
                {DAYS.map((d, i) => <label key={d} className="small"><input type="checkbox" checked={sc.days.includes(i)} onChange={(e) => setSc({ ...sc, days: e.target.checked ? [...sc.days, i] : sc.days.filter((x) => x !== i) })} /> {d}</label>)}
              </div>
            )}
          </div>
        )}
        <ToggleRow title="Turned on" checked={f.enabled} onChange={(v) => setF({ ...f, enabled: v })} />
      </div>
    </Modal>
  );
}

export default function Blocks() {
  const { data, reload } = useData('/api/blocks', { every: 15000 });
  const [edit, setEdit] = useState(null);
  const [run, busy] = useAction();
  const rules = data?.rules || [];
  return (
    <Card title="Block apps, topics and websites" desc="Choose what to block and for whom: everyone, a network, a profile or one device. Add hours if you like."
      actions={<Button small icon="plus" onClick={() => setEdit({})}>Add a block</Button>}>
      <Note>If anything goes wrong, nothing is blocked. A fault never cuts a device off.</Note>
      <div className="list">
        {rules.map((r) => {
          const [word, tone] = STATUS[r.status] || [r.status, ''];
          return (
            <div className="item" key={r.id}>
              <div style={{ flex: 1, minWidth: 0 }}>
                <div className="name">{r.text} <Pill tone={tone}>{word}</Pill></div>
                <div className="meta">{r.when}{r.method === 'thorough' ? ' · thorough' : ' · by name'}</div>
              </div>
              <div className="actions">
                {r.status === 'paused'
                  ? <Button small disabled={busy} onClick={() => run(() => post(`/api/blocks/${r.id}/pause`, { minutes: 0 }), 'Resumed.').then(reload)}>Resume</Button>
                  : r.enabled && <Button small disabled={busy} onClick={() => run(() => post(`/api/blocks/${r.id}/pause`, { minutes: 60 }), 'Paused for an hour.').then(reload)}>Pause 1 hour</Button>}
                <Button small disabled={busy} onClick={() => run(() => post(`/api/blocks/${r.id}/enable`, { enabled: !r.enabled })).then(reload)}>{r.enabled ? 'Turn off' : 'Turn on'}</Button>
                <Button small icon="edit" onClick={() => setEdit(r)}>Edit</Button>
                <Button small kind="danger" icon="trash" onClick={async () => await confirmBox('Delete this block?') && run(() => del(`/api/blocks/${r.id}`)).then(reload)}>Delete</Button>
              </div>
            </div>
          );
        })}
        {data && !rules.length && <Empty>Nothing is blocked this way yet.</Empty>}
      </div>
      <p className="small muted">{data?.inspection?.note}</p>
      {edit && <BlockForm initial={edit} data={data} onClose={() => setEdit(null)} onSaved={reload} />}
    </Card>
  );
}

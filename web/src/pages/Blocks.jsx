import React, { useEffect, useState } from 'react';
import { hashQuery, setHashQuery, useWantedEdit } from '../hashtab.js';
import { del, post, put, useData } from '../api.js';
import { Button, Card, Empty, Field, Input, Modal, Note, Pill, Segmented, Select, ToggleRow, useAction, useUndoDelete, fmt, Limited } from '../ui.jsx';
import { PolicyControls } from '../policykit.jsx';

// Block an app, a topic or a website name for everyone, a network, a profile or one device, always
// or on a schedule. Today the blocking works by name. Thorough checking is shown but not on yet.

const blockBody = (r) => ({ kind: r.kind, value: r.value, scope: r.scope, target: r.target, allow: r.allow, method: r.method, schedule: r.schedule, enabled: r.enabled });
const DAYS = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];
const STATUS = { blocking: ['Blocking', 'bad'], allowing: ['Allowed', 'good'], paused: ['Paused', 'warn'], off: ['Off', ''], 'outside hours': ['Not in its hours', ''], waiting: ['Waiting', 'warn'] };

function parseSchedule(s) { try { return s ? JSON.parse(s) : { mode: 'always' }; } catch { return { mode: 'always' }; } }

// The names a topic blocks, searchable, so a person can see what choosing it means.
function TopicNames({ topic, onClose }) {
  const [q, setQ] = useState('');
  const { data, error } = useData(`/api/blocks/topics/${encodeURIComponent(topic.key)}/names?q=${encodeURIComponent(q)}`);
  return (
    <Modal title={`Names in ${topic.name}`} onClose={onClose} footer={<Button onClick={onClose}>Close</Button>}>
      <div className="stack">
        <Input value={q} onChange={setQ} placeholder="Search these names" aria-label="Search these names" />
        {!data && !error && <p className="small muted">Loading…</p>}
        {error && <p className="small muted">{String(error.message || error)}</p>}
        {data && <p className="small muted">{data.total ? `${fmt.n(data.total)} names are blocked by this topic${data.updated ? `, list updated ${fmt.ago(data.updated)}` : ''}.${data.names.length >= 100 ? ' Showing the first 100 that match; search to narrow it down.' : ''}` : 'The list has not been downloaded yet, so there are no names to show.'}</p>}
        {data && <div className="list">{data.names.map((n) => <div className="item" key={n.name}><span className="mono">{n.name}</span></div>)}</div>}
        {data && q && !data.names.length && data.total > 0 && <p className="small muted">None of the names match.</p>}
      </div>
    </Modal>
  );
}

function BlockForm({ initial, data, onClose, onSaved }) {
  const [f, setF] = useState({ kind: 'app', value: '', scope: 'everyone', target: '', allow: false, method: 'fast', enabled: true, ...initial });
  const sc0 = parseSchedule(initial?.schedule);
  const [sc, setSc] = useState({ mode: sc0.mode || 'always', from: sc0.from || '21:00', to: sc0.to || '07:00', days: sc0.days || [1, 2, 3, 4, 5] });
  const [q, setQ] = useState('');
  const [browse, setBrowse] = useState(null);
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
          <Field label="Which topic" hint="Each topic is a list NopeWarden downloads and keeps up to date.">
            <div className="stack">
              {(data?.topics || []).map((t) => (
                <label key={t.key} className="small" style={{ display: 'flex', gap: 'var(--space-2)', alignItems: 'center' }}>
                  <input type="radio" name="topic" checked={f.value === t.key} onChange={() => setF({ ...f, value: t.key })} /> <span><strong>{t.name}</strong> · {t.descr}</span>
                  {!t.key.startsWith('cat:') && <button type="button" className="link small" onClick={(e) => { e.preventDefault(); setBrowse(t); }}>View names</button>}
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
            <label className="small" style={{ display: 'flex', gap: 'var(--space-2)' }}><input type="radio" name="method" checked={f.method === 'fast'} onChange={() => setF({ ...f, method: 'fast' })} /> <span><strong>Option A: By name (fast)</strong>. NopeWarden refuses the app's website names. No slowdown. It can miss an app that uses its own private DNS, or a call already in progress.</span></label>
            <label className="small" style={{ display: 'flex', gap: 'var(--space-2)' }}><input type="radio" name="method" checked={f.method === 'thorough'} onChange={() => setF({ ...f, method: 'thorough' })} /> <span><strong>Option B: Thorough</strong>. Looks at the traffic itself. Catches more but can be slower.</span></label>
            <label className="small" style={{ display: 'flex', gap: 'var(--space-2)' }}><input type="radio" name="method" checked={f.method === 'both'} onChange={() => setF({ ...f, method: 'both' })} /> <span><strong>Both</strong>. By name now, and thorough as well once it is on.</span></label>
            {f.method !== 'fast' && <Note tone="warn">{data?.inspection?.note}</Note>}
            <p className="small muted">If either way fails, nothing is blocked. A fault never cuts a device off.</p>
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
      {browse && <TopicNames topic={browse} onClose={() => setBrowse(null)} />}
    </Modal>
  );
}

export default function Blocks() {
  const { data, reload } = useData('/api/blocks', { every: 15000 });
  const [edit, setEdit] = useState(() => (hashQuery('new') ? {} : null));
  useEffect(() => { if (hashQuery('new')) setHashQuery('new', ''); }, []);
  const [run, busy] = useAction();
  const undoDelete = useUndoDelete();
  const rules = data?.rules || [];
  useWantedEdit(data && rules, (r, w) => String(r.id) === w, setEdit);
  return (
    <Card title="Block apps, topics and websites" desc="Choose what to block and for whom: everyone, a network, a profile or one device. Add hours if you like."
      actions={<Button small icon="plus" onClick={() => setEdit({})}>Add a block</Button>}>
      <Note>If anything goes wrong, nothing is blocked. A fault never cuts a device off.</Note>
      <div className="list">
        {<Limited items={rules} noun="blocks" render={(r) => {
          const [word, tone] = STATUS[r.status] || [r.status, ''];
          return (
            <div className="item" key={r.id}>
              <div className="fill">
                <div className="name">{r.text} <Pill tone={tone}>{word}</Pill></div>
                <div className="meta">{r.when}{r.method === 'thorough' ? ' · thorough, waiting for it to be switched on' : r.method === 'both' ? ' · by name now, thorough once it is on' : ' · by name'}</div>
              </div>
              <div className="actions">
                <PolicyControls name={r.name || 'this block'} enabled={r.enabled} pausedUntil={r.paused_until} busy={busy}
                  onPause={(m) => run(() => post(`/api/blocks/${r.id}/pause`, { minutes: m }), m === 0 ? 'Resumed.' : 'Paused.').then(reload)}
                  onToggle={() => run(() => post(`/api/blocks/${r.id}/enable`, { enabled: !r.enabled })).then(reload)}
                  onEdit={() => setEdit(r)}
                  onRemove={() => undoDelete('The block', () => del(`/api/blocks/${r.id}`), () => put('/api/blocks', blockBody(r)), reload)} />
              </div>
            </div>
          );
        }} />}
        {data && !rules.length && <Empty>Nothing is blocked this way yet.</Empty>}
      </div>
      <p className="small muted">{data?.inspection?.note} {data?.fail_open}</p>
      {edit && <BlockForm initial={edit} data={data} onClose={() => setEdit(null)} onSaved={reload} />}
    </Card>
  );
}

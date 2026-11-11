import React, { useEffect, useState } from 'react';
import { hashQuery, setHashQuery, useWantedEdit } from '../hashtab.js';
import { del, post, put, useData } from '../api.js';
import { Button, Card, Empty, Field, Input, Modal, Note, Pill, Segmented, Select, TimeField, ToggleRow, useAction, useUndoDelete, fmt, Limited } from '../ui.jsx';
import { PolicyControls } from '../policykit.jsx';

// A block is a named list of apps, categories and websites that applies to everyone or to a chosen
// list of networks, profiles and devices, with one How, one When and one on/off switch. Exceptions
// are blocks of their own that let things through. Today the blocking works by name.

const blockBody = (r) => ({ name: r.name, items: r.items.map(({ kind, value }) => ({ kind, value })), targets: r.targets.map(({ scope, target }) => ({ scope, target })), allow: r.allow, method: r.method, schedule: r.schedule, enabled: r.enabled });
const DAYS = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];
const STATUS = { blocking: ['Blocking', 'bad'], allowing: ['Allowed', 'good'], paused: ['Paused', 'warn'], off: ['Off', ''], 'outside hours': ['Not in its hours', ''], waiting: ['Waiting', 'warn'], nowhere: ['Applies nowhere', 'warn'] };

function parseSchedule(s) { try { return s ? JSON.parse(s) : { mode: 'always' }; } catch { return { mode: 'always' }; } }

// The names a category blocks, searchable, so a person can see what choosing it means.
function TopicNames({ topic, onClose }) {
  const [q, setQ] = useState('');
  const { data, error } = useData(`/api/blocks/topics/${encodeURIComponent(topic.key)}/names?q=${encodeURIComponent(q)}`);
  return (
    <Modal title={`Names in ${topic.name}`} onClose={onClose} footer={<Button onClick={onClose}>Close</Button>}>
      <div className="stack">
        <Input value={q} onChange={setQ} placeholder="Search these names" aria-label="Search these names" />
        {!data && !error && <p className="small muted">Loading…</p>}
        {error && <p className="small muted">{String(error.message || error)}</p>}
        {data && <p className="small muted">{data.total ? `${fmt.n(data.total)} names are blocked by this category${data.updated ? `, list updated ${fmt.ago(data.updated)}` : ''}.${data.names.length >= 100 ? ' Showing the first 100 that match; search to narrow it down.' : ''}` : 'The list has not been downloaded yet, so there are no names to show.'}</p>}
        {data && <div className="list">{data.names.map((n) => <div className="item" key={n.name}><span className="mono">{n.name}</span></div>)}</div>}
        {data && q && !data.names.length && data.total > 0 && <p className="small muted">None of the names match.</p>}
      </div>
    </Modal>
  );
}


// Starting points: each fills the form with a sensible list the person can change before saving.
const STARTERS = [
  { name: 'Social media', apps: ['Facebook', 'Instagram', 'TikTok', 'Snapchat', 'X', 'Twitter', 'Reddit', 'Pinterest'], topics: [] },
  { name: 'Streaming', apps: [], topics: ['cat:Video', 'cat:Music'] },
  { name: 'Gaming', apps: [], topics: ['cat:Games'] },
  { name: 'AI chat', apps: [], topics: ['cat:AI'] },
];

const chip = (on, label, click, key) => <Button key={key || label} small kind={on ? 'primary' : undefined} onClick={click}>{label}</Button>;
const toggle = (list, v) => (list.includes(v) ? list.filter((x) => x !== v) : [...list, v]);

function BlockForm({ initial, data, allow, onClose, onSaved }) {
  const items0 = initial?.items || [];
  const tg0 = initial?.targets || [];
  const [name, setName] = useState(initial?.name || '');
  const [apps, setApps] = useState(items0.filter((i) => i.kind === 'app').map((i) => i.value));
  const [topics, setTopics] = useState(items0.filter((i) => i.kind === 'topic').map((i) => i.value));
  const [sites, setSites] = useState(items0.filter((i) => i.kind === 'name').map((i) => i.value).join('\n'));
  const [everyone, setEveryone] = useState(tg0.some((t) => t.scope === 'everyone') || (!tg0.length && !initial?.id));
  const [nets, setNets] = useState(tg0.filter((t) => t.scope === 'network').map((t) => t.target));
  const [profs, setProfs] = useState(tg0.filter((t) => t.scope === 'profile').map((t) => t.target));
  const [devs, setDevs] = useState(tg0.filter((t) => t.scope === 'device').map((t) => t.target.toLowerCase()));
  const [method, setMethod] = useState(initial?.method || 'fast');
  const [enabled, setEnabled] = useState(initial ? initial.enabled !== false : true);
  const sc0 = parseSchedule(initial?.schedule);
  const [sc, setSc] = useState({ mode: sc0.mode || 'always', from: sc0.from || '21:00', to: sc0.to || '07:00', days: sc0.days || [1, 2, 3, 4, 5] });
  const [q, setQ] = useState('');
  const [browse, setBrowse] = useState(null);
  const [run, busy] = useAction();
  const { data: groups } = useData('/api/dns/groups');
  const { data: ch } = useData('/api/vpn/choices');
  const { data: ov } = useData('/api/fw/overview');
  const networks = (ov?.matrix_zones || []).map((z) => z.name);
  const shownApps = (data?.apps || []).filter((a) => !q || a.name.toLowerCase().includes(q.toLowerCase())).sort((x, y) => Number(apps.includes(y.name)) - Number(apps.includes(x.name)));
  const schedule = sc.mode === 'always' ? '' : JSON.stringify(sc.mode === 'daily' ? { mode: 'daily', from: sc.from, to: sc.to } : { mode: 'weekly', from: sc.from, to: sc.to, days: sc.days });
  const siteList = sites.split(/[\s,]+/).map((x) => x.trim()).filter(Boolean);
  const items = [...apps.map((v) => ({ kind: 'app', value: v })), ...topics.map((v) => ({ kind: 'topic', value: v })), ...siteList.map((v) => ({ kind: 'name', value: v }))];
  const targets = everyone ? [{ scope: 'everyone', target: '' }] : [...nets.map((t) => ({ scope: 'network', target: t })), ...profs.map((t) => ({ scope: 'profile', target: t })), ...devs.map((t) => ({ scope: 'device', target: t }))];
  const ready = items.length > 0 && targets.length > 0;
  const noun = allow ? 'exception' : 'block';
  const save = async () => { if (await run(() => put('/api/blocks', { id: initial?.id || 0, name, items, targets, allow, method, schedule, enabled }), 'Saved.')) { onSaved(); onClose(); } };
  return (
    <Modal title={initial?.id ? `Edit this ${noun}` : (allow ? 'Add an exception' : 'Add a block')} onClose={onClose}
      footer={<><Button onClick={onClose}>Cancel</Button><Button kind="primary" onClick={save} disabled={busy || !ready}>Save</Button></>}>
      <div className="stack">
        <Field label="Name" hint={`Optional. Left empty, NopeWarden names it from what it ${allow ? 'allows' : 'blocks'}.`}>
          <Input value={name} onChange={setName} placeholder={allow ? 'For example: Homework sites' : 'For example: Bedtime'} aria-label="Name" />
        </Field>
        <Field label={allow ? 'What to let through' : 'What to block'} hint={`${items.length} chosen. Mix apps, categories and websites in one ${noun}.`}>
          <div className="stack">
            <Input value={q} onChange={setQ} placeholder="Search apps" aria-label="Search apps" />
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: 'var(--space-2)', maxHeight: '10rem', overflow: 'auto' }}>
              {shownApps.map((a) => chip(apps.includes(a.name), a.name, () => setApps(toggle(apps, a.name))))}
            </div>
          </div>
        </Field>
        <Field label="Categories" hint="Each category is a list NopeWarden downloads and keeps up to date.">
          <div className="stack">
            {(data?.topics || []).map((t) => (
              <label key={t.key} className="small" style={{ display: 'flex', gap: 'var(--space-2)', alignItems: 'center' }}>
                <input type="checkbox" checked={topics.includes(t.key)} onChange={() => setTopics(toggle(topics, t.key))} /> <span><strong>{t.name}</strong> · {t.descr}</span>
                {!t.key.startsWith('cat:') && <button type="button" className="link small" onClick={(e) => { e.preventDefault(); setBrowse(t); }}>View names</button>}
              </label>
            ))}
          </div>
        </Field>
        <Field label="Websites" hint="One per line, such as games.example.com. Each one covers everything under it.">
          <textarea className="in mono" rows={3} value={sites} onChange={(e) => setSites(e.target.value)} aria-label="Websites" />
        </Field>
        <Field label="Where it applies">
          <div className="stack">
            <ToggleRow title="Everyone" desc="Every device behind NopeWarden." checked={everyone} onChange={setEveryone} />
            {!everyone && (
              <>
                {networks.length > 0 && <Field label="Networks"><div style={{ display: 'flex', flexWrap: 'wrap', gap: 'var(--space-2)' }}>{networks.map((n) => chip(nets.includes(n), n, () => setNets(toggle(nets, n))))}</div></Field>}
                {(groups || []).length > 0 && <Field label="Profiles"><div style={{ display: 'flex', flexWrap: 'wrap', gap: 'var(--space-2)' }}>{(groups || []).map((g) => chip(profs.includes(String(g.id)), g.name, () => setProfs(toggle(profs, String(g.id))), `g${g.id}`))}</div></Field>}
                {(ch?.devices || []).length > 0 && <Field label="Devices"><div style={{ display: 'flex', flexWrap: 'wrap', gap: 'var(--space-2)', maxHeight: '8rem', overflow: 'auto' }}>{(ch.devices || []).map((d) => chip(devs.includes(d.mac.toLowerCase()), d.name, () => setDevs(toggle(devs, d.mac.toLowerCase())), d.mac))}</div></Field>}
                <p className="small muted">Choose as many networks, profiles and devices as you like.</p>
              </>
            )}
          </div>
        </Field>
        <Field label="How">
          <div className="stack">
            <label className="small" style={{ display: 'flex', gap: 'var(--space-2)' }}><input type="radio" name="method" checked={method === 'fast'} onChange={() => setMethod('fast')} /> <span><strong>Option A: By name (fast)</strong>. NopeWarden refuses the app's website names. No slowdown. It can miss an app that uses its own private DNS, or a call already in progress.</span></label>
            <label className="small" style={{ display: 'flex', gap: 'var(--space-2)' }}><input type="radio" name="method" checked={method === 'thorough'} onChange={() => setMethod('thorough')} /> <span><strong>Option B: Thorough</strong>. Looks at the traffic itself. Catches more but can be slower.</span></label>
            <label className="small" style={{ display: 'flex', gap: 'var(--space-2)' }}><input type="radio" name="method" checked={method === 'both'} onChange={() => setMethod('both')} /> <span><strong>Both</strong>. By name now, and thorough as well once it is on.</span></label>
            {method !== 'fast' && <Note tone="warn">{data?.inspection?.note}</Note>}
            {method !== 'fast' && (topics.length > 0 || siteList.length > 0) && <p className="small muted">Thorough checking applies to the apps in this {noun}. Categories and websites are always done by name.</p>}
            <p className="small muted">If either way fails, nothing is blocked. A fault never cuts a device off.</p>
          </div>
        </Field>
        <Field label="When">
          <Segmented value={sc.mode} onChange={(v) => setSc({ ...sc, mode: v })} options={[{ value: 'always', label: 'Always' }, { value: 'daily', label: 'Every day' }, { value: 'weekly', label: 'Chosen days' }]} />
        </Field>
        {sc.mode !== 'always' && (
          <div className="stack">
            <div className="formgrid">
              <Field label="From"><TimeField value={sc.from} onChange={(v) => setSc({ ...sc, from: v })} /></Field>
              <Field label="To"><TimeField value={sc.to} onChange={(v) => setSc({ ...sc, to: v })} /></Field>
            </div>
            {sc.mode === 'weekly' && (
              <div style={{ display: 'flex', gap: 'var(--space-2)', flexWrap: 'wrap' }}>
                {DAYS.map((d, i) => <label key={d} className="small"><input type="checkbox" checked={sc.days.includes(i)} onChange={(e) => setSc({ ...sc, days: e.target.checked ? [...sc.days, i] : sc.days.filter((x) => x !== i) })} /> {d}</label>)}
              </div>
            )}
          </div>
        )}
        <ToggleRow title="Turned on" checked={enabled} onChange={setEnabled} />
      </div>
      {browse && <TopicNames topic={browse} onClose={() => setBrowse(null)} />}
    </Modal>
  );
}

function BlockList({ title, desc, rows, empty, allow, data, reload, setEdit, extra }) {
  const [run, busy] = useAction();
  const undoDelete = useUndoDelete();
  return (
    <Card title={title} desc={desc} actions={<Button small icon="plus" onClick={() => setEdit({ allow })}>{allow ? 'Add an exception' : 'Add a block'}</Button>}>
      {extra}
      <div className="list">
        <Limited items={rows} noun={allow ? 'exceptions' : 'blocks'} render={(r) => {
          const [word, tone] = STATUS[r.status] || [r.status, ''];
          return (
            <div className="item" key={r.id}>
              <div className="fill">
                <div className="name">{r.title} <Pill tone={tone}>{word}</Pill></div>
                <div className="meta">{[r.what, r.where, r.when, r.method === 'thorough' ? 'thorough, waiting for it to be switched on' : r.method === 'both' ? 'by name now, thorough once it is on' : 'by name', !allow && r.recent ? `${fmt.n(r.recent)} refused in the last day` : ''].filter(Boolean).join(' · ')}</div>
              </div>
              <div className="actions">
                <PolicyControls name={r.title} enabled={r.enabled} pausedUntil={r.paused_until} busy={busy}
                  onPause={(m) => run(() => post(`/api/blocks/${r.id}/pause`, { minutes: m }), m === 0 ? 'Resumed.' : 'Paused.').then(reload)}
                  onToggle={() => run(() => post(`/api/blocks/${r.id}/enable`, { enabled: !r.enabled })).then(reload)}
                  onEdit={() => setEdit(r)}
                  onRemove={() => undoDelete(allow ? 'The exception' : 'The block', () => del(`/api/blocks/${r.id}`), () => put('/api/blocks', blockBody(r)), reload)} />
              </div>
            </div>
          );
        }} />
        {data && !rows.length && <Empty>{empty}</Empty>}
      </div>
    </Card>
  );
}

export default function Blocks() {
  const { data, reload } = useData('/api/blocks', { every: 15000 });
  const [edit, setEdit] = useState(() => (hashQuery('new') ? { allow: false } : null));
  useEffect(() => { if (hashQuery('new')) setHashQuery('new', ''); }, []);
  const sets = data?.blocks || [];
  useWantedEdit(data && sets, (r, w) => String(r.id) === w, setEdit);
  const blocks = sets.filter((s) => !s.allow);
  const exceptions = sets.filter((s) => s.allow);
  const known = new Set((data?.apps || []).map((a) => a.name));
  const start = (s) => setEdit({ allow: false, name: s.name, enabled: true, method: 'fast', items: [...s.apps.filter((n) => known.has(n)).map((value) => ({ kind: 'app', value })), ...s.topics.map((value) => ({ kind: 'topic', value }))], targets: [] });
  return (
    <div className="stack">
      <BlockList title="Blocks" desc="Each block is a named list of apps, categories and websites, kept away from everyone or from the networks, profiles and devices you choose, always or on a schedule."
        rows={blocks} empty="Nothing is blocked this way yet." allow={false} data={data} reload={reload} setEdit={setEdit}
        extra={<>
          <Note>If anything goes wrong, nothing is blocked. A fault never cuts a device off.</Note>
          <div className="small" style={{ display: 'flex', gap: 'var(--space-2)', alignItems: 'center', flexWrap: 'wrap', marginBottom: 'var(--space-3)' }}>
            <span className="muted">Start from:</span>
            {STARTERS.map((s) => <Button key={s.name} small onClick={() => start(s)}>{s.name}</Button>)}
          </div>
        </>} />
      <BlockList title="Exceptions" desc="Things to let through for a device, profile or network even when a block says no."
        rows={exceptions} empty="No exceptions." allow data={data} reload={reload} setEdit={setEdit} />
      <p className="small muted">{data?.inspection?.note} {data?.fail_open}</p>
      {edit && <BlockForm initial={edit} allow={!!edit.allow} data={data} onClose={() => setEdit(null)} onSaved={reload} />}
    </div>
  );
}

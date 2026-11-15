import React, { useState } from 'react';
import { del, post, put, useData } from './api.js';
import { Button, Field, Input, Modal, Select, useAction } from './ui.jsx';

// Rule sets and tags. A set groups policies of any kind so they can be paused, turned off or turned on
// together. A tag is only a label for finding things; it never changes what anything does. Both are
// kept apart from the policies, so the same code serves all five kinds and devices.

export const MAX_TAGS = 5;
export const keyOf = (kind, ref) => `${kind}:${kind === 'device' ? String(ref).toLowerCase() : ref}`;

export function useSets() {
  const { data, reload } = useData('/api/sets', { every: 30000 });
  const sets = data?.sets || [];
  const tags = data?.tags || {};
  const setOf = (kind, ref) => sets.find((s) => s.members.some((m) => m.kind === kind && String(m.ref) === String(ref)));
  return { loaded: !!data, sets, tags, allTags: data?.all_tags || [], setOf, tagsOf: (kind, ref) => tags[keyOf(kind, ref)] || [], reload };
}

// Tags as plain outline chips. They have no colours of their own.
export function TagChips({ tags, onPick }) {
  return (tags || []).map((t) => (onPick
    ? <button type="button" key={t} className="tag" onClick={() => onPick(t)} title={`Show everything tagged ${t}`}>{t}</button>
    : <span key={t} className="tag">{t}</span>));
}

// Edits the tags of one item and saves as you go.
export function TagEditor({ tags, known, onChange, disabled }) {
  const [text, setText] = useState('');
  const add = (raw) => {
    const t = raw.toLowerCase().replace(/[^a-z0-9 _-]+/g, '').replace(/\s+/g, ' ').trim().slice(0, 24).trim();
    if (!t || tags.includes(t) || tags.length >= MAX_TAGS) return;
    onChange([...tags, t]);
    setText('');
  };
  const full = tags.length >= MAX_TAGS;
  const options = (known || []).filter((k) => !tags.includes(k.tag));
  return (
    <div className="stack">
      <div className="inline">
        {tags.map((t) => <span key={t} className="tag">{t} <button type="button" className="tagx" aria-label={`Remove tag ${t}`} disabled={disabled} onClick={() => onChange(tags.filter((x) => x !== t))}>✕</button></span>)}
        {!tags.length && <span className="small muted">No tags yet.</span>}
      </div>
      <div className="inline">
        <Input value={text} onChange={setText} disabled={disabled || full} placeholder={full ? `${MAX_TAGS} tags is the most` : 'Add a tag…'} maxLength={24}
          onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ',') { e.preventDefault(); add(text); } }} aria-label="Add a tag" />
        <Button small disabled={disabled || full || !text.trim()} onClick={() => add(text)}>Add</Button>
      </div>
      {!full && options.length > 0 && <div className="inline small muted">Already used: {options.slice(0, 12).map((k) => <button type="button" key={k.tag} className="tag" onClick={() => add(k.tag)}>{k.tag}</button>)}</div>}
      <span className="hint">{full ? `An item can have ${MAX_TAGS} tags. Remove one to add another.` : 'Up to 5 tags. They label things so you can filter and search; they never change what anything does.'}</span>
    </div>
  );
}

// One window for a policy's set and tags, the same for every kind.
export function SetTagsModal({ kind, refId, name, sets, onClose }) {
  const [run, busy] = useAction();
  const cur = sets.setOf(kind, refId);
  const [setName, setSetName] = useState('');
  const policy = kind !== 'device';
  const saveTags = (tags) => run(() => put('/api/tags', { kind, ref: String(refId), tags })).then(() => sets.reload());
  const pick = async (v) => {
    if (v === '__new') return;
    await run(() => put('/api/sets/member', { kind, ref: String(refId), set_id: Number(v) || 0 }), v ? 'Added to the set.' : 'Taken out of the set.');
    sets.reload();
  };
  const make = async () => {
    const r = await run(() => post('/api/sets', { name: setName }));
    if (!r) return;
    await run(() => put('/api/sets/member', { kind, ref: String(refId), set_id: r.id }), 'Set made.');
    setSetName('');
    sets.reload();
  };
  return (
    <Modal title={policy ? 'Set and tags' : 'Tags'} onClose={onClose} footer={<Button kind="primary" onClick={onClose}>Done</Button>}>
      <div className="stack">
        <p className="small muted">{name}</p>
        {policy && (
          <Field label="Rule set (optional)" hint="A set can be paused, turned off or turned on all at once. A policy is in one set at most.">
            <Select value={cur ? String(cur.id) : ''} onChange={pick} disabled={busy} options={[{ value: '', label: 'Not in a set' }, ...sets.sets.map((s) => ({ value: String(s.id), label: s.name }))]} />
          </Field>
        )}
        {policy && (
          <div className="inline">
            <Input value={setName} onChange={setSetName} placeholder="Make a new set…" maxLength={40} aria-label="New set name" />
            <Button small disabled={busy || !setName.trim()} onClick={make}>Make set and add</Button>
          </div>
        )}
        <Field label="Tags">
          <TagEditor tags={sets.tagsOf(kind, refId)} known={sets.allTags} onChange={saveTags} disabled={busy} />
        </Field>
      </div>
    </Modal>
  );
}

export const renameSet = (id, name) => put(`/api/sets/${id}`, { name });
export const removeSet = (id) => del(`/api/sets/${id}`);
export const rememberOff = (id, refs) => put(`/api/sets/${id}/memory`, { off_refs: refs });

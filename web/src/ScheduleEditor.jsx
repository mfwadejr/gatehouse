import React, { useState } from 'react';
import { get, post, put, useData } from './api.js';
import { Button, Field, Input, Select, useAction, fmt } from './ui.jsx';

// One control for "how often does this run": the same everywhere a recurring job is shown.
// The text form is what the machine stores: every:N, daily:HH:MM, weekly:D:HH:MM or off.
const DAYS = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday'];
const PRESETS = [1, 3, 6, 12, 24];

function parts(spec) {
  const p = (spec || '').split(':');
  if (p[0] === 'every') return { kind: PRESETS.includes(Number(p[1])) ? 'every' : 'custom', hours: Number(p[1]) || 12, day: 1, time: '03:00' };
  if (p[0] === 'daily') return { kind: 'daily', hours: 12, day: 1, time: `${p[1]}:${p[2]}` };
  if (p[0] === 'weekly') return { kind: 'weekly', hours: 12, day: Number(p[1]), time: `${p[2]}:${p[3]}` };
  return { kind: 'off', hours: 12, day: 1, time: '03:00' };
}
function build(s) {
  if (s.kind === 'off') return 'off';
  if (s.kind === 'daily') return `daily:${s.time}`;
  if (s.kind === 'weekly') return `weekly:${s.day}:${s.time}`;
  return `every:${s.hours}`;
}

export function ScheduleEditor({ id, label, extra, hideRun }) {
  const { data, reload } = useData('/api/schedules', { every: 30000 });
  const [run, busy] = useAction();
  const [draft, setDraft] = useState(null);
  const j = (data || []).find((x) => x.id === id);
  if (!j) return null;
  const s = draft || parts(j.spec);
  const dirty = !!draft && build(draft) !== j.spec;
  const kinds = [
    ...PRESETS.map((h) => ({ value: `e${h}`, label: h === 24 ? 'Every day' : `Every ${h} hour${h === 1 ? '' : 's'}` })),
    { value: 'daily', label: 'Every day at a time…' },
    { value: 'weekly', label: 'Every week…' },
    { value: 'custom', label: 'Custom hours…' },
    ...(j.allow_off ? [{ value: 'off', label: 'Off' }] : []),
  ];
  const cur = s.kind === 'every' ? `e${s.hours}` : s.kind;
  const pick = (v) => setDraft(v.startsWith('e') ? { ...s, kind: 'every', hours: Number(v.slice(1)) } : { ...s, kind: v });
  const save = () => run(() => put(`/api/schedules/${id}`, { spec: build(s) }), 'Saved.').then((r) => { if (r) { setDraft(null); reload(); } });
  const now = () => run(() => post(`/api/schedules/${id}/run`), 'Done.').then(reload);
  return (
    <Field label={label || 'How often'} hint={j.help}>
      <div className="stack">
        <div className="inline" style={{ flexWrap: 'wrap', gap: '.5rem' }}>
          <Select value={cur} onChange={pick} options={kinds} aria-label={`How often: ${j.name}`} />
          {s.kind === 'weekly' && <Select value={String(s.day)} onChange={(v) => setDraft({ ...s, day: Number(v) })} options={DAYS.map((d, i) => ({ value: String(i), label: d }))} aria-label="Day" />}
          {(s.kind === 'daily' || s.kind === 'weekly') && <input type="time" className="in" value={s.time} onChange={(e) => setDraft({ ...s, time: e.target.value })} aria-label="Time of day (the machine's own clock)" />}
          {s.kind === 'custom' && <><Input value={String(s.hours)} onChange={(v) => setDraft({ ...s, hours: Number(v.replace(/\D/g, '')) || 0 })} inputMode="numeric" aria-label="Hours between runs" style={{ maxWidth: '5rem' }} /><span className="small muted">hours (1 to 720)</span></>}
          {dirty && <Button onClick={save} disabled={busy}>Save</Button>}
          {dirty && <Button kind="ghost" onClick={() => setDraft(null)}>Cancel</Button>}
          {!hideRun && <Button onClick={now} disabled={busy} icon="refresh">Run now</Button>}
        </div>
        <p className="small muted" style={{ margin: 0 }}>
          {j.last ? `Last done ${fmt.ago(j.last)}.` : 'Not done yet.'}{' '}
          {j.spec === 'off' ? 'Not scheduled.' : j.next ? (j.next * 1000 <= Date.now() ? 'Due now.' : `Next: ${fmt.dt(j.next)}.`) : ''}
          {(s.kind === 'daily' || s.kind === 'weekly') && ' Times use the machine’s own clock.'}
        </p>
        {extra}
      </div>
    </Field>
  );
}

// Every recurring job in one place, for Settings.
export function ScheduleTable() {
  const { data } = useData('/api/schedules', { every: 30000 });
  return (
    <div className="stack">
      {(data || []).map((j) => <ScheduleEditor key={j.id} id={j.id} label={j.name} />)}
    </div>
  );
}

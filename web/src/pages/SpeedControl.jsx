import React, { useEffect, useState } from 'react';
import { hashQuery, setHashQuery, useWantedEdit } from '../hashtab.js';
import { del, post, put, useData } from '../api.js';
import { Button, Card, Empty, Field, Input, Modal, Note, NumberInput, Pill, Segmented, TimeField, ToggleRow, useAction, useUndoDelete, Limited } from '../ui.jsx';
import { PolicyControls } from '../policykit.jsx';

// Speed control: Limit speed holds a network, profile or device (optionally only for some apps,
// categories or websites) to a number of Mb/s; Give priority puts it first or last in line when the
// internet connection is full. One master switch turns every entry off at once.

const DAYS = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];
const STATUS = { limiting: ['Limiting', 'good'], prioritising: ['Giving priority', 'good'], waiting: ['Waiting for the smart queue', 'warn'], paused: ['Paused', 'warn'], off: ['Off', ''], 'outside hours': ['Not in its hours', ''] };
const toggle = (list, v) => (list.includes(v) ? list.filter((x) => x !== v) : [...list, v]);
const chip = (on, label, click, key) => <Button key={key || label} small kind={on ? 'primary' : undefined} onClick={click}>{label}</Button>;
function parseSchedule(s) { try { return s ? JSON.parse(s) : { mode: 'always' }; } catch { return { mode: 'always' }; } }
const ruleBody = (r) => ({ id: r.id, name: r.name, kind: r.kind, prio: r.prio, down_mbps: r.down_mbps, up_mbps: r.up_mbps, each: r.each, schedule: r.schedule, enabled: r.enabled, paused_until: r.paused_until, items: r.items.map(({ kind, value }) => ({ kind, value })), targets: r.targets.map(({ scope, target }) => ({ scope, target })) });

function SpeedForm({ initial, data, onClose, onSaved }) {
  const items0 = initial?.items || [];
  const tg0 = initial?.targets || [];
  const [kind, setKind] = useState(initial?.kind || 'limit');
  const [name, setName] = useState(initial?.name || '');
  const [down, setDown] = useState(initial?.down_mbps || 0);
  const [up, setUp] = useState(initial?.up_mbps || 0);
  const [each, setEach] = useState(!!initial?.each);
  const [prio, setPrio] = useState(initial?.prio && initial.prio !== 'normal' ? initial.prio : (initial?.id ? 'normal' : 'high'));
  const [apps, setApps] = useState(items0.filter((i) => i.kind === 'app').map((i) => i.value));
  const [cats, setCats] = useState(items0.filter((i) => i.kind === 'topic').map((i) => i.value));
  const [sites, setSites] = useState(items0.filter((i) => i.kind === 'name').map((i) => i.value).join('\n'));
  const [everyone, setEveryone] = useState(tg0.some((t) => t.scope === 'everyone') || (!tg0.length && !initial?.id));
  const [nets, setNets] = useState(tg0.filter((t) => t.scope === 'network').map((t) => t.target));
  const [profs, setProfs] = useState(tg0.filter((t) => t.scope === 'profile').map((t) => t.target));
  const [devs, setDevs] = useState(tg0.filter((t) => t.scope === 'device').map((t) => t.target.toLowerCase()));
  const [enabled, setEnabled] = useState(initial ? initial.enabled !== false : true);
  const sc0 = parseSchedule(initial?.schedule);
  const [sc, setSc] = useState({ mode: sc0.mode || 'always', from: sc0.from || '21:00', to: sc0.to || '07:00', days: sc0.days || [1, 2, 3, 4, 5] });
  const [q, setQ] = useState('');
  const [run, busy] = useAction();
  const { data: groups } = useData('/api/dns/groups');
  const { data: ch } = useData('/api/vpn/choices');
  const { data: ov } = useData('/api/fw/overview');
  const networks = (ov?.matrix_zones || []).map((z) => z.name);
  const shownApps = (data?.apps || []).filter((a) => !q || a.name.toLowerCase().includes(q.toLowerCase())).sort((x, y) => Number(apps.includes(y.name)) - Number(apps.includes(x.name)));
  const schedule = sc.mode === 'always' ? '' : JSON.stringify(sc.mode === 'daily' ? { mode: 'daily', from: sc.from, to: sc.to } : { mode: 'weekly', from: sc.from, to: sc.to, days: sc.days });
  const siteList = sites.split(/[\s,]+/).map((x) => x.trim()).filter(Boolean);
  const items = [...apps.map((v) => ({ kind: 'app', value: v })), ...cats.map((v) => ({ kind: 'topic', value: v })), ...siteList.map((v) => ({ kind: 'name', value: v }))];
  const targets = everyone ? [{ scope: 'everyone', target: '' }] : [...nets.map((t) => ({ scope: 'network', target: t })), ...profs.map((t) => ({ scope: 'profile', target: t })), ...devs.map((t) => ({ scope: 'device', target: t }))];
  const ready = targets.length > 0 && (kind === 'priority' || down > 0 || up > 0);
  const save = async () => {
    const body = { id: initial?.id || 0, name, kind, prio: kind === 'priority' ? prio : 'normal', down_mbps: kind === 'limit' ? down : 0, up_mbps: kind === 'limit' ? up : 0, each: kind === 'limit' && each, schedule, enabled, items, targets };
    if (await run(() => put('/api/speed/rule', body), 'Saved.')) { onSaved(); onClose(); }
  };
  return (
    <Modal title={initial?.id ? 'Edit this speed entry' : (kind === 'limit' ? 'Limit speed' : 'Give priority')} onClose={onClose}
      footer={<><Button onClick={onClose}>Cancel</Button><Button kind="primary" onClick={save} disabled={busy || !ready}>Save</Button></>}>
      <div className="stack">
        {!initial?.id && <Segmented value={kind} onChange={setKind} options={[{ value: 'limit', label: 'Limit speed' }, { value: 'priority', label: 'Give priority' }]} />}
        <Field label="Name" hint="Optional. Left empty, NopeWarden names it from who it covers.">
          <Input value={name} onChange={setName} placeholder={kind === 'limit' ? 'For example: Guest speed' : 'For example: Video calls first'} aria-label="Name" />
        </Field>
        {kind === 'limit' ? (
          <>
            <div className="formgrid">
              <Field label="Download limit (Mb/s)" hint="Leave at 0 for no limit."><NumberInput value={down} onChange={setDown} min={0} step={1} aria-label="Download limit in megabits per second" /></Field>
              <Field label="Upload limit (Mb/s)" hint="Leave at 0 for no limit."><NumberInput value={up} onChange={setUp} min={0} step={1} aria-label="Upload limit in megabits per second" /></Field>
            </div>
            <Field label="How the limit is shared">
              <Segmented value={each ? 'each' : 'shared'} onChange={(v) => setEach(v === 'each')} options={[{ value: 'shared', label: 'All together' }, { value: 'each', label: 'Each device' }]} />
              <p className="small muted">{each ? 'Every device it covers gets this speed to itself.' : 'Everything it covers shares this speed between them.'}</p>
            </Field>
          </>
        ) : (
          <Field label="Priority">
            <Segmented value={prio} onChange={setPrio} options={[{ value: 'high', label: 'High' }, { value: 'normal', label: 'Normal' }, { value: 'low', label: 'Low' }]} />
            <p className="small muted">Only matters when the internet connection is full. High goes first, Low waits for the rest. It needs Keep delay low to be turned on for the connection (the Speed tab).</p>
          </Field>
        )}
        <Field label="What it applies to" hint={`${items.length ? `${items.length} chosen.` : 'Nothing chosen means everything.'} Mix apps, categories and websites.`}>
          <div className="stack">
            <Input value={q} onChange={setQ} placeholder="Search apps" aria-label="Search apps" />
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: 'var(--space-2)', maxHeight: '10rem', overflow: 'auto' }}>
              {shownApps.map((a) => chip(apps.includes(a.name), a.name, () => setApps(toggle(apps, a.name))))}
            </div>
          </div>
        </Field>
        <Field label="Categories">
          <div style={{ display: 'flex', flexWrap: 'wrap', gap: 'var(--space-2)' }}>
            {(data?.categories || []).map((c) => chip(cats.includes(`cat:${c}`), c, () => setCats(toggle(cats, `cat:${c}`))))}
          </div>
        </Field>
        <Field label="Websites" hint="One per line, such as video.example.com. Each one covers everything under it.">
          <textarea className="in mono" rows={3} value={sites} onChange={(e) => setSites(e.target.value)} aria-label="Websites" />
        </Field>
        {items.length > 0 && <p className="small muted">Apps, categories and websites are recognised by the names devices ask for, so it takes hold a moment after a device first asks. Traffic that never asks by name is not covered.</p>}
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
    </Modal>
  );
}

export default function SpeedControl() {
  const { data, reload } = useData('/api/speed', { every: 15000 });
  const [edit, setEdit] = useState(() => (['limit', 'priority'].includes(hashQuery('new')) ? { kind: hashQuery('new') } : null));
  useEffect(() => { if (hashQuery('new')) setHashQuery('new', ''); }, []);
  useWantedEdit(data && data.rules, (r, w) => String(r.id) === w, setEdit);
  const [run, busy] = useAction();
  const undoDelete = useUndoDelete();
  const rules = data?.rules || [];
  const on = data ? data.on : true;
  return (
    <div className="stack">
      <Card title="Speed control" desc="Hold some devices, profiles or networks to a speed, or let some go first when the internet connection is full. Optionally only for chosen apps, categories or websites, and only at chosen hours."
        actions={<><Button small icon="plus" onClick={() => setEdit({ kind: 'limit' })}>Limit speed</Button><Button small icon="plus" onClick={() => setEdit({ kind: 'priority' })}>Give priority</Button></>}>
        <ToggleRow title="Speed control is on" desc="Turn this off to pause every entry below at once. They stay saved." checked={on}
          onChange={(v) => run(() => put('/api/speed/switch', { on: v }), v ? 'Speed control is on.' : 'Speed control is off.').then(reload)} />
        {!on && <Note tone="warn">Speed control is off, so none of the entries below are working.</Note>}
        {data && rules.some((r) => r.kind === 'priority') && !data.queue.on && <Note tone="warn">Priority works through the smart queue. Turn on Keep delay low for the connection on the Speed tab.</Note>}
        {data?.queue?.disc === 'cake+priority-up' && <Note>This device could only give priority to what you upload. Download priority needs a newer kernel feature that is missing here.</Note>}
        {data?.queue?.disc === 'fq_codel' && rules.some((r) => r.kind === 'priority') && <Note>This device's smart queue cannot tell priorities apart, so Give priority is not doing anything. Limits still work.</Note>}
        <div className="list">
          <Limited items={rules} noun="entries" render={(r) => {
            const [word, tone] = STATUS[r.status] || [r.status, ''];
            return (
              <div className={`item ${r.enabled ? '' : 'off'}`} key={r.id}>
                <div className="fill">
                  <div className="name">{r.title} <Pill tone={tone}>{word}</Pill>{r.when && r.when !== 'Always' && <Pill>{r.when}</Pill>}</div>
                  <div className="meta">{r.does}</div>
                  {r.note && <div className="meta warnline">⚠ {r.note}</div>}
                </div>
                <div className="actions">
                  <PolicyControls name={r.title} enabled={r.enabled} pausedUntil={r.paused_until} busy={busy}
                    onPause={(m) => run(() => post(`/api/speed/rule/${r.id}/pause`, { minutes: m }), m === 0 ? 'Resumed.' : 'Paused.').then(reload)}
                    onToggle={() => run(() => post(`/api/speed/rule/${r.id}/enable`, { enabled: !r.enabled }), r.enabled ? 'Turned off.' : 'Turned on.').then(reload)}
                    onEdit={() => setEdit(r)}
                    onRemove={() => undoDelete('The speed entry', () => del(`/api/speed/rule/${r.id}`), () => put('/api/speed/rule', { ...ruleBody(r), id: 0 }), reload)} />
                </div>
              </div>
            );
          }} />
          {data && !rules.length && <Empty>No speed entries yet. Add one to limit a device or give something priority.</Empty>}
        </div>
        <p className="small muted">If something goes wrong, nothing is slowed. A fault never cuts a device off.</p>
      </Card>
      {edit && <SpeedForm initial={edit} data={data} onClose={() => setEdit(null)} onSaved={reload} />}
    </div>
  );
}

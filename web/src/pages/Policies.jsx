import React, { useEffect, useState } from 'react';
import { del, post, put, useData } from '../api.js';
import { PolicyControls, whenText } from '../policykit.jsx';
import CreatePolicy from '../CreatePolicy.jsx';
import { actionWord, ruleBody } from './FirewallRules.jsx';
import { Button, Card, Empty, Field, Input, Modal, PageOrPart, Pill, Select, useAction, useUndoDelete , useDelayedDelete } from '../ui.jsx';
import { hashQuery } from '../hashtab.js';
import { SetTagsModal, TagChips, keyOf, removeSet, renameSet, rememberOff, useSets } from '../sets.jsx';

// Every policy in one list, in plain words. Firewall rules, routes, blocks and open ports are four
// kinds of the same thing: who it covers, where to, what for, when. Each kind keeps its own page for
// ordering and the finer settings; this page reads the very same records and never keeps a copy.
const KINDS = [
  { id: 'rule', name: 'Firewall rule', page: 'firewall/rules' },
  { id: 'route', name: 'Route', page: 'policies/routes' },
  { id: 'block', name: 'Block', page: 'protection/blocks' },
  { id: 'port', name: 'Open port', page: 'firewall/ports' },
  { id: 'speed', name: 'Speed control', page: 'internet/control' },
];
const kindName = (id) => KINDS.find((k) => k.id === id)?.name || id;
const labels = (chips) => (chips || []).filter((c) => c.kind !== 'any').map((c) => c.label).join(', ');

function ruleWords(r) {
  const who = labels(r.src_chips) || `everything on ${r.from}`;
  const to = labels(r.dst_chips) || (r.to === 'self' ? 'NopeWarden' : `anywhere on ${r.to}`);
  const what = r.service === 'any' ? '' : ` for ${r.service.replace(/list:/g, 'list ')}`;
  return `${actionWord(r.action)} ${who} to ${to}${what}.`;
}

function RenameSet({ st, onClose }) {
  const [name, setName] = useState(st.name);
  const [run, busy] = useAction();
  return (
    <Modal title="Rule set" onClose={onClose}
      footer={<><Button kind="danger" disabled={busy} onClick={async () => { if (await run(() => removeSet(st.id), 'Set deleted. Its policies were kept.') !== undefined) onClose(); }}>Delete set</Button><Button onClick={onClose}>Cancel</Button><Button kind="primary" disabled={busy || !name.trim()} onClick={async () => { if (await run(() => renameSet(st.id, name), 'Renamed.') !== undefined) onClose(); }}>Save</Button></>}>
      <Field label="Name" hint="Deleting a set keeps every policy in it; they just leave the set."><Input value={name} onChange={setName} maxLength={40} autoFocus /></Field>
    </Modal>
  );
}

export default function Policies({ part }) {
  const delayedDelete = useDelayedDelete();
  const { data: rules, reload: rr } = useData('/api/fw/rules', { every: 20000 });
  const { data: routes, reload: rt } = useData('/api/routes', { topics: ['vpn.status', 'net.status'], every: 20000 });
  const { data: blocks, reload: rb } = useData('/api/blocks', { every: 20000 });
  const { data: ports, reload: rp } = useData('/api/fw/appforwards', { every: 20000 });
  const { data: speed, reload: rsp } = useData('/api/speed', { every: 20000 });
  const [creating, setCreating] = useState(false);
  const [kind, setKind] = useState('');
  const [run, busy] = useAction();
  const undoDelete = useUndoDelete();
  const again = () => { rr(); rt(); rb(); rp(); rsp(); };
  const loaded = rules && routes && blocks && ports && speed;
  const edit = (k, id) => { window.location.hash = `#/${KINDS.find((x) => x.id === k).page}?edit=${encodeURIComponent(id)}`; };
  const routeBody = (r) => ({ id: r.id, name: r.name, enabled: r.enabled, target_kind: r.target_kind, target: r.target, mode: r.mode, kill: r.kill, who: r.who, where: r.where, service: r.service, sched: r.sched, paused_until: r.paused_until });

  const items = [
    ...(Array.isArray(rules) ? rules : rules?.rules || []).filter((r) => !r.countries && !r.matrix).map((r) => ({
      k: 'rule', key: `rule${r.id}`, id: r.id, name: r.name, words: ruleWords(r), when: r.when !== 'Always' ? r.when : '', enabled: r.enabled, paused: r.paused_until,
      warn: r.warnings || [], tone: r.action === 'accept' ? 'good' : 'bad', state: actionWord(r.action),
      pause: (m) => run(() => post(`/api/fw/rules/${r.id}/pause`, { minutes: m }), m === 0 ? 'Resumed.' : 'Paused.').then(again),
      toggle: () => run(() => put('/api/fw/rules', ruleBody({ ...r, sched: r.sched || null, enabled: !r.enabled })), r.enabled ? 'Turned off.' : 'Turned on.').then(again),
      remove: () => undoDelete(`The rule "${r.name}"`, () => del(`/api/fw/rules/${r.id}`), () => put('/api/fw/rules', ruleBody({ ...r, id: undefined })), again),
    })),
    ...(routes?.routes || []).map((r) => ({
      k: 'route', key: `route${r.id}`, id: r.id, name: r.name, words: r.plain, when: '', enabled: r.enabled, paused: r.paused_until, warn: r.warnings || [],
      tone: r.state === 'working' ? 'good' : r.state === 'off' ? '' : 'warn', state: r.state === 'working' ? 'Working' : r.state === 'off' ? 'Off' : 'Needs a look',
      pause: (m) => run(() => post(`/api/routes/${r.id}/pause`, { minutes: m }), m === 0 ? 'Resumed.' : 'Paused.').then(again),
      toggle: () => run(() => put('/api/routes', { ...routeBody(r), enabled: !r.enabled }), r.enabled ? 'Turned off.' : 'Turned on.').then(again),
      remove: () => undoDelete(`The route "${r.name}"`, () => del(`/api/routes/${r.id}`), () => put('/api/routes', { ...routeBody(r), id: undefined }), again),
    })),
    ...(blocks?.blocks || []).map((r) => ({
      k: 'block', key: `block${r.id}`, id: r.id, name: r.title, words: [r.what, r.where, r.when && r.when !== 'Always' ? r.when : ''].filter(Boolean).join(' · '), when: '', enabled: r.enabled, paused: r.paused_until, warn: [],
      tone: r.status === 'blocking' || r.status === 'allowing' ? 'good' : '', state: r.status,
      pause: (m) => run(() => post(`/api/blocks/${r.id}/pause`, { minutes: m }), m === 0 ? 'Resumed.' : 'Paused.').then(again),
      toggle: () => run(() => post(`/api/blocks/${r.id}/enable`, { enabled: !r.enabled }), r.enabled ? 'Turned off.' : 'Turned on.').then(again),
      remove: () => undoDelete('The block', () => del(`/api/blocks/${r.id}`), () => put('/api/blocks', { name: r.name, items: r.items.map(({ kind, value }) => ({ kind, value })), targets: r.targets.map(({ scope, target }) => ({ scope, target })), allow: r.allow, method: r.method, schedule: r.schedule, enabled: r.enabled }), again),
    })),
    ...(speed?.rules || []).map((r) => ({
      k: 'speed', key: `speed${r.id}`, id: r.id, name: r.title, words: [r.does, r.when && r.when !== 'Always' ? r.when : ''].filter(Boolean).join(' · '), when: '', enabled: r.enabled, paused: r.paused_until, warn: r.note ? [r.note] : [],
      tone: r.status === 'limiting' || r.status === 'prioritising' ? 'good' : r.status === 'waiting' ? 'warn' : '', state: { limiting: 'Limiting', prioritising: 'Giving priority', waiting: 'Waiting', paused: 'Paused', off: 'Off', 'outside hours': 'Not in its hours' }[r.status] || r.status,
      pause: (m) => run(() => post(`/api/speed/rule/${r.id}/pause`, { minutes: m }), m === 0 ? 'Resumed.' : 'Paused.').then(again),
      toggle: () => run(() => post(`/api/speed/rule/${r.id}/enable`, { enabled: !r.enabled }), r.enabled ? 'Turned off.' : 'Turned on.').then(again),
      remove: () => undoDelete('The speed entry', () => del(`/api/speed/rule/${r.id}`), () => put('/api/speed/rule', { name: r.name, kind: r.kind, prio: r.prio, down_mbps: r.down_mbps, up_mbps: r.up_mbps, each: r.each, schedule: r.schedule, enabled: r.enabled, items: r.items.map(({ kind, value }) => ({ kind, value })), targets: r.targets.map(({ scope, target }) => ({ scope, target })) }), again),
    })),
    ...(ports?.forwards || []).map((g) => ({
      k: 'port', key: `port${g.group}${g.id || ''}`, id: g.legacy ? g.id : g.group, name: g.text, words: g.countries?.length ? `Only from ${g.countries.join(', ')}.` : g.src?.length ? `Only from ${g.src.join(', ')}.` : 'Anyone on the internet can try to connect.',
      when: '', enabled: g.enabled, paused: g.paused_until, warn: g.blocked ? [g.blocked] : [], tone: g.enabled ? 'good' : '', state: g.enabled ? 'Open' : 'Off',
      pause: g.legacy ? undefined : (m) => run(() => post('/api/fw/appforwards/pause', { group: g.group, minutes: m }), m === 0 ? 'Resumed.' : 'Paused.').then(again),
      toggle: g.legacy ? undefined : () => run(() => post('/api/fw/appforwards/enable', { group: g.group, enabled: !g.enabled }), g.enabled ? 'Turned off.' : 'Turned on.').then(again),
      remove: () => delayedDelete(`The port forward "${g.group}"`, () => (g.legacy ? del(`/api/fw/forwards/${g.id}`) : del(`/api/fw/appforwards/${encodeURIComponent(g.group)}`)), again),
    })),
  ];
  const [setF, setSetF] = useState(() => hashQuery('set'));
  const [tagF, setTagF] = useState(() => hashQuery('tag'));
  const [q, setQ] = useState('');
  useEffect(() => { const on = () => { setTagF(hashQuery('tag')); setSetF(hashQuery('set')); }; window.addEventListener('hashchange', on); return () => window.removeEventListener('hashchange', on); }, []);
  const [editing, setEditing] = useState(null);
  const [renaming, setRenaming] = useState(null);
  const sets = useSets();
  items.forEach((x) => { x.ref = String(x.id); x.tags = sets.tagsOf(x.k, x.ref); x.set = sets.setOf(x.k, x.ref); });
  const words = q.toLowerCase().split(/\s+/).filter(Boolean);
  const matches = (x) => (!kind || x.k === kind) && (!tagF || x.tags.includes(tagF)) && (!setF || (setF === 'none' ? !x.set : String(x.set?.id) === setF))
    && words.every((w) => `${x.name} ${x.words} ${kindName(x.k)} ${x.tags.join(' ')} ${x.set?.name || ''}`.toLowerCase().includes(w));
  const shown = items.filter(matches);
  const filtering = !!(kind || tagF || setF || words.length);
  const loose = shown.filter((x) => !x.set);
  const setRows = sets.sets.map((st) => ({ st, rows: items.filter((x) => x.set?.id === st.id), shown: shown.filter((x) => x.set?.id === st.id) })).filter((r) => !filtering || r.shown.length);
  const live = (x) => x.enabled;
  // Whole-set actions run the very same pause and turn on or off each policy has on its own.
  const pauseSet = async (rows, m) => { for (const x of rows) if (x.pause && (m === 0 ? whenText(x.paused) : live(x))) await x.pause(m); };
  const offSet = async (st, rows) => {
    const on = rows.filter((x) => live(x) && x.toggle);
    await rememberOff(st.id, on.map((x) => keyOf(x.k, x.ref)));
    for (const x of on) await x.toggle();
    sets.reload();
  };
  const onSet = async (st, rows) => {
    const was = new Set(st.off_refs || []);
    const off = rows.filter((x) => !live(x) && x.toggle);
    for (const x of off.filter((y) => !was.size || was.has(keyOf(y.k, y.ref)))) await x.toggle();
    await rememberOff(st.id, []);
    sets.reload();
  };
  const row = (x) => (
    <div className={`item ${x.enabled ? '' : 'off'}`} key={x.key}>
      <div className="fill">
        <div className="name">{x.name} <Pill>{kindName(x.k)}</Pill>{x.state && <Pill tone={x.tone}>{x.state}</Pill>}{x.when && <Pill tone="warn">{x.when}</Pill>}<TagChips tags={x.tags} onPick={setTagF} /></div>
        {x.words && <div className="meta">{x.words}</div>}
        {whenText(x.paused) && <div className="meta">{whenText(x.paused)}</div>}
        {x.warn.map((w, i) => <div className="meta warnline" key={i}>⚠ {w}</div>)}
      </div>
      <div className="actions">
        <Button small onClick={() => setEditing(x)}>Set & tags</Button>
        <PolicyControls name={x.name} enabled={x.enabled} pausedUntil={x.paused} busy={busy} onPause={x.pause} onToggle={x.toggle} onEdit={() => edit(x.k, x.id)} onRemove={x.remove} />
      </div>
    </div>
  );
  const setState = (rows) => {
    const n = rows.filter(live).length;
    const paused = rows.some((x) => live(x) && whenText(x.paused));
    if (!rows.length) return ['Empty', ''];
    if (paused) return ['Paused', 'warn'];
    return n === rows.length ? ['All on', 'good'] : n === 0 ? ['All off', ''] : ['Part on', 'warn'];
  };
  return (
    <PageOrPart part={part} title="Policies" sub="Everything that allows, blocks, routes, opens or speeds something for your devices, in one list and in plain words."
      actions={<Button kind="primary" small icon="plus" onClick={() => setCreating(true)}>Create policy</Button>}>
      <Card title="Your policies" desc="Firewall rules, routes, blocks, open ports and speed control. Ordering and the finer settings stay on the Firewall and Protection pages and the Routes tab. Put policies in a set to pause, turn off or turn on a group at once.">
        <div className="filterbar">
          <Select value={kind} onChange={setKind} aria-label="Show" options={[{ value: '', label: 'All kinds' }, ...KINDS.map((k) => ({ value: k.id, label: `${k.name}s` }))]} />
          <Select value={setF} onChange={setSetF} aria-label="Set" options={[{ value: '', label: 'All sets' }, { value: 'none', label: 'Not in a set' }, ...sets.sets.map((s) => ({ value: String(s.id), label: s.name }))]} />
          <Select value={tagF} onChange={setTagF} aria-label="Tag" options={[{ value: '', label: 'Any tag' }, ...sets.allTags.map((t) => ({ value: t.tag, label: `${t.tag} (${t.count})` }))]} />
          <Input value={q} onChange={setQ} placeholder="Search policies" aria-label="Search policies" style={{ maxWidth: '14rem' }} />
          <span className="small muted">{loaded ? `${shown.length} ${shown.length === 1 ? 'policy' : 'policies'}${sets.sets.length ? ` · ${sets.sets.length} ${sets.sets.length === 1 ? 'set' : 'sets'}` : ''}` : 'Loading…'}</span>
          {filtering && <button type="button" className="link small" onClick={() => { setKind(''); setSetF(''); setTagF(''); setQ(''); }}>Clear filters</button>}
        </div>
        {setRows.map(({ st, rows, shown: sh }) => {
          const [label, tone] = setState(rows);
          const onN = rows.filter(live).length;
          const anyOn = onN > 0;
          const anyOff = rows.some((x) => !live(x) && x.toggle);
          const pausedNow = rows.some((x) => live(x) && x.pause && whenText(x.paused));
          return (
            <details className="set" key={st.id} open={filtering ? true : undefined}>
              <summary>
                <span className="sn">{st.name}</span>
                <span className="sc">{rows.length} {rows.length === 1 ? 'policy' : 'policies'} · {onN === rows.length ? `${onN} on` : `${onN} on, ${rows.length - onN} off`}</span>
                <Pill tone={tone}>{label}</Pill>
                <span className="sp" onClick={(e) => e.preventDefault()}>
                  {rows.some((x) => x.pause) && (anyOn || pausedNow) && (pausedNow
                    ? <Button small kind="primary" disabled={busy} onClick={() => pauseSet(rows, 0)}>Resume now</Button>
                    : <Select value="" className="pausesel" onChange={(v) => v && pauseSet(rows, Number(v))} aria-label={`Pause ${st.name}`} options={[{ value: '', label: 'Pause…' }, { value: '15', label: '15 minutes' }, { value: '60', label: '1 hour' }, { value: '240', label: '4 hours' }, { value: '1440', label: '1 day' }]} />)}
                  {anyOn && <Button small disabled={busy} onClick={() => offSet(st, rows)}>Turn off</Button>}
                  {anyOff && <Button small disabled={busy} onClick={() => onSet(st, rows)}>Turn on</Button>}
                  <Button small onClick={() => setRenaming(st)}>Rename</Button>
                </span>
              </summary>
              <div className="list">
                {sh.map(row)}
                {!rows.length && <Empty>No policies in this set yet. Use Set & tags on a policy to add it.</Empty>}
              </div>
            </details>
          );
        })}
        {setRows.length > 0 && loose.length > 0 && <div className="small muted" style={{ margin: 'var(--space-3) 0 var(--space-1)' }}>Not in a set · {loose.length} {loose.length === 1 ? 'policy' : 'policies'}</div>}
        <div className="list">
          {loose.map(row)}
          {loaded && !shown.length && <Empty>{filtering ? 'Nothing matches these filters.' : 'No policies yet. Create one to allow, block, route or open something.'}</Empty>}
        </div>
      </Card>
      {editing && <SetTagsModal kind={editing.k} refId={editing.ref} name={editing.name} sets={sets} onClose={() => setEditing(null)} />}
      {renaming && <RenameSet st={renaming} onClose={() => { setRenaming(null); sets.reload(); }} />}
      {!part && <p className="small muted">Policies pick ports by name. Make and manage those names under <a href="#/firewall/services">Firewall, Services</a>.</p>}
      {creating && <CreatePolicy onClose={() => setCreating(false)} />}
    </PageOrPart>
  );
}

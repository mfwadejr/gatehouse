import React, { useMemo, useState } from 'react';
import { del, post, put, useData } from '../api.js';
import { AddrPicker, Chip, NewDevice, ScheduleEditor, ServicePicker, tokens } from '../AddrPicker.jsx';
import { Button, Card, Empty, Field, Input, Modal, NumberInput, Note, Pill, Segmented, Select, ToggleRow, useAction, fmt } from '../ui.jsx';

const ACTIONS = [{ value: 'accept', label: 'Allow' }, { value: 'drop', label: 'Block silently' }, { value: 'reject', label: 'Block and tell the sender' }];
// Only the fields the server stores; it refuses anything else.
const FIELDS = ['id', 'name', 'from', 'to', 'src', 'dst', 'service', 'action', 'limit', 'log', 'enabled', 'ipver', 'ctstate', 'sched', 'descr', 'also_mac'];
const ruleBody = (r) => Object.fromEntries(FIELDS.filter((k) => r[k] !== undefined).map((k) => [k, r[k]]));
const actionWord = (a) => (a === 'accept' ? 'Allow' : a === 'drop' ? 'Block' : 'Reject');

// ---------------------------------------------------------------- the rule form

export function RuleForm({ initial, zoneNames, pickers, reloadPickers, onClose, onSaved }) {
  const [r, setR] = useState({
    name: '', from: zoneNames[0] || '', to: zoneNames[1] || '', src: '', dst: '', service: '', action: 'accept', limit: 0, log: false, enabled: true,
    ipver: '', ctstate: '', sched: null, descr: '', also_mac: false, ...initial,
  });
  const { data: geo } = useData('/api/fw/geo');
  const [run, busy] = useAction();
  const set = (k) => (v) => setR((x) => ({ ...x, [k]: v }));
  const local = r.from !== 'Internet';
  const hasDevice = tokens(r.src).some((t) => t.startsWith('dev:') || t.startsWith('grp:'));
  const hasCountry = tokens(r.dst).some((t) => t.startsWith('geo:'));
  const toOptions = [...zoneNames.map((z) => ({ value: z, label: z })), { value: 'self', label: 'Gatehouse itself' }];
  const save = async () => {
    const body = ruleBody({ ...r, service: r.service || 'any', also_mac: r.also_mac && hasDevice && local });
    if (await run(() => put('/api/fw/rules', body), 'Saved. Apply it to start using it.')) { onSaved(); onClose(); }
  };
  return (
    <Modal title={r.id ? 'Edit rule' : 'Add a rule'} onClose={onClose}
      footer={<><Button onClick={onClose}>Cancel</Button><Button kind="primary" disabled={busy || !r.name} onClick={save}>Save</Button></>}>
      <p className="sec small">Rules are checked before the network types, top to bottom. The first match wins.</p>
      <Field label="Name"><Input value={r.name} onChange={set('name')} placeholder="Kids offline at night" /></Field>
      <div className="formgrid">
        <Field label="From network"><Select value={r.from} onChange={set('from')} options={zoneNames} /></Field>
        <Field label="To"><Select value={r.to} onChange={set('to')} options={toOptions} /></Field>
      </div>
      <Field label="Which devices"><AddrPicker side="src" value={r.src} onChange={set('src')} pickers={pickers} local={local} reload={reloadPickers} /></Field>
      <Field label="Going where" hint={r.to === 'self' ? 'Gatehouse itself.' : 'Leave empty for anywhere on that network.'}>
        {r.to === 'self' ? <span className="muted small">Services on Gatehouse itself</span> : <AddrPicker side="dst" value={r.dst} onChange={set('dst')} pickers={pickers} local={local} reload={reloadPickers} />}
      </Field>
      {hasCountry && (
        <Note tone={geo?.ready ? '' : 'warn'} title="Countries">
          {geo?.ready ? `Country lists from the regional internet registries, updated ${fmt.ago(geo.updated)}. Refreshed weekly.` : 'The country lists are downloaded the first time they are needed. Saving starts the download; applying works once it is done.'}
          {' '}Addresses on your own networks and your VPN connections are never matched.
        </Note>
      )}
      {tokens(r.dst).some((t) => t.startsWith('dom:')) && <p className="small muted">Websites are matched by the addresses devices were given when they looked them up through Gatehouse, so this works for devices that use Gatehouse for DNS.</p>}
      <Field label="What for"><ServicePicker value={r.service} onChange={set('service')} pickers={pickers} /></Field>
      <Field label="Then"><Select value={r.action} onChange={set('action')} options={ACTIONS} /></Field>
      <Field label="When"><ScheduleEditor value={r.sched} onChange={set('sched')} /></Field>
      <Field label="Note" hint="Optional. Why the rule exists, for whoever looks at it next."><Input value={r.descr} onChange={set('descr')} maxLength={300} /></Field>
      <details className="more">
        <summary>More options</summary>
        <div className="stack">
          <Field label="Internet version"><Segmented value={r.ipver || 'both'} onChange={(v) => set('ipver')(v === 'both' ? '' : v)} options={[{ value: 'both', label: 'Both' }, { value: '4', label: 'IPv4 only' }, { value: '6', label: 'IPv6 only' }]} /></Field>
          <Field label="Advanced: which connections" hint="Most rules want any. Choose new connections to leave connections that are already open alone.">
            <Select value={r.ctstate} onChange={set('ctstate')} options={[{ value: '', label: 'Any' }, { value: 'new', label: 'Only new connections' }]} />
          </Field>
          {hasDevice && local && <ToggleRow title="Also require the device's hardware address" desc="Stops another device from borrowing this one's address. Works for devices on your own networks." checked={r.also_mac} onChange={set('also_mac')} />}
          <Field label="Limit new connections per second" hint="0 for no limit"><NumberInput value={r.limit} onChange={set('limit')} /></Field>
          <ToggleRow title="Keep a log of matches" checked={r.log} onChange={set('log')} />
          <ToggleRow title="Turned on" checked={r.enabled} onChange={set('enabled')} />
        </div>
      </details>
    </Modal>
  );
}

// ---------------------------------------------------------------- the matrix

// The state of one pair of networks, taking the matrix's own rules into account.
function pairState(a, b, ov, rules) {
  const open = (x, y) => {
    const mine = (rules || []).find((r) => r.matrix && r.from === x && r.to === y && r.enabled);
    if (mine) return mine.action === 'accept';
    return !!ov?.pair_open?.[`${x}|${y}`];
  };
  const mine = (rules || []).find((r) => r.matrix && r.from === a && r.to === b && r.enabled);
  const exceptions = (rules || []).filter((r) => !r.matrix && r.from === a && r.to === b);
  let state = open(a, b) ? 'open' : 'blocked';
  const wan = (n) => (ov?.matrix_zones || []).find((z) => z.name === n)?.kind === 'wan';
  if (state === 'blocked' && !wan(a) && !wan(b) && open(b, a)) state = 'replies';
  return { state, changed: !!mine, exceptions };
}

const STATE_TEXT = { open: 'Open', blocked: 'Blocked', replies: 'Replies only' };

export function Matrix({ ov, rules, onPick }) {
  const zones = ov?.matrix_zones || [];
  if (zones.length < 2) return null;
  return (
    <Card title="Who can reach whom" desc="Traffic starting in the network on the left, going to the network along the top. Select a box to change it or to see its rules.">
      <div className="scroll">
        <div className="matrix compact" role="grid" style={{ gridTemplateColumns: `minmax(4.5rem, auto) repeat(${zones.length}, minmax(5.5rem, 1fr))` }}>
          <div className="cell hd" role="columnheader">From ↓ To →</div>
          {zones.map((z) => <div className="cell hd" role="columnheader" key={z.name} title={z.name}>{z.name}</div>)}
          {zones.map((a) => (
            <React.Fragment key={a.name}>
              <div className="cell hd" role="rowheader" title={a.name}>{a.name}</div>
              {zones.map((b) => {
                if (a.name === b.name) {
                  const open = !!ov?.pair_open?.[`${a.name}|${b.name}`];
                  return <div key={b.name} className={`cell self ${open ? 'allow' : ''}`} role="gridcell">{a.kind === 'wan' ? '–' : open ? 'Open' : 'Blocked'}</div>;
                }
                const p = pairState(a.name, b.name, ov, rules);
                const n = p.exceptions.length;
                return (
                  <button type="button" key={b.name} role="gridcell" className={`cell pair ${p.state === 'open' ? 'allow' : p.state === 'replies' ? 'lim' : ''} ${p.changed ? 'changed' : ''}`}
                    onClick={() => onPick(a.name, b.name, p)} aria-label={`${a.name} to ${b.name}: ${STATE_TEXT[p.state]}${n ? `, ${n} exceptions` : ''}${p.changed ? ', set from here' : ''}`}>
                    {STATE_TEXT[p.state]}{n > 0 && <span className="cnt"> ({n})</span>}{p.changed && <span className="dotmark" aria-hidden="true" title="Set from this matrix" />}
                  </button>
                );
              })}
            </React.Fragment>
          ))}
        </div>
      </div>
      <p className="small muted" style={{ marginTop: 'var(--space-2)' }}>Open: new connections are allowed. Blocked: they are not. Replies only: this side cannot start a connection, but can answer one the other side started. A number in brackets counts the rules for that pair.</p>
    </Card>
  );
}

export function PairModal({ pair, ov, onClose, onChanged, onShowRules, onAddRule }) {
  const { from, to, p } = pair;
  const [mode, setMode] = useState(null);
  const [run, busy] = useAction();
  const wan = (n) => (ov?.matrix_zones || []).find((z) => z.name === n)?.kind === 'wan';
  const local = !wan(from) && !wan(to);
  const choices = [
    { mode: 'allow', label: 'Allow all', disabled: wan(from), text: `Devices on ${from} may start connections to anything on ${to}.` },
    { mode: 'block', label: 'Block all', text: `Devices on ${from} can no longer start connections to ${to}.` },
    { mode: 'replies', label: 'Replies only', disabled: !local, text: `${from} can no longer start connections to ${to}, but can still answer ones ${to} starts. ${to} is allowed to start connections to ${from} if it is not already.` },
  ];
  const apply = async (m) => { if (await run(() => post('/api/fw/matrix', { from, to, mode: m }), 'Changed. Apply the firewall to put it into effect.')) { onChanged(); onClose(); } };
  const picked = choices.find((c) => c.mode === mode);
  return (
    <Modal title={`${from} → ${to}`} onClose={onClose}
      footer={picked ? <><Button onClick={() => setMode(null)}>Back</Button><Button kind="primary" disabled={busy} onClick={() => apply(mode)}>{picked.label}</Button></> : <Button onClick={onClose}>Close</Button>}>
      {picked ? (
        <>
          <p>{picked.text}</p>
          <p className="small muted">Rules you wrote by hand are kept and still apply first. The rules that keep you able to manage Gatehouse stay above everything. Nothing changes until you apply the firewall.</p>
        </>
      ) : (
        <>
          <p>Right now: <strong>{STATE_TEXT[p.state]}</strong>{p.changed ? ' (set from the matrix)' : ' (from the network types)'}.</p>
          <div className="actions" style={{ flexWrap: 'wrap' }}>
            {choices.filter((c) => !c.disabled && !(c.mode === 'allow' && p.state === 'open' && p.changed) ).map((c) => <Button key={c.mode} onClick={() => setMode(c.mode)}>{c.label}</Button>)}
            {p.changed && <Button onClick={() => apply('default')} disabled={busy}>Back to the default</Button>}
          </div>
          <div className="spread" style={{ marginTop: 'var(--space-3)' }}>
            <span>{p.exceptions.length ? `${p.exceptions.length} rule${p.exceptions.length > 1 ? 's' : ''} for this pair` : 'No rules for this pair'}</span>
            <div className="inline">
              {p.exceptions.length > 0 && <Button small onClick={() => { onShowRules(from, to); onClose(); }}>Show them</Button>}
              <Button small icon="plus" onClick={() => { onAddRule(from, to); onClose(); }}>Add a rule from {from} to {to}</Button>
            </div>
          </div>
        </>
      )}
    </Modal>
  );
}

// ---------------------------------------------------------------- the rule list

function RuleSide({ chips, pickers }) {
  return (
    <span className="chips">
      {chips.map((c, i) => (
        <span key={i} className={`chip ${c.kind === 'any' ? 'any' : ''} ${c.warn ? 'warn' : ''}`} title={c.detail || ''}>
          <span aria-hidden="true">{{ device: '🖥', group: '👥', domain: '🌐', country: '🏳', mac: '🔖', address: '#', any: '' }[c.kind]}</span> {c.kind === 'country' ? `${c.label}` : c.label}
          {c.detail && c.kind !== 'any' && c.kind !== 'address' ? <span className="muted small"> {c.detail}</span> : null}
        </span>
      ))}
    </span>
  );
}

export function RulesList({ ov, rules, pickers, reloadPickers, reload, filter, setFilter, startNew }) {
  const [edit, setEdit] = useState(null);
  const [q, setQ] = useState('');
  const [act, setAct] = useState('');
  const [showSys, setShowSys] = useState(false);
  const [run] = useAction();
  const { data: prot } = useData('/api/fw/protected');
  const names = (ov?.matrix_zones || []).map((z) => z.name);
  const fromNames = names;
  const needle = q.trim().toLowerCase();
  const text = (r) => [r.name, r.descr, r.from, r.to, r.src, r.dst, r.service, r.when, ...(r.src_chips || []).map((c) => c.label), ...(r.dst_chips || []).map((c) => c.label)].join(' ').toLowerCase();
  const visible = (rules || []).map((r, i) => ({ r, i })).filter(({ r }) =>
    (!filter?.from || r.from === filter.from) && (!filter?.to || r.to === filter.to) && (!act || r.action === act) && (!needle || text(r).includes(needle)));
  const filtered = !!(filter?.from || filter?.to || act || needle);
  const move = (pos, d) => {
    const j = pos + d;
    if (j < 0 || j >= visible.length) return;
    const ids = (rules || []).map((r) => r.id);
    const a = visible[pos].i, b = visible[j].i;
    [ids[a], ids[b]] = [ids[b], ids[a]];
    run(() => post('/api/fw/rules/order', { order: ids })).then(reload);
  };
  const toggle = (r) => run(() => put('/api/fw/rules', ruleBody({ ...r, sched: r.sched || null, enabled: !r.enabled })), 'Saved. Apply it to start using it.').then(reload);
  return (
    <Card title="Rules" desc="Special cases on top of your network types, such as keeping the kids' devices offline at night."
      actions={<Button small icon="plus" disabled={names.length < 1} onClick={() => startNew({})}>Add rule</Button>}>
      <div className="toolbar">
        <Input value={q} onChange={setQ} placeholder="Search rules, devices, ports" aria-label="Search rules" />
        <Select value={filter?.from || ''} onChange={(v) => setFilter({ ...filter, from: v })} aria-label="From network" options={[{ value: '', label: 'From any network' }, ...fromNames]} />
        <Select value={filter?.to || ''} onChange={(v) => setFilter({ ...filter, to: v })} aria-label="To network" options={[{ value: '', label: 'To any network' }, ...names, { value: 'self', label: 'Gatehouse itself' }]} />
        <Select value={act} onChange={setAct} aria-label="Action" options={[{ value: '', label: 'Allow or block' }, { value: 'accept', label: 'Allow' }, { value: 'drop', label: 'Block' }, { value: 'reject', label: 'Reject' }]} />
        {filtered && <Button small onClick={() => { setQ(''); setAct(''); setFilter({}); }}>Clear</Button>}
      </div>
      <label className="inline small" style={{ marginBottom: 'var(--space-2)' }}><input type="checkbox" checked={showSys} onChange={(e) => setShowSys(e.target.checked)} /> Show system rules</label>
      {showSys && (
        <div className="list sysrules">
          {(prot?.rules || []).map((r) => (
            <div className="item" key={r.title}><div><div className="name">🔒 {r.title}</div><div className="meta">{r.text}</div></div><Pill tone={r.active ? 'good' : ''}>{r.active ? 'Always on' : 'Standing by'}</Pill></div>
          ))}
          <p className="small muted">Built in, always above your rules, and cannot be changed or removed.</p>
        </div>
      )}
      <div className="list">
        {visible.map(({ r }, pos) => (
          <div className={`item rule ${r.enabled ? '' : 'off'}`} key={r.id}>
            <div className="rulemain">
              <div className="name">{r.matrix ? `Matrix: ${r.from} to ${r.to}` : r.name} {!r.enabled && <Pill>Off</Pill>}{r.matrix && <Pill>From the matrix</Pill>}<Pill tone={r.action === 'accept' ? 'good' : 'bad'}>{actionWord(r.action)}</Pill>{r.when !== 'Always' && <Pill tone="warn">{r.when}</Pill>}</div>
              <div className="meta rulewho">
                <RuleSide chips={r.src_chips || []} pickers={pickers} />
                <span aria-label="goes to">→</span>
                <RuleSide chips={r.dst_chips || []} pickers={pickers} />
                <span className="muted">· {r.from} → {r.to === 'self' ? 'Gatehouse' : r.to} · {r.service === 'any' ? 'everything' : r.service.replace(/list:/g, 'list ')}{r.ipver ? ` · IPv${r.ipver} only` : ''}{r.ctstate === 'new' ? ' · new connections' : ''}{r.limit ? ` · max ${r.limit}/s` : ''}</span>
              </div>
              {r.descr && <div className="meta">{r.descr}</div>}
              {(r.warnings || []).map((w, i) => <div className="meta warnline" key={i}>⚠ {w}</div>)}
              <div className="meta hits">{r.hits > 0 ? `${fmt.n(r.hits)} matches · last ${fmt.ago(r.last_hit)}` : 'No matches yet'}</div>
            </div>
            <div className="actions">
              <Button small aria-label="Move up" onClick={() => move(pos, -1)} disabled={pos === 0}>↑</Button>
              <Button small aria-label="Move down" onClick={() => move(pos, 1)} disabled={pos === visible.length - 1}>↓</Button>
              <Button small onClick={() => toggle(r)}>{r.enabled ? 'Turn off' : 'Turn on'}</Button>
              <Button small icon="edit" onClick={() => setEdit(r)}>Edit</Button>
              <Button small kind="danger" icon="trash" onClick={() => window.confirm(`Delete "${r.name}"?`) && run(() => del(`/api/fw/rules/${r.id}`)).then(reload)}>Delete</Button>
            </div>
          </div>
        ))}
        {!visible.length && <Empty>{filtered ? 'No rules match.' : 'No rules. Network types are all you need for most setups.'}</Empty>}
      </div>
      {edit && <RuleForm initial={{ ...edit, service: edit.service === 'any' ? '' : edit.service, src: edit.src === 'any' ? '' : edit.src, dst: edit.dst === 'any' ? '' : edit.dst }} zoneNames={names} pickers={pickers} reloadPickers={reloadPickers} onClose={() => setEdit(null)} onSaved={reload} />}
    </Card>
  );
}

// ---------------------------------------------------------------- groups and port lists

function GroupForm({ initial, pickers, reloadPickers, onClose, onSaved }) {
  const [g, setG] = useState({ name: '', description: '', reserved_only: false, members: [], ...initial });
  const [q, setQ] = useState('');
  const [adding, setAdding] = useState(false);
  const [run, busy] = useAction();
  const devs = (pickers?.devices || []).filter((d) => !q || `${d.name} ${d.ip} ${d.mac}`.toLowerCase().includes(q.toLowerCase()));
  const tog = (mac) => setG((x) => ({ ...x, members: x.members.includes(mac) ? x.members.filter((m) => m !== mac) : [...x.members, mac] }));
  return (
    <Modal title={g.id ? `Edit ${initial.name}` : 'New device group'} onClose={onClose}
      footer={<><Button onClick={onClose}>Cancel</Button><Button kind="primary" disabled={busy || !g.name} onClick={async () => { if (await run(() => put('/api/fw/groups', g), 'Saved.')) { onSaved(); onClose(); } }}>Save</Button></>}>
      <Field label="Name"><Input value={g.name} onChange={(v) => setG({ ...g, name: v })} placeholder="Kids' devices" /></Field>
      <ToggleRow title="Reserved addresses only" desc="A device counts only at the address you reserved for it on the DHCP page. Devices without a reservation are left out, so a stranger who borrows a free address is not covered." checked={g.reserved_only} onChange={(v) => setG({ ...g, reserved_only: v })} />
      <Field label={`Devices (${g.members.length})`}>
        <Input value={q} onChange={setQ} placeholder="Search devices" aria-label="Search devices" />
        <div className="stack" style={{ gap: 'var(--space-1)', maxHeight: '14rem', overflow: 'auto', marginTop: 'var(--space-2)' }}>
          {devs.map((d) => (
            <label key={d.mac} className="inline"><input type="checkbox" checked={g.members.includes(d.mac)} onChange={() => tog(d.mac)} /> <span className={`dot ${d.online ? '' : 'off'}`} /> <span className="name">{d.name}</span> <span className="small muted mono">{d.ip || 'no address yet'}</span></label>
          ))}
          {!devs.length && <span className="muted small">No matching devices.</span>}
        </div>
        <div style={{ marginTop: 'var(--space-2)' }}><Button small icon="plus" onClick={() => setAdding(true)}>Add a device that has not connected yet</Button></div>
      </Field>
      {adding && <NewDevice onClose={() => setAdding(false)} onCreated={(mac) => { reloadPickers(); setG((x) => ({ ...x, members: [...x.members, mac] })); }} />}
    </Modal>
  );
}

function PortListForm({ initial, onClose, onSaved }) {
  const [p, setP] = useState({ name: '', ports: [], ...initial });
  const [text, setText] = useState((initial?.ports || []).join('\n'));
  const [run, busy] = useAction();
  const body = { ...p, ports: text.split(/[\s,]+/).filter(Boolean) };
  return (
    <Modal title={p.id ? `Edit ${initial.name}` : 'New list of ports'} onClose={onClose}
      footer={<><Button onClick={onClose}>Cancel</Button><Button kind="primary" disabled={busy || !p.name || !body.ports.length} onClick={async () => { if (await run(() => put('/api/fw/portlists', body), 'Saved.')) { onSaved(); onClose(); } }}>Save</Button></>}>
      <Field label="Name"><Input value={p.name} onChange={(v) => setP({ ...p, name: v })} placeholder="Game consoles" /></Field>
      <Field label="Ports" hint="One per line. A service name like https, or tcp/8080, or udp/27000-27050.">
        <textarea className="in mono" rows={6} value={text} onChange={(e) => setText(e.target.value)} spellCheck={false} placeholder={'tcp/3074\nudp/3074\nudp/27000-27050'} />
      </Field>
    </Modal>
  );
}

export function GroupsAndLists({ pickers, reloadPickers, reload }) {
  const [eg, setEg] = useState(null);
  const [ep, setEp] = useState(null);
  const [run] = useAction();
  const deviceName = (mac) => pickers?.devices?.find((d) => d.mac === mac)?.name || mac;
  const refresh = () => { reloadPickers(); reload(); };
  return (
    <>
      <Card title="Device groups" desc="Put devices in a group once, then use the group in any rule. New members are covered right away." actions={<Button small icon="plus" onClick={() => setEg({})}>New group</Button>}>
        <div className="list">
          {(pickers?.groups || []).map((g) => (
            <div className="item" key={g.id}>
              <div><div className="name">👥 {g.name} {g.reserved_only && <Pill>Reserved addresses only</Pill>}</div>
                <div className="meta">{g.members.length ? g.members.slice(0, 6).map(deviceName).join(', ') + (g.members.length > 6 ? ` and ${g.members.length - 6} more` : '') : 'No devices yet'}</div></div>
              <div className="actions"><Button small icon="edit" onClick={() => setEg(g)}>Edit</Button>
                <Button small kind="danger" icon="trash" onClick={() => window.confirm(`Delete the group "${g.name}"?`) && run(() => del(`/api/fw/groups/${g.id}`)).then(refresh)}>Delete</Button></div>
            </div>
          ))}
          {!pickers?.groups?.length && <Empty>No groups yet.</Empty>}
        </div>
      </Card>
      <Card title="Lists of ports" desc="A named set of ports you can reuse, like everything a game console needs." actions={<Button small icon="plus" onClick={() => setEp({})}>New list</Button>}>
        <div className="list">
          {(pickers?.port_lists || []).map((l) => (
            <div className="item" key={l.id}>
              <div><div className="name">{l.name}</div><div className="meta mono">{l.ports.join(', ')}</div></div>
              <div className="actions"><Button small icon="edit" onClick={() => setEp(l)}>Edit</Button>
                <Button small kind="danger" icon="trash" onClick={() => window.confirm(`Delete the list "${l.name}"?`) && run(() => del(`/api/fw/portlists/${l.id}`)).then(refresh)}>Delete</Button></div>
            </div>
          ))}
          {!pickers?.port_lists?.length && <Empty>No lists yet.</Empty>}
        </div>
      </Card>
      {eg && <GroupForm initial={eg} pickers={pickers} reloadPickers={reloadPickers} onClose={() => setEg(null)} onSaved={refresh} />}
      {ep && <PortListForm initial={ep} onClose={() => setEp(null)} onSaved={refresh} />}
    </>
  );
}

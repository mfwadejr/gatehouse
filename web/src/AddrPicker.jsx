import React, { useMemo, useState } from 'react';
import { post, put } from './api.js';
import { Button, Field, Input, Modal, Select, useAction } from './ui.jsx';

// Pieces used by the firewall rule form: choosing who or what a rule covers, which ports, and when.

const CODES = 'AD AE AF AG AI AL AM AO AQ AR AS AT AU AW AX AZ BA BB BD BE BF BG BH BI BJ BL BM BN BO BQ BR BS BT BV BW BY BZ CA CC CD CF CG CH CI CK CL CM CN CO CR CU CV CW CX CY CZ DE DJ DK DM DO DZ EC EE EG EH ER ES ET FI FJ FK FM FO FR GA GB GD GE GF GG GH GI GL GM GN GP GQ GR GS GT GU GW GY HK HM HN HR HT HU ID IE IL IM IN IO IQ IR IS IT JE JM JO JP KE KG KH KI KM KN KP KR KW KY KZ LA LB LC LI LK LR LS LT LU LV LY MA MC MD ME MF MG MH MK ML MM MN MO MP MQ MR MS MT MU MV MW MX MY MZ NA NC NE NF NG NI NL NO NP NR NU NZ OM PA PE PF PG PH PK PL PM PN PR PS PT PW PY QA RE RO RS RU RW SA SB SC SD SE SG SH SI SJ SK SL SM SN SO SR SS ST SV SX SY SZ TC TD TF TG TH TJ TK TL TM TN TO TR TT TV TW TZ UA UG UM US UY UZ VA VC VE VG VI VN VU WF WS YE YT ZA ZM ZW'.split(' ');
let names;
export function countryName(cc) {
  try {
    names = names || new Intl.DisplayNames(['en'], { type: 'region' });
    return names.of(cc) || cc;
  } catch { return cc; }
}

const reMAC = /^([0-9a-f]{2}:){5}[0-9a-f]{2}$/i;
const reDomain = /^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$/i;
const reAddr = /^[0-9a-f:.]+(\/\d{1,3})?(-[0-9a-f:.]+)?$/i;

export const tokens = (s) => (s || '').split(/[\s,]+/).filter((t) => t && t.toLowerCase() !== 'any');

// A short, human label for a stored token, using the picker data when it has the name.
export function labelFor(tok, pickers) {
  if (tok.startsWith('dev:')) {
    const mac = tok.slice(4).toLowerCase();
    const d = pickers?.devices?.find((x) => x.mac === mac);
    return { kind: 'device', text: d?.name || mac, sub: d?.ip || '' };
  }
  if (tok.startsWith('grp:')) {
    const g = pickers?.groups?.find((x) => x.name === tok.slice(4));
    return { kind: 'group', text: tok.slice(4), sub: g ? `${g.members.length} devices` : '' };
  }
  if (tok.startsWith('dom:')) return { kind: 'domain', text: tok.slice(4), sub: 'and subdomains' };
  if (tok.startsWith('geo:')) return { kind: 'country', text: countryName(tok.slice(4)), sub: tok.slice(4) };
  if (reMAC.test(tok)) return { kind: 'mac', text: tok.toLowerCase(), sub: 'device address' };
  return { kind: 'address', text: tok, sub: '' };
}

const KIND_ICON = { device: '🖥', group: '👥', domain: '🌐', country: '🏳', mac: '🔖', address: '#' };

export function Chip({ tok, pickers, onRemove, warn }) {
  const l = labelFor(tok, pickers);
  return (
    <span className={`chip ${warn ? 'warn' : ''}`} title={l.sub}>
      <span aria-hidden="true">{KIND_ICON[l.kind]}</span> {l.text}{l.sub && l.kind !== 'country' ? <span className="muted small"> {l.sub}</span> : null}
      {onRemove && <button type="button" className="chipx" aria-label={`Remove ${l.text}`} onClick={onRemove}>×</button>}
    </span>
  );
}

export function NewDevice({ initialMac, onClose, onCreated }) {
  const [mac, setMac] = useState(initialMac || '');
  const [name, setName] = useState('');
  const [run, busy] = useAction();
  const save = async () => {
    const ok = await run(() => post('/api/devices', { mac: mac.trim().toLowerCase(), name }));
    if (ok) { onCreated(mac.trim().toLowerCase()); onClose(); }
  };
  return (
    <Modal title="Add a device" onClose={onClose}
      footer={<><Button onClick={onClose}>Cancel</Button><Button kind="primary" disabled={busy || !reMAC.test(mac.trim()) || !name.trim()} onClick={save}>Add</Button></>}>
      <p className="small muted">For a device that has not connected yet. Its address is on a label on the device, or in its network settings.</p>
      <Field label="Name"><Input value={name} onChange={setName} placeholder="Kitchen TV" /></Field>
      <Field label="Device address (MAC)" hint="Looks like aa:bb:cc:dd:ee:ff"><Input value={mac} onChange={setMac} placeholder="aa:bb:cc:dd:ee:ff" /></Field>
    </Modal>
  );
}

// AddrPicker chooses the devices, groups or addresses a rule covers. value is the stored text.
// side is 'src' or 'dst'; local says the traffic starts on one of your own networks.
export function AddrPicker({ value, onChange, side, pickers, local = true, reload }) {
  const [q, setQ] = useState('');
  const [open, setOpen] = useState(false);
  const [creating, setCreating] = useState(null);
  const toks = tokens(value);
  const has = (t) => toks.some((x) => x.toLowerCase() === t.toLowerCase());
  const add = (t) => { if (!has(t)) onChange([...toks, t].join(',')); setQ(''); };
  const remove = (t) => onChange(toks.filter((x) => x !== t).join(','));
  const needle = q.trim().toLowerCase();
  const devs = useMemo(() => (pickers?.devices || []).filter((d) => !has(`dev:${d.mac}`) && (!needle || `${d.name} ${d.ip} ${d.mac}`.toLowerCase().includes(needle))).slice(0, 8), [pickers, needle, value]);
  const groups = useMemo(() => (pickers?.groups || []).filter((g) => !has(`grp:${g.name}`) && (!needle || g.name.toLowerCase().includes(needle))).slice(0, 5), [pickers, needle, value]);
  const typed = q.trim();
  const isMac = reMAC.test(typed);
  const known = isMac && (pickers?.devices || []).some((d) => d.mac === typed.toLowerCase());
  const isAddr = typed && !isMac && reAddr.test(typed);
  const isDom = side === 'dst' && typed && reDomain.test(typed);
  const showList = open || needle;
  return (
    <div className="stack" style={{ gap: 'var(--space-2)' }}>
      <div className="chips">
        {toks.length === 0 && <span className="chip any">{side === 'src' ? 'Any device' : 'Anywhere'}</span>}
        {toks.map((t) => <Chip key={t} tok={t} pickers={pickers} onRemove={() => remove(t)} />)}
      </div>
      <div className="pick">
        <Input value={q} onChange={setQ} onFocus={() => setOpen(true)} onBlur={() => setTimeout(() => setOpen(false), 150)}
          placeholder={side === 'src' ? 'Search a device or group, or type an address' : 'Search a device or group, type an address or a website'} aria-label="Find a device" />
        {showList && (
          <div className="pickmenu" role="listbox">
            {devs.map((d) => (
              <button type="button" role="option" key={d.mac} onMouseDown={(e) => e.preventDefault()} onClick={() => add(`dev:${d.mac}`)}>
                <span aria-hidden="true">{KIND_ICON.device}</span> <span className="name">{d.name}</span> <span className="muted small mono">{d.ip || 'no address yet'}</span>{d.groups?.length ? <span className="muted small"> · {d.groups.join(', ')}</span> : null}
              </button>
            ))}
            {groups.map((g) => (
              <button type="button" role="option" key={g.id} onMouseDown={(e) => e.preventDefault()} onClick={() => add(`grp:${g.name}`)}>
                <span aria-hidden="true">{KIND_ICON.group}</span> <span className="name">{g.name}</span> <span className="muted small">group · {g.members.length} devices</span>
              </button>
            ))}
            {isAddr && !has(typed) && <button type="button" role="option" onMouseDown={(e) => e.preventDefault()} onClick={() => add(typed)}>Use the address <span className="mono">{typed}</span></button>}
            {isMac && side === 'src' && local && !known && (
              <>
                <button type="button" role="option" onMouseDown={(e) => e.preventDefault()} onClick={() => setCreating(typed)}>Add <span className="mono">{typed}</span> as a new device…</button>
                <button type="button" role="option" onMouseDown={(e) => e.preventDefault()} onClick={() => add(typed.toLowerCase())}>Match the address <span className="mono">{typed}</span> only</button>
              </>
            )}
            {isMac && side === 'dst' && <div className="muted small pad">A device address cannot be a destination. Pick the device by name instead.</div>}
            {isMac && !local && side === 'src' && <div className="muted small pad">Device addresses only work for devices on your own networks.</div>}
            {isDom && <button type="button" role="option" onMouseDown={(e) => e.preventDefault()} onClick={() => add(`dom:${typed.toLowerCase()}`)}>Use the website <span className="name">{typed.toLowerCase()}</span> and its subdomains</button>}
            {!devs.length && !groups.length && !isAddr && !isMac && !isDom && (
              <div className="muted small pad">{needle ? 'Nothing found. Type an address like 192.168.1.50, or ' + (side === 'dst' ? 'a website like example.com.' : 'a device address.') : 'No devices yet. Type an address.'}</div>
            )}
            {side === 'src' && local && !needle && <button type="button" role="option" onMouseDown={(e) => e.preventDefault()} onClick={() => setCreating('')}>+ Add a device that has not connected yet…</button>}
          </div>
        )}
      </div>
      {side === 'dst' && (
        <div className="inline">
          <Select value="" onChange={(cc) => cc && add(`geo:${cc}`)} aria-label="Add a country"
            options={[{ value: '', label: 'Add a country…' }, ...CODES.map((c) => ({ value: c, label: `${countryName(c)} (${c})` })).sort((a, b) => a.label.localeCompare(b.label))]} />
        </div>
      )}
      {creating !== null && <NewDevice initialMac={creating} onClose={() => setCreating(null)} onCreated={(mac) => { reload?.(); add(`dev:${mac}`); }} />}
    </div>
  );
}

// ServicePicker chooses what kind of traffic with one search box: built-in services, your own
// services, or ports typed by hand (which can be saved as a named service on the spot).
const rePort = /^(tcp|udp)\/\d{1,5}(-\d{1,5})?$/;

export function ServicePicker({ value, onChange, pickers, reload }) {
  const toks = tokens(value);
  const [q, setQ] = useState('');
  const [open, setOpen] = useState(false);
  const [saving, setSaving] = useState(null);
  const has = (t) => toks.some((x) => x.toLowerCase() === t.toLowerCase());
  const add = (t) => { if (t && !has(t)) onChange([...toks, t].join(',')); setQ(''); };
  const remove = (t) => onChange(toks.filter((x) => x !== t).join(','));
  const label = (t) => (t.startsWith('list:') ? t.slice(5) : t);
  const needle = q.trim().toLowerCase();
  const typed = needle.replace(/\s+/g, '');
  const isPort = rePort.test(typed);
  const customs = (pickers?.port_lists || []).map((l) => ({ tok: `list:${l.name}`, name: l.name, ports: l.ports, custom: true }));
  const builtins = (pickers?.catalog || (pickers?.services || []).map((n) => ({ name: n, ports: [] }))).map((b) => ({ tok: b.name, name: b.name, ports: b.ports }));
  const match = (x) => !has(x.tok) && (!needle || x.name.toLowerCase().includes(needle) || x.ports.some((p) => p.includes(needle) || p.split('/')[1]?.includes(needle)));
  const mine = customs.filter(match).slice(0, 8);
  const known = builtins.filter(match).slice(0, 10);
  const showList = open || needle;
  const row = (x) => (
    <button type="button" role="option" key={x.tok} onMouseDown={(e) => e.preventDefault()} onClick={() => add(x.tok)}>
      <span className="name">{x.name}</span>{x.custom && <span className="muted small">your service</span>}
      <span className="muted small mono">{x.ports.join(', ')}</span>
    </button>
  );
  return (
    <div className="stack" style={{ gap: 'var(--space-2)' }}>
      <div className="chips">
        {toks.length === 0 && <span className="chip any">Everything</span>}
        {toks.map((t) => <span className="chip" key={t}>{label(t)}<button type="button" className="chipx" aria-label={`Remove ${label(t)}`} onClick={() => remove(t)}>×</button></span>)}
      </div>
      <div className="pick">
        <Input value={q} onChange={setQ} onFocus={() => setOpen(true)} onBlur={() => setTimeout(() => setOpen(false), 150)}
          placeholder="Search a service or port, or type tcp/8080 or udp/1000-2000" aria-label="Find a service"
          onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); e.stopPropagation(); if (isPort) add(typed); else if (mine.length + known.length === 1) add((mine[0] || known[0]).tok); } }} />
        {showList && (
          <div className="pickmenu" role="listbox">
            {isPort && !has(typed) && <button type="button" role="option" onMouseDown={(e) => e.preventDefault()} onClick={() => add(typed)}>Use the port <span className="mono">{typed}</span></button>}
            {isPort && <button type="button" role="option" onMouseDown={(e) => e.preventDefault()} onClick={() => setSaving(typed)}>Save <span className="mono">{typed}</span> as a named service…</button>}
            {mine.map(row)}
            {known.map(row)}
            {!isPort && !mine.length && !known.length && <div className="muted small pad">{needle ? 'No service with that name or port. Type a port like tcp/8080 to use it or save it as a service.' : 'Nothing left to add.'}</div>}
            {!needle && <button type="button" role="option" onMouseDown={(e) => e.preventDefault()} onClick={() => setSaving('')}>+ New service…</button>}
          </div>
        )}
      </div>
      {saving !== null && <NewService initialPorts={saving} onClose={() => setSaving(null)} onCreated={(name) => { reload?.(); add(`list:${name}`); }} />}
    </div>
  );
}

// NewService saves a named set of ports. It is the same thing as a custom service on the Services page.
export function NewService({ initialPorts = '', onClose, onCreated }) {
  const [name, setName] = useState('');
  const [ports, setPorts] = useState(initialPorts);
  const [run, busy] = useAction();
  const list = ports.split(/[\s,]+/).filter(Boolean).map((x) => x.toLowerCase());
  const save = async () => {
    const ok = await run(() => put('/api/fw/portlists', { name: name.trim(), ports: list }), 'Service saved.');
    if (ok) { onCreated(name.trim()); onClose(); }
  };
  return (
    <Modal title="New service" onClose={onClose}
      footer={<><Button onClick={onClose}>Cancel</Button><Button kind="primary" disabled={busy || !name.trim() || !list.length} onClick={save}>Save</Button></>}>
      <Field label="Name"><Input value={name} onChange={setName} placeholder="Plex" /></Field>
      <Field label="Ports" hint="One per line, like tcp/32400 or udp/1000-2000. A built-in name such as https also works.">
        <textarea className="in mono" rows={4} value={ports} onChange={(e) => setPorts(e.target.value)} spellCheck={false} placeholder={'tcp/32400\nudp/1900'} />
      </Field>
    </Modal>
  );
}

const DAYS = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];

// ScheduleEditor sets when a rule is in force.
export function ScheduleEditor({ value, onChange }) {
  const s = value || { mode: 'always' };
  const mode = s.mode || 'always';
  const set = (patch) => onChange({ ...s, ...patch });
  const choose = (m) => {
    if (m === 'always') return onChange(null);
    if (m === 'daily') return onChange({ mode: 'daily', from: s.from || '21:00', to: s.to || '07:00' });
    if (m === 'weekly') return onChange({ mode: 'weekly', from: s.from || '08:00', to: s.to || '17:00', days: s.days?.length ? s.days : [1, 2, 3, 4, 5] });
    return onChange({ mode: 'once', start: s.start || '', end: s.end || '' });
  };
  return (
    <div className="stack">
      <Select value={mode} onChange={choose} aria-label="When" options={[
        { value: 'always', label: 'Always' }, { value: 'daily', label: 'Every day, between two times' },
        { value: 'weekly', label: 'On certain days' }, { value: 'once', label: 'Just once' }]} />
      {(mode === 'daily' || mode === 'weekly') && (
        <div className="formgrid">
          <Field label="From"><input className="in" type="time" value={s.from || ''} onChange={(e) => set({ from: e.target.value })} /></Field>
          <Field label="Until" hint={s.from && s.to && s.to <= s.from ? 'Until the next morning' : ''}><input className="in" type="time" value={s.to || ''} onChange={(e) => set({ to: e.target.value })} /></Field>
        </div>
      )}
      {mode === 'weekly' && (
        <div className="inline" role="group" aria-label="Days">
          {DAYS.map((d, i) => {
            const on = (s.days || []).includes(i);
            return <button type="button" key={d} className={`daybtn ${on ? 'on' : ''}`} aria-pressed={on} onClick={() => set({ days: on ? s.days.filter((x) => x !== i) : [...(s.days || []), i].sort() })}>{d}</button>;
          })}
        </div>
      )}
      {mode === 'once' && (
        <div className="formgrid">
          <Field label="Starts"><input className="in" type="datetime-local" value={s.start || ''} onChange={(e) => set({ start: e.target.value })} /></Field>
          <Field label="Ends"><input className="in" type="datetime-local" value={s.end || ''} onChange={(e) => set({ end: e.target.value })} /></Field>
        </div>
      )}
      {mode !== 'always' && <span className="hint">Uses the time zone set on this machine. The firewall switches the rule on and off by itself.</span>}
    </div>
  );
}

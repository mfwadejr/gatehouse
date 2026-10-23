import React, { useState } from 'react';
import { del, put, useData } from '../api.js';
import SourcePicker from '../SourcePicker.jsx';
import { Button, Card, Empty, Field, Input, Modal, Note, Pill, Segmented, Select, Trend, useAction } from '../ui.jsx';

// ---------------------------------------------------------------- rates

const unitFor = (peak) => (peak >= 1e9 ? [1e9, 'Gb/s'] : peak >= 1e6 ? [1e6, 'Mb/s'] : [1e3, 'kb/s']);
export function rate(bps) {
  if (!bps) return '0 kb/s';
  const [d, u] = unitFor(bps);
  const v = bps / d;
  return `${v >= 100 ? Math.round(v) : Math.round(v * 10) / 10} ${u}`;
}

function RateChart({ values, label }) {
  const recent = (values || []).slice(-100);
  const [d, unit] = unitFor(Math.max(0, ...recent));
  return <Trend values={recent.map((v) => v / d)} unit={` ${unit}`} series={label} seconds={300} height={70} empty="No traffic" />;
}

// ---------------------------------------------------------------- traffic

export function Traffic({ wans, links }) {
  const { data } = useData('/api/net/throughput', { every: 3000 });
  const on = wans.filter((w) => w.enabled);
  if (!on.length) return null;
  return (
    <Card title="Traffic on each connection" desc="What is flowing through each internet connection right now, over the last five minutes.">
      <div className="stack">
        {on.map((w) => {
          const s = data?.links?.[w.name];
          const rx = s?.rx || [];
          const tx = s?.tx || [];
          const l = links[w.name];
          return (
            <div key={w.id} className="wg-traffic">
              <div className="wg-head">
                <span className="name">{w.name}</span>
                <Pill tone={!l ? '' : !l.up ? 'bad' : 'good'}>{!l ? 'Not checked yet' : !l.up ? 'Down' : l.active ? 'In use' : 'Standby'}</Pill>
                <span className="small muted">Download <strong>{rate(rx[rx.length - 1])}</strong> · Upload <strong>{rate(tx[tx.length - 1])}</strong></span>
              </div>
              <div className="wg-charts">
                <RateChart values={rx} label="Download" />
                <RateChart values={tx} label="Upload" />
              </div>
            </div>
          );
        })}
      </div>
    </Card>
  );
}

// ---------------------------------------------------------------- member editor

function evenly(n) {
  const base = Math.floor(100 / n);
  return Array.from({ length: n }, (_, i) => base + (i === 0 ? 100 - base * n : 0));
}

// Picks the connections in a group, their order (failover) or percentages (sharing the load).
function Members({ mode, all, value, onChange, fixed }) {
  const chosen = value.map((m) => ({ ...m, w: all.find((x) => x.id === m.wan_id) })).filter((m) => m.w);
  const rest = all.filter((x) => !value.some((m) => m.wan_id === x.id));
  const spread = (list) => {
    const sh = evenly(list.length);
    return list.map((m, i) => ({ ...m, weight: sh[i] }));
  };
  const add = (id) => onChange(mode === 'balance' ? spread([...value, { wan_id: id, weight: 1 }]) : [...value, { wan_id: id, weight: 1 }]);
  const drop = (id) => { const n = value.filter((m) => m.wan_id !== id); onChange(mode === 'balance' ? spread(n) : n); };
  const move = (i, d) => { const n = [...value]; [n[i], n[i + d]] = [n[i + d], n[i]]; onChange(n); };
  const setW = (id, v) => onChange(value.map((m) => (m.wan_id === id ? { ...m, weight: v } : m)));
  const total = value.reduce((s, m) => s + (Number(m.weight) || 0), 0);
  return (
    <div className="stack">
      {chosen.map((m, i) => (
        <div className="wg-member" key={m.wan_id}>
          {mode === 'failover' && <span className="wg-rank" aria-label={`Choice ${i + 1}`}>{i + 1}</span>}
          <span className="name">{m.w.name}</span>
          {!m.w.enabled && <Pill>Off</Pill>}
          <span className="grow" />
          {mode === 'balance' && chosen.length > 1 && (
            <label className="inline"><input className="in wg-pct" type="number" min="1" max="100" value={m.weight} onChange={(e) => setW(m.wan_id, Number(e.target.value))} aria-label={`Share for ${m.w.name}`} /> <span className="muted">%</span></label>
          )}
          {mode === 'failover' && chosen.length > 1 && (
            <>
              <Button small disabled={i === 0} onClick={() => move(i, -1)} aria-label={`Move ${m.w.name} up`}>↑</Button>
              <Button small disabled={i === chosen.length - 1} onClick={() => move(i, 1)} aria-label={`Move ${m.w.name} down`}>↓</Button>
            </>
          )}
          {!fixed && <Button small kind="danger" onClick={() => drop(m.wan_id)}>Remove</Button>}
        </div>
      ))}
      {mode === 'failover' && chosen.length > 1 && <p className="small muted">The first one is used. If it stops working, the next one takes over, and the first takes back over when it recovers.</p>}
      {mode === 'balance' && chosen.length > 1 && (
        <div className="inline">
          <Pill tone={total === 100 ? 'good' : 'bad'}>{total}% of 100%</Pill>
          <Button small onClick={() => onChange(spread(value))}>Split evenly</Button>
        </div>
      )}
      {!fixed && rest.length > 0 && (
        <Select value="" onChange={(v) => v && add(Number(v))} aria-label="Add a connection"
          options={[{ value: '', label: 'Add a connection…' }, ...rest.map((x) => ({ value: String(x.id), label: x.name }))]} />
      )}
    </div>
  );
}

const MODES = [{ value: 'failover', label: 'Use one, switch if it fails' }, { value: 'balance', label: 'Share the load' }];

function changeMode(mode, members, set) {
  if (mode === 'balance') {
    const sh = evenly(Math.max(members.length, 1));
    set({ mode, members: members.map((m, i) => ({ ...m, weight: sh[i] })) });
  } else set({ mode });
}

// ---------------------------------------------------------------- forms

function GroupForm({ initial, wans, onClose, onSaved }) {
  const [g, setG] = useState({ name: '', mode: 'failover', fallback: 'default', members: [], uses: [], ...initial });
  const [run, busy] = useAction();
  const patch = (p) => setG((x) => ({ ...x, ...p }));
  const save = async () => {
    const body = { ...g, members: g.members.map((m) => ({ wan_id: m.wan_id, weight: m.weight })) };
    const r = await run(() => put('/api/net/groups', body), 'Saved. Apply it to start using it.');
    if (r) { (r.warnings || []).forEach((w) => window.alert(w)); onSaved(); onClose(); }
  };
  return (
    <Modal title={g.id ? `Edit ${initial.name}` : 'New connection group'} onClose={onClose}
      footer={<><Button onClick={onClose}>Cancel</Button><Button kind="primary" disabled={busy || !g.name.trim() || !g.members.length} onClick={save}>Save</Button></>}>
      <div className="stack">
        <Field label="Name" hint="For example Cameras, Work, or Guests."><Input value={g.name} onChange={(v) => patch({ name: v })} placeholder="Work" /></Field>
        <Field label="Which connections does it use?">
          <Segmented value={g.mode} onChange={(m) => changeMode(m, g.members, patch)} options={MODES} />
          <div style={{ marginTop: 'var(--space-3)' }}><Members mode={g.mode} all={wans} value={g.members} onChange={(members) => patch({ members })} /></div>
        </Field>
        <Field label="If none of them work">
          <Select value={g.fallback} onChange={(v) => patch({ fallback: v })} options={[
            { value: 'default', label: 'Use the default connections instead' },
            { value: 'block', label: 'Cut these devices off from the internet' }]} />
        </Field>
        <p className="sec">Choose who uses this group. Anything you do not choose keeps using the default connections.</p>
        <SourcePicker value={g.uses} onChange={(uses) => patch({ uses })} />
      </div>
    </Modal>
  );
}

function DefaultForm({ def, wans, onClose, onSaved }) {
  const on = def.members.filter((m) => m.enabled);
  const sum = on.reduce((t, m) => t + m.weight, 0);
  const sh = evenly(Math.max(on.length, 1));
  const [d, setD] = useState({ label: def.label, mode: def.mode, members: on.map((m, i) => ({ wan_id: m.wan_id, weight: def.mode === 'balance' && sum !== 100 ? sh[i] : m.weight })) });
  const [run, busy] = useAction();
  const patch = (p) => setD((x) => ({ ...x, ...p }));
  const save = async () => {
    const ok = await run(() => put('/api/net/default-group', d), 'Saved. Apply it to start using it.');
    if (ok) { onSaved(); onClose(); }
  };
  return (
    <Modal title="Default connections" onClose={onClose}
      footer={<><Button onClick={onClose}>Cancel</Button><Button kind="primary" disabled={busy || !d.label.trim()} onClick={save}>Save</Button></>}>
      <div className="stack">
        <p className="sec">The default group carries everything that is not sent through another group.</p>
        <Field label="Name"><Input value={d.label} onChange={(v) => patch({ label: v })} /></Field>
        <Field label="How should it use your connections?">
          <Segmented value={d.mode} onChange={(m) => changeMode(m, d.members, patch)} options={MODES} />
          <div style={{ marginTop: 'var(--space-3)' }}><Members fixed mode={d.mode} all={wans} value={d.members} onChange={(members) => patch({ members })} /></div>
        </Field>
      </div>
    </Modal>
  );
}

// ---------------------------------------------------------------- the card

function summary(mode, members) {
  const on = members.filter((m) => m.enabled !== false);
  if (!on.length) return 'No connection';
  if (mode === 'balance' && on.length > 1) {
    const sum = on.reduce((t, m) => t + (m.weight || 1), 0);
    return `Shared: ${on.map((m) => `${m.name} ${Math.round(((m.weight || 1) / sum) * 100)}%`).join(' · ')}`;
  }
  if (on.length === 1) return `Only ${on[0].name}`;
  return `In this order: ${on.map((m) => m.name).join(' → ')}`;
}

function liveTone(s) { if (!s) return ['', 'Not applied yet']; if (!s.up) return ['bad', 'No connection works']; return ['good', `Using ${(s.via || []).join(' and ')}`]; }

export default function WanGroups({ wans, links, onChanged }) {
  const { data, reload } = useData('/api/net/groups', { topics: ['net.status'] });
  const [edit, setEdit] = useState(null);
  const [editDef, setEditDef] = useState(false);
  const [run] = useAction();
  const enabled = wans.filter((w) => w.enabled);
  if (enabled.length < 2 || !data) return null;
  const done = () => { reload(); onChanged?.(); };
  const def = data.default;
  return (
    <Card title="Connection groups" desc="Send chosen networks or devices through specific connections. Everything else uses the default group."
      actions={<Button icon="plus" disabled={data.groups.length >= data.max} onClick={() => setEdit({})}>New group</Button>}>
      <div className="list">
        <div className="item">
          <div>
            <div className="name">{def.label} <Pill>Everything else</Pill></div>
            <div className="meta">{summary(def.mode, def.members)}</div>
          </div>
          <div className="actions"><Button small icon="edit" onClick={() => setEditDef(true)}>Edit</Button></div>
        </div>
        {data.groups.map((g) => {
          const [tone, text] = liveTone(links[`group:${g.name}`]);
          const live = links[`group:${g.name}`];
          return (
            <div className="item" key={g.id}>
              <div>
                <div className="name">{g.name}</div>
                <div className="meta">{summary(g.mode, g.members)}{g.fallback === 'block' ? ' · cut off if none work' : ''}</div>
                <div className="inline" style={{ marginTop: 'var(--space-1)' }}>
                  {g.uses.length === 0 && <span className="small muted">Nobody assigned yet</span>}
                  {g.uses.map((u) => <Pill key={`${u.kind}|${u.value}`}>{u.kind === 'network' ? `${u.value} network` : u.kind === 'device' ? (u.label || u.value) : u.value}</Pill>)}
                </div>
                {live?.error && <div className="small" style={{ color: 'var(--color-status-warning)' }}>{live.error}</div>}
                {(g.warnings || []).map((w) => <div key={w} className="small muted">{w}</div>)}
              </div>
              <div className="actions">
                <Pill tone={tone}>{text}</Pill>
                <Button small icon="edit" onClick={() => setEdit(g)}>Edit</Button>
                <Button small kind="danger" icon="trash" onClick={() => { if (window.confirm(`Remove the group ${g.name}? Its devices go back to the default connections.`)) run(() => del(`/api/net/groups/${g.id}`)).then(done); }}>Remove</Button>
              </div>
            </div>
          );
        })}
      </div>
      {data.groups.length === 0 && <Empty>No extra groups. Add one to keep, say, your cameras on the backup line or work devices on the faster one.</Empty>}
      {edit && <GroupForm initial={edit} wans={enabled} onClose={() => setEdit(null)} onSaved={done} />}
      {editDef && <DefaultForm def={def} wans={enabled} onClose={() => setEditDef(false)} onSaved={done} />}
    </Card>
  );
}

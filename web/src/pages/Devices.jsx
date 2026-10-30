import React, { useEffect, useMemo, useRef, useState } from 'react';
import { del, get, patch, post, put, useData } from '../api.js';
import DeviceIcon, { ICON_LABELS } from '../DeviceIcon.jsx';
import { NewDevice } from '../AddrPicker.jsx';
import DeviceMap from './DeviceMap.jsx';
import { pauseChoices, whenText } from './Profiles.jsx';
import { WhyBody } from './Why.jsx';
import { Button, Card, Empty, Field, Input, Modal, Note, Page, Pill, Segmented, Select, Spark, ToggleRow, useAction, useToast, fmt, confirmBox } from '../ui.jsx';

const POLICIES = [
  { value: 'filtered', label: 'Allowed, ads and trackers filtered' },
  { value: 'unfiltered', label: 'Allowed, no filtering' },
];

const bytes = (n) => {
  if (!n) return '0 B';
  const u = ['B', 'KB', 'MB', 'GB', 'TB'];
  let i = 0, v = n;
  while (v >= 1000 && i < u.length - 1) { v /= 1000; i++; }
  return `${v >= 100 || i === 0 ? Math.round(v) : v.toFixed(1)} ${u[i]}`;
};
const bits = (n) => {
  if (!n || n < 1000) return n ? '<1 kb/s' : '0';
  const u = ['kb/s', 'Mb/s', 'Gb/s'];
  let i = 0, v = n / 1000;
  while (v >= 1000 && i < u.length - 1) { v /= 1000; i++; }
  return `${v >= 100 ? Math.round(v) : v.toFixed(1)} ${u[i]}`;
};

const COLUMNS = [
  { id: 'vendor', label: 'Maker', show: (d) => d.vendor || '–' },
  { id: 'network', label: 'Network', show: (d) => d.network || '–' },
  { id: 'conn', label: 'Connection', show: (d) => <Conn d={d} /> },
  { id: 'ip', label: 'Address', show: (d) => <span className="mono">{d.ip || '–'}</span> },
  { id: 'activity', label: 'Activity', show: (d) => d.online && (d.down_bps || d.up_bps) ? <span className="small">↓ {bits(d.down_bps)} · ↑ {bits(d.up_bps)}</span> : <span className="muted small">idle</span> },
  { id: 'today', label: 'Used today', show: (d) => d.today_down || d.today_up ? <span className="small">↓ {bytes(d.today_down)} · ↑ {bytes(d.today_up)}</span> : <span className="muted small">–</span> },
  { id: 'group', label: 'Profile', show: (d) => d.group || 'Default' },
  { id: 'internet', label: 'Internet', show: (d) => <Pill tone={d.internet ? 'good' : 'bad'}>{d.internet ? 'Allowed' : 'Paused'}</Pill> },
  { id: 'seen', label: 'Seen', show: (d) => (d.online ? 'now' : fmt.ago(d.last_seen)) },
];
const DEFAULT_COLS = ['network', 'conn', 'ip', 'activity'];
const PAGE_SIZES = [25, 50, 100];

function Conn({ d }) {
  if (!d.conn) return <span className="muted">–</span>;
  if (d.conn === 'wifi') {
    const sig = d.signal ? ` · ${d.signal} dBm` : '';
    return <span>Wi-Fi{d.band ? ` ${d.band} GHz` : ''}{d.channel ? ` ch ${d.channel}` : ''}<span className="muted small">{sig}</span></span>;
  }
  if (d.conn === 'vpn') return <span>VPN</span>;
  return <span>Wired{d.port ? <span className="muted small"> · {d.port}</span> : ''}</span>;
}

function IconPicker({ d, onChanged }) {
  const [run, busy] = useAction();
  const file = useRef(null);
  const set = (body) => run(() => patch(`/api/devices/${d.id}`, body)).then(onChanged);
  const upload = async (f) => {
    if (!f) return;
    if (f.size > 100 * 1024) { await run(async () => { throw new Error('The picture is too big. Keep it under 100 KB.'); }); return; }
    const data = await new Promise((res, rej) => { const r = new FileReader(); r.onload = () => res(String(r.result).split(',')[1]); r.onerror = rej; r.readAsDataURL(f); });
    const r = await run(() => post('/api/device-icons', { name: f.name, data }));
    if (r) set({ icon_id: r.id });
  };
  return (
    <div className="stack">
      <div className="iconpick">
        {Object.entries(ICON_LABELS).map(([k, label]) => (
          <button key={k} type="button" title={label} aria-label={label} disabled={busy}
            className={!d.icon_id && (d.icon || d.icon_guess) === k ? 'on' : ''} onClick={() => set({ icon: k, icon_id: 0 })}>
            <DeviceIcon d={{ icon: k }} size={22} />
          </button>
        ))}
      </div>
      <div className="inline">
        <Button small onClick={() => file.current?.click()} disabled={busy}>Upload your own picture</Button>
        {d.icon_id > 0 && <Button small kind="danger" onClick={() => set({ icon_id: 0 })} disabled={busy}>Remove it</Button>}
        {(d.icon || d.icon_id > 0) && <Button small onClick={() => set({ icon: '', icon_id: 0 })} disabled={busy}>Let Gatehouse guess</Button>}
        <input ref={file} type="file" accept="image/png,image/jpeg,image/webp" hidden onChange={(e) => { upload(e.target.files?.[0]); e.target.value = ''; }} />
      </div>
      <span className="hint">PNG, JPEG or WebP, up to 100 KB.</span>
    </div>
  );
}

function Details({ d, groups, onClose, onSaved }) {
  const toast = useToast();
  const [run, busy] = useAction();
  const { data: det, reload } = useData(`/api/devices/${d.id}/details`);
  const [f, setF] = useState({ name: d.name || '', group_id: d.group_id || 0, policy: d.policy || 'filtered', internet: d.internet, notes: d.notes || '' });
  const [fixIP, setFixIP] = useState('');
  const vpn = d.source === 'vpn';
  const speed = useMemo(() => (det?.traffic?.points || []).map((p) => p.down + p.up), [det]);
  const save = async () => { if (await run(() => patch(`/api/devices/${d.id}`, f), 'Saved.')) { onSaved(); onClose(); } };
  const keep = (keepIt) => run(() => put(`/api/devices/${d.id}/reservation`, { keep: keepIt, ip: fixIP }), keepIt ? 'This device will always get this address.' : 'Address no longer fixed.').then(() => { reload(); onSaved(); });
  return (
    <Modal title={d.name || d.hostname || d.mac} onClose={onClose}
      footer={vpn ? <Button onClick={onClose}>Close</Button> : <><Button onClick={onClose}>Cancel</Button><Button kind="primary" disabled={busy} onClick={save}>Save</Button></>}>
      <div className="inline" style={{ gap: '.75rem' }}>
        <span className="devicon big"><DeviceIcon d={d} size={30} /></span>
        <div>
          <div className="small muted mono">{d.mac}</div>
          <div className="small">{[d.vendor, d.network, d.conn === 'wifi' ? 'Wi-Fi' : d.conn === 'wired' ? `Wired ${d.port || ''}` : d.conn === 'vpn' ? 'VPN' : ''].filter(Boolean).join(' · ') || 'Not seen on the network yet'}</div>
        </div>
        <Pill tone={d.online ? 'good' : ''}>{d.online ? 'Online' : `Seen ${fmt.ago(d.last_seen)}`}</Pill>
      </div>
      {!vpn && (
        <>
          <Field label="Name"><Input value={f.name} onChange={(v) => setF({ ...f, name: v })} placeholder={d.hostname || 'Give it a friendly name'} /></Field>
          <Field label="Icon"><IconPicker d={d} onChanged={onSaved} /></Field>
          <Field label="Profile" hint="Decides which blocklists apply. Manage profiles on the Profiles page."><Select value={String(f.group_id)} onChange={(v) => setF({ ...f, group_id: Number(v) })} options={[{ value: '0', label: 'Default' }, ...groups.map((g) => ({ value: String(g.id), label: g.name }))]} /></Field>
          <Field label="DNS filtering"><Select value={f.policy} onChange={(v) => setF({ ...f, policy: v })} options={POLICIES} /></Field>
          <Field label="Pause the internet" hint={whenText(det?.paused_until) || 'Cuts this device off from the internet for a while. It comes back by itself.'}>
            <div className="inline" style={{ gap: 6, flexWrap: 'wrap' }}>
              {det?.paused_until * 1000 > Date.now()
                ? <Button small kind="primary" disabled={busy} onClick={() => run(() => post(`/api/devices/${d.id}/pause`, { minutes: 0 }), 'Resumed.').then(reload)}>Resume now</Button>
                : pauseChoices().map((c) => <Button key={c.m} small disabled={busy} onClick={() => run(() => post(`/api/devices/${d.id}/pause`, { minutes: c.m }), 'Paused.').then(reload)}>{c.t}</Button>)}
            </div>
          </Field>
          <ToggleRow title="Allowed online" desc="Turn off to pause this device without losing its settings." checked={f.internet} onChange={(v) => setF({ ...f, internet: v })} />
          <Field label="Notes"><Input value={f.notes} onChange={(v) => setF({ ...f, notes: v })} /></Field>

          <Field label="Fixed address" hint="Always hand this device the same address.">
            {det?.reservation ? (
              <div className="inline"><span className="mono">{det.reservation.ip}</span><Button small kind="danger" onClick={() => keep(false)} disabled={busy}>Stop fixing it</Button></div>
            ) : (
              <div className="inline"><Input value={fixIP} onChange={setFixIP} aria-label="Fixed address for this device" placeholder={d.ip || '192.168.1.50'} className="in mono" style={{ maxWidth: '12rem' }} /><Button small onClick={() => keep(true)} disabled={busy || (!fixIP && !d.ip)}>Always use {fixIP || d.ip || 'this address'}</Button></div>
            )}
          </Field>
        </>
      )}
      {!vpn && (
        <details className="more"><summary>Why can't it reach something?</summary><WhyBody device={d.id} /></details>
      )}
      <Field label="Firewall rules that mention it">
        {det?.rules?.length ? <ul className="stack" style={{ margin: 0, paddingLeft: '1.2rem' }}>{det.rules.map((r) => <li key={r}>{r}</li>)}</ul> : <span className="muted small">None.</span>}
      </Field>
      {!vpn && (
        <Field label="Last 24 hours" hint={`Used today: ↓ ${bytes(d.today_down)} · ↑ ${bytes(d.today_up)}`}>
          <Spark values={speed} width={320} height={46} fluid />
        </Field>
      )}
    </Modal>
  );
}

function Filters({ rows, f, setF }) {
  const count = (fn) => rows.filter(fn).length;
  const networks = [...new Set(rows.map((d) => d.network).filter(Boolean))].sort();
  const groups = [...new Set(rows.flatMap((d) => d.groups || []))].sort();
  const set = (k) => (v) => setF((x) => ({ ...x, [k]: v }));
  return (
    <div className="inline" style={{ marginBottom: 'var(--space-3)' }}>
      <Segmented value={f.state} onChange={set('state')} options={[
        { value: '', label: `All ${rows.length}` }, { value: 'online', label: `Online ${count((d) => d.online)}` }, { value: 'offline', label: `Offline ${count((d) => !d.online)}` }]} />
      <Segmented value={f.conn} onChange={set('conn')} options={[
        { value: '', label: 'Any connection' }, { value: 'wired', label: `Wired ${count((d) => d.conn === 'wired')}` },
        { value: 'wifi', label: `Wi-Fi ${count((d) => d.conn === 'wifi')}` }, { value: 'vpn', label: `VPN ${count((d) => d.conn === 'vpn')}` }]} />
      {networks.length > 0 && <div style={{ width: '11rem' }}><Select value={f.network} onChange={set('network')} aria-label="Network" options={[{ value: '', label: 'All networks' }, ...networks.map((n) => ({ value: n, label: n }))]} /></div>}
      {groups.length > 0 && <div style={{ width: '11rem' }}><Select value={f.group} onChange={set('group')} aria-label="Group" options={[{ value: '', label: 'All groups' }, ...groups.map((g) => ({ value: g, label: g }))]} /></div>}
      {(f.state || f.conn || f.network || f.group) && <Button small onClick={() => setF({ state: '', conn: '', network: '', group: '' })}>Clear filters</Button>}
    </div>
  );
}

export default function Devices() {
  const { data, reload } = useData('/api/devices', { topics: ['dhcp.leases'], every: 6000 });
  const { data: groups } = useData('/api/dns/groups');
  const { data: prefs } = useData('/api/prefs/devices');
  const [run] = useAction();
  const [q, setQ] = useState('');
  const [f, setF] = useState({ state: '', conn: '', network: '', group: '' });
  const [open, setOpen] = useState(null);
  const [adding, setAdding] = useState(false);
  const [cols, setCols] = useState(null);
  const [rowsPer, setRowsPer] = useState(null);
  const [page, setPage] = useState(0);
  const [pick, setPick] = useState(false);
  const [view, setView] = useState('list');

  const colIds = cols || prefs?.cols || DEFAULT_COLS;
  const per = rowsPer || prefs?.rows || 25;
  const savePrefs = (c, r) => put('/api/prefs/devices', { cols: c, rows: r }).catch(() => {});
  const all = data?.devices || [];

  const rows = useMemo(() => {
    const s = q.trim().toLowerCase();
    return all.filter((d) => {
      if (f.state === 'online' && !d.online) return false;
      if (f.state === 'offline' && d.online) return false;
      if (f.conn && d.conn !== f.conn) return false;
      if (f.network && d.network !== f.network) return false;
      if (f.group && !d.groups?.includes(f.group)) return false;
      return !s || [d.name, d.hostname, d.vendor, d.mac, d.ip, d.network].some((x) => (x || '').toLowerCase().includes(s));
    });
  }, [all, q, f]);
  useEffect(() => setPage(0), [q, f, per]);
  const pages = Math.max(1, Math.ceil(rows.length / per));
  const shown = rows.slice(page * per, page * per + per);
  const open1 = open && all.find((d) => d.id === open);

  return (
    <Page title="Devices & access" sub="Every device that has joined your network. Rename it, give it an icon, filter it, or pause its internet."
      actions={<><Segmented value={view} onChange={setView} options={[{ value: 'list', label: 'List' }, { value: 'map', label: 'Map' }]} /><Button icon="plus" onClick={() => setAdding(true)}>Add a device</Button></>}>
      {view === 'map' && <DeviceMap devices={all} onOpen={setOpen} />}
      <div style={view === 'map' ? { display: 'none' } : undefined}>
        <Card actions={
          <div className="inline">
            <div style={{ width: '15rem' }}><Input value={q} onChange={setQ} placeholder="Search name, maker, address" aria-label="Search devices" /></div>
            <Button small onClick={() => setPick(true)}>Columns</Button>
          </div>}>
          <Filters rows={all} f={f} setF={setF} />
          <div className="scroll">
            <table className="table">
              <thead><tr><th>Device</th>{COLUMNS.filter((c) => colIds.includes(c.id)).map((c) => <th key={c.id}>{c.label}</th>)}<th /></tr></thead>
              <tbody>
                {shown.map((d) => (
                  <tr key={d.id} className="clickrow" onClick={() => setOpen(d.id)}>
                    <td>
                      <div className="inline">
                        <span className={`devicon ${d.online ? '' : 'off'}`}><DeviceIcon d={d} /></span>
                        <div><div className="name">{d.name || d.hostname || <span className="muted">unnamed</span>}</div><div className="small muted mono">{d.mac || ''}</div></div>
                      </div>
                    </td>
                    {COLUMNS.filter((c) => colIds.includes(c.id)).map((c) => <td key={c.id}>{c.show(d)}</td>)}
                    <td className="r" onClick={(e) => e.stopPropagation()}>
                      {d.source !== 'vpn' && (
                        <div className="actions" style={{ justifyContent: 'flex-end' }}>
                          <Button small onClick={() => run(() => patch(`/api/devices/${d.id}`, { internet: !d.internet }), d.internet ? 'Paused.' : 'Back online.').then(reload)}>{d.internet ? 'Pause' : 'Resume'}</Button>
                          {!d.online && <Button small kind="danger" icon="trash" onClick={async () => await confirmBox('Forget this device?') && run(() => del(`/api/devices/${d.id}`), 'Forgotten.').then(reload)}>Forget</Button>}
                        </div>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
            {!shown.length && <Empty>{all.length ? 'No device matches.' : 'No devices have joined yet.'}</Empty>}
          </div>
          {rows.length > per && (
            <div className="inline" style={{ justifyContent: 'space-between', marginTop: '.75rem' }}>
              <span className="muted small">{page * per + 1}–{Math.min(rows.length, page * per + per)} of {rows.length}</span>
              <div className="inline"><Button small disabled={page === 0} onClick={() => setPage(page - 1)}>Previous</Button><Button small disabled={page >= pages - 1} onClick={() => setPage(page + 1)}>Next</Button></div>
            </div>
          )}
        </Card>
      </div>
      {open1 && <Details d={open1} groups={groups || []} onClose={() => setOpen(null)} onSaved={reload} />}
      {adding && <NewDevice onClose={() => setAdding(false)} onCreated={reload} />}
      {pick && (
        <Modal title="Columns and rows" onClose={() => setPick(false)} footer={<Button kind="primary" onClick={() => setPick(false)}>Done</Button>}>
          <div className="stack">
            {COLUMNS.map((c) => (
              <ToggleRow key={c.id} title={c.label} checked={colIds.includes(c.id)}
                onChange={(on) => { const n = on ? [...colIds, c.id] : colIds.filter((x) => x !== c.id); setCols(n); savePrefs(n, per); }} />
            ))}
            <Field label="Rows per page"><Segmented value={String(per)} onChange={(v) => { setRowsPer(Number(v)); savePrefs(colIds, Number(v)); }} options={PAGE_SIZES.map((n) => ({ value: String(n), label: String(n) }))} /></Field>
            <Note>This is remembered for your account.</Note>
          </div>
        </Modal>
      )}
    </Page>
  );
}

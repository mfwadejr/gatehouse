import React, { useMemo, useState } from 'react';
import { del, patch, useData } from '../api.js';
import { Button, Card, Empty, Field, Input, Modal, Page, Pill, Select, ToggleRow, useAction, fmt } from '../ui.jsx';

const POLICIES = [
  { value: 'allow', label: 'Allowed, no filtering' },
  { value: 'filter', label: 'Allowed, ads and trackers filtered' },
  { value: 'restrict', label: 'Restricted' },
  { value: 'block', label: 'Blocked from the internet' },
];
const tone = { allow: 'good', filter: 'good', restrict: 'warn', block: 'bad' };

function Edit({ d, groups, onClose, onSaved }) {
  const [f, setF] = useState({ name: d.name || '', group_id: d.group_id || 0, policy: d.policy || 'filter', internet: d.internet, notes: d.notes || '' });
  const [run, busy] = useAction();
  return (
    <Modal title={d.name || d.hostname || d.mac} onClose={onClose} footer={<><Button onClick={onClose}>Cancel</Button><Button kind="primary" disabled={busy} onClick={async () => { if (await run(() => patch(`/api/devices/${d.id}`, f), 'Saved.')) { onSaved(); onClose(); } }}>Save</Button></>}>
      <p className="muted small mono">{d.mac} · {d.ip || 'no address now'}</p>
      <Field label="Name"><Input value={f.name} onChange={(v) => setF({ ...f, name: v })} placeholder={d.hostname || 'Give it a friendly name'} /></Field>
      <Field label="Profile" hint="Decides which blocklists apply. Manage profiles on the DNS page."><Select value={String(f.group_id)} onChange={(v) => setF({ ...f, group_id: Number(v) })} options={[{ value: '0', label: 'Default' }, ...groups.map((g) => ({ value: String(g.id), label: g.name }))]} /></Field>
      <Field label="Internet access"><Select value={f.policy} onChange={(v) => setF({ ...f, policy: v })} options={POLICIES} /></Field>
      <ToggleRow title="Allowed online" desc="Turn off to pause this device without losing its settings." checked={f.internet} onChange={(v) => setF({ ...f, internet: v })} />
      <Field label="Notes"><Input value={f.notes} onChange={(v) => setF({ ...f, notes: v })} /></Field>
    </Modal>
  );
}

export default function Devices() {
  const { data, reload } = useData('/api/devices', { topics: ['dhcp.leases'], every: 15000 });
  const { data: groups } = useData('/api/dns/groups');
  const [q, setQ] = useState('');
  const [edit, setEdit] = useState(null);
  const [run] = useAction();
  const rows = useMemo(() => (data || []).filter((d) => !q || [d.name, d.hostname, d.mac, d.ip, d.group].some((x) => (x || '').toLowerCase().includes(q.toLowerCase()))), [data, q]);
  return (
    <Page title="Devices & access" sub="Every device that has joined your network. Rename it, filter it, or pause its internet.">
      <Card actions={<div style={{ width: '16rem' }}><Input value={q} onChange={setQ} placeholder="Search devices" aria-label="Search devices" /></div>}>
        <div className="scroll">
          <table className="table">
            <thead><tr><th>Device</th><th>Address</th><th>Profile</th><th>Internet</th><th>Seen</th><th /></tr></thead>
            <tbody>
              {rows.map((d) => (
                <tr key={d.id}>
                  <td><div className="inline"><span className={`dot ${d.online ? '' : 'off'}`} title={d.online ? 'Online' : 'Offline'} /><div><div className="name">{d.name || d.hostname || <span className="muted">unnamed</span>}</div><div className="small muted mono">{d.mac}</div></div></div></td>
                  <td className="mono">{d.ip || '–'}</td>
                  <td>{d.group || 'Default'}</td>
                  <td><Pill tone={d.internet ? tone[d.policy] || 'good' : 'bad'}>{d.internet ? POLICIES.find((p) => p.value === d.policy)?.label.split(',')[0] || 'Allowed' : 'Paused'}</Pill></td>
                  <td>{d.online ? 'now' : fmt.ago(d.last_seen)}</td>
                  <td className="r"><div className="actions" style={{ justifyContent: 'flex-end' }}>
                    <Button small onClick={() => run(() => patch(`/api/devices/${d.id}`, { internet: !d.internet }), d.internet ? 'Paused.' : 'Back online.').then(reload)}>{d.internet ? 'Pause' : 'Resume'}</Button>
                    <Button small icon="edit" onClick={() => setEdit(d)}>Edit</Button>
                    {!d.online && <Button small kind="danger" icon="trash" onClick={() => window.confirm('Forget this device?') && run(() => del(`/api/devices/${d.id}`)).then(reload)}>Forget</Button>}
                  </div></td>
                </tr>
              ))}
            </tbody>
          </table>
          {!rows.length && <Empty>{data?.length ? 'No device matches your search.' : 'No devices have joined yet.'}</Empty>}
        </div>
      </Card>
      {edit && <Edit d={edit} groups={groups || []} onClose={() => setEdit(null)} onSaved={reload} />}
    </Page>
  );
}

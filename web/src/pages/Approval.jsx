import React, { useState } from 'react';
import { post, put, useData } from '../api.js';
import { Button, Card, Note, Pill, Select, ToggleRow, useAction, fmt } from '../ui.jsx';

// New-device approval: a device never seen before waits on a holding network until the person
// decides. Off by default; devices already in the list are never held.
export default function Approval() {
  const { data, reload } = useData('/api/approval', { topics: ['dhcp.leases'], every: 8000 });
  const [run, busy] = useAction();
  const [where, setWhere] = useState({});
  if (!data) return null;
  const nets = data.networks || [];
  const waiting = data.pending || [];
  const target = (id) => where[id] || nets.find((n) => n.iface !== data.holding)?.iface || '';
  const card = (
    <Card title="New devices" desc="A device that has never joined before can wait on a holding network until you decide. Devices you already have are never held.">
      <div className="stack">
        {data.error && <Note tone="warn" title="Could not place devices">{data.error}</Note>}
        {!nets.length && <Note title="No VLAN networks yet">Approval needs a holding network with a VLAN number. Add one on the Networks page, ideally of the Isolated type, with DHCP on.</Note>}
        {!!nets.length && (
          <>
            <ToggleRow title="Hold new devices until I approve them" checked={data.enabled} disabled={busy}
              desc="While this is on, a brand-new device gets an address on the holding network, where it can reach nothing if that network is Isolated. If Gatehouse cannot place it, the device is let onto its own network, so nobody is left without a connection."
              onChange={(v) => run(() => put('/api/approval', { enabled: v, holding: data.holding || nets[0].iface }), v ? 'New devices will wait for you.' : 'New devices join as before.').then(reload)} />
            <label className="field"><span className="label">Holding network</span>
              <Select value={data.holding || ''} onChange={(v) => run(() => put('/api/approval', { enabled: data.enabled, holding: v }), 'Saved.').then(reload)}
                options={[{ value: '', label: 'Choose one' }, ...nets.map((n) => ({ value: n.iface, label: `${n.network} (VLAN ${n.vid})` }))]} />
            </label>
          </>
        )}
        {waiting.length === 0 && data.enabled && <p className="small muted">Nothing is waiting.</p>}
        {waiting.length > 0 && (
          <div className="list">
            {waiting.map((p) => (
              <div className="item" key={p.id} style={{ display: 'block' }}>
                <div className="row" style={{ justifyContent: 'space-between', gap: 8 }}>
                  <div><div className="name">{p.name || p.vendor || p.mac} <Pill tone={p.state === 'blocked' ? 'bad' : 'warn'}>{p.state === 'blocked' ? 'Blocked' : 'Waiting'}</Pill></div>
                    <div className="meta mono">{p.mac}{p.vendor ? ` · ${p.vendor}` : ''} · first seen {fmt.ago(p.first_seen)}</div></div>
                  <div className="inline" style={{ gap: 6, flexWrap: 'wrap' }}>
                    <Button small kind="primary" disabled={busy} onClick={() => run(() => post(`/api/approval/${p.id}/allow`, {}), 'Allowed.').then(reload)}>Allow</Button>
                    {p.state !== 'blocked' && <Button small kind="danger" disabled={busy} onClick={() => run(() => post(`/api/approval/${p.id}/block`, {}), 'Blocked.').then(reload)}>Block</Button>}
                  </div>
                </div>
                {nets.length > 1 && (
                  <div className="inline" style={{ gap: 6, marginTop: 6 }}>
                    <span className="small muted">Or place it on</span>
                    <Select value={target(p.id)} onChange={(v) => setWhere({ ...where, [p.id]: v })} options={nets.filter((n) => n.iface !== data.holding).map((n) => ({ value: n.iface, label: n.network }))} />
                    <Button small disabled={busy || !target(p.id)} onClick={() => run(() => post(`/api/approval/${p.id}/place`, { network: target(p.id) }), 'Placed.').then(reload)}>Place</Button>
                  </div>
                )}
              </div>
            ))}
          </div>
        )}
      </div>
    </Card>
  );
  return !data.enabled && waiting.length === 0 ? <details className="more"><summary>New device approval</summary>{card}</details> : card;
}

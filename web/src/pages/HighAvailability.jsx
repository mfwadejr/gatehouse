import React, { useEffect, useState } from 'react';
import { get, post, put, useData } from '../api.js';
import ApplyBar from '../ApplyBar.jsx';
import FailbackPrompt from './FailbackPrompt.jsx';
import { Button, Card, Field, Input, Note, Pill, Segmented, Select, ToggleRow, useAction, confirmBox } from '../ui.jsx';

// High availability: two Gatehouse machines, one active at a time. The first-time setup is a short
// list of steps; once it runs, this card is the status page.
const guess = (cidr, n) => { const m = /^(\d+\.\d+\.\d+)\.\d+\/\d+$/.exec(cidr || ''); return m ? `${m[1]}.${n}` : ''; };
const ago = (t) => (t ? `${Math.max(0, Math.round(Date.now() / 1000 - t))} seconds ago` : 'not yet');

export default function HighAvailability({ me }) {
  const { data, reload } = useData('/api/ha', { topics: ['fw.status', 'net.status'], every: 5000 });
  const { data: net, reload: rn } = useData('/api/net/status', { topics: ['net.status'], every: 4000 });
  const { data: fwo, reload: rf } = useData('/api/fw/overview', { topics: ['fw.status'] });
  const [run, busy] = useAction();
  const [side, setSide] = useState('main');
  const [f, setF] = useState({ own: '', standby: '' });
  const [code, setCode] = useState('');
  const [look, setLook] = useState(null);
  const [keep, setKeep] = useState({});
  useEffect(() => {
    const n = data?.networks?.[0]?.cidr;
    if (n && !f.own) setF({ own: guess(n, 2), standby: guess(n, 3) });
  }, [data]); // eslint-disable-line react-hooks/exhaustive-deps
  if (!data) return null;
  const all = () => { reload(); rn(); rf(); };

  if (!data.paired) {
    return (
      <Card title="High availability" desc="Run two Gatehouse machines as a pair. If the active one fails, the other takes over in a few seconds, and your devices keep the same gateway.">
        <div className="stack">
          {data.bridge_on && <Note tone="warn" title="Bridge mode is on">Bridge mode and a pair cannot be used together yet. Switch bridge mode off first.</Note>}
          <Segmented value={side} onChange={(v) => { setSide(v); setLook(null); }} options={[{ value: 'main', label: 'This is the main unit' }, { value: 'backup', label: 'This is the backup' }]} />
          {side === 'main' && (
            <>
              <Note title="Before you start">The second machine needs at least as many wired ports as this one uses, on the same switches. Its Wi-Fi is not part of the pair. Each machine gets its own address on your network, and the address your devices use as a gateway becomes shared between them.</Note>
              <div className="formgrid">
                <Field label="This unit's own address" hint="An unused address on your network. The gateway address you use today becomes the shared one."><Input value={f.own} onChange={(v) => setF({ ...f, own: v })} className="in mono" /></Field>
                <Field label="The backup's own address" hint="Another unused address. The backup machine must already have it."><Input value={f.standby} onChange={(v) => setF({ ...f, standby: v })} className="in mono" /></Field>
              </div>
              <div><Button kind="primary" disabled={busy || data.bridge_on || !data.can_start} onClick={() => run(async () => { await post('/api/ha/start', { own_addr: f.own, standby_addr: f.standby }); all(); }, 'Ready. Apply the changes below, then paste the code on the second machine.')}>Start</Button></div>
            </>
          )}
          {side === 'backup' && (
            <>
              <Field label="Pairing code" hint="Shown on the main unit."><Input value={code} onChange={setCode} className="in mono" placeholder="GATEHOUSE-PAIR-1:..." /></Field>
              <div><Button disabled={busy || !code} onClick={() => run(async () => { setLook(await post('/api/ha/discover', { code })); setKeep({}); })}>Check the main unit</Button></div>
              {look && (
                <div className="stack">
                  {look.warning && <Note tone="warn">{look.warning}</Note>}
                  {look.refused && <Note tone="bad" title="This machine cannot be the backup">{look.refused}</Note>}
                  {look.mapping && (
                    <>
                      <strong>Match the ports</strong>
                      <div className="list">
                        {look.mapping.map((m) => (
                          <div className="item" key={m.role} style={{ display: 'block' }}>
                            <div className="spread">
                              <div><div className="name">{m.label}</div><div className="meta">Main unit port {m.primary}</div></div>
                              <Select value={keep[m.role] || m.backup} onChange={(v) => setKeep({ ...keep, [m.role]: v })} options={look.ports.filter((p) => !p.wifi).map((p) => ({ value: p.name, label: `${p.name}${p.speed > 0 ? ` (${p.speed} Mbps)` : ' (no cable)'}` }))} />
                            </div>
                            {m.warning && <div className="small" style={{ color: 'var(--warn, #d9a441)' }}>{m.warning}</div>}
                          </div>
                        ))}
                      </div>
                      <div><Button kind="primary" disabled={busy} onClick={() => run(async () => { await post('/api/ha/join', { code, keep }); all(); }, 'Joined. The settings are being copied across.')}>Join as the backup</Button></div>
                    </>
                  )}
                </div>
              )}
            </>
          )}
        </div>
      </Card>
    );
  }

  const peer = data.peer || {};
  const mism = peer.reachable && peer.version && peer.version !== data.version;
  if (!data.activated) {
    const waiting = data.role === 'standby' && !data.synced_at;
    return (
      <Card title="Finish setting up the pair" desc={data.role === 'primary' ? 'Main unit' : 'Backup'} actions={<Button small kind="danger" onClick={async () => { if (await confirmBox('Cancel the pairing on this unit?')) run(async () => { await post('/api/ha/unpair', {}); all(); }); }}>Cancel</Button>}>
        <div className="stack">
          {data.role === 'primary' && data.code && (
            <>
              <Field label="1. Paste this code on the second machine (Settings > Advanced > High availability > This is the backup)"><Input value={data.code} onChange={() => {}} className="in mono" readOnly onFocus={(e) => e.target.select()} /></Field>
              <div className="small muted">{data.joined ? 'The backup has joined.' : 'Waiting for the backup to join.'}</div>
            </>
          )}
          {waiting && <Note title="Waiting for the settings">The main unit sends them within a few seconds. Sign in again if this page asks you to: the accounts are copied too.</Note>}
          {!waiting && (
            <>
              <strong>{data.role === 'primary' ? '2' : '1'}. Apply the changes</strong>
              <ApplyBar kind="net" label="your network settings" status={net?.status} me={me} onChanged={all} />
              {!net?.status?.dirty && !net?.status?.pending && fwo?.status && <ApplyBar kind="fw" label="the firewall" status={fwo.status} me={me} onChanged={all} />}
              <strong>{data.role === 'primary' ? '3' : '2'}. Turn it on</strong>
              <div><Button kind="primary" disabled={busy || !data.joined || net?.status?.dirty || fwo?.status?.dirty || net?.status?.pending || fwo?.status?.pending} onClick={() => run(async () => { await post('/api/ha/activate', {}); all(); }, 'High availability is on.')}>Turn on high availability</Button></div>
            </>
          )}
          {data.error && <Note tone="warn">{data.error}</Note>}
        </div>
      </Card>
    );
  }

  return (
    <Card title="High availability" desc="Two units, one active at a time." actions={data.active ? <Pill tone="good">This unit is active</Pill> : <Pill tone="warn">This unit is the standby</Pill>}>
      <div className="stack">
        <FailbackPrompt ha={data} onChanged={all} canHide={false} />
        {mism && <Note tone="warn" title="Different versions">This unit runs {data.version} and its partner runs {peer.version}. Update the older one so both match.</Note>}
        {!peer.reachable && <Note tone="bad" title="The partner cannot be reached">If this unit is the standby it will take over by itself when the active one is really down. {peer.error || ''}</Note>}
        {data.error && <Note tone="warn">{data.error}</Note>}
        <div className="list">
          <div className="item"><div><div className="name">This unit ({data.primary ? 'main' : 'backup'}) {data.active ? <Pill tone="good">Active</Pill> : <Pill>Standby</Pill>}</div><div className="meta">{data.own_addr} · version {data.version}</div></div></div>
          <div className="item"><div><div className="name">Partner {peer.reachable ? (peer.active ? <Pill tone="good">Active</Pill> : <Pill>Standby</Pill>) : <Pill tone="bad">Not reachable</Pill>}</div><div className="meta">{data.peer_addr}{peer.version ? ` · version ${peer.version}` : ''}</div></div></div>
          <div className="item"><div><div className="name">Shared addresses</div><div className="meta">{(data.vips || []).map((v) => `${v.cidr} on ${v.iface}`).join(' · ')}</div></div></div>
          <div className="item"><div><div className="name">Settings copied</div><div className="meta">{ago(data.synced_at)}. Changes are made on the active unit and copied across.</div></div></div>
        </div>
        <ToggleRow title="Keep the spare's internet connection ready" desc="The spare keeps its internet port connected but silent, copies the active unit's hardware address and takes over faster. Set it on the active unit. A login connection (PPPoE) and a Wi-Fi connection are not part of this; they start fresh on takeover." checked={!!data.warm} disabled={busy || !data.active} onChange={(v) => run(async () => { await put('/api/ha/warm', { on: v }); all(); }, v ? 'The spare is kept ready.' : 'Back to keeping the spare\'s internet port down.')} />
        <Field label="Cable between the two units (optional)" hint="Plug a cable between one spare port on each unit and choose that port on both. The two units then talk over the cable (settings copy, updates, connection copy) and fall back to your shared network if it is unplugged. An unplugged cable never makes either unit take over.">
          <div className="inline">
            <Select value={data.heart?.dev || ''} onChange={(v) => run(async () => { await put('/api/ha/heart', { dev: v }); all(); }, v ? 'The cable port is set on this unit. Set the other unit too.' : 'The cable is no longer used.')}
              options={[{ value: '', label: 'No cable' }, ...((data.heart_choices || []).concat(data.heart?.dev && !(data.heart_choices || []).includes(data.heart.dev) ? [data.heart.dev] : [])).map((p) => ({ value: p, label: p }))]} />
            {data.heart && <Pill tone={data.heart.connected ? 'good' : 'warn'}>{data.heart.connected ? 'Cable connected' : 'Cable not connected'}</Pill>}
          </div>
        </Field>
        <div className="inline">
          {data.active && <Button disabled={busy} onClick={() => run(async () => { await post('/api/ha/sync', {}); all(); }, 'Sent.')}>Copy settings now</Button>}
          {!data.active && <Button kind="primary" disabled={busy} onClick={async () => { if (await confirmBox('Make this unit the active one? Connections may pause for a few seconds.', { title: 'Switch units', yes: 'Switch' })) run(async () => { await post('/api/ha/failover', { code: '' }); all(); }, 'The active role is moving to this unit.'); }}>Make this unit active</Button>}
          <Button kind="danger" disabled={busy} onClick={async () => { if (await confirmBox('Remove the pair? Both units become ordinary single units. Addresses are not changed back for you.', { title: 'Remove the pair', yes: 'Remove' })) run(async () => { await post('/api/ha/unpair', {}); all(); }, 'Removed.'); }}>Remove the pair</Button>
        </div>
        <span className="small muted">Failover is automatic. When the main unit comes back, it stays standby until you choose: fail back to it, or keep the other unit as the main unit. Nothing moves by itself. The standby keeps its internet ports down until it takes over, so the provider sees one machine; the first moments after a switch include getting the internet address again. VPN tunnels reconnect after a switch.</span>
      </div>
    </Card>
  );
}

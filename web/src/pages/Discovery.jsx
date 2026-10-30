import React, { useEffect, useState } from 'react';
import { put, useData } from '../api.js';
import { Button, Card, Empty, Note, Select, useAction } from '../ui.jsx';

// Devices finding each other across networks: printers, casting, AirPlay, Home Assistant. Pick
// pairs of networks. Gatehouse copies the announcements; what the devices may then do is still
// decided by the firewall rules between the two networks.

export default function Discovery({ reload }) {
  const { data, reload: again } = useData('/api/fw/discovery');
  const [pairs, setPairs] = useState(null);
  const [kinds, setKinds] = useState([]);
  const [a, setA] = useState('');
  const [b, setB] = useState('');
  const [run, busy] = useAction();
  useEffect(() => { if (data && !pairs) { setPairs(data.pairs); setKinds(data.kinds); } }, [data, pairs]);
  if (!data || !pairs) return <Card title="Let networks see each other's devices"><span className="muted">Loading…</span></Card>;
  const nets = data.networks.map((n) => ({ value: n, label: n }));
  const dirty = JSON.stringify(pairs) !== JSON.stringify(data.pairs) || JSON.stringify([...kinds].sort()) !== JSON.stringify([...data.kinds].sort());
  const add = () => {
    if (!a || !b || a === b) return;
    if (pairs.some((p) => (p.a === a && p.b === b) || (p.a === b && p.b === a))) return;
    setPairs([...pairs, { a, b }]);
    setA(''); setB('');
  };
  const save = () => run(async () => { await put('/api/fw/discovery', { pairs, kinds }); await again(); reload?.(); }, 'Saved. Apply the firewall to start using it.');
  const tick = (id, v) => setKinds(v ? [...kinds, id] : kinds.filter((k) => k !== id));
  return (
    <Card title="Let networks see each other's devices" desc="Printers, Chromecast, AirPlay and Home Assistant find each other by announcing themselves, which normally stops at the edge of a network. Pair two networks and Gatehouse passes those announcements across.">
      <div className="stack">
        {data.networks.length < 2 ? <Empty>You need at least two networks. Add another on the Networks page.</Empty> : (
          <>
            <div className="list">
              {pairs.map((p, i) => (
                <div className="item" key={`${p.a}-${p.b}`}>
                  <div><div className="name">{p.a} ↔ {p.b}</div><div className="meta">Devices on each can see announcements from the other.</div></div>
                  <div className="actions"><Button small kind="danger" icon="trash" onClick={() => setPairs(pairs.filter((_, j) => j !== i))}>Remove</Button></div>
                </div>
              ))}
              {!pairs.length && <Empty>No networks are paired. Each network only sees its own devices.</Empty>}
            </div>
            <div className="addrow" style={{ gridTemplateColumns: 'minmax(0, 1fr) minmax(0, 1fr) auto', alignItems: 'end' }}>
              <div><div className="small muted">First network</div><Select value={a} onChange={setA} options={[{ value: '', label: 'Choose…' }, ...nets]} /></div>
              <div><div className="small muted">Second network</div><Select value={b} onChange={setB} options={[{ value: '', label: 'Choose…' }, ...nets.filter((n) => n.value !== a)]} /></div>
              <Button disabled={!a || !b || a === b} onClick={add}>Pair them</Button>
            </div>
            <div>
              <strong>What to pass across</strong>
              <p className="small muted">Leave all unticked to pass everything. Ticking some passes only those kinds.</p>
              {data.catalog.map((k) => (
                <label key={k.id} className="small" style={{ display: 'flex', gap: 8, alignItems: 'center', margin: '4px 0' }}>
                  <input type="checkbox" checked={kinds.includes(k.id)} onChange={(e) => tick(k.id, e.target.checked)} /> {k.label}
                </label>
              ))}
            </div>
            <Note tone="warn" title="This only lets devices find each other">
              Seeing a printer is not the same as being allowed to use it. If the two networks are kept apart by the firewall, add a rule on the Rules tab for what you want to allow, such as printing. Passing announcements also tells every device on a paired network what exists on the other, so do not pair your guests with your home.
            </Note>
            <div className="actions"><Button kind="primary" onClick={save} disabled={busy || !dirty}>Save</Button></div>
          </>
        )}
      </div>
    </Card>
  );
}

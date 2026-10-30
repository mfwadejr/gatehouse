import React from 'react';
import { put, useData } from '../api.js';
import { Card, Note, Pill, ToggleRow, useAction } from '../ui.jsx';

// Traffic labels: a small separate program watches copies of the packets on your inside ports and
// names the application behind each connection. It never changes or blocks traffic.
export default function TrafficLabels() {
  const { data, reload } = useData('/api/ndpi', { topics: ['fw.status'], every: 15000 });
  const [run, busy] = useAction();
  if (!data) return null;
  const warns = data.warnings || [];
  return (
    <Card title="Traffic labels" desc="Names the app behind each connection, such as YouTube, Zoom or BitTorrent, even when the device never asked Gatehouse for a website name.">
      <div className="stack">
        {!data.installed && <Note title="Not on this system">The labelling program comes with a fresh install image built with it. Updates alone do not add it. The name-based app labels in the log keep working either way.</Note>}
        <ToggleRow title="Label traffic by looking at it" disabled={busy || !data.installed}
          desc="Reads the first packets of each connection on your inside ports. It needs some processor time and is the most private thing Gatehouse does, because it sees which apps each person uses. Off by default."
          checked={data.enabled} onChange={(v) => run(() => put('/api/ndpi', { enabled: v }), v ? 'Traffic labels are on.' : 'Traffic labels are off.').then(reload)} />
        {data.enabled && (
          <p className="small muted">
            {data.connected ? <Pill tone="good">Running</Pill> : <Pill tone="warn">Not connected yet</Pill>} {data.seen} connections labelled so far, on {(data.ifaces || []).join(', ') || 'no ports'}.
          </p>
        )}
        {data.enabled && warns.length > 0 && (
          <div className="list">
            {warns.map((w, i) => <div className="item" key={i}><div><div className="name">{w.src} to {w.dst}:{w.dp} <Pill tone="warn">Caution</Pill></div><div className="meta">{w.app}: {(w.risk || []).join('; ')}</div></div></div>)}
          </div>
        )}
        <p className="small muted">If the program stops or cannot read a connection, Gatehouse falls back to the website name. Nothing about your internet depends on it.</p>
      </div>
    </Card>
  );
}

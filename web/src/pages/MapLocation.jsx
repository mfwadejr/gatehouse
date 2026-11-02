import React, { useState } from 'react';
import { put, useData } from '../api.js';
import { Button, Card, Field, Note, Select, useAction } from '../ui.jsx';
import { COUNTRIES, MAP_H, MAP_W } from '../worldmap.js';
import { centreOf } from '../mapgeo.js';

// Where "you" are on the Overview map. Click the map, or pick a country from the list. It is kept
// only on this machine. Without it the lines start from the middle of the country the browser is
// set to.
export default function MapLocation() {
  const { data, reload } = useData('/api/overview/home');
  const [run, busy] = useAction();
  const [pt, setPt] = useState(null);
  if (!data) return null;
  const shown = pt || (data.set ? [data.x, data.y] : null);
  const place = (e) => {
    const r = e.currentTarget.getBoundingClientRect();
    setPt([Math.round(((e.clientX - r.left) / r.width) * MAP_W), Math.round(((e.clientY - r.top) / r.height) * MAP_H)]);
  };
  const options = [{ value: '', label: 'Choose a country…' }, ...Object.entries(COUNTRIES).filter(([cc]) => cc !== 'AQ').map(([cc, c]) => ({ value: cc, label: c.name })).sort((a, b) => a.label.localeCompare(b.label))];
  return (
    <Card title="Your place on the map" desc="Where the lines on the Overview map start. It stays on this machine and is not sent anywhere.">
      <div className="stack">
        <Field label="Pick a country" hint="Or click the map below to place yourself exactly.">
          <Select value="" onChange={(cc) => { const c = cc && centreOf(cc); if (c) setPt([Math.round(c[0]), Math.round(c[1])]); }} options={options} aria-label="Country" />
        </Field>
        <div className="ov-map" style={{ cursor: 'crosshair' }}>
          <svg viewBox={`0 0 ${MAP_W} ${MAP_H}`} role="img" aria-label="World map. Click to place yourself." onClick={place}>
            {Object.entries(COUNTRIES).filter(([cc]) => cc !== 'AQ').map(([cc, c]) => <path key={cc} d={c.d} />)}
            {shown && <g className="ov-home out" pointerEvents="none"><circle className="ov-ping" cx={shown[0]} cy={shown[1]} r="4" /><circle className="ov-pin" cx={shown[0]} cy={shown[1]} r="4.5" /></g>}
          </svg>
        </div>
        {!shown && <Note>No place set. The Overview uses the middle of the country your browser is set to.</Note>}
        <div className="actions">
          <Button kind="primary" disabled={busy || !pt} onClick={() => run(() => put('/api/overview/home', { x: pt[0], y: pt[1] }), 'Saved.').then(() => { setPt(null); reload(); })}>Save my place</Button>
          <Button disabled={busy || !data.set} onClick={() => run(() => put('/api/overview/home', { clear: true }), 'Back to the default.').then(() => { setPt(null); reload(); })}>Use the default</Button>
        </div>
      </div>
    </Card>
  );
}

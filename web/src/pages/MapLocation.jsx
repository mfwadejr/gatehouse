import React, { useState } from 'react';
import { post, put, useData } from '../api.js';
import { Button, Card, Field, Input, Note, Select, useAction } from '../ui.jsx';
import { COUNTRIES, MAP_H, MAP_W } from '../worldmap.js';
import { centreOf, project, unproject } from '../mapgeo.js';

// Where "you" are on the Overview map. Click the map, or pick a country from the list. It is kept
// only on this machine. Without it the lines start from the middle of the country the browser is
// set to.
export default function MapLocation() {
  const { data, reload } = useData('/api/overview/home');
  const [run, busy] = useAction();
  const [pt, setPt] = useState(null);
  const [ll, setLl] = useState({ lat: '', lon: '' });
  const [q, setQ] = useState('');
  const [found, setFound] = useState(null);
  if (!data) return null;
  const setFromLatLon = (next) => {
    setLl(next);
    const la = Number(next.lat), lo = Number(next.lon);
    if (next.lat !== '' && next.lon !== '' && Number.isFinite(la) && Number.isFinite(lo) && Math.abs(la) <= 90 && Math.abs(lo) <= 180) setPt(project(la, lo));
  };
  const find = () => run(async () => { const r = await post('/api/overview/find', { q }); setFound(r.results || []); }, '');
  const pick = (h) => { setPt(project(h.lat, h.lon)); setLl({ lat: String(h.lat), lon: String(h.lon) }); setFound(null); };
  const shown = pt || (data.set ? [data.x, data.y] : null);
  const place = (e) => {
    const r = e.currentTarget.getBoundingClientRect();
    setPt([Math.round(((e.clientX - r.left) / r.width) * MAP_W), Math.round(((e.clientY - r.top) / r.height) * MAP_H)]);
  };
  const options = [{ value: '', label: 'Choose a country…' }, ...Object.entries(COUNTRIES).filter(([cc]) => cc !== 'AQ').map(([cc, c]) => ({ value: cc, label: c.name })).sort((a, b) => a.label.localeCompare(b.label))];
  return (
    <Card title="Your place on the map" desc="Where the lines on the Overview map start. Search for a zip code, city or address, type your latitude and longitude, click the map, or pick a country. It stays on this machine and is not sent anywhere.">
      <div className="stack">
        <Field label="Find a place" hint="A zip or postal code, a city, or a street address. Only what you type is sent, to OpenStreetMap's search, and only when you press Find. Your saved place is never sent.">
          <div className="actions">
            <Input value={q} onChange={setQ} placeholder="10001, Lisbon, or 1 Main St, Springfield" onKeyDown={(e) => { if (e.key === 'Enter' && q.trim() && !busy) find(); }} />
            <Button disabled={busy || q.trim().length < 2} onClick={find}>Find</Button>
          </div>
        </Field>
        {found && found.length === 0 && <Note>Nothing was found for that. Try the city and country, or type the latitude and longitude.</Note>}
        {found && found.length > 0 && (
          <div className="stack">
            {found.map((h, i) => <button type="button" key={i} className="btn leftalign" onClick={() => pick(h)}>{h.name}</button>)}
          </div>
        )}
        <Field label="Pick a country" hint="Or type your coordinates, or click the map below.">
          <Select value="" onChange={(cc) => { const c = cc && centreOf(cc); if (c) setPt([Math.round(c[0]), Math.round(c[1])]); }} options={options} aria-label="Country" />
        </Field>
        <div className="formgrid">
          <Field label="Latitude" hint="Degrees, north is positive. For example 40.71."><Input value={ll.lat} onChange={(v) => setFromLatLon({ ...ll, lat: v })} placeholder="40.71" inputMode="decimal" /></Field>
          <Field label="Longitude" hint="Degrees, east is positive. For example -74.01."><Input value={ll.lon} onChange={(v) => setFromLatLon({ ...ll, lon: v })} placeholder="-74.01" inputMode="decimal" /></Field>
        </div>
        {shown && <p className="small muted" style={{ margin: 0 }}>Placed at about {unproject(shown[0], shown[1]).map((n) => n.toFixed(1)).join(', ')} (latitude, longitude).</p>}
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

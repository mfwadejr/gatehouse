import React, { useState } from 'react';
import { post, put, useData } from '../api.js';
import { Button, Card, Field, Input, Note, Pill, Segmented, Select, useAction } from '../ui.jsx';
import { COUNTRIES, MAP_H, MAP_W } from '../worldmap.js';
import { centreOf, project, unproject } from '../mapgeo.js';

// Where "you" are on the Overview map. Four ways to say it (search, country, coordinates, click the
// map); all of them end in the same pin and the same Save button. It is kept only on this machine.
// Without it the lines start from the middle of the country the browser is set to.
const TABS = [
  { value: 'search', label: 'Search' },
  { value: 'country', label: 'Country' },
  { value: 'coords', label: 'Coordinates' },
  { value: 'click', label: 'Click the map' },
];

const two = (n) => Number(n).toFixed(1);

export default function MapLocation() {
  const { data, reload } = useData('/api/overview/home');
  const [run, busy] = useAction();
  const [tab, setTab] = useState('search');
  const [pt, setPt] = useState(null); // the new choice, not saved yet
  const [label, setLabel] = useState('');
  const [ll, setLl] = useState({ lat: '', lon: '' });
  const [q, setQ] = useState('');
  const [found, setFound] = useState(null);
  const [hi, setHi] = useState(0);
  const [editing, setEditing] = useState(false);
  if (!data) return null;
  const saved = data.set ? [data.x, data.y] : null;
  const reset = () => { setPt(null); setLabel(''); setLl({ lat: '', lon: '' }); setFound(null); setQ(''); setHi(0); };
  const choose = (h) => { setPt(project(h.lat, h.lon)); setLl({ lat: String(h.lat), lon: String(h.lon) }); setLabel(h.name); setFound(null); };
  const find = () => run(async () => {
    const r = await post('/api/overview/find', { q });
    const list = r.results || [];
    setHi(0);
    if (list.length === 1) { choose(list[0]); setFound(null); } else setFound(list);
  }, '');
  const setFromLatLon = (next) => {
    setLl(next);
    const la = Number(next.lat), lo = Number(next.lon);
    if (next.lat !== '' && next.lon !== '' && Number.isFinite(la) && Number.isFinite(lo) && Math.abs(la) <= 90 && Math.abs(lo) <= 180) { setPt(project(la, lo)); setLabel(''); }
  };
  const place = (e) => {
    if (tab !== 'click') return;
    const r = e.currentTarget.getBoundingClientRect();
    const p = [Math.round(((e.clientX - r.left) / r.width) * MAP_W), Math.round(((e.clientY - r.top) / r.height) * MAP_H)];
    setPt(p);
    const [la, lo] = unproject(p[0], p[1]);
    setLl({ lat: two(la), lon: two(lo) });
    setLabel('');
  };
  const countries = Object.entries(COUNTRIES).filter(([cc]) => cc !== 'AQ').map(([cc, c]) => ({ value: cc, label: c.name }))
    .sort((a, b) => (a.value === 'US' ? -1 : b.value === 'US' ? 1 : a.label.localeCompare(b.label)));
  const options = [{ value: '', label: 'Choose a country…' }, ...countries];
  if (!editing) {
    const [la, lo] = data.set ? unproject(data.x, data.y).map(two) : [];
    return (
      <Card title="Your place on the map" desc="Where the lines on the Overview map start."
        actions={<><Pill tone={data.set ? 'good' : ''}>{data.set ? 'Configured' : 'Not set'}</Pill><Button small onClick={() => setEditing(true)}>{data.set ? 'Change' : 'Set my place'}</Button></>}>
        <p className="small muted" style={{ margin: 0 }}>{data.set ? `Placed at about ${la}, ${lo} (latitude, longitude). It stays on this machine.` : 'No place set. The Overview uses the middle of the country your browser is set to.'}</p>
      </Card>
    );
  }
  const preview = found && found.length ? project(found[Math.min(hi, found.length - 1)].lat, found[Math.min(hi, found.length - 1)].lon) : null;
  const typed = ll.lat !== '' && ll.lon !== '' && Number.isFinite(Number(ll.lat)) && Number.isFinite(Number(ll.lon));
  const here = pt ? (typed ? [two(ll.lat), two(ll.lon)] : unproject(pt[0], pt[1]).map(two)) : null;
  const savedAt = saved ? unproject(saved[0], saved[1]).map(two) : null;
  const close = () => { reset(); setEditing(false); };
  return (
    <Card title="Your place on the map" desc="Where the lines on the Overview map start. Pick the way that suits you; they all end the same way. It stays on this machine.">
      <div className="stack">
        <Segmented value={tab} onChange={setTab} options={TABS} />

        {tab === 'search' && (
          <>
            <Field label="Find a place" hint="A zip or postal code, a city, or a street address, then press Find. Only the words you type are sent, to OpenStreetMap's search, and only when you press Find. Your saved place is never sent.">
              <div className="actions">
                <Input value={q} onChange={setQ} placeholder="10001, Lisbon, or 1 Main St, Springfield" onKeyDown={(e) => { if (e.key === 'Enter' && q.trim().length >= 2 && !busy) find(); }} />
                <Button kind="primary" disabled={busy || q.trim().length < 2} onClick={find}>Find</Button>
              </div>
            </Field>
            {found && found.length === 0 && <Note>Nothing was found for that. Try the city and country, or use the Coordinates tab.</Note>}
            {found && found.length > 1 && (
              <div className="stack">
                <span className="small muted">Pick the right one. The map shows where each one is.</span>
                {found.map((h, i) => (
                  <button type="button" key={i} className={`btn leftalign ${i === hi ? 'on' : ''}`} onMouseEnter={() => setHi(i)} onFocus={() => setHi(i)} onClick={() => choose(h)}>{h.name}</button>
                ))}
              </div>
            )}
          </>
        )}

        {tab === 'country' && (
          <Field label="Pick a country" hint="The pin goes to the middle of the country. Use Search or Coordinates for something closer.">
            <Select value="" onChange={(cc) => { const c = cc && centreOf(cc); if (c) { setPt([Math.round(c[0]), Math.round(c[1])]); setLabel(COUNTRIES[cc]?.name || ''); const [la, lo] = unproject(c[0], c[1]); setLl({ lat: two(la), lon: two(lo) }); } }} options={options} aria-label="Country" />
          </Field>
        )}

        {tab === 'coords' && (
          <div className="formgrid">
            <Field label="Latitude" hint="Degrees, north is positive. For example 40.71."><Input value={ll.lat} onChange={(v) => setFromLatLon({ ...ll, lat: v })} placeholder="40.71" inputMode="decimal" /></Field>
            <Field label="Longitude" hint="Degrees, east is positive. For example -74.01."><Input value={ll.lon} onChange={(v) => setFromLatLon({ ...ll, lon: v })} placeholder="-74.01" inputMode="decimal" /></Field>
          </div>
        )}

        {tab === 'click' && <p className="small muted" style={{ margin: 0 }}>Click anywhere on the map to put the pin there.</p>}

        {here && (
          <div className="mapplace-new" role="status">
            <b>{label || 'Your new place'}</b><br />
            <span>Not saved yet: {here.join(', ')} (latitude, longitude)</span>
          </div>
        )}

        <div className="ov-map" style={{ cursor: tab === 'click' ? 'crosshair' : 'default' }}>
          <svg viewBox={`0 0 ${MAP_W} ${MAP_H}`} role="img" aria-label="World map. In the Click the map tab, click to place yourself." onClick={place}>
            {Object.entries(COUNTRIES).filter(([cc]) => cc !== 'AQ').map(([cc, c]) => <path key={cc} d={c.d} />)}
            {saved && <circle className="mapplace-saved" cx={saved[0]} cy={saved[1]} r="4.5" pointerEvents="none" />}
            {preview && <circle className="mapplace-preview" cx={preview[0]} cy={preview[1]} r="7" pointerEvents="none" />}
            {pt && (
              <g pointerEvents="none">
                <circle className="mapplace-halo" cx={pt[0]} cy={pt[1]} r="14" />
                <circle className="mapplace-pin" cx={pt[0]} cy={pt[1]} r="6.5" />
                <g transform={`translate(${Math.min(pt[0] + 12, MAP_W - 84)},${Math.max(pt[1] - 30, 4)})`}>
                  <rect className="mapplace-tag" width="76" height="20" rx="5" />
                  <text x="38" y="14" textAnchor="middle" className="mapplace-tagtext">Your place</text>
                </g>
              </g>
            )}
          </svg>
        </div>
        <p className="small muted" style={{ margin: 0 }}>
          {savedAt ? `Saved now: about ${savedAt.join(', ')}. It shows as the small grey ring.` : 'No place saved yet. The Overview uses the middle of the country your browser is set to.'}
        </p>

        <div className="actions">
          <Button kind="primary" disabled={busy || !pt} onClick={() => run(() => put('/api/overview/home', { x: pt[0], y: pt[1] }), 'Saved.').then(() => { close(); reload(); })}>{here ? `Save ${here.join(', ')}` : 'Save my place'}</Button>
          <Button onClick={close}>Cancel</Button>
          <Button disabled={busy || !data.set} onClick={() => run(() => put('/api/overview/home', { clear: true }), 'Back to the default.').then(() => { close(); reload(); })}>Use the default</Button>
        </div>
      </div>
    </Card>
  );
}

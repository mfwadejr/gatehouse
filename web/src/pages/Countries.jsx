import React, { useEffect, useMemo, useState } from 'react';
import { post, put, useData } from '../api.js';
import { CODES, countryName } from '../AddrPicker.jsx';
import { Button, Card, Input, Note, Pill, Segmented, ToggleRow, useAction, confirmBox } from '../ui.jsx';

const flag = (cc) => String.fromCodePoint(...[...cc].map((c) => 0x1f1e6 + c.charCodeAt(0) - 65));
const ALL = CODES.map((c) => ({ cc: c, name: countryName(c) })).sort((a, b) => a.name.localeCompare(b.name));

// One card: block countries, or allow only some, in one direction or both. The rules it makes are
// ordinary firewall rules (shown, read-only, under Rules > Show system rules).
export default function Countries({ reload }) {
  const { data, reload: again } = useData('/api/fw/countries', { every: 15000 });
  const [p, setP] = useState(null);
  const [q, setQ] = useState('');
  const [run, busy] = useAction();
  useEffect(() => { if (data && !p) setP({ ...data.policy, countries: data.policy.countries || [] }); }, [data, p]);
  const sel = useMemo(() => new Set(p?.countries || []), [p]);
  const shown = useMemo(() => {
    const n = q.trim().toLowerCase();
    return ALL.filter((c) => !n || c.name.toLowerCase().includes(n) || c.cc.toLowerCase() === n);
  }, [q]);
  if (!p) return <Card title="Block or allow countries"><span className="muted">Loading…</span></Card>;
  const home = data.home;
  const max = data.max || 40;
  const set = (patch) => setP((x) => ({ ...x, ...patch }));
  const toggle = (cc) => set({ countries: sel.has(cc) ? p.countries.filter((c) => c !== cc) : [...p.countries, cc] });
  const save = async (confirm) => {
    try {
      await put('/api/fw/countries', { ...p, confirm });
      await again();
      reload?.();
      return true;
    } catch (e) {
      const m = e.message || '';
      if (m.startsWith('needs-confirm:') && !confirm) {
        if (await confirmBox(m.replace('needs-confirm:', '').trim() + '\nYou could lose your own access to some sites and services.', { title: 'This includes your own country', yes: 'Save anyway' })) return save(true);
        return false;
      }
      throw e;
    }
  };
  const doSave = () => run(() => save(false), 'Saved. Apply the firewall to start using it.');
  const preset = () => home && set({ enabled: true, mode: 'allow', dir: 'both', countries: [home] });
  const verb = p.mode === 'allow' ? 'Only these countries are allowed' : 'These countries are blocked';
  const dirText = { both: 'both ways', out: 'for what your devices send out', in: 'for what arrives from the internet' }[p.dir];
  const over = p.countries.length > max;
  return (
    <Card title="Block or allow countries" desc="Stop traffic to or from whole countries. The country comes from public address registries, so VPN and hosting addresses can be wrong.">
      {!data.ready && <Note tone="warn" title="Country data not downloaded yet">Gatehouse is fetching the address lists. This can take a few minutes. <button type="button" className="linkbtn" onClick={() => post('/api/fw/geo/refresh').then(again)}>Download now</button></Note>}
      <ToggleRow title="Use country rules" desc={p.enabled ? `${verb}, ${dirText}.` : 'Off. Nothing is blocked by country.'} checked={!!p.enabled} onChange={(v) => set({ enabled: v })} />
      <div className="countries-opts">
        <div><div className="small muted">What to do</div><Segmented value={p.mode} onChange={(v) => set({ mode: v })} options={[{ value: 'block', label: 'Block these' }, { value: 'allow', label: 'Allow only these' }]} /></div>
        <div><div className="small muted">Which traffic</div><Segmented value={p.dir} onChange={(v) => set({ dir: v })} options={[{ value: 'both', label: 'Both ways' }, { value: 'out', label: 'Going out' }, { value: 'in', label: 'Coming in' }]} /></div>
      </div>
      {p.mode === 'allow' && (
        <Note tone="warn" title="Allow only is strict">
          Anything not in your list is cut off, including the many sites that use servers in other countries (video, updates, apps). Your own country{home ? ` (${countryName(home)})` : ''} should be in the list.
        </Note>
      )}
      {p.dir !== 'out' && <p className="small muted">Coming in also covers the ports you opened on purpose (port forwards and VPN servers), so people in a blocked country cannot reach them.</p>}
      <div className="inline" style={{ margin: 'var(--space-2) 0' }}>
        {home && <Button small onClick={preset}>Only allow {countryName(home)}</Button>}
        {p.countries.length > 0 && <Button small onClick={() => set({ countries: [] })}>Clear the list</Button>}
        <span className="small muted">{p.countries.length} chosen{over ? ` (up to ${max}; allow-only is better for longer lists)` : ''}</span>
      </div>
      {p.countries.length > 0 && (
        <div className="chips" style={{ marginBottom: 'var(--space-2)' }}>
          {p.countries.map((cc) => <button type="button" key={cc} className="chip" onClick={() => toggle(cc)} title="Remove">{flag(cc)} {countryName(cc)} ✕</button>)}
        </div>
      )}
      <Input value={q} onChange={setQ} placeholder="Search countries" aria-label="Search countries" />
      <div className="countrylist" role="group" aria-label="Countries">
        {shown.map((c) => (
          <label key={c.cc} className="countryrow">
            <input type="checkbox" checked={sel.has(c.cc)} onChange={() => toggle(c.cc)} />
            <span aria-hidden="true">{flag(c.cc)}</span> <span>{c.name}</span>{c.cc === home && <Pill>You are here</Pill>}
          </label>
        ))}
        {!shown.length && <span className="muted small">Nothing matches.</span>}
      </div>
      <div className="inline" style={{ marginTop: 'var(--space-3)' }}>
        <Button kind="primary" disabled={busy || over} onClick={doSave}>Save</Button>
        <span className="small muted">Your own networks and VPN far ends are never matched. Rules appear under Rules, Show system rules.</span>
      </div>
    </Card>
  );
}

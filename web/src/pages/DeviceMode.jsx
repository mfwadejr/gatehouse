import React, { useEffect, useRef, useState } from 'react';
import { post, put, useData } from '../api.js';
import { Button, Card, Field, Input, Modal, Note, Pill, Segmented, useAction, useToast, usePortNames } from '../ui.jsx';
import { BridgePending } from './BridgeMode.jsx';

// Device mode: Router or Bridge, with plain cable instructions. Nothing is saved until the last
// step; applying uses the same 60-second keep-or-undo as every other network change.

// Watches the ports and reports each cable as it is plugged in, in order.
function usePlugs() {
  const { data } = useData('/api/net/ports', { every: 1500 });
  const base = useRef(null);
  const [plugs, setPlugs] = useState([]);
  useEffect(() => {
    if (!Array.isArray(data)) return;
    const now = Object.fromEntries(data.map((p) => [p.name, p.link]));
    if (base.current) {
      const hits = data.filter((p) => p.link && !p.wifi && base.current[p.name] === false).map((p) => p.name);
      if (hits.length) setPlugs((x) => [...x, ...hits]);
    }
    base.current = now;
  }, [data]);
  return { ports: Array.isArray(data) ? data : [], plugs };
}

// A list of ports to pick from, the fallback when a cable is already in.
function PortList({ ports, value, onPick, off = [], pn, here }) {
  return (
    <div className="stack">
      {ports.filter((p) => !p.wifi).map((p) => {
        const gone = off.includes(p.name);
        return (
          <button type="button" key={p.name} disabled={gone} className={`preset ${value === p.name ? 'on' : ''}`} aria-pressed={value === p.name} onClick={() => onPick(p.name)}>
            <span className="t">{pn(p.name)} {p.link ? <Pill tone="good">cable connected</Pill> : <Pill>no cable</Pill>}{here.includes(p.name) && <Pill tone="info">you are connected here</Pill>}</span>
          </button>
        );
      })}
    </div>
  );
}

const sentenceList = (a) => (a.length < 2 ? a.join('') : `${a.slice(0, -1).join(', ')} and ${a[a.length - 1]}`);

function Walk({ data, onClose, onApplied }) {
  const pn = usePortNames();
  const toast = useToast();
  const [run, busy] = useAction();
  const { ports, plugs } = usePlugs();
  const here = data.current || [];
  const live = data.bridge;
  const draft = data.draft || {};
  const [step, setStep] = useState(1);
  const [mode, setMode] = useState(data.mode);
  const [internet, setInternet] = useState('');
  const [outside, setOutside] = useState(live?.enabled ? live.outside : '');
  const [inside, setInside] = useState(live?.enabled ? live.inside : here);
  const [oob, setOob] = useState(live?.enabled ? live.oob || '' : '');
  const [target, setTarget] = useState(outside ? 'inside' : 'outside');
  const [addr, setAddr] = useState({ mode: draft.mode || 'static', addr: draft.addr || '', gateway: draft.gateway || '', dns: draft.dns || '' });
  const seen = useRef(0);
  const wired = ports.filter((p) => !p.wifi);

  // Hand each newly detected cable to what the person is plugging in now.
  useEffect(() => {
    while (seen.current < plugs.length) {
      const n = plugs[seen.current]; seen.current += 1;
      if (here.includes(n) && step !== 3) continue;
      if (step !== 2) continue;
      if (mode === 'router') { if (data.mode === 'router') setInternet(n); continue; }
      if (target === 'outside') { setOutside(n); setInside((x) => x.filter((v) => v !== n)); setTarget('inside'); }
      else if (target === 'inside') { if (n !== outside && n !== oob) setInside((x) => (x.includes(n) ? x : [...x, n])); }
      else if (target === 'oob') { setOob(n); setInside((x) => x.filter((v) => v !== n)); setTarget('inside'); }
    }
  }, [plugs, step]); // eslint-disable-line react-hooks/exhaustive-deps

  const toggleInside = (n) => setInside((x) => (x.includes(n) ? x.filter((v) => v !== n) : [...x, n]));
  const addrOK = addr.mode === 'dhcp' || (/^\d+\.\d+\.\d+\.\d+\/\d+$/.test(addr.addr) && /^\d+\.\d+\.\d+\.\d+$/.test(addr.gateway));
  const canGo = () => {
    if (step === 1) return !data.paired && !!mode;
    if (step === 2) {
      if (mode === 'bridge') return !!outside && inside.length > 0 && !here.includes(outside);
      if (data.mode === 'router') return !internet || !here.includes(internet);
      return true;
    }
    return mode === 'router' || addrOK;
  };

  const apply = async () => {
    if (mode === 'bridge') {
      const base = { enabled: true, outside: '', inside: [], oob: '', oob_addr: '', mode: 'static', addr: '', gateway: '', dns: '', subnets: [], allow: [], open: false, ...(live?.enabled ? live : draft) };
      const body = { ...base, enabled: true, outside, inside, oob, oob_addr: oob ? (base.oob_addr || '192.168.99.1/24') : '', mode: addr.mode, addr: addr.addr, gateway: addr.gateway, dns: addr.dns };
      const ok = await run(async () => { await put('/api/bridge', body); await post('/api/bridge/apply', { code: '' }); return true; });
      if (ok) { toast('Applied. You have 60 seconds to keep it; otherwise everything goes back by itself.'); onApplied(); }
      return;
    }
    if (data.mode === 'bridge') {
      const ok = await run(async () => { await put('/api/bridge', { ...live, enabled: false }); await post('/api/bridge/apply', { code: '' }); return true; });
      if (ok) { toast('Going back to routing. You have 60 seconds to keep it; otherwise it goes back by itself.'); onApplied(); }
      return;
    }
    if (!internet || internet === data.wan?.iface) { toast('Nothing to change. Gatehouse is already set up this way.'); onClose(); return; }
    const r = await run(() => put('/api/mode/internet-port', { iface: internet }));
    if (r) { toast('Saved. Press Review and apply at the top of the page to start using it; you can undo within 60 seconds.'); onApplied(); }
  };

  const title = `Device mode · Step ${step} of 3`;
  const footer = (
    <>
      <Button onClick={onClose}>Cancel</Button>
      {step > 1 && <Button onClick={() => setStep(step - 1)}>Back</Button>}
      {step < 3 ? <Button kind="primary" disabled={!canGo()} onClick={() => setStep(step + 1)}>Next</Button> : <Button kind="primary" disabled={busy || !canGo()} onClick={apply}>{mode === 'bridge' || data.mode === 'bridge' ? 'Apply' : 'Save'}</Button>}
    </>
  );
  const hereNote = here.length > 0 && (
    <Note tone="good" title="Leave this cable plugged in">You are connected to Gatehouse through {sentenceList(here.map(pn))}. It is never used for the internet or the other router.</Note>
  );

  return (
    <Modal title={title} onClose={onClose} footer={footer}>
      {step === 1 && (
        <div className="stack">
          <p className="sec">How should this box work? Nothing changes until the last step, and Cancel changes nothing.</p>
          {data.paired && <Note tone="warn" title="This box is in a pair">Mode changes are not allowed while it is part of a high-availability pair. Take it out of the pair first (Settings, Advanced, High availability).</Note>}
          <button type="button" className={`preset ${mode === 'router' ? 'on' : ''}`} aria-pressed={mode === 'router'} onClick={() => setMode('router')}>
            <span className="t">Router {data.mode === 'router' && <Pill tone="info">current</Pill>}</span>
            <span className="d">Gatehouse connects you to the internet, hands out addresses (DHCP), runs DNS and the firewall.</span>
          </button>
          <button type="button" className={`preset ${mode === 'bridge' ? 'on' : ''}`} aria-pressed={mode === 'bridge'} onClick={() => setMode('bridge')}>
            <span className="t">Bridge {data.mode === 'bridge' && <Pill tone="info">current</Pill>}</span>
            <span className="d">Gatehouse sits between your existing router and your devices and filters what passes. Your other router keeps handing out addresses.</span>
          </button>
        </div>
      )}
      {step === 2 && mode === 'router' && data.mode === 'bridge' && (
        <div className="stack">
          <p className="sec">Gatehouse goes back to routing with the settings you had before bridge mode. They were kept.</p>
          <Note title="Cables for a router">
            {data.wan ? `Plug your modem or provider into ${pn(data.wan.iface)} (${data.wan.name}).` : 'No internet connection is saved. After this, add one on the Internet page.'}{' '}
            {data.lans.length > 0 && `Plug your switch or devices into ${sentenceList(data.lans.map(pn))}.`}
          </Note>
          {hereNote}
        </div>
      )}
      {step === 2 && mode === 'router' && data.mode === 'router' && (
        <div className="stack">
          <p className="sec">Plug the cable from your modem or provider into the port you want for the internet. Gatehouse notices which port it is.</p>
          {hereNote}
          {internet ? <Note tone="good" title="Internet port">{pn(internet)}{internet === data.wan?.iface ? ' (the same as now)' : ''}</Note> : <Note title="Waiting for a cable">Nothing new plugged in yet. Keep the current port by pressing Next, or pick the port below if the cable is already in.</Note>}
          <PortList ports={wired} value={internet || data.wan?.iface} onPick={setInternet} off={here} pn={pn} here={here} />
          <p className="small muted">Your devices stay on {data.lans.length ? sentenceList(data.lans.map(pn)) : 'their current ports'}. To move them, use the Networks page.</p>
        </div>
      )}
      {step === 2 && mode === 'bridge' && (
        <div className="stack">
          {hereNote}
          <div className="stack">
            <strong>1. The cable from your other router</strong>
            <p className="small muted">Plug it in now and Gatehouse notices the port.</p>
            {outside ? <Note tone="good" title="Faces your other router">{pn(outside)}</Note> : <Note title="Waiting for a cable">{target === 'outside' ? 'Listening…' : ''}</Note>}
            <PortList ports={wired} value={outside} onPick={(n) => { setOutside(n); setInside((x) => x.filter((v) => v !== n)); setOob((x) => (x === n ? '' : x)); setTarget('inside'); }} off={here} pn={pn} here={here} />
          </div>
          <div className="stack">
            <strong>2. Your devices</strong>
            <p className="small muted">Plug in each switch or computer, or tick the ports below.</p>
            <div className="stack" style={{ gap: 'var(--space-1)' }}>
              {wired.filter((p) => p.name !== outside && p.name !== oob).map((p) => (
                <label key={p.name} className="inline"><input type="checkbox" checked={inside.includes(p.name)} onChange={() => toggleInside(p.name)} /> {pn(p.name)}{here.includes(p.name) ? ' (you are connected here)' : ''}</label>
              ))}
            </div>
          </div>
          <div className="stack">
            <strong>3. A management port (recommended, optional)</strong>
            <p className="small muted">A spare port that is never part of the bridge, so you can always reach this box from it. You can skip it.</p>
            {oob ? <Note tone="good" title="Management port">{pn(oob)} <Button small onClick={() => setOob('')}>Remove</Button></Note> : (
              <div className="inline"><Button small onClick={() => setTarget('oob')}>{target === 'oob' ? 'Listening… plug it in now' : 'Plug in a management port'}</Button></div>
            )}
          </div>
        </div>
      )}
      {step === 3 && (
        <div className="stack">
          <strong>{mode === 'bridge' ? 'Bridge' : 'Router'}: your cabling</strong>
          {mode === 'bridge' ? (
            <ul className="small">
              <li>{pn(outside)}: the cable from your other router.</li>
              <li>{sentenceList(inside.map(pn))}: your switch or devices.</li>
              {oob && <li>{pn(oob)}: for managing this box only.</li>}
            </ul>
          ) : data.mode === 'bridge' ? (
            <ul className="small">
              <li>{data.wan ? `${pn(data.wan.iface)}: your modem or provider.` : 'No internet connection is saved yet.'}</li>
              {data.lans.length > 0 && <li>{sentenceList(data.lans.map(pn))}: your switch or devices.</li>}
            </ul>
          ) : (
            <ul className="small">
              <li>{pn(internet || data.wan?.iface)}: your modem or provider{internet && internet !== data.wan?.iface ? ' (moved from ' + pn(data.wan?.iface) + ')' : ''}.</li>
              <li>{data.lans.length ? sentenceList(data.lans.map(pn)) : 'Your network ports'}: your switch or devices, unchanged.</li>
            </ul>
          )}
          {mode === 'bridge' && (
            <>
              <Note title="Addresses (DHCP) stay with your other router">Bridge mode does not hand out addresses. Your other router keeps doing that, so nothing on your network changes. Gatehouse itself needs one address to be managed at.</Note>
              <Field label="Gatehouse's own address"><Segmented value={addr.mode} onChange={(v) => setAddr((x) => ({ ...x, mode: v }))} options={[{ value: 'static', label: 'Fixed (recommended)' }, { value: 'dhcp', label: 'From your other router' }]} /></Field>
              {addr.mode === 'static' && (
                <div className="formgrid">
                  <Field label="Address" hint="An unused address on your network, with its size."><Input value={addr.addr} onChange={(v) => setAddr((x) => ({ ...x, addr: v }))} placeholder="192.168.1.2/24" className="in mono" /></Field>
                  <Field label="Your other router's address"><Input value={addr.gateway} onChange={(v) => setAddr((x) => ({ ...x, gateway: v }))} placeholder="192.168.1.1" className="in mono" /></Field>
                </div>
              )}
            </>
          )}
          <p className="small muted">{mode === 'router' && data.mode === 'router' ? 'This is only saved here; apply it from the bar at the top of the page.' : 'After you press Apply you have 60 seconds. If you can still reach this page, the change is kept on its own; if not, everything goes back by itself.'}</p>
        </div>
      )}
    </Modal>
  );
}

export default function DeviceMode() {
  const { data, reload } = useData('/api/mode', { topics: ['fw.status', 'net.status'], every: 4000 });
  const [open, setOpen] = useState(false);
  if (!data) return null;
  const pair = () => { window.location.hash = '#/settings/advanced'; setTimeout(() => document.getElementById('set-ha')?.scrollIntoView({ behavior: 'smooth' }), 200); };
  return (
    <Card title="Device mode" desc="How this box works on your network."
      actions={<Pill tone={data.mode === 'bridge' ? 'info' : 'good'}>{data.mode === 'bridge' ? 'Bridge' : 'Router'}</Pill>}>
      <div className="stack">
        {data.pending ? <BridgePending pending={data.pending} reload={reload} /> : (
          <>
            {data.undone && <Note tone="warn" title="The last change was not kept">It was undone on its own, and your previous settings are back.</Note>}
            <p>{data.headline}</p>
            <div className="inline">
              <Button kind="primary" onClick={() => setOpen(true)}>Change</Button>
              <button type="button" className="link small" onClick={pair}>Using two boxes? Set up a pair</button>
            </div>
            {data.paired && <p className="small muted">This box is in a pair, so its mode cannot be changed from here.</p>}
          </>
        )}
      </div>
      {open && <Walk data={data} onClose={() => setOpen(false)} onApplied={() => { setOpen(false); reload(); }} />}
    </Card>
  );
}

import React, { useState } from 'react';
import { del, put, useData } from '../api.js';
import { AddrPicker, tokens } from '../AddrPicker.jsx';
import { Button, Card, Empty, Field, Input, Note, Pill, Select, ToggleRow, useAction, fmt } from '../ui.jsx';

// Forced DNS: devices that ask a DNS server of their own are quietly sent to the one you chose. It is
// set for each network, with optional exceptions for single devices or groups.

const MODES = [
  { value: 'force', label: 'Force' },
  { value: 'server', label: 'Force to a different server' },
  { value: 'off', label: 'Leave alone' },
];

const EX_MODES = [
  { value: 'off', label: 'Leave alone' },
  { value: 'gatehouse', label: 'Force to Gatehouse' },
  { value: 'server', label: 'Force to a different server' },
];

function NetworkRow({ n, target, onSave }) {
  const [mode, setMode] = useState(n.mode);
  const [addr, setAddr] = useState(n.target || '');
  React.useEffect(() => { setMode(n.mode); setAddr(n.target || ''); }, [n.mode, n.target]);
  const choose = (v) => {
    setMode(v);
    if (v !== 'server') onSave({ id: n.id, mode: v, target: '' });
  };
  const commit = () => { if (mode === 'server' && addr.trim() && addr.trim() !== n.target) onSave({ id: n.id, mode: 'server', target: addr.trim() }); };
  return (
    <div className="item" style={{ alignItems: 'flex-start' }}>
      <div>
        <div className="name">{n.name}</div>
        <div className="meta">
          {n.mode === 'off' && 'Devices here may use any DNS server.'}
          {n.mode === 'force' && `Every lookup goes to ${target || 'Gatehouse'}.`}
          {n.mode === 'server' && `Every lookup goes to ${n.target}.`}
          {!n.internet && n.mode !== 'off' && ' This network has no internet, so it always uses Gatehouse.'}
        </div>
      </div>
      <div className="actions" style={{ flexWrap: 'wrap' }}>
        {mode === 'server' && (
          <Input value={addr} onChange={setAddr} placeholder="192.168.1.2" aria-label={`DNS server for ${n.name}`} style={{ width: '10rem' }}
            onBlur={commit} onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); e.stopPropagation(); commit(); } }} />
        )}
        <Select value={mode} aria-label={`Forced DNS on ${n.name}`} onChange={choose} options={MODES} />
      </div>
    </div>
  );
}

export default function ForcedDns({ s, save, pickers, reload }) {
  const { data, reload: again } = useData('/api/fw/dnsforce');
  const [run, busy] = useAction();
  const [adding, setAdding] = useState(false);
  const [who, setWho] = useState('');
  const [mode, setMode] = useState('off');
  const [srv, setSrv] = useState('');
  const refresh = () => { again(); reload?.(); };
  const saveNet = async (b) => { await run(() => put('/api/fw/dnsforce/network', b), 'Saved. Apply the firewall to put it into effect.'); refresh(); };
  const label = (w) => {
    if (w.startsWith('grp:')) return `Group ${w.slice(4)}`;
    const mac = w.slice(4);
    return pickers?.devices?.find((d) => d.mac === mac)?.name || mac;
  };
  const gone = (w) => (w.startsWith('grp:') ? !pickers?.groups?.some((g) => `grp:${g.name}` === w) : !pickers?.devices?.some((d) => `dev:${d.mac}` === w));
  const addEx = async () => {
    const list = tokens(who).filter((t) => t.startsWith('dev:') || t.startsWith('grp:'));
    if (!list.length) return;
    let allOk = true;
    for (const t of list) {
      const r = await run(() => put('/api/fw/dnsforce/exception', { who: t, mode, target: mode === 'server' ? srv.trim() : '' }));
      if (!r) { allOk = false; break; }
    }
    if (allOk) { setAdding(false); setWho(''); setMode('off'); setSrv(''); refresh(); }
  };
  const nets = data?.networks || [];
  const covered = nets.filter((n) => n.mode !== 'off').map((n) => n.name);
  return (
    <Card title="Forced DNS" desc="Smart TVs, cameras and some phones carry their own DNS server and skip your filters. Forced DNS sends their lookups to the server you choose.">
      <ToggleRow title="Force devices to use one DNS server" desc="Turn off to leave every network alone. The settings for each network and device are kept." checked={s.dns_redirect} onChange={save('dns_redirect')} />
      {s.dns_redirect && <DnsTargetPicker s={s} save={save('dns_target')} />}
      {s.dns_redirect && (
        <>
          <div className="small muted" style={{ marginTop: 'var(--space-3)' }}>
            {covered.length ? `Forced on ${covered.join(', ')}.` : 'No network is forced.'}
            {data?.redirected > 0 ? ` ${fmt.n(data.redirected)} lookups sent to the chosen server so far.` : ''}
          </div>
          <div className="list">
            {nets.map((n) => <NetworkRow key={n.id} n={n} target={s.dns_target} onSave={saveNet} />)}
            {!nets.length && <Empty>No networks yet.</Empty>}
          </div>
        </>
      )}
      <div className="stack" style={{ marginTop: 'var(--space-4)' }}>
        <b>Exceptions</b>
        {!(data?.exceptions || []).length && !adding && <span className="small muted">None. A device or group can be left alone, or sent to a different server, whatever its network says.</span>}
        <div className="list">
          {(data?.exceptions || []).map((e) => (
            <div className="item" key={e.id}>
              <div>
                <div className="name">{label(e.who)} {gone(e.who) && <Pill tone="warn">No longer exists</Pill>}</div>
                <div className="meta">{e.mode === 'off' ? 'Left alone: may use any DNS server.' : e.mode === 'gatehouse' ? 'Always sent to Gatehouse.' : `Always sent to ${e.target}.`}</div>
              </div>
              <div className="actions"><Button small kind="danger" icon="trash" onClick={async () => { await run(() => del(`/api/fw/dnsforce/exception/${e.id}`)); refresh(); }}>Remove</Button></div>
            </div>
          ))}
        </div>
        {!adding && <button type="button" className="link small" style={{ textAlign: 'left' }} onClick={() => setAdding(true)}>Add an exception for a device or group</button>}
        {adding && (
          <div className="stack" style={{ gap: 'var(--space-3)' }}>
            <Field label="Device or group"><AddrPicker side="src" value={who} onChange={setWho} pickers={pickers} local reload={() => {}} /></Field>
            <Field label="What happens to its lookups"><Select value={mode} onChange={setMode} options={EX_MODES} /></Field>
            {mode === 'server' && <Field label="DNS server address" hint="Make sure the device can reach it from its network."><Input value={srv} onChange={setSrv} placeholder="192.168.1.2" /></Field>}
            <div className="actions"><Button kind="primary" disabled={busy || !tokens(who).some((t) => t.startsWith('dev:') || t.startsWith('grp:')) || (mode === 'server' && !srv.trim())} onClick={addEx}>Add exception</Button><Button onClick={() => setAdding(false)}>Cancel</Button></div>
          </div>
        )}
      </div>
      <Note title="What this cannot stop">
        Forced DNS redirects ordinary DNS. A device that sends its lookups hidden inside normal web traffic (DNS-over-HTTPS, which some browsers and phones use) can still go around it. Turn that off in the browser or device if it matters.
      </Note>
    </Card>
  );
}

function DnsTargetPicker({ s, save }) {
  const [mode, setMode] = useState(s.dns_target ? 'custom' : 'self');
  const [addr, setAddr] = useState(s.dns_target || '');
  const choose = (v) => { setMode(v); if (v === 'self') { setAddr(''); if (s.dns_target) save(''); } };
  return (
    <div className="stack">
      <Field label="Forced lookups go to">
        <Select value={mode} onChange={choose} options={[{ value: 'self', label: 'Gatehouse (recommended)' }, { value: 'custom', label: 'A different DNS server' }]} />
      </Field>
      {mode === 'custom' && (
        <Field label="DNS server address" hint="For example your Pi-hole. That server's own lookups are not redirected. Networks without internet access still use Gatehouse.">
          <Input value={addr} onChange={setAddr} onBlur={() => addr.trim() && addr.trim() !== s.dns_target && save(addr.trim())} placeholder="192.168.1.2" />
        </Field>
      )}
    </div>
  );
}

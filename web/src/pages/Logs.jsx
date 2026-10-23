import React, { useCallback, useEffect, useRef, useState } from 'react';
import { del, get, put, useStream } from '../api.js';
import { Button, Card, Empty, Input, Modal, Note, Page, Pill, Segmented, Select, Switch, ToggleRow, useAction, fmt } from '../ui.jsx';

const KINDS = [
  { value: '', label: 'Everything' },
  { value: 'firewall', label: 'Connections' },
  { value: 'dns', label: 'Names (DNS)' },
  { value: 'event', label: 'Events' },
];
const KIND_NAME = { firewall: 'Connection', dns: 'DNS', event: 'Event' };
const VERDICT = { allowed: ['good', 'Allowed'], blocked: ['bad', 'Blocked'], warning: ['warn', 'Problem'], info: ['', 'Info'] };

function Recording({ onClose, state, onSaved }) {
  const [run, busy] = useAction();
  const [f, setF] = useState({ record_allowed: state.record_allowed, record_blocked: state.record_blocked });
  return (
    <Modal title="What gets recorded" onClose={onClose}
      footer={<><Button onClick={onClose}>Cancel</Button><Button kind="primary" disabled={busy} onClick={async () => { if (await run(() => put('/api/logs/settings', f), 'Saved.')) { onSaved(); onClose(); } }}>Save</Button></>}>
      <div className="stack">
        <ToggleRow title="Allowed connections" desc="Records each new connection that the firewall let through, and the rule that allowed it. Turn off to keep the log smaller." checked={f.record_allowed} onChange={(v) => setF({ ...f, record_allowed: v })} />
        <ToggleRow title="Blocked connections" desc="Records each new connection that the firewall stopped, and why." checked={f.record_blocked} onChange={(v) => setF({ ...f, record_blocked: v })} />
        <p className="small muted">Only the first packet of each connection is recorded, and a flood is capped, so a busy network cannot fill the disk. Names asked for through DNS are always recorded.</p>
        <Button kind="danger" onClick={() => window.confirm('Delete every log entry?') && run(() => del('/api/logs'), 'Logs cleared.').then(() => { onSaved(); onClose(); })}>Delete all log entries</Button>
      </div>
    </Modal>
  );
}

export default function Logs() {
  const [q, setQ] = useState('');
  const [kind, setKind] = useState('');
  const [verdict, setVerdict] = useState('');
  const [hours, setHours] = useState('1');
  const [rows, setRows] = useState([]);
  const [more, setMore] = useState(false);
  const [meta, setMeta] = useState({ record_allowed: true, record_blocked: true });
  const [live, setLive] = useState(true);
  const [loading, setLoading] = useState(true);
  const [rec, setRec] = useState(false);
  const t = useRef(null);
  const paged = useRef(false);

  const params = useCallback((before) => {
    const p = new URLSearchParams({ limit: '100', hours });
    if (q) p.set('q', q);
    if (kind) p.set('kind', kind);
    if (verdict) p.set('verdict', verdict);
    if (before) p.set('before', String(before));
    return p;
  }, [q, kind, verdict, hours]);

  const load = useCallback(async () => {
    try {
      const r = await get(`/api/logs?${params()}`);
      setRows(r.items); setMore(r.more); setMeta({ record_allowed: r.record_allowed, record_blocked: r.record_blocked });
      paged.current = false;
    } catch { /* keep what is shown */ }
    setLoading(false);
  }, [params]);

  const older = async () => {
    const last = rows[rows.length - 1];
    if (!last) return;
    const r = await get(`/api/logs?${params(last.ts)}`);
    const seen = new Set(rows.map((x) => x.key));
    setRows((x) => [...x, ...r.items.filter((i) => !seen.has(i.key))]);
    setMore(r.more); paged.current = true;
  };

  useEffect(() => { clearTimeout(t.current); t.current = setTimeout(load, 250); return () => clearTimeout(t.current); }, [load]);
  // New entries arrive a second or two after they happen; reload unless the person is reading older pages.
  const tick = useRef(0);
  useStream(live ? ['logs.live', 'dns.live'] : [], () => {
    if (paged.current || Date.now() - tick.current < 1500) return;
    tick.current = Date.now();
    load();
  });

  const none = !loading && !rows.length;
  return (
    <Page title="Logs" sub="What happened on your network and why: connections the firewall allowed or blocked, names your devices looked up, and changes."
      actions={<><label className="inline small"><Switch checked={live} onChange={setLive} label="Live updates" />Live</label><Button small onClick={() => setRec(true)}>Recording</Button></>}>
      {(!meta.record_allowed || !meta.record_blocked) && (
        <Note tone="warn" title="Some connections are not being recorded">
          {!meta.record_allowed && !meta.record_blocked ? 'Neither allowed nor blocked connections are recorded.' : !meta.record_allowed ? 'Allowed connections are not recorded, so you cannot see why something was let through.' : 'Blocked connections are not recorded, so you cannot see why something was stopped.'}{' '}
          <button className="link" onClick={() => setRec(true)}>Change this</button>
        </Note>
      )}
      <Card>
        <div className="stack">
          <div className="inline">
            <div style={{ flex: '1 1 18rem' }}><Input value={q} onChange={setQ} placeholder="Search a device, address, website, port or reason" aria-label="Search the logs" /></div>
            <div style={{ flex: '0 0 13rem' }}><Select value={verdict} onChange={setVerdict} options={[{ value: '', label: 'Allowed and blocked' }, { value: 'blocked', label: 'Blocked only' }, { value: 'allowed', label: 'Allowed only' }]} aria-label="Result" /></div>
            <Segmented value={hours} onChange={setHours} options={[{ value: '1', label: '1 h' }, { value: '24', label: '24 h' }, { value: '168', label: '7 d' }]} />
          </div>
          <Segmented value={kind} onChange={setKind} options={KINDS} />
        </div>
        <div className="scroll">
          <table className="table">
            <thead><tr><th>Time</th><th>Device</th><th>What</th><th>Result</th><th>Why</th></tr></thead>
            <tbody>
              {rows.map((r) => {
                const [tone, label] = VERDICT[r.verdict] || ['', r.verdict];
                return (
                  <tr key={r.key}>
                    <td className="mono" title={fmt.dt(r.ts)}>{fmt.time(r.ts)}</td>
                    <td>{r.who && r.addr ? <button className="link" title="Search for this device" onClick={() => setQ(r.addr)}>{r.who}</button> : (r.who || <span className="muted">–</span>)}{r.addr && r.addr !== r.who && <div className="small muted mono">{r.addr}</div>}</td>
                    <td style={{ wordBreak: 'break-all' }}>{r.what}<div className="small muted">{KIND_NAME[r.kind]}{r.detail ? ` · ${r.detail}` : ''}</div></td>
                    <td><Pill tone={tone}>{label}</Pill></td>
                    <td className="small sec">{r.why}</td>
                  </tr>
                );
              })}
            </tbody>
          </table>
          {none && <Empty>{q || kind || verdict ? 'Nothing matches that search in this time.' : 'Nothing has been recorded yet.'}</Empty>}
        </div>
        {more && <div className="actions"><Button onClick={older}>Show older</Button></div>}
      </Card>
      {rec && <Recording state={meta} onClose={() => setRec(false)} onSaved={load} />}
    </Page>
  );
}

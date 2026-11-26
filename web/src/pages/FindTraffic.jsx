import React, { useCallback, useEffect, useRef, useState } from 'react';
import { get, post, useData } from '../api.js';
import { hashQuery } from '../hashtab.js';
import { Button, Card, Empty, Field, Input, Note, Pill, Segmented, Select, useAction, useToast, fmt } from '../ui.jsx';
import { fmtBytes } from '../charts.jsx';

// Find traffic: say who and where to, and see what became of the connections between them. It reads
// the firewall log and the connection tracker, so each row says which rule decided and whether the
// other side answered. Live starts at a marked moment and shows new connections as they happen.

const WHEN = [{ value: '15', label: '15 minutes' }, { value: '60', label: 'Hour' }, { value: '1440', label: '24 hours' }, { value: 'live', label: 'Live' }];
const RESULT = [{ value: '', label: 'Anything' }, { value: 'allowed', label: 'Allowed' }, { value: 'blocked', label: 'Blocked' }, { value: 'noreply', label: 'No reply' }];
const LIVE_MAX = 200;
const LIVE_MINUTES = 10;

const outcomeOf = (r) => {
  if (r.verdict === 'blocked') return ['bad', 'Blocked'];
  if (r.outcome === 'Timed out') return ['warn', 'No reply'];
  if (r.outcome === 'Reset') return ['warn', 'Reset'];
  if (r.outcome === 'Still open') return ['info', 'Still open'];
  if (r.outcome === 'Completed') return ['good', 'Completed'];
  return ['', r.verdict === 'allowed' && Date.now() / 1000 - r.ts < 60 ? 'Waiting for reply' : 'Allowed'];
};
const dur = (s) => (s == null ? '–' : s < 1 ? 'under 1 s' : s < 60 ? `${s} s` : s < 3600 ? `${Math.floor(s / 60)} min` : `${Math.floor(s / 3600)} h`);
const bytes = (v) => (v == null ? '–' : fmtBytes(v));

function Rows({ items, onEnd }) {
  if (!items.length) return null;
  return (
    <div className="scroll"><table className="table ft-table">
      <thead><tr><th>Started</th><th>From</th><th>To</th><th>Service</th><th>Rule</th><th>Result</th><th className="num">Sent</th><th className="num">Received</th><th className="num">Lasted</th></tr></thead>
      <tbody>{items.map((r) => {
        const [tone, label] = outcomeOf(r);
        return (
          <tr key={r.key} className={r.fresh ? 'ft-new' : ''}>
            <td className="small mono">{fmt.time(r.ts)}</td>
            <td className="small">{r.who && r.who !== r.src ? <>{r.who}<div className="muted mono">{r.src}</div></> : <span className="mono">{r.src}</span>}</td>
            <td className="small">{r.dst_name ? <>{r.dst_name}<div className="muted mono">{r.dst}</div></> : <span className="mono">{r.dst}</span>}</td>
            <td className="small">{r.service || `${(r.proto || '').toUpperCase()} ${r.port || ''}`}</td>
            <td className="small">{r.rule_name || <span className="muted">{r.why || '–'}</span>}</td>
            <td><Pill tone={tone}>{label}</Pill>{r.before_change && <div className="small muted">Open before your change</div>}</td>
            <td className="num small">{bytes(r.bytes_out)}</td>
            <td className="num small">{bytes(r.bytes_in)}</td>
            <td className="num small">{r.outcome && r.outcome !== 'Blocked' ? dur(r.dur_s) : '–'}</td>
          </tr>
        );
      })}</tbody>
    </table></div>
  );
}

function OpenNow({ src, dst, port }) {
  const toast = useToast();
  const [run, busy] = useAction();
  const { data, reload } = useData(`/api/diag/open?src=${encodeURIComponent(src)}&dst=${encodeURIComponent(dst)}&port=${encodeURIComponent(port)}`, { every: 5000 });
  const end = (c) => run(() => post('/api/diag/open/end', { proto: c.proto, src: c.src, sport: c.sport, dst: c.dst, dport: c.dport }), 'Ended. The next packet starts a new connection under the current rules.').then(reload).catch(() => toast('Could not end it.'));
  if (!data) return null;
  if (data.error) return <Note tone="warn">{data.error}</Note>;
  return (
    <div className="stack">
      <p className="small muted">The connections the firewall is following right now, busiest first. {data.full ? 'Only the first 500 are shown. Narrow it down above. ' : ''}Ending one does not block anything: the next packet is treated as a new connection and meets today's rules.</p>
      {!data.items.length ? <Empty>No open connections match.</Empty> : (
        <div className="scroll"><table className="table">
          <thead><tr><th>From</th><th>To</th><th>Service</th><th>State</th><th className="num">Sent</th><th className="num">Received</th><th /></tr></thead>
          <tbody>{data.items.slice(0, 100).map((c) => (
            <tr key={`${c.proto}${c.src}${c.sport}${c.dst}${c.dport}`}>
              <td className="small">{c.src_name ? <>{c.src_name}<div className="muted mono">{c.src}</div></> : <span className="mono">{c.src}</span>}</td>
              <td className="small">{c.dst_name ? <>{c.dst_name}<div className="muted mono">{c.dst}</div></> : <span className="mono">{c.dst}</span>}</td>
              <td className="small">{c.service || `${c.proto.toUpperCase()} ${c.dport}`}</td>
              <td className="small">{c.unreplied ? 'Waiting for reply' : c.state ? c.state.toLowerCase().replace('_', ' ') : 'Open'}</td>
              <td className="num small">{bytes(c.bytes_out >= 0 ? c.bytes_out : null)}</td>
              <td className="num small">{bytes(c.bytes_in >= 0 ? c.bytes_in : null)}</td>
              <td><Button small disabled={busy} onClick={() => end(c)}>End this connection</Button></td>
            </tr>
          ))}</tbody>
        </table></div>
      )}
    </div>
  );
}

export default function FindTraffic() {
  const [src, setSrc] = useState(hashQuery('src'));
  const [dst, setDst] = useState(hashQuery('dst'));
  const [port, setPort] = useState(hashQuery('port'));
  const [when, setWhen] = useState(hashQuery('when') || '60');
  const [result, setResult] = useState('');
  const [res, setRes] = useState(null);
  const [rows, setRows] = useState([]);
  const [paused, setPaused] = useState(false);
  const [mark, setMark] = useState(0);
  const [stopped, setStopped] = useState(false);
  const [view, setView] = useState('find');
  const [run, busy] = useAction();
  const seen = useRef(new Set());
  const q = useCallback((extra) => {
    const p = new URLSearchParams({ src, dst, port, result, ...extra });
    return `/api/diag/find?${p}`;
  }, [src, dst, port, result]);
  const live = when === 'live';

  const search = () => run(async () => {
    seen.current = new Set();
    setStopped(false);
    if (live) {
      const m = Math.floor(Date.now() / 1000);
      setMark(m); setRows([]);
      const r = await get(q({ since: m }));
      setRes(r);
    } else {
      const r = await get(q({ minutes: when }));
      setRes(r); setRows(r.items || []);
    }
  });

  // Live: ask again every two seconds; new connections go to the top and older ones change in place.
  useEffect(() => {
    if (!live || !mark || paused || stopped) return undefined;
    const t = setInterval(async () => {
      if (Date.now() / 1000 - mark > LIVE_MINUTES * 60) { setStopped(true); return; }
      try {
        const r = await get(q({ since: mark }));
        setRes(r);
        setRows((old) => {
          const fresh = (r.items || []).map((i) => ({ ...i, fresh: !seen.current.has(i.key) && seen.current.size > 0 }));
          (r.items || []).forEach((i) => seen.current.add(i.key));
          return fresh.slice(0, LIVE_MAX);
        });
      } catch (e) { /* the next try will do */ }
    }, 2000);
    return () => clearInterval(t);
  }, [live, mark, paused, stopped, q]);

  useEffect(() => { if (hashQuery('src') || hashQuery('dst')) search(); }, []); // eslint-disable-line react-hooks/exhaustive-deps

  const keepWatching = () => { setMark(Math.floor(Date.now() / 1000) - Math.max(0, 0)); setStopped(false); };
  const hint = 'A device name, an address, a network such as Guests, or a range such as 192.168.20.0/24. Leave empty for anything.';

  return (
    <div className="stack">
      <Card title="Find traffic" desc="See what became of the connections between two places: whether they were allowed, which rule decided, and whether the other side answered.">
        <div className="stack">
          <div className="formgrid">
            <Field label="From" hint={hint}><Input value={src} onChange={setSrc} placeholder="Laptop, 192.168.1.20, Guests" /></Field>
            <Field label="To" hint="An address, a website name, a network, or leave empty. Add a port after a colon, for example 8.8.8.8:53."><Input value={dst} onChange={setDst} placeholder="8.8.8.8, netflix.com, Internet" /></Field>
            <Field label="Port (optional)"><Input value={port} onChange={setPort} inputMode="numeric" placeholder="443" /></Field>
          </div>
          <div className="inline" style={{ flexWrap: 'wrap', gap: 'var(--space-4)' }}>
            <Field label="When"><Segmented value={when} onChange={(v) => { setWhen(v); setRes(null); setRows([]); setMark(0); }} options={WHEN} /></Field>
            <Field label="Result"><Segmented value={result} onChange={setResult} options={RESULT} /></Field>
          </div>
          <div className="inline">
            <Button kind="primary" disabled={busy} onClick={search}>{live ? 'Start watching' : 'Find traffic'}</Button>
            {live && mark > 0 && !stopped && <Button onClick={() => setPaused(!paused)}>{paused ? 'Resume' : 'Pause'}</Button>}
            {live && mark > 0 && <Button onClick={() => { setRows([]); seen.current = new Set(); setMark(Math.floor(Date.now() / 1000)); }}>Clear</Button>}
          </div>
        </div>
      </Card>

      {res && (
        <Card title={live ? `Watching since ${fmt.time(mark)}` : 'What was found'}
          actions={<Segmented value={view} onChange={setView} options={[{ value: 'find', label: 'Connections' }, { value: 'open', label: 'Open now' }]} />}>
          {view === 'find' ? (
            <div className="stack">
              <Note tone={rows.some((r) => r.verdict === 'blocked') && !rows.some((r) => r.verdict === 'allowed') ? 'warn' : 'info'}>{live && !rows.length ? 'Waiting for the next matching connection. Start the connection you want to test now.' : res.sentence}</Note>
              {live && stopped && <Note tone="warn">Stopped after {LIVE_MINUTES} minutes so it does not run forever. <Button small onClick={keepWatching}>Keep watching</Button></Note>}
              {live && paused && <p className="small muted">Paused. New connections are not shown until you resume.</p>}
              {live && !paused && !stopped && <p className="small muted">Updating every two seconds. The newest {LIVE_MAX} are kept. A connection waiting for a reply changes to its result when the other side answers or gives up.</p>}
              <Rows items={rows} />
              {res.full && !live && <p className="small muted">Only the newest {res.max} are shown. Narrow it down with a shorter time, a port or a result.</p>}
              {(res.notes || []).map((n) => <p key={n} className="small muted">{n}</p>)}
            </div>
          ) : <OpenNow src={src} dst={dst} port={port} />}
        </Card>
      )}
    </div>
  );
}

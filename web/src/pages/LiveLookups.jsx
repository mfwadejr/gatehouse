import React, { useEffect, useMemo, useRef, useState } from 'react';
import { useData, useStream } from '../api.js';
import { Button, Pill } from '../ui.jsx';
import SiteDialog from './SiteDialog.jsx';

// The live lookups list on DNS Overview. It opens with the latest 50 lookups already in it, so it
// matches the chart above, keeps up to 200, shows about 15 at a time, can be paused, and folds a
// device asking for the same name the same way in the same second into one row with a count.
const KEEP = 200;
const RESULT = { forwarded: 'Answered', cached: 'From memory', blocked: 'Blocked', local: 'Your names', rate_limited: 'Slowed', failed: 'Failed' };
const tone = (r) => (r === 'blocked' ? 'bad' : r === 'failed' || r === 'rate_limited' ? 'warn' : '');
const clock = (t) => new Date(t * 1000).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' });

// Newest first in, folded rows out.
export function fold(rows) {
  const out = [];
  for (const e of rows) {
    const last = out[out.length - 1];
    if (last && last.ts === e.ts && last.client === e.client && last.qname === e.qname && last.result === e.result) {
      last.count += 1;
      if (e.qtype && !last.types.includes(e.qtype)) last.types.push(e.qtype);
    } else out.push({ ...e, count: 1, types: e.qtype ? [e.qtype] : [] });
  }
  return out;
}

export default function LiveLookups({ minuteTotal = 0, admin = false, onLogs }) {
  const { data: first } = useData('/api/dns/recent?limit=50');
  const [rows, setRows] = useState([]);
  const [paused, setPaused] = useState(false);
  const [frozen, setFrozen] = useState(null);
  const [open, setOpen] = useState(null);
  const [site, setSite] = useState(null);
  const seeded = useRef(false);
  useEffect(() => {
    if (first?.items && !seeded.current) {
      seeded.current = true;
      // A lookup that streamed in before the first load finished is not listed twice.
      setRows((r) => {
        const have = new Map();
        r.forEach((e) => { const k = `${e.ts}|${e.client}|${e.qname}|${e.qtype}|${e.result}`; have.set(k, (have.get(k) || 0) + 1); });
        const older = first.items.filter((e) => { const k = `${e.ts}|${e.client}|${e.qname}|${e.qtype}|${e.result}`; const n = have.get(k) || 0; if (n > 0) { have.set(k, n - 1); return false; } return true; });
        return [...r, ...older].slice(0, KEEP);
      });
    }
  }, [first]);
  useStream(['dns.live'], (t, p) => {
    if (!Array.isArray(p)) return;
    const fresh = p.filter((e) => e.qname !== 'health.nopewarden.invalid').reverse();
    if (fresh.length) setRows((r) => [...fresh, ...r].slice(0, KEEP));
  });
  const shownRows = paused && frozen ? frozen : rows;
  const folded = useMemo(() => fold(shownRows), [shownRows]);
  const waiting = paused && frozen ? rows.length - frozen.length : 0;
  const togglePause = () => { if (paused) { setPaused(false); setFrozen(null); } else { setFrozen(rows); setPaused(true); } };
  const keyOf = (e, i) => `${e.ts}-${e.client}-${e.qname}-${e.result}-${i}`;

  return (
    <div className="livefeed" aria-live="off">
      <div className="lf-head">
        <div className="small muted">
          Newest first. {folded.length ? `${shownRows.length} lookup${shownRows.length === 1 ? '' : 's'} listed, the latest ${KEEP} are kept` : 'Waiting for the next lookup'}
          {minuteTotal > 0 ? `; ${minuteTotal.toLocaleString()} in the last minute` : ''}. Older ones are in{' '}
          <button type="button" className="link" onClick={onLogs}>Logs</button>.
        </div>
        <Button small onClick={togglePause}>{paused ? `Resume${waiting > 0 ? ` (${waiting} new)` : ''}` : 'Pause'}</Button>
      </div>
      {!folded.length ? <div className="small muted">Waiting for the next lookup.</div> : (
        <div className="lf-box">
          {folded.map((e, i) => {
            const k = keyOf(e, i);
            const on = open === k;
            return (
              <div key={k} className={`lf-item${on ? ' on' : ''}`}>
                <button type="button" className="lf-row" aria-expanded={on} onClick={() => setOpen(on ? null : k)}>
                  <span>{clock(e.ts)}</span>
                  <span className="lf-who">{e.client_name || e.client}</span>
                  <span className="lf-name">{e.qname}{e.count > 1 ? <span className="lf-count"> ×{e.count}</span> : null}</span>
                  <span className="lf-type">{e.types.join(' ')}</span>
                  <Pill tone={tone(e.result)}>{RESULT[e.result] || e.result}</Pill>
                </button>
                {on && (
                  <div className="lf-why small">
                    <div>{e.why || 'No reason was recorded for this one.'}</div>
                    <div className="muted">{e.client_name ? `${e.client_name} (${e.client})` : e.client} asked for <span className="mono">{e.qname}</span> at {clock(e.ts)}{e.latency_us > 0 ? `; answered in ${e.latency_us >= 1000 ? `${Math.round(e.latency_us / 1000)} ms` : 'under 1 ms'}` : ''}{e.upstream ? ` by ${e.upstream}` : ''}.</div>
                    {admin && e.result !== 'local' && <div><Button small onClick={() => setSite({ what: e.qname, verdict: e.result === 'blocked' ? 'blocked' : 'allowed', addr: e.client })}>{e.result === 'blocked' ? 'Allow this site' : 'Block this site'}</Button></div>}
                  </div>
                )}
              </div>
            );
          })}
        </div>
      )}
      {site && <SiteDialog row={site} onClose={() => setSite(null)} />}
    </div>
  );
}

import React, { useEffect, useState } from 'react';
import { get } from '../api.js';
import { hashQuery, setHashQuery } from '../hashtab.js';
import { Button, Card, Empty, Input, Pill, Segmented, fmt } from '../ui.jsx';

// Highlight the words that were searched for inside a row.
export function Hit({ text, words }) {
  const t = String(text ?? '');
  const ws = (words || []).filter(Boolean);
  if (!ws.length) return t;
  const re = new RegExp(`(${ws.map((w) => w.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')).join('|')})`, 'ig');
  return t.split(re).map((p, i) => (i % 2 ? <mark key={i}>{p}</mark> : p));
}

const RANGES = [{ value: '0', label: 'All' }, { value: '1', label: '1 h' }, { value: '24', label: '24 h' }, { value: '168', label: '7 d' }, { value: '720', label: '30 d' }];

// Admin activity: who did what to Gatehouse, when, and from which address.
export default function AdminActivity({ onQ }) {
  const [q, setQ] = useState(() => hashQuery('q'));
  useEffect(() => { setHashQuery('q', q.trim()); }, [q]);
  const [hours, setHours] = useState('0');
  const [d, setD] = useState(null);
  const [limit, setLimit] = useState(100);
  useEffect(() => {
    let live = true;
    const t = setTimeout(async () => {
      try { const r = await get(`/api/logs/admin?q=${encodeURIComponent(q)}&hours=${hours}&limit=${limit}`); if (live) setD(r); } catch { /* shown as empty */ }
    }, 250);
    return () => { live = false; clearTimeout(t); };
  }, [q, hours, limit]);
  const words = q.trim().split(/\s+/).filter(Boolean);
  const setBoth = (v) => { setQ(v); onQ?.(v); };
  return (
    <Card title="Admin activity" desc="Who did what to Gatehouse: sign-ins and failed attempts, and the changes people made. Looking for what the network did? That is the Log tab.">
      <div className="stack">
        <div className="inline">
          <div style={{ flex: '1 1 18rem' }}><Input value={q} onChange={setBoth} placeholder="Search a person, what they did, or an address" aria-label="Search admin activity" /></div>
          <Segmented value={hours} onChange={(v) => { setHours(v); setLimit(100); }} options={RANGES} />
        </div>
        {d && <p className="small muted">{words.length || hours !== '0' ? `${fmt.n(d.total)} match` : `${fmt.n(d.total)} entries`}. Searched the last {d.days} days, the time Gatehouse keeps its logs.</p>}
        {!d ? <span className="muted">Loading…</span> : !d.rows.length ? (
          <Empty>{words.length ? 'Nothing matches. System messages about Gatehouse itself are in Diagnostics, System log.' : 'Nothing recorded yet.'}</Empty>
        ) : (
          <div className="scroll">
            <table className="table">
              <thead><tr><th>When</th><th>Who</th><th>What</th><th>Details</th></tr></thead>
              <tbody>
                {d.rows.map((r) => (
                  <tr key={r.id}>
                    <td className="small">{fmt.dt(r.ts)}</td>
                    <td><Hit text={r.who || 'Gatehouse'} words={words} /></td>
                    <td>{r.severity === 'warning' || r.severity === 'error' ? <Pill tone={r.severity === 'error' ? 'bad' : 'warn'}>{r.severity === 'error' ? 'Problem' : 'Check'}</Pill> : null} <Hit text={r.what} words={words} /></td>
                    <td className="small muted"><Hit text={r.detail} words={words} /></td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        {d && d.rows.length < d.total && <div className="actions"><Button small onClick={() => setLimit(limit + 200)}>Show more</Button></div>}
      </div>
    </Card>
  );
}

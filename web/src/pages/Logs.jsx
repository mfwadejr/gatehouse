import React, { useEffect, useRef, useState } from 'react';
import { del, get, useStream } from '../api.js';
import { Button, Card, Empty, Input, Page, Pill, Segmented, Select, Switch, useAction, fmt } from '../ui.jsx';

const RESULT = { forwarded: ['', 'Answered'], cached: ['', 'From memory'], blocked: ['bad', 'Blocked'], local: ['good', 'Local'], rate_limited: ['warn', 'Slowed down'], failed: ['bad', 'Failed'] };

export default function Logs() {
  const [q, setQ] = useState('');
  const [result, setResult] = useState('');
  const [hours, setHours] = useState('1');
  const [rows, setRows] = useState([]);
  const [more, setMore] = useState(false);
  const [live, setLive] = useState(true);
  const [loading, setLoading] = useState(true);
  const [run] = useAction();
  const t = useRef(null);

  const load = async (before) => {
    const p = new URLSearchParams({ limit: '100', hours });
    if (q) p.set('q', q);
    if (result) p.set('result', result);
    if (before) p.set('before', String(before));
    const r = await get(`/api/dns/log?${p}`);
    setMore(r.length === 100);
    setRows((x) => (before ? [...x, ...r] : r));
    setLoading(false);
  };
  useEffect(() => { clearTimeout(t.current); t.current = setTimeout(() => load().catch(() => setLoading(false)), 250); return () => clearTimeout(t.current); /* eslint-disable-next-line */ }, [q, result, hours]);
  useStream(live ? ['dns.live'] : [], (_, batch) => {
    if (!Array.isArray(batch) || q || result) return;
    setRows((x) => [...batch.slice().reverse(), ...x].slice(0, 500));
  });

  return (
    <Page title="Logs" sub="Every DNS question your devices have asked, and how it was answered."
      actions={<><label className="inline small"><Switch checked={live} onChange={setLive} label="Live updates" />Live</label><Button small kind="danger" onClick={() => window.confirm('Delete the whole query history?') && run(() => del('/api/dns/log'), 'History cleared.').then(() => load())}>Clear history</Button></>}>
      <Card>
        <div className="inline">
          <div style={{ flex: '1 1 14rem' }}><Input value={q} onChange={setQ} placeholder="Search a name or device" aria-label="Search" /></div>
          <Select value={result} onChange={setResult} options={[{ value: '', label: 'All results' }, { value: 'blocked', label: 'Blocked' }, { value: 'forwarded', label: 'Answered' }, { value: 'cached', label: 'From memory' }, { value: 'local', label: 'Local' }, { value: 'rate_limited', label: 'Slowed down' }, { value: 'failed', label: 'Failed' }]} aria-label="Result" />
          <Segmented value={hours} onChange={setHours} options={[{ value: '1', label: '1 h' }, { value: '24', label: '24 h' }, { value: '168', label: '7 d' }]} />
        </div>
        <div className="scroll">
          <table className="table">
            <thead><tr><th>Time</th><th>Device</th><th>Name asked for</th><th>Type</th><th>Result</th><th className="r">Time taken</th></tr></thead>
            <tbody>
              {rows.map((r, i) => {
                const [tn, label] = RESULT[r.result] || ['', r.result];
                return (
                  <tr key={`${r.id || r.ts}-${i}`}>
                    <td className="mono">{fmt.time(r.ts)}</td>
                    <td>{r.client_name || r.client}</td>
                    <td style={{ wordBreak: 'break-all' }}>{r.qname}</td>
                    <td className="mono">{r.qtype}</td>
                    <td><Pill tone={tn}>{label}</Pill>{r.rule && <span className="small muted"> {r.rule}</span>}</td>
                    <td className="r mono">{r.latency_us ? `${fmt.d1(r.latency_us / 1000)} ms` : ''}</td>
                  </tr>
                );
              })}
            </tbody>
          </table>
          {!loading && !rows.length && <Empty>Nothing matches yet.</Empty>}
        </div>
        {more && <div className="actions"><Button onClick={() => load(rows[rows.length - 1]?.id || rows[rows.length - 1]?.ts)}>Show older</Button></div>}
      </Card>
    </Page>
  );
}

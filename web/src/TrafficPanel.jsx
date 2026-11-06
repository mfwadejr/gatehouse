import React, { useMemo, useState } from 'react';
import { useData } from './api.js';
import { Card, Segmented } from './ui.jsx';
import { Chart, fmtBits, fmtBytes } from './charts.jsx';

// Real-time speed, a graph and totals for one VPN or one network: the same view the internet
// connections have on the Overview. The minute ranges come from a once-a-second record kept for the
// last 15 minutes; the longer ones from the stored history (averages, so short bursts are smoothed).
const RANGES = [['5m', '5 min'], ['15m', '15 min'], ['1h', '1 hour'], ['24h', '24 hours'], ['7d', '7 days'], ['30d', '30 days']];
const LIVE = { '5m': 300, '15m': 900 };
const WORDS = { '5m': 'the last 5 minutes', '15m': 'the last 15 minutes', '1h': 'the last hour', '24h': 'the last 24 hours', '7d': 'the last 7 days', '30d': 'the last 30 days' };
const sumOf = (a) => a.reduce((x, y) => x + y, 0);

export default function TrafficPanel({ kind, id, histName, title = 'Traffic', sub, downWord = 'Download', upWord = 'Upload' }) {
  const [range, setRange] = useState('15m');
  const live = !!LIVE[range];
  const { data: lv } = useData(`/api/traffic/live?kind=${kind}`, { every: live ? 2000 : 10000 });
  const { data: hist } = useData(`/api/overview/history?range=${live ? '1h' : range}`, { every: live ? 0 : 30000 });
  const prefix = kind === 'vpn' ? `vpn.${id}` : `net.${id}`;
  const view = useMemo(() => {
    let d = [], u = [], step = 30, from = 0, to = 1;
    if (live) {
      const s = lv?.series?.[id] || { down: [], up: [] };
      const want = LIVE[range] / (lv?.interval || 1);
      d = s.down.slice(-want); u = s.up.slice(-want);
      step = lv?.interval || 1;
      to = Math.floor(Date.now() / 1000); from = to - LIVE[range];
      const t0 = to - d.length * step;
      return { d, u, step, from, to, dp: d.map((v, i) => [t0 + i * step, v]), up: u.map((v, i) => [t0 + i * step, v]) };
    }
    const dp = hist?.series?.[`${prefix}.down`] || [], up = hist?.series?.[`${prefix}.up`] || [];
    return { d: dp.map((p) => p[1]), u: up.map((p) => p[1]), step: hist?.step || 30, from: hist?.from || 0, to: hist?.to || 1, dp, up };
  }, [live, lv, hist, range, id, prefix]);
  const last = (a) => (a.length ? a[a.length - 1] : 0);
  const avg = (a) => (a.length ? sumOf(a) / a.length : 0);
  const peak = (a) => Math.max(0, ...a);
  const bytes = (a) => (sumOf(a) * view.step) / 8;
  const nowD = lv?.series?.[id]?.down ? last(lv.series[id].down) : 0;
  const nowU = lv?.series?.[id]?.up ? last(lv.series[id].up) : 0;
  const idle = !nowD && !nowU;
  return (
    <Card title={title} desc={sub}>
      <div className="trafpanel">
        <div>
          <div className="ov-big">{fmtBits(nowD).split(' ')[0]}<span className="unit">{fmtBits(nowD).split(' ')[1]} {downWord.toLowerCase()} · now</span></div>
          <div className="ov-big">{fmtBits(nowU).split(' ')[0]}<span className="unit">{fmtBits(nowU).split(' ')[1]} {upWord.toLowerCase()} · now</span></div>
          {idle && lv && <div className="small muted">Quiet right now.</div>}
          <div className="small muted mt2">Over {WORDS[range]}:</div>
          <table className="table small" style={{ marginTop: 'var(--space-1)' }}>
            <thead><tr><th /><th className="num">{downWord}</th><th className="num">{upWord}</th></tr></thead>
            <tbody>
              <tr><td>Total</td><td className="num">{fmtBytes(bytes(view.d))}</td><td className="num">{fmtBytes(bytes(view.u))}</td></tr>
              <tr><td>Average</td><td className="num">{fmtBits(avg(view.d))}</td><td className="num">{fmtBits(avg(view.u))}</td></tr>
              <tr><td>Busiest</td><td className="num">{fmtBits(peak(view.d))}</td><td className="num">{fmtBits(peak(view.u))}</td></tr>
            </tbody>
          </table>
          {!live && <p className="small muted" style={{ margin: 'var(--space-2) 0 0' }}>Longer ranges show averages, so very short bursts are smoothed out. Totals only count the time NopeWarden has been recording.</p>}
        </div>
        <div style={{ minWidth: 0 }}>
          <div className="spread" style={{ flexWrap: 'wrap' }}>
            <span className="small sec inline">
              <span><i className="sw" style={{ background: 'var(--color-chart-series-1)', display: 'inline-block', width: '.65rem', height: '.65rem', borderRadius: 'var(--radius-xs)', marginRight: 'var(--space-1)' }} />{downWord}</span>
              <span><i className="sw" style={{ background: 'var(--color-chart-series-3)', display: 'inline-block', width: '.65rem', height: '.65rem', borderRadius: 'var(--radius-xs)', margin: '0 var(--space-1) 0 var(--space-3)' }} />{upWord}</span>
            </span>
            <Segmented value={range} onChange={setRange} options={RANGES.map(([value, label]) => ({ value, label }))} />
          </div>
          <Chart series={[{ key: 'down', name: downWord, color: 'var(--color-chart-series-1)', pts: view.dp }, { key: 'up', name: upWord, color: 'var(--color-chart-series-3)', pts: view.up }]} from={view.from} to={view.to} kind="bits" height={200} label={`${title}: ${downWord.toLowerCase()} and ${upWord.toLowerCase()} speed`} />
        </div>
      </div>
    </Card>
  );
}

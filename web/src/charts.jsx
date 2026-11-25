import React, { useMemo, useState } from 'react';

// Charts for the Overview. Every chart says what it measures (unit), over what time (labels along the
// bottom) and shows the exact value under the pointer. The axis text is HTML so it never stretches.

export function scaleBits(max) {
  if (max >= 1e9) return { div: 1e9, u: 'Gbps' };
  if (max >= 1e6) return { div: 1e6, u: 'Mbps' };
  if (max >= 1e3) return { div: 1e3, u: 'Kbps' };
  return { div: 1, u: 'bps' };
}
const round = (n) => (n >= 100 ? Math.round(n) : Math.round(n * 10) / 10);
export function fmtBits(v) { const s = scaleBits(v); return `${round(v / s.div).toLocaleString()} ${s.u}`; }
export function fmtBytes(v) {
  const u = ['B', 'KB', 'MB', 'GB', 'TB'];
  let i = 0, n = v || 0;
  while (n >= 1000 && i < u.length - 1) { n /= 1000; i += 1; }
  return `${round(n).toLocaleString()} ${u[i]}`;
}

// The top of a chart's scale, chosen so the four gridlines below it fall on round numbers
// (for example 0, 1, 2, 3, 4 or 0, 2.5, 5, 7.5, 10) and not on 1.3, 2.5, 3.8.
export function niceMax(v) {
  if (!(v > 0)) return 1;
  const p = 10 ** Math.floor(Math.log10(v / 4));
  for (const m of [1, 1.5, 2, 2.5, 5, 10]) if (v <= 4 * m * p + 1e-9) return 4 * m * p;
  return 40 * p;
}

export function agoText(sec) {
  if (sec < 90) return `${Math.max(1, Math.round(sec))} s`;
  if (sec < 5400) return `${Math.round(sec / 60)} min`;
  if (sec < 86400 * 2) return `${Math.round(sec / 3600)} h`;
  return `${Math.round(sec / 86400)} days`;
}

const W = 1000;

function segments(pts, X, Y, gap) {
  const out = [];
  let cur = [];
  pts.forEach((p, i) => {
    if (i && p[0] - pts[i - 1][0] > gap) { if (cur.length) out.push(cur); cur = []; }
    cur.push(p);
  });
  if (cur.length) out.push(cur);
  return out.map((seg) => seg.map((p, i) => `${i ? 'L' : 'M'}${X(p[0]).toFixed(1)},${Y(p[1]).toFixed(1)}`).join(''));
}

function nearest(pts, t) {
  let best = null;
  for (const p of pts) if (best === null || Math.abs(p[0] - t) < Math.abs(best[0] - t)) best = p;
  return best;
}

// series: [{ key, name, color, pts: [[unixSeconds, value], ...] }]. kind: 'bits' | 'ms' | 'count' | 'percent'.
export function Chart({ series, from, to, kind = 'count', unit = '', height = 200, compact = false, axes = false, marks = [], filled = true, dots = false, empty = 'Collecting data. The first points appear within a minute.', label }) {
  const [hover, setHover] = useState(null);
  const H = 100;
  const all = series.flatMap((s) => s.pts.map((p) => p[1]));
  const rawMax = Math.max(0, ...all);
  let div = 1, u = unit;
  if (kind === 'bits') { const sc = scaleBits(rawMax); div = sc.div; u = sc.u; }
  if (kind === 'ms') u = 'ms';
  if (kind === 'percent') u = '%';
  const top = kind === 'percent' ? 100 : niceMax(rawMax / div);
  const X = (t) => ((t - from) / Math.max(1, to - from)) * W;
  const Y = (v) => H - Math.min(1, v / div / top) * H;
  const gap = useMemo(() => {
    const dts = series[0]?.pts.slice(1).map((p, i) => p[0] - series[0].pts[i][0]).sort((a, b) => a - b) || [];
    const med = dts.length ? dts[Math.floor(dts.length / 2)] : 30;
    return Math.max(med * 2.6, 6);
  }, [series]);
  const paths = series.map((s) => ({ s, segs: segments(s.pts, X, Y, gap) }));
  // Stretches with no samples, drawn as a faint dotted line at the bottom of the small charts so a
  // gap reads as "not recorded" and not as a broken chart.
  const holes = [];
  if (compact && all.length) {
    const ts = series.flatMap((x) => x.pts.map((p) => p[0])).sort((a, b) => a - b);
    let prev = from;
    ts.forEach((t) => { if (t - prev > gap) holes.push([prev, t]); prev = t; });
    if (to - prev > gap) holes.push([prev, to]);
  }
  const has = rawMax > 0 || all.length > 0;
  const span = to - from;
  const clock = (t) => new Date(t * 1000).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
  const xl = compact && axes ? [clock(from), clock(from + (to - from) / 2), 'now'] : [0, 1, 2, 3, 4].map((k) => (k === 4 ? 'now' : k === 0 ? `${agoText(span)} ago` : agoText(span * (1 - k / 4))));
  const move = (e) => {
    const r = e.currentTarget.getBoundingClientRect();
    setHover(Math.max(0, Math.min(1, (e.clientX - r.left) / r.width)));
  };
  const t = hover == null ? null : from + hover * span;
  const hits = t == null ? [] : series.map((s) => ({ s, p: nearest(s.pts, t) })).filter((h) => h.p && Math.abs(h.p[0] - t) <= gap);
  const showVal = (v) => (kind === 'bits' ? fmtBits(v) : kind === 'ms' ? `${round(v)} ms` : kind === 'percent' ? `${round(v)} %` : `${round(v).toLocaleString()}${unit ? ` ${unit}` : ''}`);
  const when = (p) => (span > 3 * 86400 ? new Date(p[0] * 1000).toLocaleDateString([], { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' }) : new Date(p[0] * 1000).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: span < 1800 ? '2-digit' : undefined }));
  return (
    <figure className={`ch2 ${compact ? 'compact' : ''} ${compact && axes ? 'axes' : ''}`} style={{ margin: 0 }}>
      {!compact && <div className="ch2-unit small muted">{u}</div>}
      {!compact && series.some((x) => x.name) && (
        <div className="ch2-legend small" aria-live="off">
          {series.filter((x) => x.name).map((x) => {
            const h = hits.find((q) => q.s.key === x.key);
            const p = hover != null ? h?.p : x.pts[x.pts.length - 1];
            return (
              <span key={x.key} className="ch2-lg"><i className="sw" style={{ background: x.color }} />{x.name}
                <b>{p ? showVal(p[1]) : '–'}</b></span>
            );
          })}
          <span className="muted ch2-lgwhen">{hover != null && hits[0] ? when(hits[0].p) : 'latest'}</span>
        </div>
      )}
      <div className="ch2-body">
        {(!compact || axes) && (
          <div className="ch2-y small muted" aria-hidden="true" style={{ height }}>
            {(compact ? [1, 0.5, 0] : [1, 0.75, 0.5, 0.25, 0]).map((f) => <span key={f}>{round(top * f).toLocaleString()}</span>)}
          </div>
        )}
        <div className="ch2-plot" style={{ height }} onMouseMove={move} onMouseLeave={() => setHover(null)} role="img" aria-label={label || `Chart in ${u}`}>
          <svg viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none">
            {(compact ? (axes ? [0, 50, 100] : []) : [0, 25, 50, 75, 100]).map((y) => <line key={y} x1="0" x2={W} y1={y} y2={y} className="gl" vectorEffect="non-scaling-stroke" />)}
            {holes.map(([a, b], i) => <line key={`h${i}`} x1={X(a)} x2={X(b)} y1="100" y2="100" className="hole" vectorEffect="non-scaling-stroke"><title>Not recorded</title></line>)}
            {paths.map(({ s, segs }) => segs.map((d, i) => (
              <g key={`${s.key}${i}`} style={{ '--c': s.color }}>
                {filled && series.length <= 2 && d.includes('L') && <path d={`${d}L${d.split('L').pop().split(',')[0]},${H}L${d.slice(1).split(',')[0]},${H}Z`} className="fill" />}
                <path d={d} className="ln" vectorEffect="non-scaling-stroke" />
              </g>
            )))}
            {marks.filter((m) => m.t >= from && m.t <= to).map((m, i) => <line key={i} x1={X(m.t)} x2={X(m.t)} y1="0" y2={H} className="mk" vectorEffect="non-scaling-stroke" />)}
          </svg>
          {dots && series.flatMap((s) => s.pts.filter((p) => p[0] >= from && p[0] <= to).map((p) => (
            <i key={`${s.key}${p[0]}`} className="ch2-dot" style={{ left: `${(X(p[0]) / W) * 100}%`, top: `${(Y(p[1]) / H) * 100}%`, background: s.color }} />
          )))}
          {marks.filter((m) => m.t >= from && m.t <= to).map((m, i) => <span key={i} className="ch2-mark small" style={{ right: `${100 - (X(m.t) / W) * 100}%` }}>{m.label}</span>)}
          {!has && <span className="ch2-empty small muted">{compact ? 'No data yet' : empty}</span>}
          {hover != null && hits.length > 0 && (
            <>
              <div className="ch2-cur" style={{ left: `${hover * 100}%` }} />
              <div className="ch2-tip small" style={hover > 0.6 ? { right: `${(1 - hover) * 100}%` } : { left: `${hover * 100}%` }}>
                <div className="muted">{when(hits[0].p)}</div>
                {hits.map((h) => <div key={h.s.key}><i className="sw" style={{ background: h.s.color }} />{h.s.name ? `${h.s.name}: ` : ''}<b>{showVal(h.p[1])}</b></div>)}
              </div>
            </>
          )}
        </div>
      </div>
      {(!compact || axes) && <div className="ch2-x small muted" aria-hidden="true">{xl.map((s, i) => <span key={i}>{s}</span>)}</div>}
    </figure>
  );
}

// Layers drawn on top of each other (for example answered, from memory and blocked), so the whole
// shows the total and each band shows its share. layers: [{ key, name, color, values: [n, n, ...] }],
// every layer with the same number of values, oldest first. xLabels: three words under the chart.
export function StackedArea({ layers, span = 60, height = 140, unit = '', xLabels = ['', '', 'now'], empty = 'Nothing in this period', label = 'Chart', fmt = (n) => (Math.round(n * 10) / 10).toLocaleString() }) {
  const [hover, setHover] = useState(null);
  const n = Math.max(0, ...layers.map((l) => l.values.length));
  const H = 100;
  const totals = Array.from({ length: n }, (_, i) => layers.reduce((a, l) => a + (l.values[i] || 0), 0));
  const peak = Math.max(0, ...totals);
  const top = niceMax(peak);
  const X = (i) => (n > 1 ? (i / (n - 1)) * W : 0);
  const Y = (v) => H - Math.min(1, v / top) * H;
  const below = new Array(n).fill(0);
  const bands = layers.map((l) => {
    const lo = below.slice();
    const hi = lo.map((v, i) => v + (l.values[i] || 0));
    hi.forEach((v, i) => { below[i] = v; });
    const up = hi.map((v, i) => `${i ? 'L' : 'M'}${X(i).toFixed(1)},${Y(v).toFixed(1)}`).join('');
    const down = lo.map((v, i) => `L${X(n - 1 - i).toFixed(1)},${Y(lo[n - 1 - i]).toFixed(1)}`).join('');
    return { l, area: n > 1 ? `${up}${down}Z` : '', line: up };
  });
  const at = hover == null ? n - 1 : Math.max(0, Math.min(n - 1, Math.round(hover * (n - 1))));
  return (
    <figure className="ch2 stk" style={{ margin: 0 }}>
      <div className="ch2-legend small">
        {layers.map((l) => <span key={l.key} className="ch2-lg"><i className="sw" style={{ background: l.color }} />{l.name}<b>{n ? fmt(l.values[at] || 0) : '–'}{unit}</b></span>)}
        <span className="muted ch2-lgwhen">{hover != null ? (at >= n - 1 ? 'now' : `${agoText((1 - at / Math.max(1, n - 1)) * span)} ago`) : 'latest'}</span>
      </div>
      <div className="ch2-body">
        <div className="ch2-y small muted" aria-hidden="true" style={{ height }}>{[1, 0.75, 0.5, 0.25, 0].map((f) => <span key={f}>{Math.round(top * f * 10) / 10}</span>)}</div>
        <div className="ch2-plot" style={{ height }} onMouseMove={(e) => { const r = e.currentTarget.getBoundingClientRect(); setHover(Math.max(0, Math.min(1, (e.clientX - r.left) / r.width))); }} onMouseLeave={() => setHover(null)} role="img" aria-label={label}>
          <svg viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none">
            {[0, 25, 50, 75, 100].map((y) => <line key={y} x1="0" x2={W} y1={y} y2={y} className="gl" vectorEffect="non-scaling-stroke" />)}
            {bands.map(({ l, area, line }) => (
              <g key={l.key} style={{ '--c': l.color }}>
                {area && <path d={area} className="stk-fill" />}
                {n > 1 && l.values.some((v) => v > 0) && <path d={line} className="ln" vectorEffect="non-scaling-stroke" />}
              </g>
            ))}
          </svg>
          {peak === 0 && <span className="ch2-empty small muted">{empty}</span>}
          {hover != null && n > 1 && <div className="ch2-cur" style={{ left: `${(at / (n - 1)) * 100}%` }} />}
        </div>
      </div>
      <div className="ch2-x small muted" aria-hidden="true">{xLabels.map((t, i) => <span key={i}>{t}</span>)}</div>
    </figure>
  );
}

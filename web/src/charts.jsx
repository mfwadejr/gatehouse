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

export function niceMax(v) {
  if (!(v > 0)) return 1;
  const p = 10 ** Math.floor(Math.log10(v));
  for (const m of [1, 2, 2.5, 5, 10]) if (v <= m * p) return m * p;
  return 10 * p;
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
export function Chart({ series, from, to, kind = 'count', unit = '', height = 200, compact = false, marks = [], filled = true, dots = false, empty = 'Collecting data. The first points appear within a minute.', label }) {
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
  const has = rawMax > 0 || all.length > 0;
  const span = to - from;
  const xl = [0, 1, 2, 3, 4].map((k) => (k === 4 ? 'now' : k === 0 ? `${agoText(span)} ago` : agoText(span * (1 - k / 4))));
  const move = (e) => {
    const r = e.currentTarget.getBoundingClientRect();
    setHover(Math.max(0, Math.min(1, (e.clientX - r.left) / r.width)));
  };
  const t = hover == null ? null : from + hover * span;
  const hits = t == null ? [] : series.map((s) => ({ s, p: nearest(s.pts, t) })).filter((h) => h.p && Math.abs(h.p[0] - t) <= gap);
  const showVal = (v) => (kind === 'bits' ? fmtBits(v) : kind === 'ms' ? `${round(v)} ms` : kind === 'percent' ? `${round(v)} %` : `${round(v).toLocaleString()}${unit ? ` ${unit}` : ''}`);
  const when = (p) => (span > 3 * 86400 ? new Date(p[0] * 1000).toLocaleDateString([], { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' }) : new Date(p[0] * 1000).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: span < 1800 ? '2-digit' : undefined }));
  return (
    <figure className={`ch2 ${compact ? 'compact' : ''}`} style={{ margin: 0 }}>
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
        {!compact && (
          <div className="ch2-y small muted" aria-hidden="true" style={{ height }}>
            {[1, 0.75, 0.5, 0.25, 0].map((f) => <span key={f}>{round(top * f).toLocaleString()}</span>)}
          </div>
        )}
        <div className="ch2-plot" style={{ height }} onMouseMove={move} onMouseLeave={() => setHover(null)} role="img" aria-label={label || `Chart in ${u}`}>
          <svg viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none">
            {[0, 25, 50, 75, 100].filter((y) => !compact || y === 100).map((y) => <line key={y} x1="0" x2={W} y1={y} y2={y} className="gl" vectorEffect="non-scaling-stroke" />)}
            {paths.map(({ s, segs }) => segs.map((d, i) => (
              <g key={`${s.key}${i}`} style={{ '--c': s.color }}>
                {filled && series.length <= 2 && <path d={`${d}L${d.split('L').pop().split(',')[0]},${H}L${d.slice(1).split(',')[0]},${H}Z`} className="fill" />}
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
      {!compact && <div className="ch2-x small muted" aria-hidden="true">{xl.map((s, i) => <span key={i}>{s}</span>)}</div>}
    </figure>
  );
}

import React, { useEffect, useMemo, useState } from 'react';
import { useData, useStream, put, post, get } from '../api.js';
import { Button, Card, Empty, Icon, Note, Page, Pill, Segmented, fmt, Bar, useAction } from '../ui.jsx';
import { go } from '../App.jsx';
import { COUNTRIES, MAP_H, MAP_W } from '../worldmap.js';
import { centreOf } from '../mapgeo.js';
import { Chart, fmtBits, fmtBytes } from '../charts.jsx';
import { linkMeasure, linkState } from '../linkstate.js';
import { PRESET_KEY } from './Logs.jsx';
import { WhatsNewCard } from '../whatsnew.jsx';
import { useAttention } from '../attention.jsx';

const regionName = (cc) => { try { return new Intl.DisplayNames(undefined, { type: 'region' }).of(cc); } catch { return cc; } };
const SERIES = ['var(--color-chart-series-1)', 'var(--color-chart-series-2)', 'var(--color-chart-series-3)', 'var(--color-chart-series-4)'];
const EVERY = { '10m': 2000, '1h': 30000, '24h': 300000, '30d': 900000 };

// A number that counts up once when it first appears, then simply follows the value. It does not
// move for people who ask their browser for less motion.
function CountUp({ value, format = (n) => n.toLocaleString() }) {
  const [shown, setShown] = useState(null);
  const first = React.useRef(true);
  useEffect(() => {
    const v = Number(value) || 0;
    const calm = typeof window.matchMedia === 'function' && window.matchMedia('(prefers-reduced-motion: reduce)').matches;
    if (!first.current || calm || v === 0) { first.current = false; setShown(v); return undefined; }
    first.current = false;
    const t0 = performance.now();
    let raf;
    const step = (t) => {
      const k = Math.min(1, (t - t0) / 800);
      setShown(Math.round(v * (1 - (1 - k) ** 3)));
      if (k < 1) raf = requestAnimationFrame(step);
    };
    raf = requestAnimationFrame(step);
    return () => cancelAnimationFrame(raf);
  }, [value]);
  return <>{format(shown == null ? 0 : shown)}</>;
}

// "updated 3 s ago", ticking each second, so a quiet page still shows it is alive.
function UpdatedAgo({ stamp }) {
  const [, tick] = useState(0);
  useEffect(() => { const t = setInterval(() => tick((n) => n + 1), 1000); return () => clearInterval(t); }, []);
  if (!stamp) return null;
  const age = Math.max(0, Math.round((Date.now() - stamp) / 1000));
  return <span className="small muted">updated {age < 2 ? 'just now' : `${age} s ago`}</span>;
}

// Says nothing while data keeps arriving, and shows a grey "Reconnecting" when it stops.
function LiveDot({ stamp }) {
  const [, tick] = useState(0);
  useEffect(() => { const t = setInterval(() => tick((n) => n + 1), 1000); return () => clearInterval(t); }, []);
  const age = stamp ? Math.round((Date.now() - stamp) / 1000) : null;
  const stale = age == null || age > 15;
  if (!stale) return null; // while data keeps arriving there is nothing to say
  return (
    <span className="livedot small stale" role="status">
      <i />{age == null ? 'Loading…' : 'Reconnecting…'}
    </span>
  );
}

// ---------------------------------------------------------------- internet hero

function lastOf(arr, n = 2) {
  if (!arr?.length) return 0;
  const t = arr.slice(-n);
  return t.reduce((a, b) => a + b, 0) / t.length;
}

function Hero({ wanList, links, thr, onStamp }) {
  const [dir, setDir] = useState('rx');
  const [range, setRange] = useState('1h');
  const { data: hist } = useData(`/api/overview/history?range=${range === '10m' ? '1h' : range}`, { every: EVERY[range] === 2000 ? 30000 : EVERY[range], enabled: range !== '10m' });
  const enabled = wanList.filter((w) => w.enabled);
  const states = enabled.map((w) => ({ w, l: links[w.name], s: linkState(links[w.name]) }));
  const working = states.filter((x) => x.s.health === 'working');
  const active = states.find((x) => x.l?.active) || working[0];
  const primary = enabled[0];
  let head = { tone: '', text: 'Checking the internet connection…', sub: '' };
  if (!enabled.length) head = { tone: 'bad', text: 'No internet connection is set up', sub: 'Add one so devices can get online.' };
  else if (working.length) {
    const backup = enabled.length > 1 && active && primary && active.w.name !== primary.name;
    head = backup
      ? { tone: 'warn', text: `Internet is working on the backup`, sub: `Using ${active.w.name}${active.l?.since ? ` since ${new Date(active.l.since).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}` : ''} · ${primary.name} is not available` }
      : { tone: 'good', text: 'Internet is working', sub: enabled.length > 1 ? `${enabled.length} connections · main and backup` : '1 connection' };
  } else if (states.every((x) => x.s.health === 'unknown' || x.s.health === 'none')) head = { tone: '', text: 'Checking the internet connection…', sub: 'Waiting for the first answer.' };
  else head = { tone: 'bad', text: 'Internet is down', sub: states[0].s.detail };

  const now = Math.floor(Date.now() / 1000);
  const key = dir;
  const build = () => {
    if (range === '10m') {
      const iv = thr?.interval || 2;
      return { from: now - 600, to: now, series: enabled.map((w, i) => {
        const arr = thr?.links?.[w.name]?.[key] || [];
        return { key: w.name, name: w.name, color: SERIES[i % 4], pts: arr.map((v, j) => [now - (arr.length - 1 - j) * iv, v]) };
      }) };
    }
    const h = hist || { from: now - 3600, to: now, series: {} };
    return { from: h.from, to: h.to, series: enabled.map((w, i) => ({ key: w.name, name: w.name, color: SERIES[i % 4], pts: h.series?.[`wan.${w.name}.${key}`] || [] })) };
  };
  const ch = build();
  // A connection that was up and went down in this range gets a marker.
  const marks = [];
  if (range !== '10m' && hist) {
    enabled.forEach((w) => {
      const up = hist.series?.[`wan.${w.name}.up`] || [];
      for (let i = up.length - 1; i > 0; i -= 1) if (up[i - 1][1] >= 0.5 && up[i][1] < 0.5) { marks.push({ t: up[i][0], label: `${w.name} went down` }); break; }
    });
  }
  const sum = (k) => enabled.reduce((a, w) => a + lastOf(thr?.links?.[w.name]?.[k]), 0);
  // Average and busiest over the range that is shown, so the big "now" number and the chart agree.
  const seriesOf = (w, k) => (range === '10m' ? (thr?.links?.[w.name]?.[k] || []) : (hist?.series?.[`wan.${w.name}.${k}`] || []).map((p) => p[1]));
  const avgOf = (k) => enabled.reduce((a, w) => { const v = seriesOf(w, k); return a + (v.length ? v.reduce((x, y) => x + y, 0) / v.length : 0); }, 0);
  const peakOf = (k) => enabled.reduce((a, w) => Math.max(a, ...seriesOf(w, k), 0), 0);
  const RANGE_WORDS = { '10m': 'the last 10 minutes', '1h': 'the last hour', '24h': 'the last 24 hours', '30d': 'the last 30 days' };
  const tone = head.tone;
  return (
    <Card className="ov-hero">
      <div className="ov-hero-l">
        <div className={`ov-state ${tone}`}><i className="dot" />{head.text}</div>
        <div className="small muted" style={{ marginBottom: 'var(--space-3)' }}>{head.sub}</div>
        <div className="ov-big">{fmtBits(sum('rx')).split(' ')[0]}<span className="unit">{fmtBits(sum('rx')).split(' ')[1]} download · now</span></div>
        <div className="ov-big">{fmtBits(sum('tx')).split(' ')[0]}<span className="unit">{fmtBits(sum('tx')).split(' ')[1]} upload · now</span></div>
        <div className="small muted" style={{ marginTop: 'var(--space-2)' }}>Average over {RANGE_WORDS[range]}: {fmtBits(avgOf('rx'))} down, {fmtBits(avgOf('tx'))} up · busiest {fmtBits(peakOf(dir))} {dir === 'rx' ? 'down' : 'up'}</div>
        <div className="ov-wans">
          {states.map((x, i) => {
            const m = linkMeasure(x.l);
            const rx = lastOf(thr?.links?.[x.w.name]?.rx), tx = lastOf(thr?.links?.[x.w.name]?.tx);
            return (
              <div className={`ov-wan ${x.l?.active ? '' : 'dim'}`} key={x.w.name}>
                <i className="k" style={{ background: SERIES[i % 4] }} />
                <div>
                  <b>{x.w.name}</b>{' '}
                  <Pill tone={x.s.tone}>{x.s.health === 'working' ? (x.l?.active ? 'Working · in use' : 'Standing by') : x.s.text}</Pill>
                  <div className="small muted">{[x.w.iface, x.w.addr && x.w.addr !== 'dhcp' ? x.w.addr.split('/')[0] : '', m].filter(Boolean).join(' · ')}</div>
                </div>
                <div className="num small">{x.l?.active || thr?.links?.[x.w.name] ? <>{fmtBits(rx)} ↓<br />{fmtBits(tx)} ↑</> : '–'}</div>
              </div>
            );
          })}
        </div>
      </div>
      <div style={{ minWidth: 0 }}>
        <div className="spread" style={{ flexWrap: 'wrap' }}>
          <div className="inline">
            <Segmented value={dir} onChange={setDir} options={[{ value: 'rx', label: 'Download' }, { value: 'tx', label: 'Upload' }]} />
            <span className="small sec inline">{enabled.map((w, i) => <span key={w.name}><i className="sw" style={{ background: SERIES[i % 4], display: 'inline-block', width: '.65rem', height: '.65rem', borderRadius: 'var(--radius-xs)', marginRight: 'var(--space-1)' }} />{w.name}</span>)}</span>
          </div>
          <Segmented value={range} onChange={setRange} options={[{ value: '10m', label: '10 min' }, { value: '1h', label: '1 hour' }, { value: '24h', label: '24 hours' }, { value: '30d', label: '30 days' }]} />
        </div>
        <Chart series={ch.series} from={ch.from} to={ch.to} kind="bits" height={220} marks={marks} label={`${dir === 'rx' ? 'Download' : 'Upload'} speed of each internet connection`} />
      </div>
    </Card>
  );
}

// ---------------------------------------------------------------- tiles

function Tile({ title, pill, tone, big, unit, sub, children, chart, to, caption }) {
  return (
    <Card className="tile ov-tile">
      <div className="spread"><span className="lbl">{title}</span>{pill && <Pill tone={tone}>{pill}</Pill>}</div>
      <div><span className="val">{big}</span>{unit && <span className="unit">{unit}</span>}</div>
      {sub && <div className="small muted">{sub}</div>}
      {children}
      {chart}
      {caption && <div className="small muted">{caption}</div>}
      {to && <button type="button" className="link small" style={{ textAlign: 'left' }} onClick={() => go(to)}>Open →</button>}
    </Card>
  );
}

const miniSeries = (h, key, scale = 1) => [{ key, name: '', color: 'var(--color-chart-series-1)', pts: (h?.series?.[key] || []).map((p) => [p[0], p[1] * scale]) }];

function DevicesTile({ d, h }) {
  const online = d?.online ?? 0;
  return (
    <Tile title="Devices" pill={`${online} online`} tone="good" big={<CountUp value={online} />} to="devices"
      sub={d ? `${d.wired} wired${d.vpn ? ` · ${d.vpn} VPN` : ''}` : ''}
      chart={<Chart series={miniSeries(h, 'devices.online')} from={h?.from || 0} to={h?.to || 1} unit="devices" height={44} compact label="Devices online, last hour" />}
      caption="Devices online · last hour">
      <div className="inline" style={{ marginTop: 'var(--space-2)' }}>
        {d?.new > 0 && <button type="button" className="pillbtn warn" onClick={() => go('devices')}>{d.new} new today</button>}
        {d?.unnamed > 0 && <button type="button" className="pillbtn" onClick={() => go('devices')}>{d.unnamed} without a name</button>}
        {d && !d.new && !d.unnamed && <span className="small muted">Everything on the list has a name.</span>}
      </div>
    </Tile>
  );
}

function DnsTile({ dns, h }) {
  const st = dns?.stats;
  const ups = dns?.upstreams || [];
  const ok = ups.filter((u) => u.state === 'healthy').length;
  const pill = !ups.length ? '' : ok === ups.length ? 'Working' : ok ? `${ups.length - ok} slow or down` : 'No server answering';
  const tone = !ups.length ? '' : ok === ups.length ? 'good' : ok ? 'warn' : 'bad';
  return (
    <Tile title="DNS" pill={pill} tone={tone} big={<CountUp value={st?.hour_total ?? 0} />} unit="lookups in the last hour" to="dns"
      sub={`${fmt.d1(st?.qps)}/s now · ${ups.length} server${ups.length === 1 ? '' : 's'} · ${fmt.d1(st?.blocked_pct)} % blocked · ${fmt.d1(st?.cached_pct)} % cached`}
      chart={<Chart series={miniSeries(h, 'dns.qps')} from={h?.from || 0} to={h?.to || 1} unit="queries/s" height={44} compact label="DNS queries per second, last hour" />}
      caption="Queries per second · last hour" />
  );
}

function DhcpTile({ util, h }) {
  const list = util || [];
  const used = list.reduce((a, s) => a + s.active, 0), size = list.reduce((a, s) => a + s.size, 0);
  const full = list.filter((s) => s.size && s.active / s.size > 0.8);
  return (
    <Tile title="DHCP" pill={!list.length ? 'No scope' : full.length ? `${full[0].name} nearly full` : 'Serving'} tone={!list.length ? 'warn' : full.length ? 'warn' : 'good'}
      big={<CountUp value={used} />} unit={size ? `of ${size} addresses` : 'leases'} to="dhcp"
      chart={<Chart series={miniSeries(h, 'dhcp.used')} from={h?.from || 0} to={h?.to || 1} unit="addresses" height={44} compact label="Addresses in use, last hour" />}
      caption="Addresses in use · last hour">
      <div className="ov-scopes">
        {list.map((s) => {
          const p = s.size ? (100 * s.active) / s.size : 0;
          return (
            <div className="ov-sc" key={s.scope_id}>
              <span>{s.name}</span>
              <div className={`bar ${p > 80 ? 'warnbar' : ''}`}><i style={{ width: `${Math.min(100, p)}%` }} /></div>
              <span className="num small">{s.active} of {s.size}</span>
            </div>
          );
        })}
      </div>
    </Tile>
  );
}

function SecurityTile({ sec, fw, h }) {
  const bad = fw?.status?.pending || fw?.status?.dirty;
  const alerts = sec?.threat_alerts_24h ?? 0;
  const threat = !sec ? '' : !sec.threat_on ? 'Threat watching is off' : alerts ? `Threat watching on · ${alerts} alert${alerts === 1 ? '' : 's'} today` : 'Threat watching on · no alerts today';
  return (
    <Tile title="Protection" pill={!fw ? '' : !fw.zones?.length ? 'Not set up' : bad ? 'Changes waiting' : alerts ? `${alerts} alert${alerts === 1 ? '' : 's'}` : 'Protected'} tone={!fw ? '' : !fw.zones?.length ? 'warn' : bad || alerts ? 'warn' : 'good'}
      big={<CountUp value={sec?.blocked_24h ?? 0} format={fmt.n} />} unit="blocked in 24 h" to="protection"
      sub={`${sec?.open_ports ? `${sec.open_ports} port${sec.open_ports === 1 ? '' : 's'} open to the internet` : 'No ports open to the internet'}`}
      chart={<Chart series={miniSeries(h, 'fw.blocked', 60)} from={h?.from || 0} to={h?.to || 1} unit="blocked/min" height={44} compact label="Blocked connections per minute, last hour" />}
      caption={threat || 'Blocked connections per minute · last hour'} />
  );
}

// ---------------------------------------------------------------- world map

// Colour for a country: grey for none, then blue, then green for the busiest going out; amber then
// orange for what comes in. The square root keeps one huge country from hiding all the others.
function mapFill(dir, t) {
  const pct = (x) => Math.round(Math.max(0, Math.min(1, x)) * 100);
  if (dir === 'out') {
    return t < 0.5
      ? `color-mix(in srgb, var(--color-chart-series-1) ${Math.max(22, pct(t * 2))}%, var(--color-border-strong))`
      : `color-mix(in srgb, var(--color-chart-series-3) ${pct((t - 0.5) * 2)}%, var(--color-chart-series-1))`;
  }
  return t < 0.5
    ? `color-mix(in srgb, var(--color-status-warning) ${Math.max(22, pct(t * 2))}%, var(--color-border-strong))`
    : `color-mix(in srgb, var(--color-status-serious) ${pct((t - 0.5) * 2)}%, var(--color-status-warning))`;
}
const mapLegend = (dir) => (dir === 'out'
  ? 'linear-gradient(90deg, var(--color-border-strong), var(--color-chart-series-1), var(--color-chart-series-3))'
  : 'linear-gradient(90deg, var(--color-border-strong), var(--color-status-warning), var(--color-status-serious))');

const RANGE_HOURS = { '1h': '1', '24h': '24', '7d': '168', '30d': '720' };

function WorldCard() {
  const [dir, setDir] = useState('out');
  const [range, setRange] = useState('24h');
  const { data } = useData(`/api/overview/map?dir=${dir}&range=${range}`, { every: 60000 });
  const { data: geo, reload: reloadGeo } = useData('/api/fw/geo', { every: 5000 });
  const [dl, dlBusy] = useAction();
  const [hover, setHover] = useState(null);
  const { data: homePt } = useData('/api/overview/home');
  const by = useMemo(() => Object.fromEntries((data?.countries || []).map((c) => [c.country, c])), [data]);
  const top = data?.top || 0;
  const word = dir === 'out' ? 'connections' : 'attempts';
  const open = (cc) => {
    try { sessionStorage.setItem(PRESET_KEY, JSON.stringify({ flt: { country: cc, direction: dir === 'out' ? 'outbound' : 'inbound' }, hours: RANGE_HOURS[range] })); } catch { /* ignore */ }
    go('logs');
  };
  const rows = (data?.countries || []).slice(0, 8);
  const tip = hover && by[hover];
  // Where "you" are: the place set in Settings, else the middle of the country the browser is set to.
  const home = useMemo(() => {
    if (homePt?.set) return [homePt.x, homePt.y];
    const region = (navigator.language || '').split('-')[1]?.toUpperCase();
    return (region && centreOf(region)) || null;
  }, [homePt]);
  const arcs = useMemo(() => {
    if (!home) return [];
    return (data?.countries || []).slice(0, 6).map((c) => ({ c, to: centreOf(c.country) })).filter((x) => x.to && Math.hypot(x.to[0] - home[0], x.to[1] - home[1]) > 14).slice(0, 5);
  }, [data, home]);
  return (
    <div className="ov-gm">
      <Card title="Where connections go"
        desc={dir === 'out' ? 'Where your devices are connecting' : 'Where connection attempts from the internet come from'}
        actions={<div className="inline"><Segmented value={dir} onChange={setDir} options={[{ value: 'out', label: 'Outgoing' }, { value: 'in', label: 'Incoming' }]} /><Segmented value={range} onChange={setRange} options={[{ value: '1h', label: '1 h' }, { value: '24h', label: '24 h' }, { value: '7d', label: '7 d' }, { value: '30d', label: '30 d' }]} /></div>}>
        {geo && !geo.ready && (
          <Note tone={geo.error ? 'warn' : ''} title="Country data not downloaded yet">
            {geo.busy ? 'Downloading the country address lists now. The map fills in once they arrive.' : geo.error ? `The last try failed: ${geo.error}` : 'Gatehouse needs the country address lists to place connections on the map.'}
            {' '}<Button small disabled={geo.busy || dlBusy} onClick={() => dl(async () => { await post('/api/fw/geo/refresh', {}); reloadGeo(); })}>{geo.busy ? 'Downloading…' : 'Download now'}</Button>
          </Note>
        )}
        <div className="ov-map" onMouseLeave={() => setHover(null)}>
          <svg viewBox={`0 0 ${MAP_W} ${MAP_H}`} role="img" aria-label={`${dir === 'out' ? 'Outgoing connections' : 'Incoming attempts'} by country`}>
            {Object.entries(COUNTRIES).filter(([cc]) => cc !== 'AQ').map(([cc, c]) => {
              const n = by[cc]?.n || 0;
              const t = n ? Math.sqrt(n / Math.max(1, top)) : 0;
              const style = n ? { fill: mapFill(dir, t) } : undefined;
              return <path key={cc} d={c.d} className={`${n ? 'hit' : ''} ${t > 0.45 ? `hot ${dir}` : ''}`} style={style} onMouseEnter={() => setHover(cc)} onClick={n ? () => open(cc) : undefined} />;
            })}
            {home && arcs.map(({ c, to }, i) => {
              const mx = (home[0] + to[0]) / 2, my = Math.min(home[1], to[1]) - Math.abs(to[0] - home[0]) * 0.22 - 18;
              const d = dir === 'out' ? `M${home[0]},${home[1]} Q${mx},${my} ${to[0]},${to[1]}` : `M${to[0]},${to[1]} Q${mx},${my} ${home[0]},${home[1]}`;
              const w = 0.8 + 2.2 * Math.sqrt(c.n / Math.max(1, top));
              return (
                <g key={c.country} className={`ov-arc ${dir}`} pointerEvents="none">
                  <path d={d} strokeWidth={w} />
                  <circle className="ov-travel" r={w + 1.1}><animateMotion dur={`${3 + i * 0.6}s`} repeatCount="indefinite" path={d} /></circle>
                </g>
              );
            })}
            {home && (
              <g className={`ov-home ${dir}`} pointerEvents="none">
                <circle className="ov-ping" cx={home[0]} cy={home[1]} r="4" />
                <circle className="ov-pin" cx={home[0]} cy={home[1]} r="4.5" />
              </g>
            )}
          </svg>
          {tip && (
            <div className="ov-maptip small">
              <b>{regionName(hover)}</b><br />
              {fmt.n(tip.n)} {word}{tip.up + tip.down > 0 ? <><br />{fmtBytes(tip.up)} sent · {fmtBytes(tip.down)} received</> : null}
              {dir === 'in' && <><br />{fmt.n(tip.blocked)} blocked · {fmt.n(tip.allowed)} allowed</>}
              <br /><span className="muted">Click to see them</span>
            </div>
          )}
        </div>
        {!homePt?.set && <p className="small muted" style={{ margin: 0 }}>Lines start from {home ? 'the middle of your country' : 'you'}. {home ? 'To place yourself exactly, ' : 'To show where you are, '}<button type="button" className="link" onClick={() => go('settings')}>set your location in Settings</button>.</p>}
        <div className="ov-scale small muted">
          <span>None</span><i style={{ background: mapLegend(dir) }} /><span>{fmt.n(top)} {word}</span>
          <span className="grow" />
          <span>The country is approximate for CDN, cloud and VPN traffic.</span>
        </div>
      </Card>
      <Card title={dir === 'out' ? 'Top countries (outgoing)' : 'Top countries (incoming)'} desc={dir === 'out' ? 'By connections, with the data sent and received · your devices talking out' : 'Attempts from the internet'}>
        {!rows.length ? <Empty>{data ? 'Nothing recorded in this time.' : 'Loading…'}</Empty> : (
          <div className="stack" style={{ gap: 'var(--space-2)' }}>
            {rows.map((c) => (
              <button type="button" className="ov-cr" key={c.country} onClick={() => open(c.country)}>
                <span className="muted small">{c.country}</span>
                <span><span className="name">{regionName(c.country)}</span><Bar pct={(100 * c.n) / (rows[0].n || 1)} thin /></span>
                <span className="num">{fmt.n(c.n)}{c.up + c.down > 0 && <span className="small muted"> · {fmtBytes(c.up + c.down)}</span>}</span>
              </button>
            ))}
          </div>
        )}
      </Card>
    </div>
  );
}

// ---------------------------------------------------------------- lower cards

function NetworksCard({ fw, wanList, links, devs, h24 }) {
  const order = (fw?.matrix_zones || []).map((z) => z.name);
  const stored = (fw?.zones || []).filter((z) => z.kind !== 'wan');
  const zones = [...stored].sort((a, b) => (order.indexOf(a.name) < 0 ? 999 : order.indexOf(a.name)) - (order.indexOf(b.name) < 0 ? 999 : order.indexOf(b.name)));
  const enabled = wanList.filter((w) => w.enabled);
  const states = enabled.map((w) => linkState(links[w.name]));
  const ok = states.filter((s) => s.health === 'working').length;
  return (
    <Card title="Your networks" desc="Same order as Who can reach whom. Download speed, last hour.">
      <div className="ov-nets">
        {zones.map((z) => (
          <div className="ov-net" key={z.id}>
            <div className="spread"><b>{z.name}</b><span className="num small">{devs?.by_network?.[z.name] ?? 0} devices</span></div>
            <div className="small muted">{z.ifaces?.length ? z.ifaces.join(', ') : 'no ports yet'}</div>
            <Chart series={miniSeries(h24, `net.${z.name}.down`)} from={h24?.from || 0} to={h24?.to || 1} kind="bits" height={30} compact label={`Download speed of ${z.name}, last hour`} />
          </div>
        ))}
        <div className="ov-net">
          <div className="spread"><b>Internet</b>{enabled.length ? <Pill tone={ok ? 'good' : states.every((s) => s.health === 'unknown' || s.health === 'none') ? '' : 'bad'}>{ok ? (enabled.length > 1 ? `${ok} of ${enabled.length} working` : 'Connected') : states.every((s) => s.health === 'unknown' || s.health === 'none') ? 'Checking…' : 'Down'}</Pill> : <Pill>Not set up</Pill>}</div>
          <div className="small muted">{enabled.map((w) => w.iface).join(', ') || 'no port yet'} · managed automatically</div>
        </div>
      </div>
      <button type="button" className="link small" style={{ textAlign: 'left' }} onClick={() => go('networks')}>Open networks →</button>
    </Card>
  );
}

function BusiestCard({ rows }) {
  const max = rows?.[0]?.total || 1;
  return (
    <Card title="Busiest devices" desc="Data used, last 24 hours">
      {!rows?.length ? <Empty>No data used yet today.</Empty> : (
        <div className="stack" style={{ gap: 'var(--space-2)' }}>
          {rows.map((r) => (
            <div className="ov-bd" key={r.mac}>
              <span><span className="name">{r.name || r.mac}</span><Bar pct={(100 * r.total) / max} thin /></span>
              <span className="num small" title={`${fmtBytes(r.down)} down · ${fmtBytes(r.up)} up`}>{fmtBytes(r.total)}</span>
            </div>
          ))}
        </div>
      )}
    </Card>
  );
}

function HappenedCard({ items }) {
  return (
    <Card title="What happened" desc="Newest first. Repeats are folded together.">
      {!items?.length ? <Empty>Nothing has happened yet.</Empty> : (
        <div className="ov-evs">
          {items.map((e, i) => (
            <div className={`ov-ev ${e.severity === 'warning' ? 'warn' : ''}`} key={i}>
              <time className="muted small">{new Date(e.ts * 1000).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}</time>
              <span>{e.to ? <button type="button" className="link" style={{ textAlign: 'left' }} onClick={() => go(e.to)}>{e.title}</button> : e.title}</span>
              {e.count > 1 ? <Pill>×{e.count}</Pill> : e.tag ? <Pill>{e.tag}</Pill> : <span />}
            </div>
          ))}
        </div>
      )}
      <button type="button" className="link small" style={{ textAlign: 'left' }} onClick={() => go('logs')}>Open logs →</button>
    </Card>
  );
}

const upText = (s) => { const d = Math.floor(s / 86400), h = Math.floor((s % 86400) / 3600), m = Math.floor((s % 3600) / 60); return d ? `${d} d ${h} h` : h ? `${h} h ${m} min` : `${m} min`; };

function MachineCard({ sys, upd }) {
  const [disk, setDisk] = useState(null);
  const [open, setOpen] = useState(false);
  const toggleDisk = async () => {
    if (open) return setOpen(false);
    setOpen(true);
    try { setDisk(await get('/api/system/disk')); } catch { setDisk({ parts: [] }); }
  };
  const gb = (n) => (n >= 1e11 ? Math.round(n / 1e9) : Math.round(n / 1e8) / 10);
  const g = (label, v, text, title, extra) => (
    <div key={label} title={title}>
      <div className="spread small"><span>{label}</span><b>{text}</b></div>
      <div className={`bar ${v > 85 ? 'warnbar' : ''}`}><i style={{ width: `${Math.min(100, v || 0)}%` }} /></div>
      {extra}
    </div>
  );
  const memText = sys?.mem_total ? `${Math.round(sys.mem || 0)} % · ${gb(sys.mem_used || 0)} of ${gb(sys.mem_total)} GB` : `${Math.round(sys?.mem || 0)} %`;
  const diskText = sys?.disk_size ? `${Math.round(sys.disk || 0)} % · ${gb(sys.disk_used || 0)} of ${gb(sys.disk_size)} GB` : `${Math.round(sys?.disk || 0)} %`;
  const cpuText = sys?.cores ? `${Math.round(sys.cpu || 0)} % · ${sys.cores} core${sys.cores === 1 ? '' : 's'}` : `${Math.round(sys?.cpu || 0)} %`;
  const loadTip = sys?.cores ? `Load average: ${sys.load1?.toFixed(2)} (1 min) · ${sys.load5?.toFixed(2)} (5 min) · ${sys.load15?.toFixed(2)} (15 min). Around ${sys.cores} or less is comfortable.` : undefined;
  return (
    <Card title="This machine" desc={`Gatehouse ${upd?.current || ''}${upd?.needs_reboot ? ' · restart needed' : ''}${sys?.uptime ? ` · up ${upText(sys.uptime)}` : ''}`}>
      <div className="ov-gauges">
        {g('Processor', sys?.cpu, cpuText, loadTip)}
        {g('Memory', sys?.mem, memText, 'Memory in use, not counting what the system keeps as a speed-up cache')}
        {g('Disk', sys?.disk, diskText, undefined, (
          <>
            <button type="button" className="linkbtn small" onClick={toggleDisk}>{open ? 'Hide' : "What's using it?"}</button>
            {open && (
              <div className="small stack" style={{ gap: 'var(--space-1)', marginTop: 'var(--space-1)' }}>
                {!disk ? <span className="muted">Working it out…</span> : !disk.parts?.length ? <span className="muted">Could not work that out.</span> : disk.parts.map((p) => (
                  <div className="spread" key={p.name}><span>{p.name}</span><span className="num muted">{fmtBytes(p.bytes)}</span></div>
                ))}
              </div>
            )}
          </>
        ))}
        {sys?.temp > 0 && g('Temperature', sys.temp, `${Math.round(sys.temp)} °C`)}
      </div>
    </Card>
  );
}

// ---------------------------------------------------------------- the page

export default function Overview() {
  const { data: dns, setData: setDns, updatedAt: dnsAt } = useData('/api/dns/summary', { every: 5000 });
  useStream(['dns.stats'], (t, p) => p && setDns((d) => (d ? { ...d, stats: p } : d)));
  const { data: net, updatedAt: netAt } = useData('/api/net/status', { topics: ['net.status'], every: 5000 });
  const { data: wans } = useData('/api/net/wans', { topics: ['net.status'] });
  const { data: thr, updatedAt: thrAt } = useData('/api/net/throughput', { every: 1000 });
  const { data: util } = useData('/api/dhcp/utilisation', { topics: ['dhcp.utilisation', 'dhcp.leases'], every: 10000 });
  const { data: devs } = useData('/api/overview/devices', { topics: ['dhcp.leases'], every: 10000 });
  const { data: fw } = useData('/api/fw/overview', { topics: ['fw.status'] });
  const { data: upd } = useData('/api/system/update', { every: 300000 });
  const { data: sec } = useData('/api/overview/security', { every: 10000 });
  const { data: h1 } = useData('/api/overview/history?range=1h', { every: 30000 });
  const { data: busiest } = useData('/api/overview/busiest', { every: 30000 });
  const { data: sys } = useData('/api/system/health', { every: 5000 });
  const { data: happened } = useData('/api/overview/happened', { topics: ['events', 'threat'], every: 20000 });

  const links = net?.links || {};
  const wanList = wans?.wans || [];
  const { items: attention, needs, worst } = useAttention();
  const stamp = Math.max(thrAt || 0, netAt || 0, dnsAt || 0) || null;
  const first = needs[0];

  return (
    <Page title="Overview"
      sub={`${devs?.online ?? 0} device${devs?.online === 1 ? '' : 's'} online`}
      actions={<LiveDot stamp={stamp} />}>
      <div className="ov-statusline">
        <span className={`ov-state ${needs.length ? (worst === 'bad' ? 'bad' : 'warn') : 'good'}`}><i className="dot" />{needs.length ? `${needs.length} thing${needs.length === 1 ? '' : 's'} need${needs.length === 1 ? 's' : ''} you` : 'Everything is working'}</span>
        {first && <button type="button" className="link" onClick={() => go(first.to)}>{first.t} →</button>}
        <UpdatedAgo stamp={stamp} />
      </div>
      {attention.length > 0 && (
        <Card className={`ov-attn ${needs.length ? '' : 'calm'}`}>
          <div className="stack" style={{ gap: 'var(--space-2)' }}>
            {attention.map((a, i) => (
              <div className="spread" key={i} style={{ alignItems: 'flex-start' }}>
                <div className="inline" style={{ alignItems: 'flex-start', flexWrap: 'nowrap' }}><i className={`navdot ${a.level}`} style={{ marginLeft: 0, marginTop: '.4rem', animation: 'none' }} /><div><div className="name">{a.t}</div><div className="small muted">{a.s}</div></div></div>
                <button className="btn small" onClick={() => go(a.to)}>Open</button>
              </div>
            ))}
          </div>
        </Card>
      )}
      <WhatsNewCard current={upd?.current} />
      <Hero wanList={wanList} links={links} thr={thr} />
      <section className="ov-g4" aria-label="Status">
        <DevicesTile d={devs} h={h1} />
        <DnsTile dns={dns} h={h1} />
        <DhcpTile util={util} h={h1} />
        <SecurityTile sec={sec} fw={fw} h={h1} />
      </section>
      <WorldCard />
      <section className="ov-g3">
        <HappenedCard items={happened} />
        <BusiestCard rows={busiest} />
        <MachineCard sys={sys} upd={upd} />
      </section>
      <NetworksCard fw={fw} wanList={wanList} links={links} devs={devs} h24={h1} />
    </Page>
  );
}

import React, { useEffect, useMemo, useState } from 'react';
import { del, put, post, useData, useStream } from '../api.js';
import { Button, Card, Empty, Field, Input, Note, NumberInput, Pill, Segmented, Select, ToggleRow, useAction, useToast, fmt, confirmBox } from '../ui.jsx';
import { Chart, fmtBits, fmtBytes } from '../charts.jsx';
import { linkState } from '../linkstate.js';

const GRADE = { A: 'good', B: 'good', C: 'warn', D: 'warn', F: 'bad' };
const GRADE_TEXT = {
  A: 'Calls and games stay smooth even while the line is busy.',
  B: 'Delay grows a little when the line is busy.',
  C: 'Delay grows noticeably when the line is busy. Calls may stutter during big downloads.',
  D: 'Delay grows a lot when the line is busy. Calls and games will suffer during downloads.',
  F: 'Delay explodes when the line is busy. Anything live will break up during downloads.',
};
const HOURS = Array.from({ length: 24 }, (_, h) => ({ value: String(h), label: `${String(h).padStart(2, '0')}:00` }));

function Result({ r, plan }) {
  if (!r) return <span className="small muted">Not tested yet.</span>;
  if (r.error) return <span className="small" style={{ color: 'var(--color-status-serious)' }}>{fmt.dt(r.ts)}: {r.error}</span>;
  const pct = plan?.down_mbps ? Math.round((100 * r.down_bps) / (plan.down_mbps * 1e6)) : 0;
  return (
    <div className="small">
      <div><b>{fmtBits(r.down_bps)}</b> down · <b>{fmtBits(r.up_bps)}</b> up · delay <b>{Math.round(r.delay_ms)} ms</b>{r.jitter_ms >= 1 ? ` (varies ${Math.round(r.jitter_ms)} ms)` : ''}
        {r.grade && <> · busy-line grade <Pill tone={GRADE[r.grade]}><span className="sp-grade">{r.grade}</span></Pill></>}</div>
      {pct > 0 && <div className="muted">{fmtBits(r.down_bps).replace(/ .*/, '')} of {plan.down_mbps} Mbps promised ({pct} %)</div>}
      <div className="muted">{fmt.dt(r.ts)} · {r.quick ? 'short test' : 'full test'} · {r.source === 'scheduled' ? 'scheduled' : 'run by hand'} · {r.server}</div>
      {r.grade && <div className="muted">{GRADE_TEXT[r.grade]}</div>}
    </div>
  );
}

function History({ links }) {
  const [link, setLink] = useState('');
  const [days, setDays] = useState('30');
  const { data } = useData(`/api/speedtest/history?days=${days}${link ? `&link=${encodeURIComponent(link)}` : ''}`, { topics: ['speedtest'] });
  const list = (data || []).filter((r) => !r.error);
  const now = Math.floor(Date.now() / 1000);
  const asc = [...list].reverse();
  const series = link
    ? [{ key: 'down', name: 'Download', color: 'var(--color-chart-series-1)', pts: asc.map((r) => [r.ts, r.down_bps]) }, { key: 'up', name: 'Upload', color: 'var(--color-chart-series-2)', pts: asc.map((r) => [r.ts, r.up_bps]) }]
    : links.map((l, i) => ({ key: l.name, name: `${l.name} download`, color: `var(--color-chart-series-${(i % 4) + 1})`, pts: asc.filter((r) => r.link === l.name).map((r) => [r.ts, r.down_bps]) }));
  return (
    <Card title="Speed test history" desc="Kept for one year. Each dot is one test."
      actions={<div className="inline"><Select value={link} onChange={setLink} options={[{ value: '', label: 'All connections (download)' }, ...links.map((l) => ({ value: l.name, label: l.name }))]} aria-label="Connection" />
        <Segmented value={days} onChange={setDays} options={[{ value: '30', label: '30 days' }, { value: '90', label: '90 days' }, { value: '365', label: '1 year' }]} /></div>}>
      {!list.length ? <Empty>No speed tests in this time yet. Run one above, or switch on the daily test.</Empty> : (
        <>
          <div className="small sec inline">{series.map((s) => <span key={s.key}><i style={{ display: 'inline-block', width: '.65rem', height: '.65rem', borderRadius: 3, marginRight: 5, background: s.color }} />{s.name}</span>)}</div>
          <Chart series={series} from={now - Number(days) * 86400} to={now} kind="bits" height={200} dots filled={false} label="Speed test results over time" />
          <div className="scroll">
            <table className="table">
              <thead><tr><th>When</th><th>Connection</th><th>Download</th><th>Upload</th><th>Delay</th><th>Busy-line grade</th><th>Server</th><th>How</th></tr></thead>
              <tbody>
                {(data || []).slice(0, 15).map((r) => (
                  <tr key={r.id}>
                    <td>{fmt.dt(r.ts)}</td><td>{r.link}</td>
                    {r.error ? <td colSpan={4} style={{ color: 'var(--color-status-serious)' }}>{r.error}</td> : <><td>{fmtBits(r.down_bps)}</td><td>{fmtBits(r.up_bps)}</td><td>{Math.round(r.delay_ms)} ms</td><td>{r.grade && <Pill tone={GRADE[r.grade]}>{r.grade}</Pill>}</td></>}
                    <td className="small muted">{r.server}</td><td className="small muted">{r.source === 'scheduled' ? 'Scheduled' : 'By hand'}{r.quick ? ' · short' : ''}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </>
      )}
    </Card>
  );
}

// The servers a test can run against. The default is used by the daily test.
function Servers({ st, reload }) {
  const [run, busy] = useAction();
  const [name, setName] = useState('');
  const [url, setUrl] = useState('');
  if (!st) return null;
  const list = st.servers || [];
  const add = async () => { if (await run(() => post('/api/speedtest/servers', { name, url }), 'Added.')) { setName(''); setUrl(''); reload(); } };
  return (
    <Card title="Test servers" desc="Where the speed test sends its data. The default is used by the daily test; when you run a test by hand you can pick another from the list.">
      <div className="list">
        {list.map((v) => (
          <div className="item" key={v.url}>
            <div>
              <div className="name">{v.name} {v.url === st.server && <Pill tone="good">Default</Pill>}</div>
              <div className="small muted mono">{v.url}</div>
            </div>
            <div className="actions">
              {v.url !== st.server && <Button small disabled={busy} onClick={async () => { if (await run(() => put('/api/speedtest/default', { url: v.url }), 'Default changed.')) reload(); }}>Make default</Button>}
              {v.url !== st.default_server && <Button small kind="danger" disabled={busy} onClick={async () => { if (await run(() => del(`/api/speedtest/servers?url=${encodeURIComponent(v.url)}`), 'Removed.')) reload(); }}>Remove</Button>}
            </div>
          </div>
        ))}
      </div>
      <div className="addrow">
        <Field label="Name"><Input value={name} onChange={setName} placeholder="My server" /></Field>
        <Field label="Address"><Input value={url} onChange={setUrl} className="in mono" placeholder="https://speed.example.com" /></Field>
        <Button disabled={busy || !url} onClick={add}>Add server</Button>
      </div>
      <p className="small muted">The address starts with https:// and must be the same kind of server as Cloudflare's (it answers /__down and /__up).</p>
      <p className="small muted">Cloudflare's test is public and needs no account. Its terms for automated daily use are not confirmed, so check them or add your own server if that matters to you. Gatehouse checks that a new server answers before keeping it.</p>
    </Card>
  );
}

export default function SpeedTest({ wans, links }) {
  const { data: st, reload } = useData('/api/speedtest', { topics: ['speedtest'], every: 5000 });
  const { data: recent, reload: rr } = useData('/api/speedtest/history?days=365', { topics: ['speedtest'] });
  const [run, busy] = useAction();
  const toast = useToast();
  const [edit, setEdit] = useState(null);
  useStream(['speedtest'], () => { reload(); rr(); });
  // While a test runs, ask every second so the result shows up as soon as it is saved.
  const running = st?.run?.running;
  useEffect(() => {
    if (!running) return undefined;
    const t = setInterval(() => { reload(); rr(); }, 1500);
    return () => clearInterval(t);
  }, [running, reload, rr]);

  const rows = st?.links || [];
  const last = useMemo(() => { const m = {}; (recent || []).forEach((r) => { if (!m[r.link]) m[r.link] = r; }); return m; }, [recent]);
  const form = edit || (st && { schedule: st.schedule, hour: st.hour, links: Object.fromEntries(rows.map((l) => [l.name, { down_mbps: l.down_mbps || 0, up_mbps: l.up_mbps || 0, metered: l.metered }])) });
  const setF = (patch) => setEdit({ ...form, ...patch });
  const setL = (n, patch) => setF({ links: { ...form.links, [n]: { ...form.links[n], ...patch } } });
  const dirty = !!edit;
  const save = async () => { if (await run(() => put('/api/speedtest', { ...form, hour: Number(form.hour) }), 'Saved.')) { setEdit(null); reload(); } };
  const go = async (name, full, server) => { if (await run(() => post('/api/speedtest/run', { link: name, full, server }))) { toast(`Testing ${name}…`); reload(); } };
  if (!wans?.length) return null;

  return (
    <>
      <Card title="Speed test" desc="Measures each connection by itself, even one that is only standing by. A test takes about 20 seconds. It uses as much data as the line can carry in that time, about 400 MB on a 400 Mbps line.">
        {running && <Note tone="" title={`Testing ${st.run.link}…`}>Downloading, then uploading. Other devices may notice the line is busy for a moment.</Note>}
        <div className="list">
          {rows.map((l) => {
            const ls = linkState(links?.[l.name]);
            return (
              <div className="item" key={l.name} style={{ alignItems: 'flex-start' }}>
                <div>
                  <div className="name">{l.name} <Pill tone={ls.tone}>{ls.health === 'working' ? (links?.[l.name]?.active ? 'In use' : 'Standing by') : ls.text}</Pill>{l.metered && <Pill tone="warn">Data limit</Pill>}</div>
                  <Result r={last[l.name]} plan={l} />
                  {!last[l.name] && l.last_error && <div className="small" style={{ color: 'var(--color-status-serious)' }}>{l.last_error}</div>}
                  {l.metered && <div className="small muted">This connection looks like it has a data limit, so a short test is run (usually under 10 MB). The full test would use hundreds of MB.</div>}
                </div>
                <div className="actions">
                  <Button small disabled={busy || running} onClick={() => go(l.name, false)}>{l.metered ? 'Run short test' : 'Run test'}</Button>
                  {(st?.servers || []).length > 1 && (
                    <Select value="" onChange={(v) => v && go(l.name, false, v)} options={[{ value: '', label: 'Test using…' }, ...st.servers.map((v) => ({ value: v.url, label: v.name }))]} />
                  )}
                  {l.metered && <Button small disabled={busy || running} onClick={async () => { if (await confirmBox(`The full test may use several hundred MB of ${l.name}'s data allowance. Run it anyway?`)) go(l.name, true); }}>Run full test</Button>}
                </div>
              </div>
            );
          })}
        </div>
        {form && (
          <div className="stack" style={{ marginTop: 'var(--space-3)' }}>
            <h3 style={{ fontSize: 'var(--font-size-base)', fontWeight: 'var(--font-weight-semibold)' }}>What you pay for</h3>
            <p className="small muted">Optional. Fill in the speed your plan promises and results show how close you get.</p>
            {rows.map((l) => (
              <div className="payrow" key={l.name}>
                <b>{l.name}</b>
                <div className="formgrid">
                  <Field label="Download (Mbps)"><NumberInput value={form.links[l.name]?.down_mbps || 0} min={0} max={100000} onChange={(v) => setL(l.name, { down_mbps: v })} /></Field>
                  <Field label="Upload (Mbps)"><NumberInput value={form.links[l.name]?.up_mbps || 0} min={0} max={100000} onChange={(v) => setL(l.name, { up_mbps: v })} /></Field>
                </div>
                <ToggleRow title="Has a data limit" desc="Short tests only, never scheduled." checked={!!form.links[l.name]?.metered} onChange={(v) => setL(l.name, { metered: v })} />
              </div>
            ))}
            <ToggleRow title="Test every day" desc="Runs one connection after another at the hour you choose, so you can see how the line behaves over weeks. Connections with a data limit are never tested this way." checked={form.schedule} onChange={(v) => setF({ schedule: v })} />
            {form.schedule && <Field label="Start at"><Select value={String(form.hour)} onChange={(v) => setF({ hour: Number(v) })} options={HOURS} /></Field>}
            {dirty && <div className="inline"><Button kind="primary" disabled={busy} onClick={save}>Save</Button><Button onClick={() => setEdit(null)}>Cancel</Button></div>}
          </div>
        )}
      </Card>
      <Servers st={st} reload={reload} />
      <History links={rows} />
    </>
  );
}

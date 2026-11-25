import { useHashTab } from '../hashtab.js';
import React, { useState } from 'react';
import { get, post, put, useData } from '../api.js';
import { Button, Card, Empty, Field, Input, Limited, Note, NumberInput, Page, Pill, Segmented, Select, Switch, Tabs, TimeField, ToggleRow, useAction, fmt } from '../ui.jsx';
import { Hit } from './AdminActivity.jsx';
import { fmtBytes } from '../charts.jsx';
import NetTools from './NetTools.jsx';
import Terminal from './Terminal.jsx';

// What each service is doing, its log, and the way in when something has gone wrong. It only looks
// and restarts; nothing here touches traffic.

const tone = { running: 'good', starting: 'warn', failing: 'bad', stopped: '', missing: '' };
const word = { running: 'Running', starting: 'Starting', failing: 'Failing', stopped: 'Off', missing: 'Not installed' };

function Services() {
  const { data, reload } = useData('/api/diag/services', { every: 10000 });
  const [run, busy] = useAction();
  const [log, setLog] = useState(null);
  if (!data) return null;
  return (
    <div className="stack">
      <Card title="Services" desc={`${data.version ? `NopeWarden ${data.version}. ` : ''}Each part of the system, and how it is doing.`}
        actions={<Button small onClick={() => run(async () => { const r = await fetch('/api/diag/bundle', { credentials: 'same-origin' }); if (!r.ok) throw new Error('The bundle could not be made.'); const u = URL.createObjectURL(await r.blob()); const a = document.createElement('a'); a.href = u; a.download = 'nopewarden-support-bundle.txt'; a.click(); URL.revokeObjectURL(u); }, 'Saved. Passwords and keys are hidden in it.')} disabled={busy}>Save a support bundle</Button>}>
        <div className="list">
          {(data.services || []).filter((s) => s.state !== 'missing').map((s) => (
            <div className="item" key={s.unit}>
              <div className="fill">
                <div className="name">{s.title} <Pill tone={tone[s.state]}>{word[s.state]}</Pill></div>
                {s.reason && <div className="meta">{s.reason}</div>}
              </div>
              <div className="actions">
                <Button small onClick={() => setLog(s.unit)}>Show log</Button>
                {s.can_restart && <Button small disabled={busy} onClick={() => run(() => post('/api/diag/restart', { unit: s.unit }), 'Restarting.').then(reload)}>Restart</Button>}
              </div>
            </div>
          ))}
          {!(data.services || []).length && <Empty>No services found on this machine.</Empty>}
        </div>
        <p className="small muted">Services that are not installed on this machine are left out. A support bundle is a text file with the machine, network and service logs, with passwords and keys hidden. Send it when asking for help.</p>
      </Card>
      {log && <LogView unit={log} />}
    </div>
  );
}

function LogView({ unit: first }) {
  const { data } = useData('/api/diag/services');
  const [unit, setUnit] = useState(first);
  const [lines, setLines] = useState('200');
  const [q, setQ] = useState('');
  const [text, setText] = useState(null);
  const [run, busy] = useAction();
  const load = async () => { const r = await run(() => get(`/api/diag/log?unit=${encodeURIComponent(unit)}&lines=${lines}`)); if (r) setText(r.log); };
  React.useEffect(() => { load(); /* eslint-disable-next-line */ }, [unit, lines]);
  const shown = (text || '').split('\n').filter((l) => !q || l.toLowerCase().includes(q.toLowerCase())).join('\n');
  const opts = [{ value: 'system', label: 'Warnings from everything' }, ...(data?.services || []).filter((s) => s.state !== 'missing').map((s) => ({ value: s.unit, label: s.title }))];
  return (
    <Card title="Log" desc="The most recent lines the service wrote. Anything that looks like a password or key is hidden.">
      <div className="formgrid">
        <Field label="Show the log of"><Select value={unit} onChange={setUnit} options={opts} /></Field>
        <Field label="How many lines"><Select value={lines} onChange={setLines} options={[{ value: '100', label: 'Last 100' }, { value: '200', label: 'Last 200' }, { value: '500', label: 'Last 500' }]} /></Field>
        <Field label="Only lines containing"><Input value={q} onChange={setQ} placeholder="a word" /></Field>
      </div>
      <div className="actions"><Button small disabled={busy} onClick={load}>Refresh</Button></div>
      <pre className="logpre mono small" tabIndex={0}>{text === null ? 'Loading.' : shown || 'Nothing matches.'}</pre>
    </Card>
  );
}

// ---------------------------------------------------------------- verdict, health, connections

function Verdict() {
  const { data } = useData('/api/diag/health', { every: 15000 });
  if (!data) return null;
  const to = (t) => { const [pg, tab] = t.split('/'); window.location.hash = `#/${pg}${tab ? `/${tab}` : ''}`; };
  return (
    <Card title="Is NopeWarden itself working?" desc="How NopeWarden is running, not what your network is doing. For that, open Logs.">
      <div className="stack">
        <div><Pill tone={data.level === 'good' ? 'good' : data.level === 'bad' ? 'bad' : 'warn'}>{data.text}</Pill></div>
        {data.issues.length > 0 && (
          <div className="list">
            {data.issues.map((i, k) => (
              <div className="item" key={k}>
                <div className="fill"><div className="name"><Pill tone={i.level === 'bad' ? 'bad' : 'warn'}>{i.level === 'bad' ? 'Problem' : 'Check'}</Pill> {i.text}</div></div>
                {i.to && <div className="actions"><Button small onClick={() => to(i.to)}>Look</Button></div>}
              </div>
            ))}
          </div>
        )}
        <dl className="facts">
          {data.facts.map((f) => <div key={f.label}><dt className="small muted">{f.label}</dt><dd>{f.level ? <Pill tone={f.level === 'bad' ? 'bad' : 'warn'}>{f.value}</Pill> : f.value}</dd></div>)}
        </dl>
      </div>
    </Card>
  );
}

function Connections() {
  const { data } = useData('/api/diag/connections', { every: 15000 });
  const [older, setOlder] = useState(false);
  if (!data) return null;
  // One row per visit: the closing entry says how long it lasted and why, so the matching opening row is left out.
  const visits = (data.recent || []).filter((e) => e.event === 'closed');
  const shown = older ? visits : visits.slice(0, 5);
  const reasons = Object.entries(data.by_reason || {});
  return (
    <Card title="Console connections" desc="The live connection your browser keeps open to NopeWarden. If the browser reports dropped connections, this shows whether NopeWarden ended them or the network between ended them.">
      <div className="stack">
        <p className="small muted">{data.open} open now, {fmt.n(data.opened)} opened and {fmt.n(data.closed)} closed since NopeWarden started.{reasons.length ? ` Closed because: ${reasons.map(([w, n]) => `${w} (${n})`).join(', ')}.` : ''} The browser going away is normal (a page closed or reloaded); a failed write usually means the network between you and the box dropped it.</p>
        {!visits.length ? <Empty>{data.open ? 'The only console connection so far is still open.' : 'No console has connected since NopeWarden started.'}</Empty> : (
          <>
            <div className="scroll"><table className="table">
              <thead><tr><th>Opened</th><th>From</th><th>Lasted</th><th>Why it ended</th></tr></thead>
              <tbody>{shown.map((e, i) => <tr key={i}><td className="small">{fmt.dt(e.at - (e.seconds || 0))}</td><td className="mono small">{e.addr}</td><td className="small">{e.seconds ? `${e.seconds} s` : 'under a second'}</td><td className="small muted">{e.why || ''}</td></tr>)}</tbody>
            </table></div>
            {visits.length > 5 && <div><Button small onClick={() => setOlder(!older)}>{older ? 'Show fewer' : `Show older (${visits.length - 5})`}</Button></div>}
          </>
        )}
      </div>
    </Card>
  );
}

// ---------------------------------------------------------------- system log

// Messages the box prints that have a plainer meaning, and noise that means nothing. A matching line
// gets its explanation; known noise is hidden unless "everything" is on.
const KNOWN = [
  [/link (is )?down|NIC Link is Down/i, 'A network cable or port lost its link.'],
  [/link (is )?up|NIC Link is Up/i, 'A network cable or port came up.'],
  [/out of memory|oom-kill/i, 'The machine ran out of memory and stopped a program.'],
  [/I\/O error|ext4-fs error|EXT4-fs error/i, 'The disk reported a read or write error. Check the disk.'],
  [/Failed password|authentication failure/i, 'Someone typed a wrong password for remote login.'],
  [/Accepted (password|publickey)/i, 'A remote login succeeded.'],
  [/ntpd?|timesyncd.*(Synchroni[sz]ed|Initial)/i, 'The clock was set from the internet.'],
];
const NOISE = [/systemd\[1\]: (Started|Starting|Stopped|Stopping) (Session|User Manager|Time & Date)/i, /pam_unix\(cron:session\)/i, /CRON\[\d+\]/i];

const SOURCES = [{ value: 'system-all', label: 'Everything on this machine' }];

function SystemLog() {
  const { data: svc } = useData('/api/diag/services');
  const [unit, setUnit] = useState('system-all');
  const [everything, setEverything] = useState(false);
  const [q, setQ] = useState('');
  const [hours, setHours] = useState('24');
  const [follow, setFollow] = useState(false);
  const [res, setRes] = useState(null);
  const [run, busy] = useAction();
  const load = async () => {
    const u = unit === 'system-all' && !everything ? 'system' : unit;
    const r = await run(() => get(`/api/diag/log?unit=${encodeURIComponent(u)}&lines=300&hours=${hours}&q=${encodeURIComponent(q)}`));
    if (r) setRes(r);
  };
  React.useEffect(() => { const t = setTimeout(load, 300); return () => clearTimeout(t); /* eslint-disable-next-line */ }, [unit, everything, q, hours]);
  React.useEffect(() => { if (!follow) return undefined; const t = setInterval(load, 5000); return () => clearInterval(t); /* eslint-disable-next-line */ }, [follow, unit, everything, q, hours]);
  const words = q.trim().split(/\s+/).filter(Boolean);
  const rows = (res?.log || '').split('\n').filter(Boolean).map((l) => ({ l, known: (KNOWN.find(([re]) => re.test(l)) || [])[1], noise: NOISE.some((re) => re.test(l)) })).filter((x) => everything || !x.noise);
  const opts = [...SOURCES, ...(svc?.services || []).filter((s) => s.state !== 'missing').map((s) => ({ value: s.unit, label: s.title }))];
  return (
    <Card title="System log" desc="Messages from NopeWarden and the machine it runs on. By default only warnings and errors are shown; switch on Everything to see all of it. Anything that looks like a password or key is hidden.">
      <div className="stack">
        <div className="formgrid">
          <Field label="From"><Select value={unit} onChange={setUnit} options={opts} /></Field>
          <Field label="Time"><Select value={hours} onChange={setHours} options={[{ value: '1', label: 'Last hour' }, { value: '24', label: 'Last 24 hours' }, { value: '168', label: 'Last 7 days' }, { value: '0', label: 'All that is kept' }]} /></Field>
          <Field label="Search" hint="Every word must appear in the line."><Input value={q} onChange={setQ} placeholder="a word or several" aria-label="Search the system log" /></Field>
        </div>
        <div className="inline">
          <label className="inline small"><input type="checkbox" checked={everything} onChange={(e) => setEverything(e.target.checked)} /> Everything, not only warnings and errors</label>
          <label className="inline small"><input type="checkbox" checked={follow} onChange={(e) => setFollow(e.target.checked)} /> Follow (refresh every 5 seconds)</label>
          <Button small disabled={busy} onClick={load}>Refresh</Button>
        </div>
        {res && <p className="small muted">{fmt.n(res.matched)} of {fmt.n(res.scanned)} lines searched match{words.length ? '' : ' your choices'}. Showing the newest {fmt.n(rows.length)}.</p>}
        {!res ? <span className="muted">Loading…</span> : !rows.length ? (
          <Empty>{words.length ? 'Nothing matches. What your network did is in Logs; who changed what is in Logs, Admin activity.' : everything ? 'Nothing to show for these choices.' : 'No warnings or errors in this time. That is good news.'}{!everything && !words.length && <> <button type="button" className="link" onClick={() => setEverything(true)}>Show everything</button></>}</Empty>
        ) : (
          <pre className="logpre mono small" tabIndex={0}>{rows.map((x, i) => <div key={i}><Hit text={x.l} words={words} />{x.known && <span className="muted">{'  ← '}{x.known}</span>}</div>)}</pre>
        )}
      </div>
    </Card>
  );
}

function DiskUse() {
  const { data } = useData('/api/system/disk', { every: 60000 });
  const [all, setAll] = useState(false);
  return (
    <Card title="What is using the disk" desc="The biggest places on this machine's disk, so you can see what to clear if it fills up.">
      {!data ? <Empty>Working it out…</Empty> : !data.parts?.length ? <Empty>Could not work that out.</Empty> : (
        <div className="list">
          {(all ? data.parts : data.parts.slice(0, 3)).map((p) => <div className="item" key={p.name}><div className="fill"><div className="name">{p.name}</div></div><span className="num muted">{fmtBytes(p.bytes)}</span></div>)}
          {data.parts.length > 3 && <div><Button small onClick={() => setAll(!all)}>{all ? 'Show fewer' : `Show all (${data.parts.length})`}</Button></div>}
        </div>
      )}
    </Card>
  );
}

// Drive health, from what each drive says about itself. One plain word for each drive; Details opens
// a blue box with everything it reported and, in plain lines, what it did not.
const DISK_TONE = { bad: 'bad', warn: 'warn', info: 'info' };
const DAYS = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday'].map((l, i) => ({ value: i, label: l }));
function DiskHealth() {
  const { data, reload } = useData('/api/disk/health', { every: 300000 });
  const [run, busy] = useAction();
  const [open, setOpen] = useState('');
  const [sched, setSched] = useState(null);
  if (!data) return null;
  if (!data.drives?.length) return null;
  const needsTool = !data.tool && data.drives.some((d) => d.kind !== 'Virtual disk');
  const size = (b) => (b >= 1e12 ? `${(b / 1e12).toFixed(1)} TB` : `${Math.round(b / 1e9)} GB`);
  const problem = data.drives.some((d) => d.level === 'warn' || d.level === 'bad');
  const alertsOff = !data.alerts?.on || !data.alerts?.channel;
  const sc = sched || data.schedule;
  const setWhen = (k, v) => setSched({ ...sc, [k]: { ...sc[k], ...v } });
  const testable = (d) => d.kind !== 'Virtual disk' && d.word !== 'Not reported' && !d.gone;
  return (
    <Card title="Drive health" desc="What each drive says about itself, read every 15 minutes. A drive is only marked amber or red when it reports a problem or its error counts rise.">
      <div className="stack">
        {needsTool && (
          <Note tone="info" title="NopeWarden needs a small program to read drive health">
            It is called smartmontools. {data.can_install ? 'Install it here; it is a small download from Debian.' : 'This machine cannot install it by itself, so install smartmontools yourself.'}
            {data.can_install && <div className="actions"><Button kind="primary" disabled={busy} onClick={() => run(() => post('/api/disk/tool', {}), 'Installed.').then(reload)}>Install the SMART tool</Button></div>}
          </Note>
        )}
        {problem && alertsOff && (
          <Note tone="warn" title="A drive has a problem and drive alerts are off">
            {data.alerts?.channel ? 'The alert "A drive has a problem" is switched off, so nothing will tell you about this.' : 'No email or phone alerts are set up, so nothing will tell you about this.'} It shows here, on the Overview and on the Diagnostics menu dot. Turn the alert on under Alerts.
          </Note>
        )}
        <div className="list">
          {data.drives.map((d) => (
            <div key={d.name} className="stack gap1">
              <div className="item">
                <div className="fill">
                  <div className="name">{d.model}</div>
                  <div className="meta">{d.kind}{d.bytes ? `, ${size(d.bytes)}` : ''}, /dev/{d.name}. {d.reason}</div>
                  {d.trend?.map((t) => <div key={t} className="meta"><strong>{t}.</strong></div>)}
                  {d.log_errors?.length > 0 && <div className="meta"><strong>The system log shows disk errors in the last day.</strong></div>}
                  {d.test && <div className="meta">{d.test.running ? `A ${d.test.kind.toLowerCase()} self-test is running.` : `Last self-test (${d.test.kind.toLowerCase()}): ${d.test.result.toLowerCase()}.`}</div>}
                </div>
                <div className="actions">
                  <Pill tone={DISK_TONE[d.level] || (d.word === 'Healthy' ? 'good' : '')}>{d.word}</Pill>
                  {d.word !== 'Not reported' && !d.gone && <Button small onClick={() => setOpen(open === d.name ? '' : d.name)}>{open === d.name ? 'Hide details' : 'Details'}</Button>}
                </div>
              </div>
              {open === d.name && (
                <Note tone="info" title={`${d.model}: what it reported`}>
                  <div className="list">
                    {d.lines.map((l) => <div className="item" key={l.label}><div className="fill">{l.label}</div><span className="num">{l.value}</span></div>)}
                  </div>
                  {d.missing?.length > 0 && <p className="small muted">Not reported by this hardware: {d.missing.join(', ')}.</p>}
                  {d.log_errors?.length > 0 && <pre className="small">{d.log_errors.join('\n')}</pre>}
                  {testable(d) && data.tool && (
                    <div className="actions">
                      <Button small disabled={busy || d.test?.running} onClick={() => run(() => post('/api/disk/test', { drive: d.name, kind: 'short' }), 'Short test started. It takes a couple of minutes.').then(reload)}>Run a short self-test</Button>
                      <Button small disabled={busy || d.test?.running} onClick={() => run(() => post('/api/disk/test', { drive: d.name, kind: 'long' }), 'Long test started. It can take an hour or more.').then(reload)}>Run a long self-test</Button>
                    </div>
                  )}
                </Note>
              )}
            </div>
          ))}
        </div>
        {data.power && <p className="small muted">{data.power}</p>}
        {data.backup?.note && <p className="small muted">{data.backup.note}</p>}
        {data.tool && (
          <div className="stack gap1">
            <div className="name">Self-test schedule</div>
            <p className="small muted">Each drive tests itself in the background and keeps working. The result shows beside its verdict. A test starts within 15 minutes of the time chosen.</p>
            <div className="row" style={{ alignItems: "center", gap: "var(--space-2)" }}>
              <Switch checked={sc.short.on} onChange={(v) => setWhen('short', { on: v })} label="Short self-test" />
              <span>Short test every</span>
              <Select value={sc.short.day} onChange={(v) => setWhen('short', { day: Number(v) })} options={DAYS} style={{ width: '11rem' }} />
              <span>at</span>
              <span style={{ width: '9rem' }}><TimeField value={sc.short.at} onChange={(v) => setWhen('short', { at: v })} /></span>
            </div>
            <div className="row" style={{ alignItems: "center", gap: "var(--space-2)" }}>
              <Switch checked={sc.long.on} onChange={(v) => setWhen('long', { on: v })} label="Long self-test" />
              <span>Long test</span>
              <Select value={sc.long.every} onChange={(v) => setWhen('long', { every: v })} options={[{ value: 'month', label: 'on the first' }, { value: 'week', label: 'every' }]} style={{ width: '10rem' }} />
              <Select value={sc.long.day} onChange={(v) => setWhen('long', { day: Number(v) })} options={DAYS} style={{ width: '11rem' }} />
              <span>at</span>
              <span style={{ width: '9rem' }}><TimeField value={sc.long.at} onChange={(v) => setWhen('long', { at: v })} /></span>
            </div>
            {sched && <div className="actions"><Button kind="primary" disabled={busy} onClick={() => run(() => put('/api/disk/schedule', sched), 'Saved.').then(() => { setSched(null); reload(); })}>Save schedule</Button><Button disabled={busy} onClick={() => setSched(null)}>Discard</Button></div>}
          </div>
        )}
      </div>
    </Card>
  );
}

// A UPS (battery backup) watched through NUT. What happens for each event is chosen here, per event.
const POWER_ACTS = [['log', 'Write it in the log'], ['alert', 'Send an alert'], ['overview', 'Show on Overview'], ['shutdown', 'Shut down cleanly']];
function PowerUPS() {
  const { data, reload } = useData('/api/power', { every: 20000, topics: ['events'] });
  const [run, busy] = useAction();
  const [form, setForm] = useState(null);
  const [src, setSrc] = useState({ source: 'usb', host: '', name: '' });
  const [real, setReal] = useState(false);
  if (!data) return null;
  const st = data.status || {};
  const cfg = form || data.config;
  const set = (k, v) => setForm({ ...cfg, [k]: v });
  const setAct = (id, k, v) => setForm({ ...cfg, events: { ...cfg.events, [id]: { ...cfg.events[id], [k]: v } } });
  const alertsOff = !data.alerts?.on || !data.alerts?.channel;
  const mins = (s) => (s < 0 ? 'Not reported' : s >= 5400 ? `${(s / 3600).toFixed(1)} hours` : `${Math.round(s / 60)} minutes`);
  const pct = (n) => (n < 0 ? 'Not reported' : `${n}%`);
  if (!data.config.on) {
    return (
      <Card title="Power (UPS)" desc="Watch a battery backup. NopeWarden tells you when the power fails and can shut the machine down cleanly before the battery runs out.">
        <div className="stack">
          <p className="small muted">APC and most other makes work. A UPS on a USB cable is found by itself; one on the network is read from the machine it is attached to.</p>
          <div><Segmented value={src.source} onChange={(v) => setSrc({ ...src, source: v })} options={[{ value: 'usb', label: 'On a USB cable' }, { value: 'network', label: 'On the network' }]} /></div>
          {src.source === 'network' && (
            <div className="grid2" style={{ alignItems: 'start' }}>
              <Field label="Address of the machine"><Input value={src.host} onChange={(v) => setSrc({ ...src, host: v })} placeholder="192.168.1.5" /></Field>
              <Field label="UPS name" hint="Usually ups."><Input value={src.name} onChange={(v) => setSrc({ ...src, name: v })} placeholder="ups" /></Field>
            </div>
          )}
          <div className="actions"><Button kind="primary" disabled={busy} onClick={() => run(() => post('/api/power/setup', src), 'UPS monitoring is on.').then(reload)}>Set up UPS monitoring</Button></div>
          {!st.tool && st.can_install && <p className="small muted">This installs the small UPS program (NUT) from Debian first.</p>}
        </div>
      </Card>
    );
  }
  const state = !st.answering ? ['Not answering', 'bad'] : st.on_battery ? ['On battery', 'warn'] : ['On mains power', 'good'];
  return (
    <Card title="Power (UPS)" desc="Watch a battery backup. NopeWarden tells you when the power fails and can shut the machine down cleanly before the battery runs out.">
      <div className="stack">
        {data.shutdown_at > 0 && (
          <Note tone="bad" title="A clean shutdown is about to start">
            {data.shutdown_why}. The machine shuts down at {new Date(data.shutdown_at * 1000).toLocaleTimeString()}.
            <div className="actions"><Button kind="primary" disabled={busy} onClick={() => run(() => post('/api/power/cancel', {}), 'Cancelled.').then(reload)}>Cancel the shutdown</Button></div>
          </Note>
        )}
        {alertsOff && <Note tone="warn" title="Power alerts are off">{data.alerts?.channel ? 'The alert "The power fails (UPS)" is switched off, so nothing will message you.' : 'No email or phone alerts are set up, so nothing will message you.'} Events still show here, in the log and on the Overview. Turn it on under Alerts.</Note>}
        <div className="item">
          <div className="fill"><div className="name">{[st.maker, st.model].filter(Boolean).join(' ') || 'UPS'}</div><div className="meta">{st.answering ? (st.replace ? 'The battery needs replacing.' : 'The battery is reported healthy.') : (st.error || 'The UPS did not answer.')}</div></div>
          <Pill tone={state[1]}>{state[0]}</Pill>
        </div>
        {st.answering && (
          <div className="list">
            {[['Battery charge', pct(st.charge)], ['Time left on battery', mins(st.runtime)], ['Load', pct(st.load)], ['Input voltage', st.input_v ? `${st.input_v} V` : 'Not reported'], ['Battery health', st.replace ? 'Needs replacing' : 'Good']].map(([l, v]) => <div className="item" key={l}><div className="fill">{l}</div><span className="num">{v}</span></div>)}
          </div>
        )}
        {data.past?.length > 0 && <p className="small muted">Recent power cuts: {data.past.map((p) => `${new Date(p.start * 1000).toLocaleString([], { dateStyle: 'medium', timeStyle: 'short' })}${p.end ? `, ${Math.max(1, Math.round((p.end - p.start) / 60))} min` : ', still out'}`).join('; ')}.</p>}
        <div className="name">What happens when</div>
        <div className="list">
          {data.events.map((e) => (
            <div key={e.ID} className="stack gap1" style={{ marginBottom: "var(--space-3)" }}>
              <div className="fill"><div className="name">{e.Label}</div><div className="meta">{e.When}</div></div>
              <div className="row">
                {POWER_ACTS.filter(([k]) => k !== 'shutdown' || e.CanShutdown).map(([k, l]) => (
                  <label key={k} className="row" style={{ alignItems: "center", gap: "var(--space-2)" }}><Switch checked={!!cfg.events[e.ID]?.[k]} onChange={(v) => setAct(e.ID, k, v)} label={`${e.Label}: ${l}`} /><span className="small">{l}</span></label>
                ))}
              </div>
            </div>
          ))}
        </div>
        <div className="grid2" style={{ alignItems: 'start' }}>
          <Field label="Battery counts as low at (% charge)"><NumberInput value={cfg.low_charge} onChange={(v) => set('low_charge', v)} /></Field>
          <Field label="or when this many minutes are left"><NumberInput value={cfg.low_minutes} onChange={(v) => set('low_minutes', v)} /></Field>
          <Field label="Wait before shutting down (seconds)" hint="Gives the power a chance to return."><NumberInput value={cfg.delay} onChange={(v) => set('delay', v)} /></Field>
        </div>
        <ToggleRow title="Switch the UPS off and back on with the machine" desc="After a low-battery shutdown, NopeWarden tells the UPS to cut its output and start again when the power returns, so the machine comes back by itself. The machine's firmware setting 'Restore on AC power loss' must be 'Power on'." checked={!!cfg.cut_output} onChange={(v) => set('cut_output', v)} />
        <div className="actions">
          {form && <Button kind="primary" disabled={busy} onClick={() => run(() => put('/api/power/config', form), 'Saved.').then(() => { setForm(null); reload(); })}>Save</Button>}
          {form && <Button disabled={busy} onClick={() => setForm(null)}>Discard</Button>}
        </div>
        <div className="stack gap1">
          <div className="name">Test power cut</div>
          <p className="small muted">Plays a power cut, a low battery and the power coming back inside NopeWarden. The alerts and log lines are real and marked as a test. The machine is not shut down unless you tick the box.</p>
          <label className="row" style={{ alignItems: "center", gap: "var(--space-2)" }}><Switch checked={real} onChange={setReal} label="Really shut down this machine" /><span className="small">Really shut down this machine during the test (you can cancel it)</span></label>
          <div className="actions">
            <Button disabled={busy || data.testing} onClick={() => run(() => post('/api/power/test', { shutdown: real }), 'The test is running.').then(reload)}>Test power cut</Button>
            <Button disabled={busy} onClick={() => run(() => post('/api/power/off', {}), 'UPS monitoring is off.').then(reload)}>Stop watching the UPS</Button>
          </div>
        </div>
      </div>
    </Card>
  );
}

// How long each certificate NopeWarden holds has left. Blue from 60 days out, amber from 30, red once
// it has run out. The console's own certificate is listed only when it is in its last 30 days.
function Certificates() {
  const { data } = useData('/api/certs', { every: 300000 });
  const list = data?.certs || [];
  const word = (c) => (c.days < 0 ? `Ran out ${-c.days} day${c.days === -1 ? '' : 's'} ago` : c.days === 0 ? 'Runs out today' : c.days > 999 ? `${Math.floor(c.days / 365)} years left` : `${fmt.n(c.days)} day${c.days === 1 ? '' : 's'} left`);
  return (
    <Card title="Certificates" desc="How long the certificates NopeWarden holds have left: VPN authorities, servers and users, and the link between two machines in a pair.">
      {!data ? <Empty>Looking…</Empty> : !list.length ? <Empty>No VPN or pairing certificates yet.</Empty> : (
        <div className="list">
          <Limited items={list} noun="certificates" render={(c) => (
            <div className="item" key={c.id}>
              <div className="fill"><div className="name">{c.label}</div><div className="meta">{c.level ? c.advice : `${c.kind}. Runs out ${new Date(c.expires * 1000).toLocaleDateString(undefined, { dateStyle: 'medium' })}.`}</div></div>
              <Pill tone={c.level === 'bad' ? 'bad' : c.level === 'warn' ? 'warn' : c.level === 'info' ? 'info' : 'good'}>{word(c)}</Pill>
            </div>
          )} />
        </div>
      )}
    </Card>
  );
}

export default function Diagnostics() {
  const [tab, setTab] = useHashTab('diagnostics', ['services','log','tools','terminal'], 'services');
  return (
    <Page title="Diagnostics" sub="How NopeWarden itself is running: its services, its system log, and tools to test the connection. Looking for what your network did? Open Logs.">
      <Tabs value={tab} onChange={setTab} tabs={[{ value: 'services', label: 'Health & services' }, { value: 'log', label: 'System log' }, { value: 'tools', label: 'Network tools' }, { value: 'terminal', label: 'Terminal' }]} />
      {tab === 'services' && <><Verdict /><Services /><DiskUse /><DiskHealth /><PowerUPS /><Certificates /><Connections />
        <Card title="My network" desc="A printable summary for a handover or your records: networks, addresses, devices, open ports and how to get back in. It never contains passwords or keys.">
          <a className="btn" href="/api/summary/print" target="_blank" rel="noreferrer">Open the printable page</a>
        </Card></>}
      {tab === 'log' && <SystemLog />}
      {tab === 'tools' && <NetTools />}
      {tab === 'terminal' && <Terminal />}
    </Page>
  );
}

import { useHashTab } from '../hashtab.js';
import React, { useState } from 'react';
import { get, post, useData } from '../api.js';
import { Button, Card, Empty, Field, Input, Note, Page, Pill, Select, Tabs, useAction, fmt } from '../ui.jsx';
import { Hit } from './AdminActivity.jsx';
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
      <Card title="Services" desc={`Gatehouse ${data.version || ''}. Each part of the system, and how it is doing.`}
        actions={<Button small onClick={() => run(async () => { const r = await fetch('/api/diag/bundle', { credentials: 'same-origin' }); if (!r.ok) throw new Error('The bundle could not be made.'); const u = URL.createObjectURL(await r.blob()); const a = document.createElement('a'); a.href = u; a.download = 'gatehouse-support-bundle.txt'; a.click(); URL.revokeObjectURL(u); }, 'Saved. Passwords and keys are hidden in it.')} disabled={busy}>Save a support bundle</Button>}>
        <div className="list">
          {(data.services || []).filter((s) => s.state !== 'missing').map((s) => (
            <div className="item" key={s.unit}>
              <div style={{ flex: 1, minWidth: 0 }}>
                <div className="name">{s.title} <Pill tone={tone[s.state]}>{word[s.state]}</Pill></div>
                {s.reason && <div className="meta">{s.reason}</div>}
              </div>
              <div className="actions">
                <Button small onClick={() => setLog(s.unit)}>Show log</Button>
                {s.can_restart && <Button small disabled={busy} onClick={() => run(() => post('/api/diag/restart', { unit: s.unit }), 'Restarting.').then(reload)}>Restart</Button>}
              </div>
            </div>
          ))}
          {!(data.services || []).length && <Empty>No services to show.</Empty>}
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
    <Card title="Is Gatehouse itself working?" desc="How Gatehouse is running, not what your network is doing. For that, open Logs.">
      <div className="stack">
        <div><Pill tone={data.level === 'good' ? 'good' : data.level === 'bad' ? 'bad' : 'warn'}>{data.text}</Pill></div>
        {data.issues.length > 0 && (
          <div className="list">
            {data.issues.map((i, k) => (
              <div className="item" key={k}>
                <div style={{ flex: 1, minWidth: 0 }}><div className="name"><Pill tone={i.level === 'bad' ? 'bad' : 'warn'}>{i.level === 'bad' ? 'Problem' : 'Check'}</Pill> {i.text}</div></div>
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
  if (!data) return null;
  const reasons = Object.entries(data.by_reason || {});
  return (
    <Card title="Console connections" desc="The live connection your browser keeps open to Gatehouse. If the browser reports dropped connections, this shows whether Gatehouse ended them or the network between ended them.">
      <div className="stack">
        <p className="small muted">{data.open} open now, {fmt.n(data.opened)} opened and {fmt.n(data.closed)} closed since Gatehouse started.{reasons.length ? ` Closed because: ${reasons.map(([w, n]) => `${w} (${n})`).join(', ')}.` : ''} The browser going away is normal (a page closed or reloaded); a failed write usually means the network between you and the box dropped it.</p>
        {!data.recent.length ? <Empty>No console has connected since Gatehouse started.</Empty> : (
          <div className="scroll"><table className="table">
            <thead><tr><th>When</th><th>What</th><th>From</th><th>Lasted</th><th>Why</th></tr></thead>
            <tbody>{data.recent.slice(0, 30).map((e, i) => <tr key={i}><td className="small">{fmt.dt(e.at)}</td><td>{e.event === 'opened' ? 'Opened' : 'Closed'}</td><td className="mono small">{e.addr}</td><td className="small">{e.seconds ? `${e.seconds} s` : ''}</td><td className="small muted">{e.why || ''}</td></tr>)}</tbody>
          </table></div>
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
    <Card title="System log" desc="Messages from Gatehouse and the machine it runs on. By default only warnings and errors are shown; switch on Everything to see all of it. Anything that looks like a password or key is hidden.">
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
          <Empty>{words.length ? 'Nothing matches. What your network did is in Logs; who changed what is in Logs, Admin activity.' : 'Nothing to show. That is good news when only warnings and errors are on.'}</Empty>
        ) : (
          <pre className="logpre mono small" tabIndex={0}>{rows.map((x, i) => <div key={i}><Hit text={x.l} words={words} />{x.known && <span className="muted">{'  ← '}{x.known}</span>}</div>)}</pre>
        )}
      </div>
    </Card>
  );
}

export default function Diagnostics() {
  const [tab, setTab] = useHashTab('diagnostics', ['services','log','tools','terminal'], 'services');
  return (
    <Page title="Diagnostics" sub="How Gatehouse itself is running: its services, its system log, and tools to test the connection. Looking for what your network did? Open Logs.">
      <Tabs value={tab} onChange={setTab} tabs={[{ value: 'services', label: 'Health & services' }, { value: 'log', label: 'System log' }, { value: 'tools', label: 'Network tools' }, { value: 'terminal', label: 'Terminal' }]} />
      {tab === 'services' && <><Verdict /><Services /><Connections /></>}
      {tab === 'log' && <SystemLog />}
      {tab === 'tools' && <NetTools />}
      {tab === 'terminal' && <Terminal />}
    </Page>
  );
}

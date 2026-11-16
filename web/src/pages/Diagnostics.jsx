import { useHashTab } from '../hashtab.js';
import React, { useState } from 'react';
import { get, post, useData } from '../api.js';
import { Button, Card, Empty, Field, Input, Limited, Note, Page, Pill, Select, Tabs, useAction, fmt } from '../ui.jsx';
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
function DiskHealth() {
  const { data, reload } = useData('/api/disk/health', { every: 300000 });
  const [run, busy] = useAction();
  const [open, setOpen] = useState('');
  if (!data) return null;
  if (!data.drives?.length) return null;
  const needsTool = !data.tool && data.drives.some((d) => d.kind !== 'Virtual disk');
  const size = (b) => (b >= 1e12 ? `${(b / 1e12).toFixed(1)} TB` : `${Math.round(b / 1e9)} GB`);
  return (
    <Card title="Drive health" desc="What each drive says about itself. A drive is only marked amber or red when it reports a problem.">
      <div className="stack">
        {needsTool && (
          <Note tone="info" title="NopeWarden needs a small program to read drive health">
            It is called smartmontools. {data.can_install ? 'Install it here; it is a small download from Debian.' : 'This machine cannot install it by itself, so install smartmontools yourself.'}
            {data.can_install && <div className="actions"><Button kind="primary" disabled={busy} onClick={() => run(() => post('/api/disk/tool', {}), 'Installed.').then(reload)}>Install the SMART tool</Button></div>}
          </Note>
        )}
        <div className="list">
          {data.drives.map((d) => (
            <div key={d.name} className="stack gap1">
              <div className="item">
                <div className="fill"><div className="name">{d.model}</div><div className="meta">{d.kind}{d.bytes ? `, ${size(d.bytes)}` : ''}, /dev/{d.name}. {d.reason}</div></div>
                <div className="actions">
                  <Pill tone={DISK_TONE[d.level] || (d.word === 'Healthy' ? 'good' : '')}>{d.word}</Pill>
                  {d.word !== 'Not reported' && <Button small onClick={() => setOpen(open === d.name ? '' : d.name)}>{open === d.name ? 'Hide details' : 'Details'}</Button>}
                </div>
              </div>
              {open === d.name && (
                <Note tone="info" title={`${d.model}: what it reported`}>
                  <div className="list">
                    {d.lines.map((l) => <div className="item" key={l.label}><div className="fill">{l.label}</div><span className="num">{l.value}</span></div>)}
                  </div>
                  {d.missing?.length > 0 && <p className="small muted">Not reported by this hardware: {d.missing.join(', ')}.</p>}
                </Note>
              )}
            </div>
          ))}
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
      {tab === 'services' && <><Verdict /><Services /><DiskUse /><DiskHealth /><Certificates /><Connections />
        <Card title="My network" desc="A printable summary for a handover or your records: networks, addresses, devices, open ports and how to get back in. It never contains passwords or keys.">
          <a className="btn" href="/api/summary/print" target="_blank" rel="noreferrer">Open the printable page</a>
        </Card></>}
      {tab === 'log' && <SystemLog />}
      {tab === 'tools' && <NetTools />}
      {tab === 'terminal' && <Terminal />}
    </Page>
  );
}

import React, { useState } from 'react';
import { get, post, useData } from '../api.js';
import { Button, Card, Empty, Field, Input, Note, Page, Pill, Select, Tabs, useAction } from '../ui.jsx';
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
          {data.services.filter((s) => s.state !== 'missing').map((s) => (
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
          {!data.services.length && <Empty>No services to show.</Empty>}
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

export default function Diagnostics() {
  const [tab, setTab] = useState('services');
  return (
    <Page title="Diagnostics" sub="See how each part of Gatehouse is doing, read its log, test the connection, and open a terminal if you need one.">
      <Tabs value={tab} onChange={setTab} tabs={[{ value: 'services', label: 'Services' }, { value: 'tools', label: 'Network tools' }, { value: 'terminal', label: 'Terminal' }]} />
      {tab === 'services' && <Services />}
      {tab === 'tools' && <NetTools />}
      {tab === 'terminal' && <Terminal />}
    </Page>
  );
}

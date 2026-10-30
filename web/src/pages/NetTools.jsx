import React, { useState } from 'react';
import { post } from '../api.js';
import { Button, Card, Field, Input, Note, Pill, Segmented, useAction } from '../ui.jsx';

// "Test my internet" and the small tools (ping, traceroute, name lookup) for a machine that has
// no command line by default.

const tone = { ok: 'good', warn: 'warn', bad: 'bad', skip: '' };
const word = { ok: 'Fine', warn: 'Look at this', bad: 'Problem', skip: 'Skipped' };

function InternetTest() {
  const [res, setRes] = useState(null);
  const [run, busy] = useAction();
  const go = () => run(async () => { setRes(await post('/api/diag/test')); });
  return (
    <Card title="Test my internet" desc="Checks the cable, the modem, your provider and website names in turn, and says what to try if something is wrong."
      actions={<Button kind="primary" onClick={go} disabled={busy}>{busy ? 'Testing…' : res ? 'Test again' : 'Test now'}</Button>}>
      {!res ? <p className="small muted">Press Test now. It takes a few seconds.</p> : (
        <div className="stack">
          <Note tone={res.ok ? 'good' : 'bad'}>{res.ok ? 'No problem found.' : 'Found a problem. The first red step is the one to fix.'}</Note>
          <div className="list">
            {res.steps.map((s, i) => (
              <div className="item" key={s.id}>
                <div style={{ flex: 1, minWidth: 0 }}>
                  <div className="name">{i + 1}. {s.title}</div>
                  <div className="meta">{s.detail}</div>
                  {s.next && s.state !== 'ok' && <div className="small" style={{ marginTop: 4 }}><strong>What to try: </strong>{s.next}</div>}
                </div>
                <Pill tone={tone[s.state]}>{word[s.state]}</Pill>
              </div>
            ))}
          </div>
        </div>
      )}
    </Card>
  );
}

function Tools() {
  const [kind, setKind] = useState('ping');
  const [target, setTarget] = useState('');
  const [rtype, setRtype] = useState('A');
  const [out, setOut] = useState('');
  const [run, busy] = useAction();
  const go = () => run(async () => {
    setOut('');
    if (kind === 'dns') {
      const r = await post('/api/diag/dns', { name: target, type: rtype });
      setOut(`${r.status} in ${r.ms} ms\n${r.answers.length ? r.answers.join('\n') : 'No records.'}`);
    } else {
      const r = await post('/api/diag/run', { kind, target });
      setOut(r.output || 'No output.');
    }
  });
  return (
    <Card title="Tools" desc="Run a check from Gatehouse itself: whether a site answers, the path to it, or what a name points to.">
      <div className="stack">
        <Segmented value={kind} onChange={(v) => { setKind(v); setOut(''); }} options={[{ value: 'ping', label: 'Ping' }, { value: 'trace', label: 'Trace the path' }, { value: 'dns', label: 'Look up a name' }]} />
        <Field label={kind === 'dns' ? 'Website name' : 'Website name or address'} hint="For example example.com or 8.8.8.8">
          <Input value={target} onChange={setTarget} className="in mono" onKeyDown={(e) => { if (e.key === 'Enter' && target && !busy) go(); }} />
        </Field>
        {kind === 'dns' && <Field label="Kind of record"><Segmented value={rtype} onChange={setRtype} options={['A', 'AAAA', 'MX', 'TXT', 'NS', 'CNAME'].map((x) => ({ value: x, label: x }))} /></Field>}
        <div><Button kind="primary" onClick={go} disabled={busy || !target.trim()}>{busy ? 'Running…' : 'Run'}</Button></div>
        {out && <pre className="mono small" style={{ whiteSpace: 'pre-wrap', margin: 0, padding: 12, background: 'var(--color-surface-sunken, rgba(127,127,127,.1))', borderRadius: 8, overflowX: 'auto' }}>{out}</pre>}
      </div>
    </Card>
  );
}

export default function NetTools() {
  return <><InternetTest /><Tools /></>;
}

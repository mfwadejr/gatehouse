import React, { useEffect, useState } from 'react';
import { post } from '../api.js';
import { useChanges } from '../changes.jsx';
import { Button, Card, Empty, Field, Input, Note, Page, Problems, confirmBox, fmt, useStepUp, useToast } from '../ui.jsx';

// Everything that is saved but not in use yet, in plain words, with what Gatehouse found when it
// checked it. One button puts all of it live with the usual 60-second safety.
const AREAS = [['net', 'Network settings'], ['fw', 'Firewall']];

export default function Review() {
  const ch = useChanges();
  const toast = useToast();
  const stepUp = useStepUp();
  const [prev, setPrev] = useState({});
  const [code, setCode] = useState('');
  const [msg, setMsg] = useState('');
  const dirty = ch?.dirtyKinds || [];
  const key = dirty.join(',');

  useEffect(() => {
    let dead = false;
    (async () => {
      const out = {};
      for (const k of dirty) { try { out[k] = await post(`/api/${k}/preview`); } catch (e) { out[k] = { ok: false, problems: [e.message] }; } }
      if (!dead) setPrev(out);
    })();
    return () => { dead = true; };
  }, [key, ch?.staged?.items?.length]); // eslint-disable-line react-hooks/exhaustive-deps

  if (!ch) return null;
  const items = ch.staged?.items || [];
  const problems = dirty.flatMap((k) => prev[k]?.ok === false ? (prev[k].problems || []) : []);
  const warnings = dirty.flatMap((k) => prev[k]?.warnings || []);
  const summary = dirty.flatMap((k) => (prev[k]?.ok ? prev[k].summary || [] : []));
  const run = async () => {
    setMsg('');
    const err = await ch.commit(code);
    if (err) setMsg(err); else setCode('');
  };
  const discard = async () => {
    if (!(await confirmBox('Put everything back the way it was running? Every change waiting here is dropped.', { title: 'Discard all changes?', yes: 'Discard all' }))) return;
    try { await post('/api/staged/discard'); toast('Discarded. Everything is back as it was running.'); ch.reload(); }
    catch (e) { toast(e.message, 'bad'); }
  };
  return (
    <Page title="Review changes" sub="Saved for later and not in use yet. Check them here, then put them all live in one step.">
      {ch.pend && <Note tone="warn" title="A change is being tried out">Wait for it to be kept or undone first. It takes up to a minute.</Note>}
      {!dirty.length && !ch.pend && (
        <Card title="Nothing is waiting"><Empty>Everything you have saved is in use. Changes made with Save for later show up here.</Empty></Card>
      )}
      {dirty.length > 0 && (
        <>
          <Card title={`${items.length || dirty.length} ${(items.length || dirty.length) === 1 ? 'change' : 'changes'} waiting`} desc="In the order you made them.">
            {items.length > 0 ? (
              <ul className="reviewlist">{items.map((e) => <li key={e.id}><time>{fmt.time(e.ts)}</time><span>{e.text}</span></li>)}</ul>
            ) : (
              <ul className="reviewlist">{summary.length ? summary.map((s, i) => <li key={i}><span>{s}</span></li>) : AREAS.filter(([k]) => dirty.includes(k)).map(([k, l]) => <li key={k}><span>{l} changed.</span></li>)}</ul>
            )}
            {items.length > 0 && summary.length > 0 && (
              <details className="mt2"><summary className="small">What the firewall will do afterwards</summary><ul className="stack small">{summary.map((s, i) => <li key={i}>{s}</li>)}</ul></details>
            )}
          </Card>
          <Problems list={problems} />
          {warnings.length > 0 && <Note tone="warn" title="Worth knowing before you commit"><ul>{warnings.map((w, i) => <li key={i}>{w}</li>)}</ul></Note>}
          {!problems.length && <Note tone="good">When you commit, Gatehouse checks that you can still reach it and keeps the changes. If a change cuts you off, nothing more is needed: it undoes itself within 60 seconds.</Note>}
          {stepUp.everyday && !problems.length && (
            <Field label="Code from your authenticator app" hint="Needed to apply changes to the network. Gatehouse remembers it for 15 minutes.">
              <Input className="codebox" value={code} onChange={setCode} inputMode="numeric" autoComplete="one-time-code" />
            </Field>
          )}
          {msg && <Note tone="bad">{msg}</Note>}
          <div className="actions">
            <Button kind="primary" disabled={ch.busy || !!ch.pend || problems.length > 0} onClick={run}>Verify and commit</Button>
            {ch.staged?.can_discard && <Button kind="danger" disabled={ch.busy || !!ch.pend} onClick={discard}>Discard all changes</Button>}
          </div>
        </>
      )}
    </Page>
  );
}

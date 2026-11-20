import React, { useState } from 'react';
import { useData } from '../api.js';
import { useAttention } from '../attentionctx.js';
import { Button, Card, CheckLine, Pill, useCheck } from '../ui.jsx';

// The security check: one sentence of verdict, then each risk with a button that goes to where it
// is fixed. Things that are fine are folded away.
const tone = { bad: 'bad', warn: 'warn', ok: 'good' };

export default function Security({ bare }) {
  const { data, reload } = useData('/api/security/check', { topics: ['fw.status'], every: 60000 });
  const { advice } = useAttention();
  const [all, setAll] = useState(false);
  const [shownDismissed, setShownDismissed] = useState(false);
  const [chk, chkGo] = useCheck();
  const again = () => chkGo(async () => { await reload(); }, 'Checking…', 'Checked just now.');
  if (!data) return null;
  // Advice (two-factor, alerts) can be dismissed by someone who accepts the risk; a real fault never can.
  const open = data.items.filter((i) => i.level !== 'ok');
  const risks = open.filter((i) => !advice.hidden(i));
  const dismissed = open.filter((i) => advice.hidden(i));
  const bad = risks.filter((i) => i.level === 'bad').length;
  const warn = risks.length - bad;
  const plural = (n) => (n === 1 ? '' : 's');
  const summary = bad ? `${bad} thing${plural(bad)} should be fixed now.` : warn ? `Safe enough, with ${warn} thing${plural(warn)} worth tightening.` : 'Everything checked looks good.';
  const fine = data.items.filter((i) => i.level === 'ok');
  const Wrap = bare ? ({ children }) => <div>{children}</div> : (p) => <Card {...p} />;
  return (
    <Wrap title="Security check" desc={summary} actions={<Button small disabled={chk.phase === 'working'} onClick={again}>Check again</Button>}>
      <CheckLine st={chk} />
      {bare && <p className="small muted">{summary} <button type="button" className="link" onClick={again}>Check again</button></p>}
      <div className="list">
        {risks.map((i) => (
          <div className="item" key={i.id}>
            <div><div className="name">{i.title} <Pill tone={tone[i.level]}>{i.level === 'bad' ? 'Fix now' : 'Worth fixing'}</Pill></div><div className="meta">{i.text}</div></div>
            <div className="inline">
              {i.advice && <Button small title="You have decided to live with this. It stays listed below as Dismissed, and you can bring it back." onClick={() => advice.dismiss(i)}>Dismiss</Button>}
              {i.fix && <Button small kind="primary" onClick={() => { window.location.hash = `#/${i.fix.page}`; }}>{i.fix.label}</Button>}
            </div>
          </div>
        ))}
        {risks.length === 0 && <div className="item"><div className="name">Nothing needs your attention.</div></div>}
      </div>
      {dismissed.length > 0 && (
        <p className="small muted">
          <button type="button" className="link" onClick={() => setShownDismissed(!shownDismissed)}>{shownDismissed ? 'Hide' : 'Show'} {dismissed.length} dismissed {dismissed.length === 1 ? 'item' : 'items'}</button>
        </p>
      )}
      {shownDismissed && <div className="list">{dismissed.map((i) => <div className="item" key={i.id}><div><div className="name">{i.title} <Pill>Dismissed</Pill></div><div className="meta">{i.text}</div></div><Button small onClick={() => advice.bringBack(i)}>Bring it back</Button></div>)}</div>}
      {fine.length > 0 && (
        <p className="small muted">
          <button type="button" className="link" onClick={() => setAll(!all)}>{all ? 'Hide' : 'Show'} the {fine.length} checks that are fine</button>
        </p>
      )}
      {all && <div className="list">{fine.map((i) => <div className="item" key={i.id}><div><div className="name">{i.title}</div><div className="meta">{i.text}</div></div><Pill tone="good">Fine</Pill></div>)}</div>}
    </Wrap>
  );
}

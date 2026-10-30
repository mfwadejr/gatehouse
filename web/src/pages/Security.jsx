import React, { useState } from 'react';
import { useData } from '../api.js';
import { Button, Card, Pill } from '../ui.jsx';

// The security check: one sentence of verdict, then each risk with a button that goes to where it
// is fixed. Things that are fine are folded away.
const tone = { bad: 'bad', warn: 'warn', ok: 'good' };

export default function Security() {
  const { data, reload } = useData('/api/security/check', { topics: ['fw.status'], every: 60000 });
  const [all, setAll] = useState(false);
  if (!data) return null;
  const risks = data.items.filter((i) => i.level !== 'ok');
  const fine = data.items.filter((i) => i.level === 'ok');
  return (
    <Card title="Security check" desc={data.summary} actions={<Button small onClick={reload}>Check again</Button>}>
      <div className="list">
        {risks.map((i) => (
          <div className="item" key={i.id}>
            <div><div className="name">{i.title} <Pill tone={tone[i.level]}>{i.level === 'bad' ? 'Fix now' : 'Worth fixing'}</Pill></div><div className="meta">{i.text}</div></div>
            {i.fix && <Button small kind="primary" onClick={() => { window.location.hash = `#/${i.fix.page}`; }}>{i.fix.label}</Button>}
          </div>
        ))}
        {risks.length === 0 && <div className="item"><div className="name">Nothing needs your attention.</div></div>}
      </div>
      {fine.length > 0 && (
        <p className="small muted">
          <button type="button" className="link" onClick={() => setAll(!all)}>{all ? 'Hide' : 'Show'} the {fine.length} checks that are fine</button>
        </p>
      )}
      {all && <div className="list">{fine.map((i) => <div className="item" key={i.id}><div><div className="name">{i.title}</div><div className="meta">{i.text}</div></div><Pill tone="good">Fine</Pill></div>)}</div>}
    </Card>
  );
}

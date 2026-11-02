import React from 'react';
import { useData } from '../api.js';
import { Card, Pill } from '../ui.jsx';
import { go } from '../App.jsx';
import Recipes from './Recipes.jsx';

// The "Finish setting up" list. It is worked out from what the machine has, so it is right in any
// order, and it goes away by itself when everything is done.
export function SetupChecklist({ bare }) {
  const { data } = useData('/api/setup/checklist', { topics: ['net.status', 'fw.status'], every: 15000 });
  if (!data || !data.items?.length) return null;
  if (data.left === 0) return null;
  const done = data.items.length - data.left;
  const Wrap = bare ? ({ children }) => <div>{children}</div> : (p) => <Card {...p} />;
  return (
    <Wrap title="Finish setting up" desc={`${done} of ${data.items.length} done. You can do these in any order, and change any of them later.`}>
      {bare && <p className="small muted">You can do these in any order, and change any of them later.</p>}
      <ul className="stack" style={{ listStyle: 'none', margin: 0, padding: 0 }}>
        {data.items.map((i) => (
          <li key={i.id} className="spread" style={{ gap: 'var(--space-3)', alignItems: 'flex-start' }}>
            <div>
              <div style={{ fontWeight: 600 }}>{i.done ? '✓ ' : ''}{i.text}</div>
              <div className="small muted">{i.why}</div>
            </div>
            {i.done ? <Pill tone="good">Done</Pill> : <button type="button" className="btn small primary" onClick={() => go(i.to)}>Set up</button>}
          </li>
        ))}
      </ul>
    </Wrap>
  );
}


// Where the first-run list and the step-by-step guides live now that the Overview is only status.
export default function GetStarted() {
  return (
    <div className="stack">
      <SetupChecklist />
      <Card title="Common setups" desc="Step-by-step guides for things people often want to do."><Recipes bare /></Card>
    </div>
  );
}

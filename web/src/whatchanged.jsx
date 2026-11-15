import React from 'react';
import { useData } from './api.js';
import { Card, fmt } from './ui.jsx';

const HOURS = { '1h': 1, '24h': 24, '7d': 168, live: 1 };

// A short strip of what changed lately on a page: the recorded events (updates, warnings, new devices,
// settings changed) that belong to it, for the same period as the page's charts. `match` picks the
// events that belong to this page from who raised them and what they say. `more` opens the full list.
export function WhatChanged({ match, range = '24h', more = '#/logs' }) {
  const hours = HOURS[range] || 24;
  const { data } = useData(`/api/logs?kind=event&hours=${hours}&limit=100`, { topics: ['events'], every: 60000 });
  const rows = (data?.items || []).filter((e) => match.test(`${e.who} ${e.what}`)).slice(0, 4);
  return (
    <Card title="What changed" desc={`Updates, warnings and changes recorded in the last ${hours === 1 ? 'hour' : hours === 24 ? '24 hours' : '7 days'}.`}
      actions={<a className="btn small" href={more}>Open Logs</a>}>
      <div className="list">
        {rows.map((e) => (
          <div className="item" key={e.key}>
            <div className="fill"><div className="name">{e.what}</div>{e.why ? <div className="meta">{e.why}</div> : null}</div>
            <span className="small muted">{fmt.ago(e.ts)}</span>
          </div>
        ))}
        {data && !rows.length && <div className="small muted">Nothing changed in this period.</div>}
      </div>
    </Card>
  );
}

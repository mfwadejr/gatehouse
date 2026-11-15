import React from 'react';
import { useData } from './api.js';
import { usePeriod } from './period.js';
import { Card, Limited, Segmented, fmt } from './ui.jsx';

const HOURS = { '1h': 1, '24h': 24, '7d': 168, live: 1 };

// A strip of what changed lately on a page: the recorded events (updates, warnings, new devices,
// settings changed) that belong to it, a page at a time. `match` picks the events that belong to this
// page from who raised them and what they say. With a `range` the page's own time choice is followed;
// without one the card has the shared 1 h / 24 h / 7 d choice of its own. `more` opens the full list.
export function WhatChanged({ match, range, page = 'changed', more = '#/logs' }) {
  const [mine, setMine, options] = usePeriod(`${page}-changes`);
  const shown = range || mine;
  const hours = HOURS[shown] || 24;
  const { data } = useData(`/api/logs?kind=event&hours=${hours}&limit=300`, { topics: ['events'], every: 60000 });
  const rows = (data?.items || []).filter((e) => match.test(`${e.who} ${e.what}`));
  const words = hours === 1 ? 'hour' : hours === 24 ? '24 hours' : '7 days';
  return (
    <Card title="What changed" desc={`Updates, warnings and changes recorded in the last ${words}.`}
      actions={<><a className="btn small" href={more}>Open Logs</a>{!range && <Segmented value={mine} onChange={setMine} options={options} />}</>}>
      <div className="list">
        <Limited items={rows} noun="changes" render={(e) => (
          <div className="item" key={e.key}>
            <div className="fill"><div className="name">{e.what}</div>{e.why ? <div className="meta">{e.why}</div> : null}</div>
            <span className="small muted">{fmt.ago(e.ts)}</span>
          </div>
        )} />
        {data && !rows.length && <div className="small muted">Nothing changed in this period.</div>}
        {data?.more && <div className="small muted">There are older changes in Logs.</div>}
      </div>
    </Card>
  );
}

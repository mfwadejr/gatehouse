import { useHashTab } from '../hashtab.js';
import React, { useState } from 'react';
import { useData } from '../api.js';
import { Card, Guard, Note, Page, Pill, Tabs } from '../ui.jsx';
import Security from './Security.jsx';
import Blocks from './Blocks.jsx';
import TrafficLabels from './TrafficLabels.jsx';
import BlockVersions from './BlockVersions.jsx';

// One place for everything that protects the network from the inside and the outside.
// Fail open: the app and category blocks, and the threat watching, never stop the network when they
// themselves go wrong. They only add protection; a fault takes the protection away, not the internet.

function Summary({ go }) {
  const { data: sec } = useData('/api/security/check', { topics: ['fw.status'], every: 60000 });
  const { data: th } = useData('/api/threat', { every: 15000 });
  const { data: blocks } = useData('/api/blocks', { every: 30000 });
  const risks = (sec?.items || []).filter((i) => i.level !== 'ok');
  const bad = risks.filter((i) => i.level === 'bad').length;
  const nBlocks = (blocks?.blocks || []).filter((b) => b.enabled && !b.allow).length;
  const watching = th?.mode === 'watch' && th?.program?.running;
  const installed = th?.program?.installed;
  const verdict = bad ? `${bad} thing${bad === 1 ? '' : 's'} to fix now.` : risks.length ? `${risks.length} thing${risks.length === 1 ? '' : 's'} worth fixing.` : 'Nothing needs your attention.';
  return (
    <div className="stack">
      <Card title="Protection at a glance" desc={verdict}>
        <div className="list">
          <div className="item"><div><div className="name">Security check</div><div className="meta">{sec?.summary || 'Checking.'}</div></div><Pill tone={bad ? 'bad' : risks.length ? 'warn' : 'good'}>{bad ? 'Fix now' : risks.length ? 'Worth fixing' : 'All fine'}</Pill></div>
          <div className="item"><div><div className="name">Blocks</div><div className="meta">{nBlocks ? `${nBlocks} block${nBlocks === 1 ? '' : 's'} in place.` : 'Nothing is blocked.'}</div></div><button type="button" className="link" onClick={() => go('blocks')}>Open</button></div>
          <div className="item"><div><div className="name">Threat watching</div><div className="meta">{!installed ? 'Not installed. Install it on the Threats page if you want it.' : watching ? 'On. Watching and telling you; it never blocks.' : 'Installed, switched off.'}</div></div><a className="link" href="#/threats">Open</a></div>
        </div>
        <p className="small muted">Block lists for ads, trackers and adult sites are under <a href="#/dns">DNS</a>.</p>
      </Card>
      <Security />
      <Note>If anything on this page stops working, your internet carries on as normal. These features only add protection, so a fault takes the protection away; it never takes the connection away.</Note>
    </div>
  );
}

export default function Protection() {
  const [tab, setTab] = useHashTab('protection', ['overview','blocks','lists'], 'overview');
  return (
    <Page title="Protection" sub="Check how safe the network is, and block apps, categories and websites.">
      <Tabs value={tab} onChange={setTab} tabs={[{ value: 'overview', label: 'Overview' }, { value: 'blocks', label: 'Blocks' }, { value: 'lists', label: 'Lists and labels' }]} />
      {tab === 'overview' && <Summary go={setTab} />}
      {tab === 'blocks' && <Blocks />}
      {tab === 'lists' && <><Guard what="Blocking lists"><BlockVersions /></Guard><Guard what="Traffic labels"><TrafficLabels /></Guard></>}
    </Page>
  );
}

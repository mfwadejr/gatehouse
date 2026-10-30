import React from 'react';
import { useData } from './api.js';
import { Button, Note, useToast } from './ui.jsx';

// Says when an internet connection cannot be reached from outside (shared carrier address, or
// another router in front) and what to do. `compact` is the short form beside forwards and UPnP.
export default function NatNotice({ compact }) {
  const { data } = useData('/api/internet/nat', { topics: ['net.status'], every: 30000 });
  const toast = useToast();
  const bad = (data?.links || []).filter((l) => l.blocked);
  if (!bad.length) return null;
  const copy = async (text) => {
    try { await navigator.clipboard.writeText(text); toast('Copied.'); } catch { toast('Could not copy. Select the text and copy it.', 'bad'); }
  };
  return bad.map((l) => (
    <Note key={l.name} tone="warn" title={compact ? `${l.name}: connections from outside cannot reach this` : l.title}>
      <div className="stack">
        <span>{compact ? (l.class === 'cgnat' ? 'Your provider shares one public address, so forwards and UPnP cannot work on this connection.' : `Another router sits in front (${l.gateway || l.address}), so forwards and UPnP only work from outside once that router passes the traffic on.`) : l.text}</span>
        {!compact && l.options?.length > 0 && <><strong>What you can do</strong><ul className="notes">{l.options.map((o, i) => <li key={i}>{o}</li>)}</ul></>}
        {!compact && l.forwards?.length > 0 && (
          <>
            <strong>Ports to open on the other router</strong>
            <ul className="notes">{l.forwards.map((f, i) => <li key={i}>{f.text}</li>)}</ul>
            <div><Button small onClick={() => copy(l.forwards.map((f) => f.text).join('\n'))}>Copy this list</Button></div>
          </>
        )}
        {compact && <a href="#/internet">What to do about it</a>}
      </div>
    </Note>
  ));
}

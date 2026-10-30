import React, { useState } from 'react';
import { Button, Card, Modal } from './ui.jsx';
import { RELEASES } from './releasenotes.js';

const KEY = 'gatehouse.seenRelease';
const seen = () => { try { return localStorage.getItem(KEY) || ''; } catch { return ''; } };
const remember = (v) => { try { localStorage.setItem(KEY, v); } catch { /* fine */ } };

export const latestRelease = () => RELEASES[0];

function Notes({ list }) {
  return (
    <div className="stack">
      {list.map((r) => (
        <div key={r.version}>
          <h3 className="ov-h3">{r.version} · {r.title}</h3>
          <ul className="notes">{r.items.map((t) => <li key={t}>{t}</li>)}</ul>
        </div>
      ))}
    </div>
  );
}

export function ReleaseNotes({ onClose }) {
  return <Modal title="What's new" onClose={onClose} footer={<Button kind="primary" onClick={onClose}>Close</Button>}><Notes list={RELEASES} /></Modal>;
}

// Shown on the Overview until it is dismissed, once per release.
export function WhatsNewCard({ current }) {
  const [gone, setGone] = useState(false);
  const [all, setAll] = useState(false);
  const r = RELEASES.find((x) => x.version === String(current || '').replace(/^v/, '')) || null;
  if (!r || gone || seen() === r.version) return null;
  return (
    <Card title={`What's new in ${r.version}`} desc={r.title}>
      <ul className="notes">{r.items.slice(0, 4).map((t) => <li key={t}>{t}</li>)}</ul>
      <div className="inline">
        <Button kind="primary" small onClick={() => { remember(r.version); setGone(true); }}>Got it</Button>
        <button type="button" className="linkbtn small" onClick={() => setAll(true)}>All release notes</button>
      </div>
      {all && <ReleaseNotes onClose={() => setAll(false)} />}
    </Card>
  );
}

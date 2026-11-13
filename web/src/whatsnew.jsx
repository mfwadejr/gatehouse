import React, { useState } from 'react';
import { Button, Card, Icon, Input, Modal } from './ui.jsx';
import { RELEASES } from './releasenotes.js';

const KEY = 'nopewarden.seenRelease';
const seen = () => { try { return localStorage.getItem(KEY) || ''; } catch { return ''; } };
const remember = (v) => { try { localStorage.setItem(KEY, v); } catch { /* fine */ } };

export const latestRelease = () => RELEASES[0];

// A release's notes, kept for the list below.
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

const CHEV = 'M6 9l6 6 6-6';

// Which line of releases a version belongs to: 0.9.4.x, 0.9.3.x and so on; older ones are grouped as 0.8.x, 0.5.x.
function lineOf(version, fours) {
  if (!/^\d/.test(version) || version.includes(' ')) return 'Earliest builds';
  const p = version.split('.');
  if (fours.has(p.slice(0, 3).join('.')) ) return p.slice(0, 3).join('.');
  return p.slice(0, 2).join('.');
}

function Release({ r, open, onToggle }) {
  return (
    <div className="rel">
      <button type="button" className="relhead" aria-expanded={open} onClick={onToggle}>
        <span className="relchev" style={{ transform: open ? 'none' : 'rotate(-90deg)' }}><Icon d={CHEV} size={16} /></span>
        <strong>{r.version}</strong><span className="muted">{r.title}</span>
      </button>
      {open && <ul className="notes relbody">{r.items.map((t) => <li key={t}>{t}</li>)}</ul>}
    </div>
  );
}

// Every release, newest first. The newest is open; older ones are one line each, grouped by line, and
// a search shows only the releases whose notes mention every word typed.
export function ReleaseList({ list }) {
  const [q, setQ] = useState('');
  const [open, setOpen] = useState(() => new Set(list[0] ? [list[0].version] : []));
  const [groups, setGroups] = useState(() => new Set());
  const words = q.toLowerCase().split(/\s+/).filter(Boolean);
  const toggle = (set, setSet, k) => setSet((s) => { const n = new Set(s); if (n.has(k)) n.delete(k); else n.add(k); return n; });
  const fours = new Set(list.filter((r) => r.version.split('.').length >= 4).map((r) => r.version.split('.').slice(0, 3).join('.')));
  const rest = list.slice(1);
  const byLine = [];
  rest.forEach((r) => {
    const line = lineOf(r.version, fours);
    let g = byLine.find((x) => x.line === line);
    if (!g) { g = { line, items: [] }; byLine.push(g); }
    g.items.push(r);
  });
  const hits = words.length ? list.filter((r) => words.every((w) => `${r.version} ${r.title} ${r.items.join(' ')}`.toLowerCase().includes(w))) : [];
  const expandAll = () => { setOpen(new Set(list.map((r) => r.version))); setGroups(new Set(byLine.map((g) => g.line))); };
  const collapseAll = () => { setOpen(new Set()); setGroups(new Set()); };
  return (
    <div className="stack">
      <div className="inline" style={{ flexWrap: 'wrap' }}>
        <div style={{ flex: '1 1 14rem', maxWidth: '24rem' }}><Input value={q} onChange={setQ} placeholder="Search the release notes" aria-label="Search the release notes" /></div>
        {!words.length && <><Button small onClick={expandAll}>Expand all</Button><Button small onClick={collapseAll}>Collapse all</Button></>}
      </div>
      {words.length > 0 ? (
        <>
          <p className="small muted">{hits.length === 0 ? 'No release mentions that.' : `${hits.length} release${hits.length === 1 ? '' : 's'} mention${hits.length === 1 ? 's' : ''} that.`}</p>
          {hits.map((r) => <Release key={r.version} r={r} open onToggle={() => {}} />)}
        </>
      ) : (
        <>
          {list[0] && <Release r={list[0]} open={open.has(list[0].version)} onToggle={() => toggle(open, setOpen, list[0].version)} />}
          {byLine.map((g) => {
            const gOpen = groups.has(g.line);
            const first = lineOf(list[0].version, fours) === g.line;
            const label = g.line === 'Earliest builds' ? g.line : `${first ? 'Earlier in ' : ''}${g.line}.x`;
            return (
              <div className="rel relgroup" key={g.line}>
                <button type="button" className="relhead" aria-expanded={gOpen} onClick={() => toggle(groups, setGroups, g.line)}>
                  <span className="relchev" style={{ transform: gOpen ? 'none' : 'rotate(-90deg)' }}><Icon d={CHEV} size={16} /></span>
                  <strong>{label}</strong><span className="muted">{g.items.length} release{g.items.length === 1 ? '' : 's'}, {g.items[0].version}{g.items.length > 1 ? ` to ${g.items[g.items.length - 1].version}` : ''}</span>
                </button>
                {gOpen && <div className="relkids">{g.items.map((r) => <Release key={r.version} r={r} open={open.has(r.version)} onToggle={() => toggle(open, setOpen, r.version)} />)}</div>}
              </div>
            );
          })}
        </>
      )}
    </div>
  );
}

export function ReleaseNotes({ onClose }) {
  return <Modal title="What's new" onClose={onClose} footer={<Button kind="primary" onClick={onClose}>Close</Button>}><ReleaseList list={RELEASES} /></Modal>;
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

// plainNotes turns the release text GitHub publishes (markdown, "Full Changelog" links, "by @name in
// URL" tails) into plain lines, so the console never shows stray ** or a bare link.
export function plainNotes(text) {
  const out = [];
  for (let line of String(text || '').split(/\r?\n/)) {
    if (/full changelog/i.test(line)) continue;
    line = line.replace(/\[([^\]]+)\]\([^)]*\)/g, '$1')
      .replace(/\s+by @\S+ in https?:\/\/\S+/g, '')
      .replace(/https?:\/\/\S+/g, '')
      .replace(/^#{1,6}\s*/, '')
      .replace(/^\s*[*-]\s+/, '\u2022 ')
      .replace(/(\*\*|__|`)/g, '')
      .trimEnd();
    if (!line.trim() && (!out.length || !out[out.length - 1])) continue;
    out.push(line);
  }
  while (out.length && !out[out.length - 1].trim()) out.pop();
  return out.join('\n');
}

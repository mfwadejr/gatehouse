import React, { useEffect, useMemo, useRef, useState } from 'react';
import { useData } from '../api.js';
import { PolicyControls, whenText } from '../policykit.jsx';
import { Button, Pill, fmt } from '../ui.jsx';

// The Rules page as lines: one row per rule, the way most firewalls list them, with the plain
// sentence, the live counts and the buttons one click away. Built-in rules are listed too, and open
// read-only.

// ---------------------------------------------------------------- live counts

// useLiveHits follows the match counters. With Live on it asks every 1.5 seconds and reports which
// rows just matched; with Live off it asks every 20 seconds and only updates the numbers.
export function useLiveHits(live) {
  const { data } = useData('/api/fw/rules/counts', { every: live ? 1500 : 20000 });
  const prev = useRef(null);
  const [just, setJust] = useState({});
  useEffect(() => {
    if (!data?.hits) return undefined;
    const before = prev.current;
    prev.current = data.hits;
    if (!before || !live) return undefined;
    const fresh = {};
    Object.entries(data.hits).forEach(([k, h]) => { const d = h.hits - (before[k]?.hits || 0); if (d > 0) fresh[k] = d; });
    if (!Object.keys(fresh).length) return undefined;
    setJust((j) => {
      const n = { ...j };
      Object.entries(fresh).forEach(([k, d]) => { n[k] = { n: (j[k]?.n || 0) + d, at: Date.now() }; });
      return n;
    });
    return undefined;
  }, [data, live]);
  // A row keeps its "+N just now" for three seconds after its last match.
  useEffect(() => {
    if (!Object.keys(just).length) return undefined;
    const t = setInterval(() => setJust((j) => Object.fromEntries(Object.entries(j).filter(([, v]) => Date.now() - v.at < 3000))), 1000);
    return () => clearInterval(t);
  }, [just]);
  const hits = data?.hits || {};
  return { hit: (tag, fallback) => hits[tag] || fallback || { hits: 0, last_hit: 0 }, just: (tag) => just[tag]?.n || 0, ready: !!data };
}

// ---------------------------------------------------------------- small pieces

const actionWord = (a) => (a === 'accept' ? 'Allow' : a === 'drop' ? 'Block' : 'Reject');
const LinesCtx = React.createContext(null);

const svcText = (s) => (s === 'any' || !s ? ['everything'] : s.split(',').map((x) => x.trim().replace(/^list:/, '')).filter(Boolean));

// Chips shows the first few entries and "+N" for the rest, with the rest in its tooltip.
export function Chips({ items, max = 2, quiet }) {
  const shown = items.slice(0, max);
  const rest = items.slice(max);
  return (
    <span className="lchips">
      {shown.map((x, i) => <span key={i} className={`lchip ${quiet ? 'quiet' : ''}`}>{x}</span>)}
      {rest.length > 0 && <span className="lchip more" title={rest.join(', ')}>+{rest.length}</span>}
    </span>
  );
}

function Matches({ info, just }) {
  if (!info.hits) return <span className="muted">None yet</span>;
  return <span className={`lhit ${just ? 'flash' : ''}`}>{fmt.n(info.hits)}{just ? <span className="lplus"> +{just}</span> : <span className="muted"> · {fmt.ago(info.last_hit)}</span>}</span>;
}

const sideItems = (chips, fallback) => {
  const real = (chips || []).filter((c) => c.kind !== 'any').map((c) => c.label);
  return real.length ? real : [fallback];
};

// The sentence for a rule, built from its parts.
export function ruleSentence(r) {
  const who = (r.src_chips || []).some((c) => c.kind !== 'any') ? r.src_chips.filter((c) => c.kind !== 'any').map((c) => c.label).join(', ') : `devices on ${r.from}`;
  const where = r.to === 'self' ? 'NopeWarden itself' : (r.dst_chips || []).some((c) => c.kind !== 'any') ? r.dst_chips.filter((c) => c.kind !== 'any').map((c) => c.label).join(', ') : `anything on ${r.to}`;
  const what = r.service === 'any' || !r.service ? '' : ` for ${svcText(r.service).join(', ')}`;
  const verb = r.action === 'accept' ? `Allow ${who} to reach` : `${r.action === 'reject' ? 'Reject' : 'Block'} ${who} from reaching`;
  return `${verb} ${where}${what}${r.when && r.when !== 'Always' ? `, ${r.when.charAt(0).toLowerCase()}${r.when.slice(1)}` : ''}.`;
}

// ---------------------------------------------------------------- the table

function Row({ k, num, name, from, to, svc, action, info, tag, when, off, children }) {
  const { open, flip, hits } = React.useContext(LinesCtx);
  const onKey = (e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); flip(k); } };
  return (
    <>
      <tr className={`lrow ${off ? 'off' : ''} ${hits.just(tag) ? 'flash' : ''}`} tabIndex={0} aria-expanded={!!open[k]} onClick={() => flip(k)} onKeyDown={onKey}>
        <td className="n">{num}</td>
        <td className="lname">{name}</td>
        <td><Chips items={from} quiet /></td>
        <td><Chips items={to} quiet /></td>
        <td><Chips items={svc} /></td>
        <td>{action}</td>
        <td>{tag ? <Matches info={info} just={hits.just(tag)} /> : <span className="muted">–</span>}</td>
        <td className="lwhen">{when}</td>
      </tr>
      {open[k] && <tr className="ldetail"><td colSpan={8}><div className="ldet">{children}</div></td></tr>}
    </>
  );
}

// rows: built in, set on other pages, and the person's own rules, in the order they are checked.
export function RuleLines({ prot, pages, rules, opened, live, hits, onEdit, onMove, onPause, onToggle, onRemove, goTab, goPorts, pausedNames, canMove }) {
  const [open, setOpen] = useState({});
  const flip = (k) => setOpen((o) => ({ ...o, [k]: !o[k] }));
  const sys = prot?.rules || [];
  return (
    <LinesCtx.Provider value={{ open, flip, hits }}>
    <div className="scroll">
      <table className="ltable">
        <thead><tr><th>#</th><th>Name</th><th>From</th><th>To</th><th>Service</th><th>Action</th><th>Matches{live ? '' : ' (every 20 s)'}</th><th>When</th></tr></thead>
        <tbody>
          {sys.length > 0 && <tr className="lsect"><td colSpan={8}>Built in, always above your rules (cannot be changed)</td></tr>}
          {sys.map((p, i) => (
            <Row key={`s${i}`} k={`s${i}`} num={`S${i + 1}`} name={<><span aria-hidden="true">🔒</span> {p.title}</>} from={p.tag ? ['Management'] : ['–']} to={p.tag ? ['This device'] : ['–']} svc={p.tag ? ['console'] : ['–']}
              action={<Pill tone={p.active ? 'good' : ''}>{p.active ? 'Always on' : 'Standing by'}</Pill>} info={hits.hit(p.tag, p)} tag={p.tag} when="Always">
              <div className="say">{p.text}</div>
              <div className="note">Built in. It keeps you able to manage NopeWarden, so it cannot be edited, moved or removed.{p.note ? ` ${p.note}` : ''}</div>
            </Row>
          ))}
          {(pages.length > 0 || (opened?.forwards || []).length > 0) && <tr className="lsect"><td colSpan={8}>Set on other pages</td></tr>}
          {pages.map((r, i) => {
            const paused = r.name.startsWith('Paused');
            const members = pausedNames || [];
            const standingBy = paused && members.length === 0;
            return (
              <Row key={`p${i}`} k={`p${i}`} num="–" name={`${r.name}: ${r.from} → ${r.to === 'self' ? 'NopeWarden' : r.to}`} from={sideItems(r.src_chips, r.from).map((x) => (x === 'Paused' ? 'Paused devices' : x))} to={sideItems(r.dst_chips, r.to === 'self' ? 'NopeWarden' : r.to)} svc={['everything']}
                off={standingBy}
                action={standingBy ? <Pill>Standing by, nobody paused</Pill> : <Pill tone={r.action === 'accept' ? 'good' : 'bad'}>{actionWord(r.action)}</Pill>} info={hits.hit(r.tag, r)} tag={r.tag} when="Always">
                <div className="say">{paused ? `Devices that are paused, or inside their profile's off-hours, cannot reach ${/^(wan|internet)$/i.test(r.to) ? 'the internet' : r.to} from ${r.from}.` : (r.descr || ruleSentence(r))}</div>
                {standingBy && <div className="note">Nobody is paused right now, so this rule is not blocking anything. It switches on by itself when you pause a device or profile, which you do on the Profiles page, and a profile's off-hours switch it on too.</div>}
                {paused && !standingBy && (
                  <div>
                    <div className="llbl">Blocked right now ({members.length})</div>
                    <div>{members.length ? <Chips items={members} max={8} /> : <span className="muted">No device is paused at the moment.</span>}</div>
                  </div>
                )}
                <div className="note">This rule is made by another page, so it opens here for reading only.</div>
                <div className="lbtns">{paused ? <Button small onClick={() => { window.location.hash = '#/profiles'; }}>Open Profiles</Button> : <Button small onClick={() => goTab('countries')}>Open Countries</Button>}</div>
              </Row>
            );
          })}
          {(opened?.forwards || []).map((g) => (
            <Row key={`o${g.group}`} k={`o${g.group}`} num="–" name={`Open port: ${g.group}`} from={['Internet']} to={['Your devices']} svc={[g.text]} action={<Pill tone="good">Allow</Pill>} info={{ hits: 0, last_hit: 0 }} tag="" when="Always" off={!g.enabled}>
              <div className="say">{g.text}</div>
              <div className="lbtns"><Button small onClick={goPorts}>Set on the Open ports tab</Button></div>
            </Row>
          ))}
          <tr className="lsect"><td colSpan={8}>Your rules, checked top to bottom, first match wins</td></tr>
          {rules.map(({ r }, pos) => {
            const k = `r${r.id}`;
            const info = hits.hit(r.tag, r);
            return (
              <Row key={k} k={k} num={pos + 1} off={!r.enabled} name={<>{r.matrix ? `Matrix: ${r.from} to ${r.to}` : r.name} {!r.enabled && <Pill>Off</Pill>}</>}
                from={sideItems(r.src_chips, r.from)} to={sideItems(r.dst_chips, r.to === 'self' ? 'NopeWarden' : r.to)} svc={svcText(r.service)}
                action={<Pill tone={r.action === 'accept' ? 'good' : 'bad'}>{actionWord(r.action)}</Pill>} info={info} tag={r.tag}
                when={whenText(r.paused_until) || r.when}>
                <div className="say">{ruleSentence(r)}</div>
                <div className="lmeta">
                  <span>From network <b>{r.from}</b> to <b>{r.to === 'self' ? 'NopeWarden' : r.to}</b></span>
                  {r.ipver ? <span>IPv{r.ipver} only</span> : null}{r.ctstate === 'new' ? <span>new connections only</span> : null}{r.limit ? <span>max {r.limit}/s</span> : null}
                </div>
                {r.descr && <div className="note">{r.descr}</div>}
                {(r.warnings || []).map((w, i) => <div className="warnline" key={i}>⚠ {w}</div>)}
                <div className="lmeta"><span className="lhit">{info.hits > 0 ? `${fmt.n(info.hits)} matches · last ${fmt.ago(info.last_hit)}` : 'No matches yet'}</span>{hits.just(r.tag) > 0 && <span className="lplus">+{hits.just(r.tag)} just now</span>}</div>
                {(r.services || []).length > 0 && (
                  <div>
                    <div className="llbl">Services in this rule ({r.services.length})</div>
                    <div className="lsv">
                      <div className="sh">Service</div><div className="sh">Port</div><div className="sh r">Matches</div><div className="sh r">Last match</div>
                      {r.services.map((s, i) => {
                        let n = 0, last = 0;
                        s.ports.forEach((pt) => { const h = hits.hit(`${r.tag}#svc:${pt}`, null); n += h.hits || 0; last = Math.max(last, h.last_hit || 0); });
                        if (!hits.ready) { n = s.hits; last = s.last_hit; }
                        return (
                          <React.Fragment key={i}>
                            <div>{s.label || s.ports[0]}</div><div className="mono muted">{s.ports.join(', ')}</div>
                            <div className={`r ${n ? 'lhit' : 'muted'}`}>{fmt.n(n)}</div><div className="r muted">{n ? fmt.ago(last) : 'never'}</div>
                          </React.Fragment>
                        );
                      })}
                    </div>
                  </div>
                )}
                <div className="lbtns">
                  <Button small aria-label="Move up" onClick={() => onMove(pos, -1)} disabled={pos === 0 || !canMove}>↑ Up</Button>
                  <Button small aria-label="Move down" onClick={() => onMove(pos, 1)} disabled={pos === rules.length - 1 || !canMove}>↓ Down</Button>
                  <PolicyControls name={r.name} enabled={r.enabled} pausedUntil={r.paused_until} onPause={(m) => onPause(r, m)} onToggle={() => onToggle(r)} onEdit={() => onEdit(r)} onRemove={() => onRemove(r)} />
                </div>
              </Row>
            );
          })}
        </tbody>
      </table>
      {!rules.length && <p className="muted small lempty">No rules of your own match.</p>}
    </div>
    </LinesCtx.Provider>
  );
}

// ---------------------------------------------------------------- CSV

const cell = (v) => {
  const s = v === undefined || v === null ? '' : String(v);
  return /[",\n\r]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s;
};
const when = (t) => (t ? new Date(t * 1000).toISOString() : '');

// rulesCsv writes exactly the rows shown: one line for each service of a rule, or one line for a
// rule that has a single service or too many to count separately.
export function rulesCsv({ prot, pages, rules, opened, hits }) {
  const head = ['Order', 'Kind', 'Name', 'Turned on', 'Action', 'From network', 'To network', 'From', 'To', 'Service', 'Ports', 'When', 'Matches', 'Last match (UTC)', 'Rule total matches', 'Note'];
  const out = [head];
  (prot?.rules || []).forEach((p, i) => { const h = hits.hit(p.tag, p); out.push([`S${i + 1}`, 'Built in', p.title, p.active ? 'yes' : 'no', p.active ? 'Always on' : 'Standing by', '', '', '', '', p.tag ? 'console' : '', '', 'Always', h.hits || 0, when(h.last_hit), h.hits || 0, p.text]); });
  pages.forEach((r) => { const h = hits.hit(r.tag, r); out.push(['', 'Set on another page', `${r.name}: ${r.from} → ${r.to === 'self' ? 'NopeWarden' : r.to}`, 'yes', actionWord(r.action), r.from, r.to, sideItems(r.src_chips, r.from).map((x) => (x === 'Paused' ? 'Paused devices' : x)).join('; '), sideItems(r.dst_chips, r.to).join('; '), 'everything', '', 'Always', h.hits || 0, when(h.last_hit), h.hits || 0, r.descr || '']); });
  (opened?.forwards || []).forEach((g) => out.push(['', 'Open port', g.group, g.enabled ? 'yes' : 'no', 'Allow', 'Internet', 'Your devices', '', '', g.text, '', 'Always', '', '', '', '']));
  rules.forEach(({ r }, i) => {
    const total = hits.hit(r.tag, r);
    const base = [i + 1, 'Rule', r.matrix ? `Matrix: ${r.from} to ${r.to}` : r.name, r.enabled ? 'yes' : 'no', actionWord(r.action), r.from, r.to === 'self' ? 'NopeWarden' : r.to, sideItems(r.src_chips, r.from).join('; '), sideItems(r.dst_chips, r.to).join('; ')];
    const tail = [whenText(r.paused_until) || r.when, null, null, total.hits || 0, r.descr || ''];
    if ((r.services || []).length) {
      r.services.forEach((s) => {
        let n = 0, last = 0;
        s.ports.forEach((pt) => { const h = hits.hit(`${r.tag}#svc:${pt}`, null); n += h.hits || 0; last = Math.max(last, h.last_hit || 0); });
        out.push([...base, s.label || s.ports[0], s.ports.join(' '), tail[0], n, when(last), tail[3], tail[4]]);
      });
    } else {
      out.push([...base, svcText(r.service).join('; '), '', tail[0], total.hits || 0, when(total.last_hit), tail[3], tail[4]]);
    }
  });
  return `﻿${out.map((row) => row.map(cell).join(',')).join('\r\n')}\r\n`;
}

export function downloadCsv(text) {
  const url = URL.createObjectURL(new Blob([text], { type: 'text/csv;charset=utf-8' }));
  const a = document.createElement('a');
  a.href = url; a.download = `nopewarden-rules-${new Date().toISOString().slice(0, 10)}.csv`; a.click();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}

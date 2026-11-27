import React, { useEffect, useRef, useState } from 'react';
import { get, post, put, useData } from '../api.js';
import { Button, Card, Empty, Field, Input, Modal, Note, Select, Steps, ToggleRow, useAction } from '../ui.jsx';

// The small text display on the front of many firewall appliances. The display program (LCDproc) is an
// extra, installed only when asked. Detect says what is there; Test draws on it and shows the buttons.

const SURE = { confirmed: 'Sure', usb: 'Likely', guess: 'A guess' };

function copyText(t) {
  try { navigator.clipboard.writeText(t); return true; } catch { return false; }
}

// What the panel shows right now, drawn as the panel is: fixed-width characters on a dark screen.
function Picture({ m }) {
  if (!m?.connected || !m.lines?.length) return <p className="small muted">{m?.error || 'The display is not showing anything yet.'}</p>;
  return (
    <div>
      <pre className="lcdpic" aria-label="What the front panel shows now" style={{ opacity: m.backlight ? 1 : 0.45 }}>{m.lines.join('\n')}</pre>
      <p className="small muted">{m.width} characters by {m.height} lines{m.hold ? ', held on this screen' : ''}{m.backlight ? '' : ', backlight off'}.</p>
    </div>
  );
}

function Detect({ onUse }) {
  const [run, busy] = useAction();
  const [f, setF] = useState(null);
  return (
    <div className="stack">
      <div className="actions"><Button disabled={busy} onClick={async () => { const r = await run(() => post('/api/lcd/detect')); if (r) setF(r); }}>{busy ? 'Looking…' : 'Detect my display'}</Button></div>
      {f && (
        <>
          {f.matches?.length ? (
            <div className="list">
              {f.matches.map((m, i) => (
                <div className="item" key={i}>
                  <div>
                    <div className="name">{m.driver} <span className="small muted">({SURE[m.sure] || m.sure})</span></div>
                    <div className="meta">{m.why}{m.keys ? ` ${m.keys} buttons.` : ''}</div>
                  </div>
                  <Button small onClick={() => onUse(m)}>Use this</Button>
                </div>
              ))}
            </div>
          ) : <Note tone="info" title="Nothing recognised">No display was found that I could name. Choose a driver by hand below, and send the report if you want help with it.</Note>}
          {(f.notes || []).map((n, i) => <p className="small muted" key={i}>{n}</p>)}
          <details className="more">
            <summary>Everything found (to copy)</summary>
            <pre className="lcdreport">{f.report}</pre>
            <div className="actions"><Button small onClick={() => copyText(f.report)}>Copy</Button></div>
          </details>
        </>
      )}
    </div>
  );
}

// Test: draw on the display with the settings being edited (nothing is saved), and watch the buttons.
function TestModal({ draft, actions, onClose, onMap, mapped }) {
  const [info, setInfo] = useState(null);
  const [err, setErr] = useState('');
  const [said, setSaid] = useState('');
  const [keys, setKeys] = useState([]);
  const [page, setPage] = useState(0);
  const since = useRef(0);
  const live = useRef(true);
  useEffect(() => {
    live.current = true;
    post('/api/lcd/test/start', draft).then((r) => { if (live.current) setInfo(r); }).catch((e) => setErr(e.message));
    return () => { live.current = false; post('/api/lcd/test/end').catch(() => {}); };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  useEffect(() => {
    if (!info) return undefined;
    const t = setInterval(async () => {
      try {
        const r = await get(`/api/lcd/test/keys?since=${since.current}`);
        since.current = r.next;
        if (r.keys?.length) setKeys((k) => [...r.keys.map((x) => x.key), ...k].slice(0, 12));
      } catch { /* ignore */ }
    }, 700);
    return () => clearInterval(t);
  }, [info]);
  const pat = async (kind, pg = 0) => {
    try { const r = await post('/api/lcd/test/pattern', { kind, page: pg }); setSaid(r.message); } catch (e) { setSaid(e.message); }
  };
  const seen = [...new Set(keys)];
  return (
    <Modal title="Test the front panel" onClose={onClose} footer={<Button kind="primary" onClick={onClose}>Done</Button>}>
      {err ? <Note tone="bad" title="The test did not start">{err}</Note> : !info ? <p>Starting the display with these settings…</p> : (
        <div className="stack">
          <p>Can you see <b>{info.height} lines of {info.width} characters</b> on the display?</p>
          <div className="actions">
            <Button small onClick={() => pat('numbers')}>Rows and columns</Button>
            <Button small onClick={() => { const p = page + 1; setPage(p); pat('chars', p); }}>Every character</Button>
            <Button small onClick={() => pat('icons')}>Symbols</Button>
            <Button small onClick={() => pat('backlight')}>Backlight</Button>
          </div>
          {said && <p className="small" aria-live="polite">{said}</p>}
          <h3 className="small">Buttons</h3>
          <p className="small muted">Press each button on the front of the box. Each one appears here as you press it.</p>
          {seen.length === 0 ? <Empty>No button pressed yet.</Empty> : (
            <div className="list">
              {seen.map((k) => (
                <div className="item" key={k}>
                  <div className="name">{k}</div>
                  <div style={{ width: '16rem' }}><Select value={mapped[k] || 'none'} onChange={(v) => onMap(k, v)} options={actions.map((a) => ({ value: a.id, label: a.title }))} /></div>
                </div>
              ))}
            </div>
          )}
          <p className="small muted">Latest presses: {keys.length ? keys.join(', ') : 'none'}. Choices made here are kept when you save the settings.</p>
        </div>
      )}
    </Modal>
  );
}

function SettingRows({ settings, onChange }) {
  const rows = Object.entries(settings || {});
  const set = (k, nk, v) => {
    const o = {};
    Object.entries(settings || {}).forEach(([a, b]) => { if (a === k) { if (nk) o[nk] = v; } else o[a] = b; });
    onChange(o);
  };
  return (
    <div className="stack">
      {rows.map(([k, v]) => (
        <div className="formgrid" key={k}>
          <Field label="Setting"><Input value={k} onChange={(nk) => set(k, nk, v)} /></Field>
          <Field label="Value"><Input value={v} onChange={(nv) => set(k, k, nv)} /></Field>
          <div className="actions"><Button small onClick={() => set(k, '', '')}>Remove</Button></div>
        </div>
      ))}
      <div className="actions"><Button small onClick={() => onChange({ ...(settings || {}), Setting: '' })}>Add a setting</Button></div>
    </div>
  );
}

export default function FrontPanel() {
  const { data, reload } = useData('/api/lcd', { every: 3000 });
  const [run, busy] = useAction();
  const [draft, setDraft] = useState(null);
  const [testing, setTesting] = useState(false);
  useEffect(() => { if (data && !draft) setDraft(data.config); }, [data, draft]);
  if (!data || !draft) return null;
  const st = data.status || {};
  const working = st.state === 'installing' || st.state === 'removing';
  const set = (patch) => setDraft({ ...draft, ...patch });
  const useMatch = (m) => set({ driver: m.driver, settings: m.settings || {} });
  const save = () => run(() => put('/api/lcd/config', draft), 'Saved.').then(reload);
  const driverOpts = [...new Set([...(data.drivers || []).map((d) => d.id), ...(st.drivers || [])])];
  if (draft.driver && !driverOpts.includes(draft.driver)) driverOpts.push(draft.driver);
  const hint = (data.drivers || []).find((d) => d.id === draft.driver);
  const toggleScreen = (id, on) => set({ screens: on ? [...draft.screens, id] : draft.screens.filter((s) => s !== id) });
  return (
    <Card title="Front panel display" desc="Many firewall appliances have a small text display and some buttons on the front. This shows the machine's name, speeds, addresses and alerts there, and lets each button do something. It needs an extra program, installed only if you ask.">
      <div className="stack">
        <Detect onUse={(m) => { useMatch(m); }} />
        {working ? (
          <Note title={st.state === 'installing' ? 'Installing' : 'Removing'}>{st.steps?.length ? <Steps steps={st.steps} label="Progress" /> : (st.message || 'Working. This can take a few minutes.')}</Note>
        ) : !st.installed ? (
          <>
            <p className="small">The display program is not installed. Detect works without it.</p>
            {st.state === 'failed' && <Note tone="warn" title="The last install did not finish">{st.steps?.length > 0 && <Steps steps={st.steps} label="What happened" />}{st.message}</Note>}
            <div className="actions"><Button kind="primary" disabled={busy} onClick={() => run(() => post('/api/lcd/install'), 'Installing. This can take a few minutes.').then(reload)}>Install the display program</Button></div>
          </>
        ) : (
          <>
            <ToggleRow title="Show things on the front panel" checked={draft.enabled} onChange={(v) => set({ enabled: v })} />
            <Field label="Display type" hint={hint?.title}>
              <Select value={draft.driver || ''} onChange={(v) => set({ driver: v, settings: {} })} options={[{ value: '', label: 'Choose…' }, ...driverOpts.map((d) => ({ value: d, label: d }))]} />
            </Field>
            {draft.driver && (
              <details className="more" open={Object.keys(draft.settings || {}).length > 0}>
                <summary>Settings for this display{hint?.settings ? ` (${hint.settings.join(', ')})` : ''}</summary>
                <SettingRows settings={draft.settings} onChange={(s) => set({ settings: s })} />
              </details>
            )}
            <div className="actions"><Button disabled={!draft.driver} onClick={() => setTesting(true)}>Test the display</Button></div>
            <h3 className="small">What it shows</h3>
            {(data.screens || []).map((s) => (
              <ToggleRow key={s.id} title={s.title} desc={s.text} checked={draft.screens.includes(s.id)} onChange={(v) => toggleScreen(s.id, v)} />
            ))}
            <div className="formgrid">
              <Field label="Each screen stays for"><Select value={String(draft.rotate)} onChange={(v) => set({ rotate: Number(v) })} options={[2, 3, 4, 5, 8, 10, 15, 30, 60].map((n) => ({ value: String(n), label: `${n} seconds` }))} /></Field>
              <Field label="Backlight goes off after" hint="With no button pressed. Any button brings it back."><Select value={String(draft.dim_after)} onChange={(v) => set({ dim_after: Number(v) })} options={[0, 1, 5, 10, 30, 60].map((n) => ({ value: String(n), label: n ? `${n} minutes` : 'Never' }))} /></Field>
            </div>
            <ToggleRow title="Jump to alerts when a threat is seen" checked={draft.warn} onChange={(v) => set({ warn: v })} />
            <h3 className="small">Buttons</h3>
            <div className="list">
              {(data.keys || []).slice(0, 6).map((k) => (
                <div className="item" key={k}>
                  <div className="name">{k}</div>
                  <div style={{ width: '16rem' }}><Select value={draft.buttons[k] || 'none'} onChange={(v) => set({ buttons: { ...draft.buttons, [k]: v } })} options={data.actions.map((a) => ({ value: a.id, label: a.title }))} /></div>
                </div>
              ))}
            </div>
            <details className="more">
              <summary>Other buttons (A to F, for panels with lettered keys)</summary>
              <div className="list">
                {(data.keys || []).slice(6).map((k) => (
                <div className="item" key={k}>
                  <div className="name">{k}</div>
                  <div style={{ width: '16rem' }}><Select value={draft.buttons[k] || 'none'} onChange={(v) => set({ buttons: { ...draft.buttons, [k]: v } })} options={data.actions.map((a) => ({ value: a.id, label: a.title }))} /></div>
                </div>
                ))}
              </div>
            </details>
            <p className="small muted">Buttons your display does not have are simply never pressed. Use Test to see which ones it has.</p>
            <div className="actions">
              <Button kind="primary" disabled={busy} onClick={save}>Save</Button>
              <Button disabled={busy} onClick={() => setDraft(data.config)}>Undo changes</Button>
              <Button kind="danger" disabled={busy} onClick={() => run(() => post('/api/lcd/remove'), 'Removing.').then(() => { setDraft(null); reload(); })}>Remove the display program</Button>
            </div>
            <h3 className="small">What the display shows now</h3>
            <Picture m={data.mirror} />
          </>
        )}
      </div>
      {testing && <TestModal draft={draft} actions={data.actions} mapped={draft.buttons} onMap={(k, v) => set({ buttons: { ...draft.buttons, [k]: v } })} onClose={() => { setTesting(false); reload(); }} />}
    </Card>
  );
}

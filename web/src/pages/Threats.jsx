import { useHashTab } from '../hashtab.js';
import React, { useEffect, useState } from 'react';
import { del, post, put, get, useData } from '../api.js';
import { ScheduleEditor } from '../ScheduleEditor.jsx';
import { Chart, fmtBits } from '../charts.jsx';
import { Button, Card, Empty, Field, Input, Modal, Note, NumberInput, Page, Pill, Segmented, Steps, Switch, Tabs, Limited, Bar, useAction, fmt } from '../ui.jsx';

// Threat watching. An engine (Suricata) is installed only when the owner asks. It watches copies of
// the traffic and NopeWarden says what it noticed in plain words. It never holds traffic up, so a
// crash, a removal or a switch-off leaves the connection exactly as it was.

const mb = (n) => (n >= 1024 ? `${(n / 1024).toFixed(1)} GB` : `${n} MB`);

// The program itself: install it, watch it install or go, remove it. All of it lives on Settings.
function ProgramCard({ data, reload }) {
  const [run, busy] = useAction();
  const [pre, setPre] = useState(null);
  const [removing, setRemoving] = useState(false);
  const p = data.program || {};
  const working = p.state === 'installing' || p.state === 'removing';
  const look = async () => { const r = await run(() => post('/api/threat/install/preview')); if (r) setPre(r); };
  return (
    <Card title={p.installed && !working ? 'The program' : 'Install threat watching'} desc={p.installed && !working ? 'Threat watching is an extra program. It can be removed at any time.' : 'An extra program, installed only if you ask. Nothing else in NopeWarden needs it.'}>
      <div className="stack">
        {working ? <Note title={p.state === 'installing' ? 'Installing' : 'Removing'}>{p.steps?.length ? <><Steps steps={p.steps} label={p.state === 'installing' ? 'Install progress' : 'Removal progress'} /><span className="small muted">This takes a minute or two. You can leave this page.</span></> : (p.message || 'Working. This can take a few minutes. You can leave this page.')}</Note> : p.installed ? (
          <>
            <p className="small">{p.running ? 'Running' : 'Not running'}{p.version ? `. Version ${p.version}` : ''}{p.memory_bytes ? `. Using ${mb(Math.round(p.memory_bytes / 1048576))} of memory` : ''}.</p>
            <div className="actions"><Button kind="danger" disabled={busy} onClick={() => setRemoving(true)}>Remove threat watching</Button></div>
          </>
        ) : (
          <>
            <p className="small">This is not installed yet.</p>
            {p.state === 'failed' && <Note tone="warn" title="The last install did not finish">{p.steps?.length > 0 && <Steps steps={p.steps} label="What happened" />}{p.message}</Note>}
            <div className="actions"><Button kind="primary" disabled={busy} onClick={look}>Install threat watching</Button></div>
          </>
        )}
      </div>
      {pre && (
        <Modal title="Install threat watching" onClose={() => setPre(null)} footer={<><Button onClick={() => setPre(null)}>Cancel</Button><Button kind="primary" disabled={busy} onClick={async () => { if (await run(() => post('/api/threat/install'), 'Installing. This can take a few minutes.')) { setPre(null); reload(); } }}>Install</Button></>}>
          <div className="stack">
            <p className="small">NopeWarden will download and install: <strong>{(pre.preview.packages || []).join(', ')}</strong>.</p>
            <p className="small">{pre.preview.download_mb > 0 || pre.preview.disk_mb > 0 ? `Download about ${mb(Math.ceil(pre.preview.download_mb || 0))}. Disk space about ${mb(Math.ceil(pre.preview.disk_mb || 0))}.` : 'Debian did not say how big this is; it is usually under 100 MB.'} This machine has {mb(pre.disk_free_mb || 0)} free, {mb(pre.memory_mb || 0)} of memory and {pre.cpus || '?'} processor core{pre.cpus === 1 ? '' : 's'}.</p>
            {(pre.warnings || []).map((w) => <Note key={w} tone="warn">{w}</Note>)}
            {!(pre.warnings || []).length && <Note>This machine has enough room.</Note>}
            <p className="small muted">Installing does not turn watching on. You do that next, and can switch it off or remove it at any time.</p>
          </div>
        </Modal>
      )}
      {removing && (
        <Modal title="Remove threat watching" onClose={() => setRemoving(false)} footer={<><Button onClick={() => setRemoving(false)}>Cancel</Button><Button kind="danger" disabled={busy} onClick={async () => { if (await run(() => post('/api/threat/remove'), 'Removing. This can take a minute.')) { setRemoving(false); reload(); } }}>Remove</Button></>}>
          <p className="small">The program is switched off and removed. Your traffic is not affected. Your alerts and tuning are kept.</p>
        </Modal>
      )}
    </Card>
  );
}

// The Off / Watch and tell switch with everything that explains it. It moves at once and says what the
// program is doing while it catches up.
function ModeCard({ data, reload }) {
  const [run, busy] = useAction();
  const [pending, setPending] = useState(null);
  const p = data.program || {};
  const watching = data.mode === 'watch';
  return (
    <Card title="Threat watching" desc="Watches for infected devices, break-in attempts and probing, and tells you in plain words.">
      <div className="stack">
        <Segmented value={pending || data.mode} onChange={(v) => { if (v === data.mode || busy) return; setPending(v); run(() => put('/api/threat/settings', { mode: v }), v === 'watch' ? 'Watching is on.' : 'Watching is off.').then(reload).finally(() => setPending(null)); }} options={[{ value: 'off', label: 'Off' }, { value: 'watch', label: 'Watch and tell' }, { value: 'protect', label: 'Protect (not yet)', disabled: true }]} disabled={busy || p.state === 'installing' || p.state === 'removing'} />
        {pending && (
          <div className="inline small" role="status" aria-live="polite"><span className="spinner" aria-hidden="true" /><span><b>Working on it.</b> {pending === 'watch' ? 'Starting the watching program and checking its rules' : 'Stopping the watching program'}. <span className="muted">This can take a few seconds.</span></span></div>
        )}
        {data.paused_reason && !watching && <Note tone="warn" title="NopeWarden switched watching off">{data.paused_reason} Your traffic was never held up.</Note>}
        {watching && !p.running && <Note tone={p.failing ? 'bad' : 'warn'} title={p.failing ? 'The program keeps failing' : 'The program is not running'}>{p.why} Your traffic is not affected. See Advanced, Program log, or Diagnostics in the left menu.</Note>}
        {p.state === 'updating' && <Note>Updating the rules. {p.message}</Note>}
        {p.state === 'failed' && p.installed && <Note tone="warn">{p.message}</Note>}
        <p className="small muted">{data.blocking} {data.fail_open} {data.limits}</p>
      </div>
    </Card>
  );
}

// One card for each kind of alert on each device: how many times, over what time, and one set of
// buttons. The addresses involved are folded away under "Show the N addresses".
function AlertCard({ g, days, busy, run, reload }) {
  const [open, setOpen] = useState(false);
  const [names, setNames] = useState({});
  const first = g.rows[0];
  const times = g.rows.reduce((a, x) => a + (x.count || 1), 0);
  const firstAt = Math.min(...g.rows.map((x) => x.first || x.last));
  const lastAt = Math.max(...g.rows.map((x) => x.last));
  // The same address and port seen twice is one line.
  const seen = new Map();
  g.rows.forEach((x) => {
    const k = `${x.remote}|${x.port}|${x.proto}`;
    const e = seen.get(k) || { x, n: 0 };
    e.n += x.count || 1;
    seen.set(k, e);
  });
  const lines = [...seen.values()];
  const sids = [...new Set(g.rows.map((x) => x.sid))];
  // Names for the addresses: the first lookup starts when Details opens; the machine retries after
  // 10 s, 30 s, 1 min, 5 min and then every 30 min, so while this is open we keep reading how it went.
  const want = [...new Set(lines.filter(({ x }) => !x.site && x.remote).map(({ x }) => x.remote))].slice(0, 50);
  const wantKey = want.join(',');
  const ask = (force) => post('/api/threat/lookup', { addrs: want, force: !!force }).then((r) => setNames(Object.fromEntries((r || []).map((v) => [v.addr, v])))).catch(() => {});
  useEffect(() => {
    if (!open || !want.length) return undefined;
    ask(false);
    const t = setInterval(() => ask(false), 5000);
    return () => clearInterval(t);
  }, [open, wantKey]); // eslint-disable-line react-hooks/exhaustive-deps
  const allow = (mac, msg) => run(async () => { for (const sid of sids) await post('/api/threat/allow', { sid, mac, days: Number(days), note: g.rows.find((x) => x.sid === sid).signature }); return true; }, msg).then(reload);
  const who = first.started === 'device' ? 'your device started it' : first.started === 'remote' ? 'the other end started it' : '';
  const label = (x) => x.site ? `${x.site} (${x.remote})` : x.remote;
  const where = lines.length === 1
    ? `${lines[0].x.site || lines[0].x.remote}${lines[0].x.country ? `, ${lines[0].x.country}` : ''}${lines[0].x.port ? `, port ${lines[0].x.port}${lines[0].x.proto ? `/${lines[0].x.proto}` : ''}` : ''}`
    : `${lines.length} addresses${lines.some((l) => l.x.site) ? `, such as ${[...new Set(lines.map((l) => l.x.site).filter(Boolean))].slice(0, 2).join(', ')}` : ''}`;
  return (
    <div className="item alertcard">
      <div className="fill">
        <div className="name">{g.title}{times > 1 && <> <Pill>×{fmt.n(times)}</Pill></>}</div>
        <div className="meta">{[first.device, where, who, times > 1 ? `${fmt.ago(firstAt)} to ${fmt.ago(lastAt)}` : fmt.ago(lastAt)].filter(Boolean).join(' · ')}</div>
        <button type="button" className="link small leftalign" onClick={() => setOpen(!open)}>{open ? 'Hide details' : 'Details'}</button>
        {open && (
          <div className="small muted">
            <div>{first.explain} <span className="mono">{first.signature}</span></div>
            <ul style={{ margin: 'var(--space-1) 0 0', paddingLeft: '1.2rem' }}>{lines.map(({ x, n }) => {
              const nm = x.site || names[x.remote]?.name;
              const st = !x.site ? names[x.remote] : null;
              return (
                <li key={x.key}>{nm ? `${nm} (${x.remote})` : x.remote}{x.country ? ` (${x.country})` : ''}{x.port ? `, port ${x.port}${x.proto ? `/${x.proto}` : ''}` : ''}{n > 1 ? ` ×${n}` : ''}
                  {st && !st.name && <div className="small muted">{st.status}{st.state !== 'looking' && <> <button type="button" className="link small" onClick={() => ask(true)}>Look up again</button></>}</div>}
                </li>
              );
            })}</ul>
          </div>
        )}
      </div>
      <div className="actions alertacts">
        <Button small disabled={busy} onClick={() => allow(first.mac, 'This will not alert again for this device.')} title="Let this through for this device">Allow for this device</Button>
        <Button small disabled={busy} onClick={() => allow('', 'This will not alert again.')}>Allow for all</Button>
        {first.mac && !first.quiet && <Button small disabled={busy} onClick={() => run(() => post('/api/threat/trust', { mac: first.mac }), `${first.device} will not be checked.`).then(reload)} title={`Never check ${first.device}`}>Never check this device</Button>}
      </div>
    </div>
  );
}

function AlertGroups({ rows, ...p }) {
  const groups = [];
  const by = new Map();
  rows.forEach((x) => { const k = `${x.title}|${x.mac || x.device}`; if (!by.has(k)) { const g = { key: k, title: x.title, rows: [] }; by.set(k, g); groups.push(g); } by.get(k).rows.push(x); });
  return <Limited items={groups} n={10} noun="kinds of alert" render={(g) => <AlertCard key={g.key} g={g} {...p} />} />;
}

function Alerts({ data, reload }) {
  const [run, busy] = useAction();
  const [days, setDays] = useState('0');
  const [showQuiet, setShowQuiet] = useState(false);
  const all = data.alerts || [];
  const loud = all.filter((x) => !x.quiet);
  const quiet = all.filter((x) => x.quiet);
  return (
    <div className="list">
      <AlertGroups rows={loud} days={days} busy={busy} run={run} reload={reload} />
      {!loud.length && <Empty>Nothing suspicious has been seen.</Empty>}
      {quiet.length > 0 && (
        <div>
          <button className="link small" onClick={() => setShowQuiet(!showQuiet)}>{showQuiet ? 'Hide' : 'Show'} odd network behaviour and usually harmless traffic ({quiet.length})</button>
          <p className="small muted">The engine noticed unusual packets. This is common and almost never a threat, so it is kept out of the way.</p>
          {showQuiet && <AlertGroups rows={quiet} days={days} busy={busy} run={run} reload={reload} />}
        </div>
      )}
      <Field label="Allowing lasts"><Segmented value={days} onChange={setDays} options={[{ value: '0', label: 'Until I remove it' }, { value: '7', label: '7 days' }, { value: '1', label: '1 day' }]} /></Field>
    </div>
  );
}

function Settings({ data, reload }) {
  const [run, busy] = useAction();
  const [cpu, setCpu] = useState(data.cpu_percent);
  const [mem, setMem] = useState(data.memory_mb);
  const p = data.program || {};
  const base = (data.presets || []).find((x) => x.key === data.preset);
  const choose = (key) => run(() => put('/api/threat/settings', { preset: key }), 'Saved.').then(reload);
  if (!p.installed || p.state === 'installing' || p.state === 'removing') return <ProgramCard data={data} reload={reload} />;
  return (
    <div className="stack">
      <ModeCard data={data} reload={reload} />
      <Card title="How closely to watch" desc="A lighter setting looks for fewer things and uses less of the machine.">
        <div className="list" id="threat-presets">
          {(data.presets || []).map((x) => (
            <label className="item" key={x.key} style={{ cursor: 'pointer' }}>
              <input type="radio" name="preset" checked={data.preset === x.key && !data.custom} disabled={busy} onChange={() => choose(x.key)} />
              <div style={{ flex: 1 }}><div className="name">{x.title}</div><div className="meta">{x.text}</div></div>
              {data.preset === x.key && data.custom && <Button small disabled={busy} onClick={(e) => { e.preventDefault(); choose(x.key); }} title="Go back to this setting's own groups">Restore defaults</Button>}
            </label>
          ))}
          <label className="item" style={{ cursor: data.custom ? 'default' : 'not-allowed', opacity: data.custom ? 1 : 0.6 }}>
            <input type="radio" name="preset" checked={!!data.custom} disabled readOnly />
            <div style={{ flex: 1 }}>
              <div className="name">Custom{data.custom && base ? `, based on ${base.title}` : ''}</div>
              <div className="meta">{data.custom ? `${data.changes} group${data.changes === 1 ? '' : 's'} changed from ${base?.title}. Change groups under Rules.` : 'Appears when you change a group of checks under Rules.'}</div>
            </div>
            {data.custom && <Button small disabled={busy} onClick={() => choose(data.preset)}>Back to {base?.title}</Button>}
          </label>
        </div>
      </Card>
      <Card title="Limits" desc="If watching uses all of its processor share for ten minutes, NopeWarden switches it off by itself and says so in the Logs. Your traffic is never held up either way.">
        <div className="formgrid">
          <Field label="Processor limit (% of one core)" hint="10 to 400."><NumberInput value={cpu} onChange={setCpu} /></Field>
          <Field label="Memory limit (MB)" hint="256 to 16384."><NumberInput value={mem} onChange={setMem} /></Field>
        </div>
        <div className="actions"><Button disabled={busy} onClick={() => run(() => put('/api/threat/settings', { cpu_percent: Number(cpu), memory_mb: Number(mem) }), 'Saved.').then(reload)}>Save limits</Button></div>
      </Card>
      <ProgramCard data={data} reload={reload} />
    </div>
  );
}

const nice = (n) => { const t = n.replace(/_/g, ' '); return t.charAt(0).toUpperCase() + t.slice(1); };

function GroupsCard({ groups, reload, goSettings }) {
  const [run, busy] = useAction();
  const [want, setWant] = useState(null); // staged: name -> on
  const list = groups.data?.groups || [];
  const staged = (g) => (want && g.name in want ? want[g.name] : g.on);
  const pending = want ? list.filter((g) => g.name in want && want[g.name] !== g.on).length : 0;
  const flip = (g) => setWant({ ...(want || {}), [g.name]: !staged(g) });
  const apply = async () => {
    const off = list.filter((g) => !staged(g)).map((g) => g.name);
    if (await run(() => put('/api/threat/groups', { off }), 'Applied. Watching carries on while the rules update.')) { setWant(null); groups.reload(); reload(); }
  };
  const row = (g) => {
    const on = staged(g);
    const differs = on !== g.preset_on;
    return (
      <div className="item" key={g.name}>
        <div className="fill">
          <div className="name">{nice(g.name)}{differs && <> <Pill tone="warn">Changed</Pill></>}</div>
          <div className="meta">{g.text}</div>
        </div>
        <Switch checked={on} onChange={() => flip(g)} label={`${nice(g.name)}: ${g.text}`} disabled={busy} />
      </div>
    );
  };
  const grid = { display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(min(100%, 17rem), 1fr))', columnGap: 'var(--space-4)' };
  return (
    <Card title="Groups of checks" desc={<>Which groups are on follows the setting you chose ({groups.data?.preset_title}). Turn a group on or off to make your own set, or <button type="button" className="link" onClick={goSettings}>change the setting</button>.</>}>
      <div className="stack">
        <div><div className="small muted">Matter most at home</div><div className="list" style={grid}>{list.filter((g) => g.common).map(row)}</div></div>
        <div><div className="small muted">Less common</div><div className="list" style={grid}>{list.filter((g) => !g.common).map(row)}</div></div>
        {pending > 0 && (
          <div className="confirmbar" role="status">
            <span>{pending} change{pending === 1 ? '' : 's'} not applied yet.</span>
            <span className="actions"><Button disabled={busy} onClick={() => setWant(null)}>Discard</Button><Button kind="primary" disabled={busy} onClick={apply}>Apply</Button></span>
          </div>
        )}
      </div>
    </Card>
  );
}

function Rules({ data, reload, goSettings }) {
  const [run, busy] = useAction();
  const [q, setQ] = useState('');
  const [found, setFound] = useState(null);
  const p = data.program || {};
  const search = async () => { const r = await run(() => get(`/api/threat/rules?q=${encodeURIComponent(q)}`)); if (r) setFound(r); };
  const flip = async (x) => { if (await run(() => post(`/api/threat/rules/${x.sid}`, { on: !x.on }), x.on ? 'Switched off.' : 'Switched on.')) { setFound({ ...found, rules: found.rules.map((y) => (y.sid === x.sid ? { ...y, on: !y.on } : y)) }); } };
  const groups = useData('/api/threat/groups');
  return (
    <div className="stack">
      <Card title="Rules" desc={p.rules_updated ? `${fmt.n(p.rules_count)} checks, updated ${fmt.dt(p.rules_updated)}.` : 'The list of things to look for. It is updated on the schedule below while watching is on.'}>
        <div className="actions">
          <Button disabled={busy || !p.installed} onClick={() => run(() => post('/api/threat/rules/update'), 'Updating the rules. This can take a minute.').then(reload)}>Update rules now</Button>
          <Button disabled={busy || !p.has_previous} onClick={() => run(() => post('/api/threat/rules/rollback'), 'Went back to the previous rules.').then(reload)} title="Use the rules from before the last update">Roll back to the previous rules</Button>
        </div>
        {!p.has_previous && <p className="small muted">There is no earlier set to go back to yet.</p>}
        <ScheduleEditor id="threat" label="How often to update the rules" hideRun />
      </Card>
      <GroupsCard groups={groups} reload={reload} goSettings={goSettings} />
      <Card title="Single checks" desc="Find one by a word or its number, and switch it on or off.">
        <div className="actions"><Input value={q} onChange={setQ} placeholder="For example beacon, or 2001" onKeyDown={(e) => { if (e.key === 'Enter') search(); }} /><Button disabled={busy} onClick={search}>Search</Button></div>
        {found && (
          <div className="list">
            {found.rules.map((x) => <div className="item" key={x.sid}><div className="fill"><div className="name">{x.msg}</div><div className="meta">Check {x.sid}</div></div><Button small disabled={busy} onClick={() => flip(x)}>{x.on ? 'Switch off' : 'Switch on'}</Button></div>)}
            {!found.rules.length && <Empty>No check matches that.</Empty>}
            {found.total > found.rules.length && <p className="small muted">Showing {found.rules.length} of {found.total}. Narrow the search to see others.</p>}
          </div>
        )}
      </Card>
    </div>
  );
}

// The engine's own words, for when it will not start. Administrators only.
function ProgramLog() {
  const [run, busy] = useAction();
  const [text, setText] = useState(null);
  const load = async () => { const r = await run(() => get('/api/diag/log?unit=nopewarden-suricata&lines=60')); if (r) setText(r.log); };
  return (
    <Card title="Program log" desc="The last lines the threat-watching program wrote. The first place to look when it is not running.">
      <div className="actions"><Button small disabled={busy} onClick={load}>{text === null ? 'Show the log' : 'Refresh'}</Button></div>
      {text !== null && <pre className="logpre mono small" tabIndex={0}>{text.trim() || 'It has not written anything yet.'}</pre>}
    </Card>
  );
}

function Advanced({ data, reload }) {
  const [run, busy] = useAction();
  const [eve, setEve] = useState(data.eve_path === data.default_eve ? '' : data.eve_path);
  const [resetting, setResetting] = useState(false);
  const p = data.program || {};
  const importFile = async (f) => {
    if (!f) return;
    let doc;
    try { doc = JSON.parse(await f.text()); } catch { await run(() => { throw new Error('That file could not be read.'); }); return; }
    await run(() => post('/api/threat/import', doc), 'Tuning loaded.');
    reload();
  };
  return (
    <div className="stack">
      <Card title="Allowed checks" desc="Things you let through. Remove one to be told about it again.">
        <div className="list">
          {<Limited items={(data.allowed || [])} noun="allowed items" render={(x) => (
            <div className="item" key={x.id}>
              <div><div className="name">Check {x.sid} {x.auto && <Pill>Quieted by NopeWarden</Pill>}</div><div className="meta">{x.device || 'All devices'} · {x.until ? `until ${fmt.dt(x.until)}` : 'until removed'} · {x.note}</div></div>
              <Button small kind="danger" disabled={busy} onClick={() => run(() => del(`/api/threat/allow/${x.id}`), 'Removed.').then(reload)}>Remove</Button>
            </div>
          )} />}
          {!(data.allowed || []).length && <Empty>Nothing is allowed yet.</Empty>}
        </div>
      </Card>
      <Card title="Devices never checked" desc="Traffic to and from these devices is ignored.">
        <div className="list">
          {<Limited items={(data.trusted || [])} noun="trusted devices" render={(x) => (
            <div className="item" key={x.mac}><div><div className="name">{x.device}</div><div className="meta mono">{x.mac}</div></div>
              <Button small kind="danger" disabled={busy} onClick={() => run(() => del(`/api/threat/trust/${encodeURIComponent(x.mac)}`), 'Removed.').then(reload)}>Check again</Button></div>
          )} />}
          {!(data.trusted || []).length && <Empty>Every device is checked.</Empty>}
        </div>
      </Card>
      <Card title="Save or load your tuning" desc="Your preset, limits, switched-off checks, allowed checks and devices never checked, as one file.">
        <div className="actions">
          <a className="btn" href="/api/threat/export" download="nopewarden-threat-tuning.json">Save tuning to a file</a>
          <label className="btn" style={{ cursor: 'pointer' }}>Load tuning from a file<input type="file" accept=".json,application/json" hidden onChange={(e) => importFile(e.target.files[0])} /></label>
        </div>
      </Card>
      <Card title="Reset all threat settings" desc="Puts everything on this tab and the Settings and Rules tabs back to how it was when you first installed.">
        <div className="actions"><Button kind="danger" disabled={busy} onClick={() => setResetting(true)}>Reset all threat settings</Button></div>
      </Card>
      <ProgramLog />
      <Card title="Where it writes" desc="Where NopeWarden reads what the engine noticed. To remove the program, use Settings.">
        <p className="small">{data.engine?.text}</p>
        <p className="small muted">Watching: {(data.ports || []).length ? `port${data.ports.length === 1 ? '' : 's'} ${data.ports.join(', ')}` : 'no ports yet'}. {p.version ? `Version ${p.version}.` : ''}</p>
        <Field label="Log file" hint={`Leave empty to use ${data.default_eve}.`}><Input value={eve} onChange={setEve} className="in mono" /></Field>
        <div className="actions">
          <Button disabled={busy} onClick={() => run(() => put('/api/threat/settings', { eve_path: eve }), 'Saved.').then(reload)}>Save</Button>
        </div>
      </Card>
      {resetting && (
        <Modal title="Reset all threat settings" onClose={() => setResetting(false)} footer={<><Button onClick={() => setResetting(false)}>Cancel</Button><Button kind="danger" disabled={busy} onClick={async () => { if (await run(() => post('/api/threat/reset'), 'Everything is back to the defaults.')) { setResetting(false); reload(); } }}>Reset everything</Button></>}>
          <p className="small">This puts back the Balanced setting, the standard limits and the standard groups of checks. It forgets every check you switched off, every check you allowed, and every device you told it never to check. Your alerts are kept, and your traffic is not affected.</p>
        </Modal>
      )}
    </div>
  );
}

const dur = (secs) => {
  if (!secs || secs < 0) return '–';
  if (secs < 90) return 'under a minute';
  if (secs < 5400) return `${Math.round(secs / 60)} minutes`;
  if (secs < 129600) return `${Math.round(secs / 3600)} hours`;
  return `${Math.round(secs / 86400)} days`;
};
const when = (ts) => (ts ? new Date(ts * 1000).toLocaleString([], { month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit' }) : 'never');

// The Overview tab, built like the DNS one: the numbers first, then charts, then who and what.
function ThreatOverview({ data, goTab }) {
  const { data: ov } = useData('/api/threat/overview', { every: 10000 });
  const [range, setRange] = useState('1h');
  const { data: h } = useData(`/api/overview/history?range=${range}`, { every: 30000 });
  if (!ov) return null;
  const now = Date.now() / 1000;
  const watching = ov.mode === 'watch';
  const state = !watching ? 'Off' : ov.running ? 'Watching' : ov.failing ? 'Failing' : 'Not running';
  const up = watching && ov.running && ov.since ? dur(now - ov.since) : '';
  const ser = (key) => h?.series?.[key] || [];
  const last = (key) => { const p = ser(key); return p.length ? p[p.length - 1][1] : null; };
  const peakOf = (key) => Math.max(0, ...ser(key).map((p) => p[1]));
  const watched = last('threat.watched');
  const cpuTone = ov.cpu_now >= ov.cpu_limit * 0.9 ? 'bad' : ov.cpu_now >= ov.cpu_limit * 0.7 ? 'warn' : '';
  return (
    <>
      <section className="dns-tiles">
        <div className="card tile"><span className="lbl">Status</span><div><span className="val">{state}</span></div><span className="foot">{!watching ? 'Switch it on above' : up ? `Seen running for ${up}` : ov.why || 'Waiting for the program'}</span></div>
        <div className="card tile"><span className="lbl">Traffic watched</span><div><span className="val">{watched == null ? '–' : fmtBits(watched).split(' ')[0]}</span><span className="unit">{watched == null ? '' : fmtBits(watched).split(' ')[1]}</span></div><span className="foot">{ov.ports?.length ? `On ${ov.ports.length} port${ov.ports.length === 1 ? '' : 's'}: ${ov.ports.join(', ')}` : 'No ports yet'}</span></div>
        <div className="card tile"><span className="lbl">Load on this machine</span><div><span className="val">{watching && ov.running ? fmt.d1(ov.cpu_now) : '–'}</span><span className="unit">% processor</span></div><span className="foot">{watching && ov.running ? `${fmt.n(ov.mem_now_mb)} MB of ${fmt.n(ov.mem_limit_mb)} MB memory${cpuTone ? '. Close to its limit' : ''}` : `Limits: ${ov.cpu_limit}% and ${fmt.n(ov.mem_limit_mb)} MB`}</span></div>
        <div className="card tile"><span className="lbl">Rules</span><div><span className="val">{fmt.n(ov.rules_count)}</span><span className="unit">loaded</span></div><span className="foot">{ov.rules_updated ? `Updated ${when(ov.rules_updated)}` : 'Not updated yet'}{ov.rules_next ? ` · next ${when(ov.rules_next)}` : ''}</span></div>
        <div className="card tile"><span className="lbl">Alerts</span><div><span className="val">{fmt.n(ov.alerts.hour)}</span><span className="unit">last hour</span></div><span className="foot">{fmt.n(ov.alerts.day)} in a day · {fmt.n(ov.alerts.week)} in a week</span></div>
      </section>
      <Card title="Over time" desc={range === '1h' ? 'Last hour' : range === '24h' ? 'Last 24 hours' : 'Last 7 days'}
        actions={<Segmented value={range} onChange={setRange} options={[{ value: '1h', label: '1 h' }, { value: '24h', label: '24 h' }, { value: '7d', label: '7 d' }]} />}>
        <div className="dns-cards">
          <div className="stack gap1"><div className="spread small"><b>Traffic on the watched ports</b><span className="muted">{peakOf('threat.watched') > 0 ? `peak ${fmtBits(peakOf('threat.watched'))}` : ''}</span></div>
            <Chart series={[{ key: 'threat.watched', name: 'Traffic watched', color: 'var(--color-chart-series-1)', pts: ser('threat.watched') }]} from={h?.from || 0} to={h?.to || 1} kind="bits" height={150} label="Traffic on the watched ports" empty="Nothing saved yet. Points appear once watching has been on for a minute." /></div>
          <div className="stack gap1"><div className="spread small"><b>Processor used by the program</b><span className="muted">{peakOf('threat.cpu') > 0 ? `peak ${fmt.d1(peakOf('threat.cpu'))} %` : ''}</span></div>
            <Chart series={[{ key: 'threat.cpu', name: 'Processor', color: 'var(--color-chart-series-2)', pts: ser('threat.cpu') }]} from={h?.from || 0} to={h?.to || 1} unit="%" height={150} label="Processor used by the program" empty="Nothing saved yet. Points appear once watching has been on for a minute." /></div>
        </div>
      </Card>
      <div className="dns-cards">
        <Card title="What was seen" desc="Alerts by kind, last 7 days"
          actions={<button type="button" className="link small" onClick={() => goTab('alerts')}>Open alerts →</button>}>
          {!ov.kinds.length ? <Empty>Nothing suspicious has been seen.</Empty> : (
            <div className="stack">
              {ov.kinds.map((k) => (
                <div key={k.kind} className="stack gap1">
                  <div className="spread small"><span>{k.label}{k.quiet ? ' (usually harmless)' : ''}</span><span className="muted">{fmt.n(k.count)}</span></div>
                  <Bar pct={(100 * k.count) / Math.max(1, ov.kinds[0].count)} thin />
                </div>
              ))}
            </div>
          )}
        </Card>
        <Card title="Devices involved" desc="Alerts that matter, last 7 days. Odd and usually harmless traffic is left out.">
          {!ov.devices.length ? <Empty>No device has been named in an alert.</Empty> : (
            <div className="list">
              {ov.devices.map((d) => <div className="item" key={d.device}><div><div className="name">{d.device}</div></div><div className="meta">{fmt.n(d.count)} alert{d.count === 1 ? '' : 's'}</div></div>)}
            </div>
          )}
        </Card>
      </div>
      <Card title="What it has been doing" desc="Updates, switch-offs and changes. Individual alerts are on the Alerts tab.">
        {!ov.feed.length ? <Empty>Nothing yet.</Empty> : (
          <div className="list">
            {ov.feed.map((f, i) => (
              <div className="item" key={`${f.ts}-${i}`}><div><div className="name">{f.title}</div>{f.detail && <div className="meta">{f.detail}</div>}</div><div className="meta">{when(f.ts)}</div></div>
            ))}
          </div>
        )}
      </Card>
    </>
  );
}

// One line at the top of Overview that says where things stand and where to go next.
function StatusLine({ data, goTab }) {
  const p = data.program || {};
  const watching = data.mode === 'watch';
  const link = (label) => <button type="button" className="link" onClick={() => goTab('settings')}>{label}</button>;
  if (p.state === 'installing' || p.state === 'removing') return <Note title={p.state === 'installing' ? 'Installing' : 'Removing'}>Working on it. The steps are under {link('Settings')}.</Note>;
  if (!p.installed) return <Note>Threat watching is not installed. It is an extra program, installed only if you ask. Install it under {link('Settings')}.</Note>;
  if (!watching) return data.paused_reason ? <Note tone="warn" title="NopeWarden switched watching off">{data.paused_reason} Your traffic was never held up. Switch it on again under {link('Settings')}.</Note> : <Note>Threat watching is off. Switch it on under {link('Settings')}.</Note>;
  if (!p.running) return <Note tone={p.failing ? 'bad' : 'warn'} title={p.failing ? 'The program keeps failing' : 'The program is not running'}>{p.why} Your traffic is not affected.</Note>;
  return <Note tone={data.engine?.state === 'missing' ? 'warn' : undefined}>{data.engine?.text}</Note>;
}

export default function Threats() {
  const [tab, setTab] = useHashTab('threats', ['overview','alerts','settings','rules','advanced'], 'overview');
  const [fast, setFast] = useState(false);
  const { data, reload } = useData('/api/threat', { every: fast ? 1500 : 5000 });
  const working = data?.program?.state === 'installing' || data?.program?.state === 'removing';
  useEffect(() => { setFast(!!working); }, [working]);
  const p = data?.program || {};
  const tabs = [{ value: 'overview', label: 'Overview' }, { value: 'alerts', label: 'Alerts' }, { value: 'settings', label: 'Settings' }, { value: 'rules', label: 'Rules' }, { value: 'advanced', label: 'Advanced' }];
  return (
    <Page title="Threats" sub="Watches for infected devices, break-in attempts and probing, and tells you in plain words.">
      <Tabs value={tab} onChange={setTab} tabs={tabs} />
      {data && (
        <div className="stack">
          {tab === 'overview' && <><StatusLine data={data} goTab={setTab} />{p.installed && !working && <ThreatOverview data={data} goTab={setTab} />}</>}
          {tab === 'settings' && <Settings data={data} reload={reload} />}
          {tab !== 'overview' && tab !== 'settings' && !p.installed && <Note>Threat watching is not installed yet. Install it under <button type="button" className="link" onClick={() => setTab('settings')}>Settings</button>.</Note>}
          {p.installed && tab === 'alerts' && <Alerts data={data} reload={reload} />}
          {p.installed && tab === 'rules' && <Rules data={data} reload={reload} goSettings={() => setTab('settings')} />}
          {p.installed && tab === 'advanced' && <Advanced data={data} reload={reload} />}
        </div>
      )}
    </Page>
  );
}

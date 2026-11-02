import React, { useState } from 'react';
import { del, post, put, get, useData } from '../api.js';
import { Button, Card, Empty, Field, Input, Modal, Note, NumberInput, Pill, Segmented, Tabs, useAction, fmt } from '../ui.jsx';

// Threat watching. An engine (Suricata) is installed only when the owner asks. It watches copies of
// the traffic and Gatehouse says what it noticed in plain words. It never holds traffic up, so a
// crash, a removal or a switch-off leaves the connection exactly as it was.

const mb = (n) => (n >= 1024 ? `${(n / 1024).toFixed(1)} GB` : `${n} MB`);

function InstallPrompt({ data, reload }) {
  const [run, busy] = useAction();
  const [pre, setPre] = useState(null);
  const p = data.program || {};
  const working = p.state === 'installing' || p.state === 'removing';
  const look = async () => { const r = await run(() => post('/api/threat/install/preview')); if (r) setPre(r); };
  return (
    <Card title="Threat watching" desc="Watches for infected devices, break-in attempts and probing, and tells you in plain words.">
      <div className="stack">
        {working ? <Note title={p.state === 'installing' ? 'Installing' : 'Removing'}>{p.message || 'Working. This can take a few minutes. You can leave this page.'}</Note> : (
          <>
            <p className="small">This is not installed. It is an extra program, installed only if you ask. Nothing else in Gatehouse needs it.</p>
            {p.state === 'failed' && <Note tone="warn" title="The last install did not finish">{p.message}</Note>}
            <div className="actions"><Button kind="primary" disabled={busy} onClick={look}>Install threat watching</Button></div>
          </>
        )}
        <p className="small muted">{data.blocking} {data.fail_open}</p>
      </div>
      {pre && (
        <Modal title="Install threat watching" onClose={() => setPre(null)} footer={<><Button onClick={() => setPre(null)}>Cancel</Button><Button kind="primary" disabled={busy} onClick={async () => { if (await run(() => post('/api/threat/install'), 'Installing. This can take a few minutes.')) { setPre(null); reload(); } }}>Install</Button></>}>
          <div className="stack">
            <p className="small">Gatehouse will download and install: <strong>{(pre.preview.packages || []).join(', ')}</strong>.</p>
            <p className="small">{pre.preview.download_mb > 0 || pre.preview.disk_mb > 0 ? `Download about ${mb(Math.ceil(pre.preview.download_mb || 0))}. Disk space about ${mb(Math.ceil(pre.preview.disk_mb || 0))}.` : 'Debian did not say how big this is; it is usually under 100 MB.'} This machine has {mb(pre.disk_free_mb || 0)} free, {mb(pre.memory_mb || 0)} of memory and {pre.cpus || '?'} processor core{pre.cpus === 1 ? '' : 's'}.</p>
            {(pre.warnings || []).map((w) => <Note key={w} tone="warn">{w}</Note>)}
            {!(pre.warnings || []).length && <Note>This machine has enough room.</Note>}
            <p className="small muted">Installing does not turn watching on. You do that next, and can switch it off or remove it at any time.</p>
          </div>
        </Modal>
      )}
    </Card>
  );
}

function Alerts({ data, reload }) {
  const [run, busy] = useAction();
  const [days, setDays] = useState('0');
  return (
    <div className="list">
      {(data.alerts || []).map((x) => (
        <div className="item" key={x.id}>
          <div style={{ flex: 1, minWidth: 0 }}>
            <div className="name">{x.plain}</div>
            <div className="meta">{x.device} · {fmt.ago(x.ts)} · {x.signature}</div>
          </div>
          <div className="actions">
            <Button small disabled={busy} onClick={() => run(() => post('/api/threat/allow', { sid: x.sid, mac: x.mac, days: Number(days), note: x.signature }), 'This will not alert again for this device.').then(reload)} title="Let this through for this device">Allow for this device</Button>
            <Button small disabled={busy} onClick={() => run(() => post('/api/threat/allow', { sid: x.sid, mac: '', days: Number(days), note: x.signature }), 'This will not alert again.').then(reload)}>Allow for all</Button>
            {x.mac && <Button small disabled={busy} onClick={() => run(() => post('/api/threat/trust', { mac: x.mac }), `${x.device} will not be checked.`).then(reload)}>Never check this device</Button>}
          </div>
        </div>
      ))}
      {!(data.alerts || []).length && <Empty>Nothing suspicious has been seen.</Empty>}
      <Field label="Allowing lasts"><Segmented value={days} onChange={setDays} options={[{ value: '0', label: 'Until I remove it' }, { value: '7', label: '7 days' }, { value: '1', label: '1 day' }]} /></Field>
    </div>
  );
}

function Settings({ data, reload }) {
  const [run, busy] = useAction();
  const [cpu, setCpu] = useState(data.cpu_percent);
  const [mem, setMem] = useState(data.memory_mb);
  const p = data.program || {};
  return (
    <div className="stack">
      <Card title="How closely to watch" desc="A lighter setting looks for fewer things and uses less of the machine.">
        <div className="list">
          {(data.presets || []).map((x) => (
            <label className="item" key={x.key} style={{ cursor: 'pointer' }}>
              <input type="radio" name="preset" checked={data.preset === x.key} disabled={busy} onChange={() => run(() => put('/api/threat/settings', { preset: x.key }), 'Saved.').then(reload)} />
              <div style={{ flex: 1 }}><div className="name">{x.title}</div><div className="meta">{x.text}</div></div>
            </label>
          ))}
        </div>
      </Card>
      <Card title="Limits" desc="If watching uses all of its processor share for ten minutes, Gatehouse switches it off by itself and says so in the Logs. Your traffic is never held up either way.">
        <div className="formgrid">
          <Field label="Processor limit (% of one core)" hint="10 to 400."><NumberInput value={cpu} onChange={setCpu} /></Field>
          <Field label="Memory limit (MB)" hint="256 to 16384."><NumberInput value={mem} onChange={setMem} /></Field>
        </div>
        <div className="actions"><Button disabled={busy} onClick={() => run(() => put('/api/threat/settings', { cpu_percent: Number(cpu), memory_mb: Number(mem) }), 'Saved.').then(reload)}>Save limits</Button></div>
        {p.installed && <p className="small muted">Right now: {p.running ? 'running' : 'not running'}{p.memory_bytes ? `, using ${mb(Math.round(p.memory_bytes / 1048576))} of memory` : ''}.</p>}
      </Card>
    </div>
  );
}

function Rules({ data, reload }) {
  const [run, busy] = useAction();
  const [q, setQ] = useState('');
  const [found, setFound] = useState(null);
  const p = data.program || {};
  const search = async () => { const r = await run(() => get(`/api/threat/rules?q=${encodeURIComponent(q)}`)); if (r) setFound(r); };
  const flip = async (x) => { if (await run(() => post(`/api/threat/rules/${x.sid}`, { on: !x.on }), x.on ? 'Switched off.' : 'Switched on.')) { setFound({ ...found, rules: found.rules.map((y) => (y.sid === x.sid ? { ...y, on: !y.on } : y)) }); } };
  const groups = useData('/api/threat/groups');
  return (
    <div className="stack">
      <Card title="Rules" desc={p.rules_updated ? `${fmt.n(p.rules_count)} checks, updated ${fmt.dt(p.rules_updated)}.` : 'The list of things to look for. It is updated once a day while watching is on.'}>
        <div className="actions">
          <Button disabled={busy || !p.installed} onClick={() => run(() => post('/api/threat/rules/update'), 'Updating the rules. This can take a minute.').then(reload)}>Update rules now</Button>
          <Button disabled={busy || !p.has_previous} onClick={() => run(() => post('/api/threat/rules/rollback'), 'Went back to the previous rules.').then(reload)} title="Use the rules from before the last update">Roll back to the previous rules</Button>
        </div>
        {!p.has_previous && <p className="small muted">There is no earlier set to go back to yet.</p>}
      </Card>
      <Card title="Groups of checks" desc={groups.data?.note}>
        <div className="list" style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(min(100%, 17rem), 1fr))', columnGap: 'var(--space-4)' }}>
          {(groups.data?.groups || []).map((g) => <div className="item" key={g.name}><div><div className="name">{g.name}</div><div className="meta">{g.text}</div></div><Pill tone={g.on ? 'good' : ''}>{g.on ? 'On' : 'Off'}</Pill></div>)}
        </div>
      </Card>
      <Card title="Single checks" desc="Find one by a word or its number, and switch it on or off.">
        <div className="actions"><Input value={q} onChange={setQ} placeholder="For example beacon, or 2001" onKeyDown={(e) => { if (e.key === 'Enter') search(); }} /><Button disabled={busy} onClick={search}>Search</Button></div>
        {found && (
          <div className="list">
            {found.rules.map((x) => <div className="item" key={x.sid}><div style={{ flex: 1, minWidth: 0 }}><div className="name">{x.msg}</div><div className="meta">Check {x.sid}</div></div><Button small disabled={busy} onClick={() => flip(x)}>{x.on ? 'Switch off' : 'Switch on'}</Button></div>)}
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
  const load = async () => { const r = await run(() => get('/api/diag/log?unit=gatehouse-suricata&lines=60')); if (r) setText(r.log); };
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
  const [removing, setRemoving] = useState(false);
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
          {(data.allowed || []).map((x) => (
            <div className="item" key={x.id}>
              <div><div className="name">Check {x.sid} {x.auto && <Pill>Quieted by Gatehouse</Pill>}</div><div className="meta">{x.device || 'All devices'} · {x.until ? `until ${fmt.dt(x.until)}` : 'until removed'} · {x.note}</div></div>
              <Button small kind="danger" disabled={busy} onClick={() => run(() => del(`/api/threat/allow/${x.id}`), 'Removed.').then(reload)}>Remove</Button>
            </div>
          ))}
          {!(data.allowed || []).length && <Empty>Nothing is allowed yet.</Empty>}
        </div>
      </Card>
      <Card title="Devices never checked" desc="Traffic to and from these devices is ignored.">
        <div className="list">
          {(data.trusted || []).map((x) => (
            <div className="item" key={x.mac}><div><div className="name">{x.device}</div><div className="meta mono">{x.mac}</div></div>
              <Button small kind="danger" disabled={busy} onClick={() => run(() => del(`/api/threat/trust/${encodeURIComponent(x.mac)}`), 'Removed.').then(reload)}>Check again</Button></div>
          ))}
          {!(data.trusted || []).length && <Empty>Every device is checked.</Empty>}
        </div>
      </Card>
      <Card title="Save or load your tuning" desc="Your preset, limits, switched-off checks, allowed checks and devices never checked, as one file.">
        <div className="actions">
          <a className="btn" href="/api/threat/export" download="gatehouse-threat-tuning.json">Save tuning to a file</a>
          <label className="btn" style={{ cursor: 'pointer' }}>Load tuning from a file<input type="file" accept=".json,application/json" hidden onChange={(e) => importFile(e.target.files[0])} /></label>
        </div>
      </Card>
      <ProgramLog />
      <Card title="Program" desc="Where Gatehouse reads what the engine noticed.">
        <p className="small">{data.engine?.text}</p>
        <p className="small muted">Watching: {(data.ports || []).length ? `port${data.ports.length === 1 ? '' : 's'} ${data.ports.join(', ')}` : 'no ports yet'}. {p.version ? `Version ${p.version}.` : ''}</p>
        <Field label="Log file" hint={`Leave empty to use ${data.default_eve}.`}><Input value={eve} onChange={setEve} className="in mono" /></Field>
        <div className="actions">
          <Button disabled={busy} onClick={() => run(() => put('/api/threat/settings', { eve_path: eve }), 'Saved.').then(reload)}>Save</Button>
          {p.installed && <Button kind="danger" disabled={busy} onClick={() => setRemoving(true)}>Remove threat watching</Button>}
        </div>
      </Card>
      {removing && (
        <Modal title="Remove threat watching" onClose={() => setRemoving(false)} footer={<><Button onClick={() => setRemoving(false)}>Cancel</Button><Button kind="danger" disabled={busy} onClick={async () => { if (await run(() => post('/api/threat/remove'), 'Removing. This can take a minute.')) { setRemoving(false); reload(); } }}>Remove</Button></>}>
          <p className="small">The program is switched off and removed. Your traffic is not affected. Your alerts and tuning are kept.</p>
        </Modal>
      )}
    </div>
  );
}

export default function Threats() {
  const [tab, setTab] = useState('alerts');
  const [run, busy] = useAction();
  const { data, reload } = useData('/api/threat', { every: 5000 });
  if (!data) return null;
  const p = data.program || {};
  if (!p.installed) return <InstallPrompt data={data} reload={reload} />;
  const watching = data.mode === 'watch';
  return (
    <Card title="Threat watching" desc="Watches for infected devices, break-in attempts and probing, and tells you in plain words.">
      <div className="stack">
        <Segmented value={data.mode} onChange={(v) => run(() => put('/api/threat/settings', { mode: v }), v === 'watch' ? 'Watching is on.' : 'Watching is off.').then(reload)} options={[{ value: 'off', label: 'Off' }, { value: 'watch', label: 'Watch and tell' }, { value: 'protect', label: 'Protect (not yet)', disabled: true }]} disabled={busy || p.state === 'installing' || p.state === 'removing'} />
        {data.paused_reason && !watching && <Note tone="warn" title="Gatehouse switched watching off">{data.paused_reason} Your traffic was never held up.</Note>}
        {watching && !p.running && <Note tone={p.failing ? 'bad' : 'warn'} title={p.failing ? 'The program keeps failing' : 'The program is not running'}>{p.why} Your traffic is not affected. See Advanced, Program log, or Diagnostics in the left menu.</Note>}
        {p.state === 'updating' && <Note>Updating the rules. {p.message}</Note>}
        {p.state === 'failed' && <Note tone="warn">{p.message}</Note>}
        <Note tone={data.engine?.state === 'missing' && watching ? 'warn' : undefined}>{data.engine?.text}</Note>
        <p className="small muted">{data.blocking} {data.fail_open} {data.limits}</p>
        <Tabs value={tab} onChange={setTab} tabs={[{ value: 'alerts', label: 'Alerts' }, { value: 'settings', label: 'Settings' }, { value: 'rules', label: 'Rules' }, { value: 'advanced', label: 'Advanced' }]} />
        {tab === 'alerts' && <Alerts data={data} reload={reload} />}
        {tab === 'settings' && <Settings data={data} reload={reload} />}
        {tab === 'rules' && <Rules data={data} reload={reload} />}
        {tab === 'advanced' && <Advanced data={data} reload={reload} />}
      </div>
    </Card>
  );
}

import React, { useEffect, useState } from 'react';
import { get, post, put, useData } from '../api.js';
import { Guard, Button, Card, Field, Form, Input, Modal, Note, Page, Segmented, Select, Tabs, useAction, useCheck, CheckLine, fmt, useStepUp } from '../ui.jsx';
import { useHashTab } from '../hashtab.js';
import { ScheduleEditor, ScheduleTable } from '../ScheduleEditor.jsx';
import { ReleaseList, plainNotes } from '../whatsnew.jsx';
import { RELEASES } from '../releasenotes.js';
import { PowerModal } from './PowerModal.jsx';

const hourLabel = (h) => `${h === 0 ? 12 : h > 12 ? h - 12 : h}:00 ${h < 12 ? 'AM' : 'PM'}`;

function UpdateSteps({ steps, rollingBack }) {
  const mark = { done: '✓', active: '●', pending: '○' };
  return (
    <ol className="usteps" aria-label="Update progress">
      {steps.map((st) => (
        <li key={st.id} className={st.state}><span aria-hidden="true">{mark[st.state]}</span> {st.label}{st.state === 'active' && rollingBack ? ' (undoing)' : ''}</li>
      ))}
    </ol>
  );
}


function StatusTab({ me, data, reload, goTab }) {
  const [run, busy] = useAction();
  const [chk, chkGo] = useCheck();
  const [asking, setAsking] = useState(false);
  const [code, setCode] = useState('');
  const [job, setJob] = useState(null); // { since } while an update is going on
  const [live, setLive] = useState(null); // fresher status while something is happening
  const [lost, setLost] = useState(false);
  const [outcome, setOutcome] = useState(null);
  const [restart, setRestart] = useState(false);
  const d = live || data;

  // An update that is already going on (the page was opened or reloaded in the middle of it).
  useEffect(() => {
    if (!job && data && (data.working || data.state !== 'idle')) setJob({ since: 0 });
  }, [data, job]);

  // While installing the console restarts, so errors are expected; keep asking until it answers.
  useEffect(() => {
    if (!job && !data?.checking) return undefined;
    let lastOk = Date.now();
    const t = setInterval(async () => {
      try {
        const x = await get('/api/system/update');
        lastOk = Date.now();
        setLost(false);
        setLive(x);
        if (job) {
          const r = x.result;
          if (!x.working && x.state === 'idle' && (x.run_error || (r && r.at >= job.since))) {
            setOutcome({ result: r && r.at >= job.since ? r : null, error: x.run_error });
            setJob(null);
            setLive(null);
            reload();
            // The list of waiting updates is from the check before the run, so look again now;
            // otherwise it keeps showing updates that were just installed.
            post('/api/system/update/check', {}).then(() => reload()).catch(() => {});
          }
        } else if (!x.checking) {
          setLive(null);
          reload();
        }
      } catch {
        if (Date.now() - lastOk > 180000) setLost(true);
      }
    }, 3000);
    return () => clearInterval(t);
  }, [job, data?.checking, reload]);

  const everyday = useStepUp().everyday; // a hook: it must run on every render, before any early return
  if (!d) return null;
  const go = async () => {
    const r = await run(() => post('/api/system/update/install', { code }));
    if (r) { setAsking(false); setCode(''); setOutcome(null); setJob({ since: Math.floor(Date.now() / 1000) - 5 }); }
  };
  const start = () => { if (everyday) setAsking(true); else go(); };
  const l = d.latest;
  const steps = d.steps || [];
  const working = !!job;
  // After the console restarts the page reloads and forgets what it started, so fall back to the
  // result the machine kept: the last run's outcome stays visible for a day.
  const kept = d.result && d.result.ok && Date.now() / 1000 - (d.result.at || 0) < 86400 ? d.result : null;
  const res = outcome?.result || kept;
  return (
    <Card title="Software updates" desc="NopeWarden and the system underneath it update together, from one button. Nothing is installed until you press Update now, unless you choose Automatic on the Schedule tab. Your settings are kept."
      actions={<Button small disabled={busy || working || d.checking || !d.key_set} onClick={() => chkGo(async () => { await post('/api/system/update/check', {}); await reload(); }, 'Looking for a new version…', 'Checked just now.')}>Check now</Button>}>
      <div className="stack">
        <div className="inline"><span>You are running version <b>{d.current}</b>.</span>{d.checked_at > 0 && <span className="small muted">Checked {fmt.ago(d.checked_at)}</span>}</div>
        <CheckLine st={chk} />
        {!d.key_set && <Note tone="warn" title="Updates are not set up in this build">This copy of NopeWarden has no update signing key, so it cannot install updates. Build a new installer once with a key (docs/UPDATES.md); after that, updates need no installer.</Note>}
        {d.key_set && d.error && <Note tone="warn" title="Could not look for NopeWarden updates">{d.error}</Note>}
        {d.key_set && d.system_error && <Note tone="warn" title="Could not look at the system updates">{d.system_error}</Note>}
        {d.pair?.phase === 'failed' && d.pair.error && <Note tone="warn" title="The update of both units stopped">{d.pair.error}</Note>}
        {d.checking && !working && <p className="muted">Looking at what the system has waiting…</p>}
        {working && (
          <Note tone="good" title="Updating">
            {steps.length > 0 && <UpdateSteps steps={steps} rollingBack={d.rolling_back} />}
            <p className="small" style={{ margin: '.5rem 0 0' }}>{d.rolling_back ? 'Something went wrong, so the previous version is being put back.' : 'This takes a few minutes. The console restarts near the end and this page reconnects by itself. If anything goes wrong the old NopeWarden is put back automatically.'}</p>
            {lost && <p className="small" style={{ margin: '.5rem 0 0' }}><b>This page cannot reach the machine.</b> Look at the machine's own screen: it shows what is happening.</p>}
          </Note>
        )}
        {!working && (outcome || kept) && ((outcome || {}).error
          ? <Note tone="bad" title="The update could not start">{outcome.error}</Note>
          : res?.ok
            ? <Note tone="good" title={res.from === res.to ? 'System updates installed' : `Updated to ${res.to}`}>
                {res.from === res.to ? 'NopeWarden was already the newest version.' : 'Everything is running the new version.'}
                {res.system && res.system.packages > 0 && ` ${res.system.packages} system packages were updated.`}
                {res.system?.skipped && ` The system updates were skipped: ${res.system.skipped}`}
              </Note>
            : <Note tone="bad" title={res?.rolled_back ? 'The update did not work and was undone' : 'The update did not work'}>{res?.error}</Note>)}
        {!working && !outcome && d.result && !d.result.ok && <Note tone="warn" title={d.result.rolled_back ? 'The last update was undone' : 'The last update did not work'}>{d.result.error}</Note>}
        {!working && d.available && (
          <Note tone="good" title="Updates are ready">
            <ul className="ulist">
              {d.nopewarden_new && l && <li><b>NopeWarden {l.version}</b>{plainNotes(l.notes) && <div style={{ whiteSpace: 'pre-wrap' }} className="small">{plainNotes(l.notes)}</div>}</li>}
              {d.system_count > 0 && <li><b>{d.system_count} system update{d.system_count === 1 ? '' : 's'}</b>{d.system_names?.length > 0 && <span className="small muted"> ({d.system_names.slice(0, 6).join(', ')}{d.system_count > 6 ? ', …' : ''})</span>}</li>}
            </ul>
            <Button kind="primary" disabled={busy || d.state !== 'idle'} onClick={start}>Update now</Button>
          </Note>
        )}
        {!working && d.key_set && !d.available && !d.error && !d.system_error && !d.checking && d.checked_at > 0 && <p className="muted">Everything is up to date.</p>}
        {d.needs_reboot && !working && <Note tone="warn" title="A restart is needed">The system was updated and needs a restart to finish. The network is down for about a minute while it restarts; pick a moment that suits you.<div style={{ marginTop: '.5rem' }}><Button onClick={() => setRestart(true)}>Restart now</Button></div></Note>}
        <p className="small muted">{d.mode === 'auto' ? `Updates install by themselves at ${hourLabel(d.window)}.` : d.mode === 'notify' ? 'NopeWarden looks for updates and tells you when one is ready.' : 'NopeWarden only looks when you press Check now.'} <button type="button" className="link" onClick={() => goTab('schedule')}>Change when</button></p>
      </div>
      {restart && <PowerModal action="reboot" me={me} onClose={() => setRestart(false)} />}
      {asking && (
        <Modal title="Install the updates" onClose={() => setAsking(false)}
          footer={<><Button onClick={() => setAsking(false)}>Cancel</Button><Button kind="primary" disabled={busy || code.length < 6} onClick={go}>Install</Button></>}>
          <Field label="Code from your authenticator app"><Input value={code} onChange={setCode} inputMode="numeric" autoComplete="one-time-code" autoFocus /></Field>
        </Modal>
      )}
    </Card>
  );
}

function ScheduleTab({ data, reload }) {
  const [run] = useAction();
  if (!data) return null;
  const d = data;
  const save = (patch) => run(() => put('/api/system/update/settings', patch)).then(reload);
  return (
    <>
      <Card title="When updates happen" desc="Choose whether NopeWarden looks for updates, and whether it installs them for you.">
        <div className="stack">
          <Field label="Updating" hint="Off: only when you press Check now. Notify: NopeWarden looks as often as you choose below and tells you when something is ready. Automatic: it installs by itself in the hour you choose below.">
            <Segmented value={d.mode} onChange={(v) => save({ mode: v })} options={[{ value: 'off', label: 'Off' }, { value: 'notify', label: 'Notify' }, { value: 'auto', label: 'Automatic' }]} />
          </Field>
          {d.mode !== 'off' && <ScheduleEditor id="updates" label="How often to check" />}
          {d.mode === 'auto' && (
            <Field label="Start automatic updates at" hint="NopeWarden restarts for about a minute while it updates, so pick a quiet hour (the machine's own clock).">
              <Select value={String(d.window)} onChange={(v) => save({ window: Number(v) })} options={Array.from({ length: 24 }, (_, h) => ({ value: String(h), label: hourLabel(h) }))} style={{ maxWidth: '10rem' }} />
            </Field>
          )}
          <p className="small muted">An update that is ready is always shown at the top of the Overview, whichever you choose here.</p>
        </div>
      </Card>
      <Card title="Other recurring jobs" desc="When each of the other jobs looks for something new, such as block lists and country lists. The same controls appear on the page each job belongs to.">
        <ScheduleTable except={['updates']} />
      </Card>
    </>
  );
}

function ReleasesTab() {
  return (
    <Card title="What's new" desc="What changed in each version of NopeWarden, newest first.">
      <ReleaseList list={RELEASES} />
    </Card>
  );
}

function AdvancedTab({ data, reload }) {
  const [run, busy] = useAction();
  const [repo, setRepo] = useState(null);
  if (!data) return null;
  return (
    <Card title="Where releases come from" desc="Only releases signed with this build's key are ever installed, wherever they are published.">
      <Field label="Release page" hint="The GitHub page that publishes NopeWarden releases, as owner/name.">
        <Form className="inline" onSubmit={() => run(() => put('/api/system/update/settings', { repo }), 'Saved.').then(() => { setRepo(null); reload(); })}><Input value={repo ?? data.repo} onChange={setRepo} aria-label="Release page" style={{ maxWidth: '18rem' }} />
          <Button type="submit" disabled={busy || repo === null || repo === data.repo}>Save</Button></Form>
      </Field>
    </Card>
  );
}

const TABS = [
  { value: 'updates', label: 'Updates' },
  { value: 'schedule', label: 'Schedule' },
  { value: 'whatsnew', label: "What's new" },
  { value: 'advanced', label: 'Advanced' },
];

export default function Updates({ me }) {
  const { data, reload } = useData('/api/system/update', { every: 15000 });
  const [tab, setTab] = useHashTab('updates', ['updates', 'schedule', 'whatsnew', 'advanced'], 'updates');
  if (me?.user?.role !== 'admin') {
    return <Page title="Updates" sub="Keep NopeWarden and the system underneath it up to date."><Note>Only administrators can see and change updates.</Note></Page>;
  }
  return (
    <Page title="Updates" sub="Keep NopeWarden and the system underneath it up to date.">
      <Tabs value={tab} onChange={setTab} tabs={TABS} />
      {tab === 'updates' && <Guard what="Updates"><StatusTab me={me} data={data} reload={reload} goTab={setTab} /></Guard>}
      {tab === 'schedule' && <Guard what="Update schedule"><ScheduleTab data={data} reload={reload} /></Guard>}
      {tab === 'whatsnew' && <Guard what="What's new"><ReleasesTab /></Guard>}
      {tab === 'advanced' && <Guard what="Update source"><AdvancedTab data={data} reload={reload} /></Guard>}
      {(tab === 'updates' || tab === 'schedule') && <p className="small muted">Need more control, such as where updates come from? Open the <button type="button" className="link" onClick={() => setTab('advanced')}>Advanced</button> tab.</p>}
    </Page>
  );
}

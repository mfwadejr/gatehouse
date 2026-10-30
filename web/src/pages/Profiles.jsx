import React, { useState } from 'react';
import { del, post, put, useData } from '../api.js';
import { Button, Card, Field, Input, Modal, Note, Page, Pill, Segmented, ToggleRow, confirmBox, useAction } from '../ui.jsx';

// Profiles: a name for a kind of person or device (Kids, Guests, Smart home, Work) with how much to
// filter, safe search, hours when the internet is off, and a Pause button. Devices join a profile on
// the Devices page.

const DAYS = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];
const LEVELS = [
  { value: 'off', label: 'Off' },
  { value: 'basic', label: 'Basic' },
  { value: 'enhanced', label: 'Enhanced' },
  { value: 'custom', label: 'Choose' },
];
const levelText = {
  off: 'No filtering.',
  basic: 'Blocks ads, trackers, malware and phishing.',
  enhanced: 'Also blocks adult content and gambling.',
  custom: 'Blocks the categories you pick.',
};

export function pauseChoices() {
  return [{ m: 30, t: '30 minutes' }, { m: 60, t: '1 hour' }, { m: 120, t: '2 hours' }, { m: -1, t: 'Until I resume' }];
}

export function whenText(until) {
  if (!until || until * 1000 < Date.now()) return '';
  if (until * 1000 - Date.now() > 5 * 365 * 864e5) return 'Paused until you resume';
  return `Paused until ${new Date(until * 1000).toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' })}`;
}

function Editor({ init, data, onClose, onSaved }) {
  const [p, setP] = useState(init);
  const [run, busy] = useAction();
  const set = (k, v) => setP((x) => ({ ...x, [k]: v }));
  const setH = (k, v) => set('hours', { ...p.hours, [k]: v });
  const toggleDay = (d) => setH('days', p.hours.days.includes(d) ? p.hours.days.filter((x) => x !== d) : [...p.hours.days, d].sort());
  const toggleCat = (k) => set('categories', p.categories.includes(k) ? p.categories.filter((x) => x !== k) : [...p.categories, k]);
  const save = async () => {
    const body = { id: p.id, name: p.name, description: p.description || '', preset: p.preset || '', level: p.level, categories: p.categories, safe_search: p.safe_search, hours: p.hours };
    const r = await run(() => put('/api/profiles', body), 'Profile saved.');
    if (r) { onSaved(); onClose(); }
  };
  return (
    <Modal title={p.id ? `Edit ${init.name}` : 'New profile'} onClose={onClose}
      footer={<><Button onClick={onClose}>Cancel</Button><Button kind="primary" onClick={save} disabled={busy || !p.name.trim()}>Save</Button></>}>
      <div className="stack">
        <Field label="Name" hint="For example Kids, Guests, Smart home."><Input value={p.name} onChange={(v) => set('name', v)} /></Field>
        <Field label="How much to filter" hint={levelText[p.level]}>
          <Segmented value={p.level} onChange={(v) => set('level', v)} options={LEVELS} />
        </Field>
        {p.level === 'custom' && (
          <div className="stack">
            {data.categories.map((c) => (
              <ToggleRow key={c.key} title={c.name} desc={c.descr} checked={p.categories.includes(c.key)} onChange={() => toggleCat(c.key)} />
            ))}
          </div>
        )}
        <ToggleRow title="Safe search" desc="Makes Google, Bing, DuckDuckGo and YouTube hide adult results. It works for browsers that use Gatehouse for DNS (turn on Forced DNS for devices that do not)." checked={p.safe_search} onChange={(v) => set('safe_search', v)} />
        <ToggleRow title="Internet off at set hours" desc="For example a bedtime. Devices keep their local network, but cannot reach the internet." checked={p.hours.on} onChange={(v) => setH('on', v)} />
        {p.hours.on && (
          <div className="stack">
            <div className="row" style={{ gap: 6, flexWrap: 'wrap' }}>
              {DAYS.map((d, i) => <Button key={d} small kind={p.hours.days.includes(i) ? 'primary' : ''} onClick={() => toggleDay(i)}>{d}</Button>)}
            </div>
            <div className="formgrid">
              <Field label="Off from"><Input type="time" value={p.hours.from} onChange={(v) => setH('from', v)} /></Field>
              <Field label="Back on at"><Input type="time" value={p.hours.to} onChange={(v) => setH('to', v)} /></Field>
            </div>
            <span className="small muted">If the second time is earlier, the window runs past midnight and belongs to the day it starts on.</span>
          </div>
        )}
      </div>
    </Modal>
  );
}

export default function Profiles() {
  const { data, reload } = useData('/api/profiles', { every: 15000 });
  const [edit, setEdit] = useState(null);
  const [run] = useAction();
  if (!data) return <Page title="Profiles"><Card title="Profiles"><span className="muted">Loading…</span></Card></Page>;
  const blank = { id: 0, name: '', description: '', preset: '', level: 'basic', categories: [], safe_search: false, hours: { on: false, days: [0, 1, 2, 3, 4, 5, 6], from: '21:00', to: '07:00' } };
  const fromPreset = (s) => ({ ...blank, name: s.name, preset: s.key, level: s.level, categories: s.categories || [], safe_search: s.safe_search, hours: s.hours.on ? s.hours : blank.hours });
  const have = new Set(data.profiles.map((x) => x.preset));
  const pause = (id, minutes, text) => run(async () => { await post(`/api/profiles/${id}/pause`, { minutes }); reload(); }, text);
  const remove = async (x) => {
    if (!(await confirmBox(`Delete ${x.name}? Its devices go back to the Default profile.`))) return;
    run(async () => { await del(`/api/profiles/${x.id}`); reload(); }, 'Profile deleted.');
  };
  return (
    <Page title="Profiles" sub="Group people and devices by what they should be able to do, then give each group a filtering level, safe search and off-hours.">
      <div className="stack">
        {data.profiles.map((x) => (
          <Card key={x.id} title={x.name} desc={x.description || levelText[x.level]}
            actions={<><Button small onClick={() => setEdit({ ...x, hours: { ...x.hours } })}>Edit</Button>{x.name !== 'Default' && x.name !== 'Trusted' && <Button small onClick={() => remove(x)}>Delete</Button>}</>}>
            <div className="row" style={{ gap: 8, flexWrap: 'wrap' }}>
              <Pill>{x.devices} device{x.devices === 1 ? '' : 's'}</Pill>
              <Pill tone={x.level === 'off' ? '' : 'good'}>{LEVELS.find((l) => l.value === x.level)?.label} filtering</Pill>
              {x.safe_search && <Pill tone="good">Safe search</Pill>}
              {x.hours.on && <Pill>Off {x.hours.from}–{x.hours.to}</Pill>}
              {x.off_now && <Pill tone="warn">{whenText(x.paused_until) || 'Internet off now'}</Pill>}
            </div>
            <div className="row" style={{ gap: 8, marginTop: 'var(--space-2)', flexWrap: 'wrap' }}>
              {x.paused_until * 1000 > Date.now()
                ? <Button small kind="primary" onClick={() => pause(x.id, 0, `${x.name} resumed.`)}>Resume internet</Button>
                : pauseChoices().map((c) => <Button key={c.m} small onClick={() => pause(x.id, c.m, `${x.name} paused.`)}>Pause {c.t.toLowerCase()}</Button>)}
            </div>
          </Card>
        ))}
        <Card title="Add a profile" desc="Start from a preset, then change anything.">
          <div className="row" style={{ gap: 8, flexWrap: 'wrap' }}>
            {data.presets.filter((s) => !have.has(s.key)).map((s) => <Button key={s.key} onClick={() => setEdit(fromPreset(s))} title={s.descr}>{s.name}</Button>)}
            <Button kind="primary" onClick={() => setEdit(blank)}>Blank profile</Button>
          </div>
        </Card>
        <Note title="Good to know">Put a device in a profile on the Devices page. Category lists download the first time you use them, so a new category can take a minute to start working. Filtering works through Gatehouse's DNS, so a device that uses its own DNS or an encrypted-DNS setting can skip it; Forced DNS and the DoH option under Firewall close that gap.</Note>
      </div>
      {edit && <Editor init={edit} data={data} onClose={() => setEdit(null)} onSaved={reload} />}
    </Page>
  );
}


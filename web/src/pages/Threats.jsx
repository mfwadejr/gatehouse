import React, { useState } from 'react';
import { del, post, put, useData } from '../api.js';
import { Button, Card, Empty, Field, Input, Note, Pill, Segmented, Tabs, useAction, fmt } from '../ui.jsx';

// Threat protection: a detection engine watches the traffic; Gatehouse reads what it noticed and
// says it in plain words. It watches and tells; it does not block yet. A fault never touches traffic.

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

function Advanced({ data, reload }) {
  const [run, busy] = useAction();
  const [eve, setEve] = useState(data.eve_path === data.default_eve ? '' : data.eve_path);
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
      <Card title="Engine" desc="Where Gatehouse reads what the engine noticed.">
        <p className="small">{data.engine?.text}</p>
        <Field label="Log file" hint={`Leave empty to use ${data.default_eve}.`}><Input value={eve} onChange={setEve} className="in mono" /></Field>
        <div className="actions"><Button disabled={busy} onClick={() => run(() => put('/api/threat/settings', { mode: data.mode, eve_path: eve }), 'Saved.').then(reload)}>Save</Button></div>
      </Card>
    </div>
  );
}

export default function Threats() {
  const { data, reload } = useData('/api/threat', { every: 15000 });
  const [tab, setTab] = useState('alerts');
  const [run, busy] = useAction();
  if (!data) return null;
  return (
    <Card title="Threat protection" desc="Watches for infected devices, break-in attempts and probing, and tells you in plain words.">
      <div className="stack">
        <Segmented value={data.mode} onChange={(v) => run(() => put('/api/threat/settings', { mode: v, eve_path: data.eve_path === data.default_eve ? '' : data.eve_path })).then(reload)} options={[{ value: 'off', label: 'Off' }, { value: 'watch', label: 'Watch and tell' }, { value: 'protect', label: 'Protect (not yet)', disabled: true }]} disabled={busy} />
        <Note tone={data.engine?.state === 'missing' ? 'warn' : undefined}>{data.engine?.text}</Note>
        <p className="small muted">{data.blocking} {data.fail_open}</p>
        <Tabs value={tab} onChange={setTab} tabs={[{ value: 'alerts', label: 'Alerts' }, { value: 'advanced', label: 'Advanced' }]} />
        {tab === 'alerts' ? <Alerts data={data} reload={reload} /> : <Advanced data={data} reload={reload} />}
      </div>
    </Card>
  );
}

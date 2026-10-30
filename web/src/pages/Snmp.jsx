import React, { useEffect, useState } from 'react';
import { put, useData } from '../api.js';
import { Button, Card, Field, Input, Note, ToggleRow, confirmBox, useAction } from '../ui.jsx';

// Let a monitoring tool read this device's status. Read only; off by default.
export default function Snmp() {
  const { data, reload } = useData('/api/snmp', { every: 10000 });
  const [f, setF] = useState(null);
  const [run, busy] = useAction();
  useEffect(() => { if (data && !f) setF(data.config); }, [data, f]);
  if (!f) return null;
  const set = (k) => (v) => setF((x) => ({ ...x, [k]: v }));
  const save = (extra) => run(async () => { await put('/api/snmp', { on: f.on, contact: f.contact || '', location: f.location || '', ...extra }); setF(null); reload(); }, 'Saved.');
  return (
    <Card title="Let a monitoring tool read this device (SNMP)" desc="Read only. Shows the name, uptime, and each port's speed and traffic. Nothing can be changed this way. Off by default.">
      <div className="stack">
        <ToggleRow title="Allow SNMP" checked={f.on} onChange={set('on')} />
        {f.on && (
          <>
            {data.config.community && (
              <Field label="Community (the password for your monitoring tool)" hint="Copy this into your monitoring tool, with version 2c. It was made up for you.">
                <Input value={data.config.community} onChange={() => {}} readOnly className="in mono" />
              </Field>
            )}
            <div className="formgrid">
              <Field label="Contact (optional)"><Input value={f.contact || ''} onChange={set('contact')} placeholder="Your name" /></Field>
              <Field label="Location (optional)"><Input value={f.location || ''} onChange={set('location')} placeholder="Closet" /></Field>
            </div>
            <span className="small muted">{data.error ? `Problem: ${data.error}` : data.listening ? 'Answering on the networks that may manage this device, and only those.' : 'Not answering yet. Choose Save.'}</span>
          </>
        )}
        <div className="row">
          <Button kind="primary" disabled={busy} onClick={() => save({})}>Save</Button>
          {data.config.community && <Button disabled={busy} onClick={async () => { if (await confirmBox('Make a new community? Your monitoring tool will stop working until you enter the new one.')) save({ new_community: true }); }}>Make a new community</Button>}
        </div>
        <Note>SNMP version 2c is not encrypted, so only use it on a network you trust. Only the networks that may manage this device can reach it.</Note>
      </div>
    </Card>
  );
}

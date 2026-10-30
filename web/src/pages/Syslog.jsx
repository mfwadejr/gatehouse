import React, { useEffect, useState } from 'react';
import { put, useData } from '../api.js';
import { Button, Card, Field, Input, NumberInput, Note, Select, ToggleRow, useAction } from '../ui.jsx';

// Send logs to a syslog server on your network. Off by default.
export default function Syslog() {
  const { data, reload } = useData('/api/syslog', { every: 10000 });
  const [f, setF] = useState(null);
  const [run, busy] = useAction();
  useEffect(() => { if (data && !f) setF(data.config); }, [data, f]);
  if (!f) return null;
  const set = (k) => (v) => setF((x) => ({ ...x, [k]: v }));
  const save = () => run(async () => { await put('/api/syslog', { ...f, port: Number(f.port) }); reload(); }, 'Saved.');
  const st = data.status;
  return (
    <Card title="Send logs elsewhere" desc="Copies Gatehouse's events to a syslog server you run, for example on a NAS or in a log tool. Off by default.">
      <div className="stack">
        <ToggleRow title="Send logs" checked={f.on} onChange={set('on')} />
        {f.on && (
          <>
            <div className="formgrid">
              <Field label="Server address"><Input value={f.host} onChange={set('host')} placeholder="192.168.1.20" className="in mono" /></Field>
              <Field label="Port"><NumberInput value={f.port} onChange={set('port')} /></Field>
              <Field label="Protocol"><Select value={f.proto} onChange={set('proto')} options={[{ value: 'udp', label: 'UDP (the usual)' }, { value: 'tcp', label: 'TCP' }]} /></Field>
            </div>
            <ToggleRow title="Include every firewall decision" desc="Can be a lot of messages. Leave off unless you need it." checked={f.firewall} onChange={set('firewall')} />
            <span className="small muted">{st.error ? `Problem: ${st.error}` : `${st.sent} sent${st.dropped ? `, ${st.dropped} dropped` : ''}.`} If the server is down, messages are dropped; the router is never slowed down.</span>
          </>
        )}
        <div><Button kind="primary" disabled={busy} onClick={save}>Save</Button></div>
        <Note>Flow export (NetFlow) and SNMP are not part of this yet.</Note>
      </div>
    </Card>
  );
}

import React, { useEffect, useState } from 'react';
import { put, useData } from '../api.js';
import { Button, Card, Field, Input, NumberInput, Note, Select, ToggleRow, useAction } from '../ui.jsx';

// Send a record of every connection to a traffic-analysis tool on your network. Off by default.
export default function FlowExport() {
  const { data, reload } = useData('/api/flows', { every: 10000 });
  const [f, setF] = useState(null);
  const [run, busy] = useAction();
  useEffect(() => { if (data && !f) setF(data.config); }, [data, f]);
  if (!f) return null;
  const set = (k) => (v) => setF((x) => ({ ...x, [k]: v }));
  const pick = (v) => setF((x) => ({ ...x, format: v, port: x.port === 4739 || x.port === 2055 ? (v === 'ipfix' ? 4739 : 2055) : x.port }));
  const save = () => run(async () => { await put('/api/flows', { ...f, port: Number(f.port) }); reload(); }, 'Saved.');
  const st = data.status;
  return (
    <Card title="Send traffic records (NetFlow / IPFIX)" desc="Tells a traffic-analysis tool on your network which devices talked to which, and how much. Off by default.">
      <div className="stack">
        <ToggleRow title="Send traffic records" checked={f.on} onChange={set('on')} />
        {f.on && (
          <>
            <div className="formgrid">
              <Field label="Collector address"><Input value={f.host} onChange={set('host')} placeholder="192.168.1.20" className="in mono" /></Field>
              <Field label="Port"><NumberInput value={f.port} onChange={set('port')} /></Field>
              <Field label="Format"><Select value={f.format} onChange={pick} options={[{ value: 'ipfix', label: 'IPFIX (the newer standard)' }, { value: 'netflow9', label: 'NetFlow version 9' }]} /></Field>
            </div>
            <span className="small muted">{st.error ? `Problem: ${st.error}` : `${st.records} records sent in ${st.packets} packets; following ${st.open} connections.`} Records go out every 30 seconds. If the collector is down they are dropped; the router is never slowed down.</span>
          </>
        )}
        <div><Button kind="primary" disabled={busy} onClick={save}>Save</Button></div>
        <Note>Only IPv4 connections are reported. Counting the traffic on each connection is switched on in the system while this is on.</Note>
      </div>
    </Card>
  );
}

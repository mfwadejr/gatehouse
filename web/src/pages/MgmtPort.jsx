import React, { useEffect, useState } from 'react';
import { post, put, useData } from '../api.js';
import { Button, Card, Field, Input, Note, Pill, Segmented, Select, ToggleRow, useAction } from '../ui.jsx';

// A spare port that carries only a management address. Nothing is routed through it. When asked,
// the console is closed on the LAN while this port is healthy, and opens there again if it is not.

const TONE = { ok: 'good', waiting: 'warn', quiet: 'warn', down: 'bad', off: '' };

export default function MgmtPort() {
  const { data, reload } = useData('/api/mgmt/port', { topics: ['fw.status'], every: 5000 });
  const [run, busy] = useAction();
  const [f, setF] = useState(null);
  const [secs, setSecs] = useState(0);
  useEffect(() => { if (data && !f) setF({ ...data.config, mode: data.config.mode || 'dhcp' }); }, [data, f]);
  useEffect(() => { setSecs(data?.pending?.seconds ?? 0); }, [data?.pending?.until, data?.pending?.seconds]);
  useEffect(() => {
    if (!data?.pending) return undefined;
    const t = setInterval(() => setSecs((s) => Math.max(0, s - 1)), 1000);
    return () => clearInterval(t);
  }, [data?.pending?.until]);
  if (!data || !f) return null;
  const set = (k) => (v) => setF((x) => ({ ...x, [k]: v }));
  const cfg = data.config;
  const choices = data.choices || [];
  const save = (body, msg) => run(() => put('/api/mgmt/port', body), msg).then((r) => { if (r) { setF({ ...r.config, mode: r.config.mode || 'dhcp' }); reload(); } });
  const dirty = f.iface !== cfg.iface || (f.iface && (f.mode !== (cfg.mode || 'dhcp') || (f.mode === 'static' && (f.cidr !== cfg.cidr || (f.gateway || '') !== (cfg.gateway || '')))));
  return (
    <Card title="Management port" desc="Use one spare port only for managing this device. The console and remote login answer there, and nothing is routed through it.">
      <div className="stack">
        {data.bridge && <Note tone="warn" title="Not used in bridge mode">Bridge mode has its own out-of-band port. A separate management port is not used there.</Note>}
        <div className="inline">
          <Pill tone={TONE[data.state] || ''}>{data.state === 'off' ? 'Not set' : data.state === 'ok' ? 'Healthy' : data.state === 'waiting' ? 'Waiting' : data.state === 'down' ? 'No link' : 'Quiet'}</Pill>
          <span className="small">{data.status}</span>
        </div>
        {data.pending && (
          <Note tone="warn" title={`Open the console on ${data.address || 'the new address'} within ${secs} seconds`}>
            This change is kept only when the console is reached through the management port. Otherwise it is undone on its own.
            <div className="actions mt2"><Button small disabled={busy} onClick={() => run(() => post('/api/mgmt/port/revert', {}), 'Undone.').then(reload)}>Undo now</Button></div>
          </Note>
        )}
        {data.needs_apply && !data.pending && (
          <Note title="One more step">The firewall rules for this port go live with your next firewall apply. Review and apply the firewall to finish.</Note>
        )}
        <Field label="Port">
          <Select value={f.iface} onChange={set('iface')} aria-label="Management port"
            options={[{ value: '', label: 'None' }, ...choices.map((c) => ({ value: c, label: c }))]} />
        </Field>
        {f.iface && (
          <>
            <Field label="Address">
              <Segmented value={f.mode} onChange={set('mode')} options={[{ value: 'dhcp', label: 'From DHCP' }, { value: 'static', label: 'Fixed address' }]} />
            </Field>
            {f.mode === 'dhcp' && <p className="small muted">{data.dhcp_note}</p>}
            {f.mode === 'static' && (
              <>
                <Field label="Address and prefix" hint="Use a range that no network on this device already uses, like 10.99.0.5/24."><Input value={f.cidr || ''} onChange={set('cidr')} placeholder="10.99.0.5/24" className="in mono" /></Field>
                <Field label="Gateway (optional)" hint="Only used to answer people on other subnets. It never becomes this device's route to the internet."><Input value={f.gateway || ''} onChange={set('gateway')} placeholder="10.99.0.1" className="in mono" /></Field>
              </>
            )}
          </>
        )}
        <div className="actions">
          <Button kind="primary" disabled={busy || !dirty || !!data.pending || data.bridge} onClick={() => save({ ...f, close_lan: f.iface ? cfg.close_lan : false }, f.iface ? 'Saved. Open the console on the new address to keep it.' : 'Management port removed.')}>{f.iface ? 'Save and test' : 'Remove'}</Button>
        </div>
        {cfg.iface && (
          <ToggleRow title="Close the console on the LAN while this port is healthy"
            desc="Only open this switch from the management address. The LAN console opens again by itself if this port stops answering for a minute."
            checked={!!cfg.close_lan} disabled={busy || !!data.pending}
            onChange={(v) => save({ ...cfg, close_lan: v }, v ? 'The console now closes on the LAN while this port is healthy.' : 'The console stays open on the LAN.')} />
        )}
        {cfg.close_lan && <p className="small muted">{data.lan_open ? 'The console is open on the LAN right now (the port has not been healthy for half a minute).' : 'The console is closed on the LAN right now.'}</p>}
      </div>
    </Card>
  );
}

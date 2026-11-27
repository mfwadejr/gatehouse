import React, { useState } from 'react';
import { del, post, put, useData } from '../api.js';
import { Button, Card, Empty, Field, Input, Note, Pill, Segmented, useAction } from '../ui.jsx';

// "Block as it happens": the settings that only matter while it is on. Every change here is applied
// at once; none of them can cut the internet, because the checks fail open.

const LEVELS = [{ value: 'off', label: 'Off' }, { value: 'alert', label: 'Alert' }, { value: 'block', label: 'Block' }];
const TONE = { keeping_up: 'good', falling_behind: 'warn', stopped: 'bad', starting: '' };

function Category({ c, busy, set }) {
  return (
    <div className="item" style={{ alignItems: 'center' }}>
      <div className="fill">
        <div className="name">{c.text}</div>
        <div className="small muted">{c.name.replace(/_/g, ' ')}{c.default === 'block' ? ' · blocks to start with' : ''}</div>
      </div>
      <Segmented value={c.level} onChange={(v) => set(c.name, v)} options={LEVELS} disabled={busy} />
    </div>
  );
}

export default function ThreatInline({ reload }) {
  const { data, reload: again } = useData('/api/threat/inline', { every: 15000 });
  const [run, busy] = useAction();
  const [addr, setAddr] = useState('');
  if (!data || !data.on) return null;
  const done = () => { again(); reload(); };
  const set = (group, level) => run(() => put('/api/threat/inline/category', { group, level }), 'Saved. The rules are being rebuilt.').then(done);
  const cats = data.categories || [];
  const common = cats.filter((c) => c.common);
  const rest = cats.filter((c) => !c.common);
  const checked = (data.networks || []).filter((n) => n.on).map((n) => n.iface);
  const toggle = (iface) => {
    const next = checked.includes(iface) ? checked.filter((x) => x !== iface) : [...checked, iface];
    if (!next.length) return;
    run(() => put('/api/threat/inline/networks', { ifaces: next }), 'Saved.').then(done);
  };
  const changes = (data.alert_only || []).length + (data.never || []).length;
  return (
    <>
      <Card title="Block as it happens" desc="Traffic the firewall lets through is checked on its way. A packet that matches a rule set to Block is dropped before it arrives."
        actions={<Button small disabled={busy} onClick={() => run(() => put('/api/threat/settings', { mode: 'watch' }), 'Back to Watch only.').then(done)}>Back to Watch only</Button>}>
        <div className="stack">
          <Note tone={TONE[data.state] || ''} title={data.state === 'keeping_up' ? 'Keeping up' : data.state === 'falling_behind' ? 'Falling behind' : data.state === 'stopped' ? 'Not checking' : 'Starting'}>{data.state_text}</Note>
          {(data.size_warnings || []).map((w) => <Note key={w} tone="warn">{w}</Note>)}
          <div className="small muted">
            {(data.notes || []).map((n) => <p key={n}>{n}</p>)}
          </div>
          <div className="stack gap1">
            <b>Networks to check</b>
            <p className="small muted">Traffic between a checked network and the internet, in both directions. Leave out a network whose traffic you trust.</p>
            {(data.networks || []).map((n) => (
              <label key={n.iface} className="inline small" style={{ cursor: 'pointer' }}>
                <input type="checkbox" checked={n.on} disabled={busy} onChange={() => toggle(n.iface)} /> {n.name}
              </label>
            ))}
          </div>
        </div>
      </Card>
      <Card title="What each group of checks does" desc="Off: not looked for. Alert: noted in the alerts, nothing is stopped. Block: matching traffic is dropped. Turn blocking on one group at a time and watch the alerts for a few days before adding the next.">
        <div className="list">{common.map((c) => <Category key={c.name} c={c} busy={busy} set={set} />)}</div>
        <details className="more">
          <summary>Less common groups</summary>
          <div className="list">{rest.map((c) => <Category key={c.name} c={c} busy={busy} set={set} />)}</div>
        </details>
        <p className="small muted">Lists of bad addresses (such as CINS or DShield) are never used to block: they are too noisy. Groups about policy, scans and peer-to-peer start on Alert. Changing a group rebuilds the rules, which takes a minute.</p>
      </Card>
      <Card title="Changes you made" desc="Single checks you stopped blocking, and devices or addresses that are never checked. Undo puts them back.">
        {!changes && <Empty>None. On an alert, press Stop blocking this, or Never check this device.</Empty>}
        <div className="list">
          {(data.alert_only || []).map((r) => (
            <div className="item" key={r.sid}><div className="fill"><div className="name">{r.msg}</div><div className="small muted">Only alerts now. Rule {r.sid}.</div></div>
              <Button small disabled={busy} onClick={() => run(() => del(`/api/threat/inline/stop-blocking/${r.sid}`), 'Undone.').then(done)}>Undo</Button></div>
          ))}
          {(data.never || []).map((a) => (
            <div className="item" key={a}><div className="fill"><div className="name">{a}</div><div className="small muted">Never checked.</div></div>
              <Button small disabled={busy} onClick={() => run(() => del(`/api/threat/inline/never?addr=${encodeURIComponent(a)}`), 'Undone.').then(done)}>Undo</Button></div>
          ))}
        </div>
        <div className="inline" style={{ alignItems: 'flex-end' }}>
          <Field label="Never check this address or network" hint="For example 192.168.1.50 or 192.168.1.0/24."><Input value={addr} onChange={setAddr} placeholder="192.168.1.50" /></Field>
          <Button disabled={busy || !addr.trim()} onClick={() => run(() => post('/api/threat/inline/never', { addr }), 'Saved.').then(() => { setAddr(''); done(); })}>Add</Button>
        </div>
      </Card>
      <Card title="Try it" desc="A harmless check that proves blocking works.">
        <p className="small">From a device on a checked network, open <span className="mono">{data.test_url}</span> in a browser. The page should not load. Then look at the Alerts tab: a "NopeWarden inline test" alert marked Blocked appears within a minute. The test does nothing else, and the address is made up.</p>
      </Card>
    </>
  );
}

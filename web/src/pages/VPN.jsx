import React, { useMemo, useState } from 'react';
import { del, post, put, useData } from '../api.js';
import { Button, Card, Empty, Field, Input, Modal, Note, Page, Pill, Segmented, ToggleRow, useAction, fmt } from '../ui.jsx';

const STATE = {
  up: { tone: 'good', label: 'Working' },
  connecting: { tone: 'warn', label: 'Connecting' },
  degraded: { tone: 'warn', label: 'Not passing traffic' },
  down: { tone: 'bad', label: 'Down' },
  off: { tone: '', label: 'Off' },
};

const bytes = (n) => {
  if (!n) return '0 B';
  const u = ['B', 'KB', 'MB', 'GB', 'TB'];
  let i = 0; let v = n;
  while (v >= 1024 && i < u.length - 1) { v /= 1024; i += 1; }
  return `${v >= 10 || i === 0 ? Math.round(v) : v.toFixed(1)} ${u[i]}`;
};

function Fact({ label, children }) {
  return (
    <div className="stack" style={{ gap: 'var(--space-0-5)' }}>
      <span className="small muted">{label}</span>
      <span className="mono">{children}</span>
    </div>
  );
}

function download(name, text) {
  const url = URL.createObjectURL(new Blob([text], { type: 'text/plain' }));
  const a = document.createElement('a');
  a.href = url; a.download = name; a.click();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}

// ---------------------------------------------------------------- add a VPN

function AddVPN({ endpoint, onClose, onDone }) {
  const [run, busy] = useAction();
  const [goal, setGoal] = useState('');          // out | in
  const [tech, setTech] = useState('wireguard');
  const [f, setF] = useState({ label: '', endpoint: endpoint || '', access: 'full', conf: '', username: '', password: '', check: '', killswitch: true });
  const set = (k) => (v) => setF((x) => ({ ...x, [k]: v }));

  const save = async () => {
    let path; let body;
    if (goal === 'in') {
      path = `/api/vpn/${tech}/server`;
      body = { label: f.label, endpoint: f.endpoint, access: f.access };
    } else if (tech === 'wireguard') {
      path = '/api/vpn/wireguard/client';
      body = { label: f.label, conf: f.conf, check: f.check, killswitch: f.killswitch };
    } else {
      path = '/api/vpn/openvpn/client';
      body = { label: f.label, profile: f.conf, username: f.username, password: f.password, check: f.check, killswitch: f.killswitch };
    }
    const r = await run(() => post(path, body), 'VPN added.');
    if (r) { onDone(r.id, goal, tech); onClose(); }
  };

  if (!goal) {
    return (
      <Modal title="Add a VPN" onClose={onClose} footer={<Button onClick={onClose}>Cancel</Button>}>
        <div className="stack">
          <button className="preset" onClick={() => setGoal('out')}>
            <span className="t">Send some of my devices through a VPN provider</span>
            <span className="d">Use Mullvad, Proton, NordVPN or any other provider. You choose which networks or devices use it; everything else keeps using your normal internet.</span>
          </button>
          <button className="preset" onClick={() => setGoal('in')}>
            <span className="t">Connect to my home network from outside</span>
            <span className="d">Reach your devices and use your internet from your phone or laptop when you are away.</span>
          </button>
        </div>
      </Modal>
    );
  }

  const server = goal === 'in';
  return (
    <Modal title={server ? 'Connect from outside' : 'Use a VPN provider'} onClose={onClose}
      footer={<><Button onClick={() => setGoal('')}>Back</Button><Button kind="primary" disabled={busy} onClick={save}>{server ? 'Create' : 'Add and connect'}</Button></>}>
      <div className="stack">
        <Field label="Type" hint={server ? 'WireGuard is faster and simpler. Pick OpenVPN only if a device needs it.' : 'Your provider gives you a WireGuard or OpenVPN file to download.'}>
          <Segmented value={tech} onChange={setTech} options={[{ value: 'wireguard', label: 'WireGuard' }, { value: 'openvpn', label: 'OpenVPN' }]} />
        </Field>
        <Field label="Name" hint="Anything that helps you recognise it."><Input value={f.label} onChange={set('label')} placeholder={server ? 'Remote access' : 'Mullvad'} /></Field>
        {server ? (
          <>
            <Field label="Address devices connect to" hint="Your public IP address or a name like home.example.com. You only enter this once.">
              <Input value={f.endpoint} onChange={set('endpoint')} placeholder="home.example.com" />
            </Field>
            <Field label="What can they reach?">
              <Segmented value={f.access} onChange={set('access')} options={[{ value: 'full', label: 'My whole network' }, { value: 'internet', label: 'Internet only' }]} />
            </Field>
          </>
        ) : (
          <>
            <Field label={tech === 'wireguard' ? 'WireGuard file' : 'OpenVPN file (.ovpn)'} hint="Open the file from your provider in any text editor, copy everything, and paste it here.">
              <textarea className="in" rows={7} value={f.conf} onChange={(e) => set('conf')(e.target.value)} spellCheck={false} placeholder={tech === 'wireguard' ? '[Interface]\nPrivateKey = …' : 'client\nremote …'} />
            </Field>
            {tech === 'openvpn' && (
              <div className="formgrid">
                <Field label="Username" hint="Only if your provider asks for one."><Input value={f.username} onChange={set('username')} autoComplete="off" /></Field>
                <Field label="Password"><Input type="password" value={f.password} onChange={set('password')} autoComplete="new-password" /></Field>
              </div>
            )}
            <Field label="Test address" hint="Gatehouse checks the VPN is really working by reaching this address through it. Leave empty for 1.1.1.1."><Input value={f.check} onChange={set('check')} placeholder="1.1.1.1" /></Field>
            <ToggleRow title="Kill switch" desc="If the VPN drops, the devices using it lose internet instead of falling back to your normal connection." checked={f.killswitch} onChange={set('killswitch')} />
          </>
        )}
      </div>
    </Modal>
  );
}

// ---------------------------------------------------------------- who uses a tunnel

function Uses({ v, onClose, onSaved }) {
  const { data: ch } = useData('/api/vpn/choices');
  const [run, busy] = useAction();
  const [sel, setSel] = useState(() => new Set((v.uses || []).map((u) => `${u.kind}|${u.value}`)));
  const [q, setQ] = useState('');
  const [range, setRange] = useState('');
  const [extra, setExtra] = useState(() => (v.uses || []).filter((u) => u.kind === 'cidr').map((u) => u.value));
  const toggle = (k) => setSel((s) => { const n = new Set(s); if (n.has(k)) n.delete(k); else n.add(k); return n; });
  const devs = useMemo(() => (ch?.devices || []).filter((d) => !q || `${d.name} ${d.ip} ${d.mac}`.toLowerCase().includes(q.toLowerCase())), [ch, q]);
  const addRange = () => {
    const r = range.trim();
    if (!r) return;
    setExtra((x) => (x.includes(r) ? x : [...x, r]));
    setSel((s) => new Set(s).add(`cidr|${r}`));
    setRange('');
  };
  const save = async () => {
    const uses = [...sel].map((k) => {
      const [kind, ...rest] = k.split('|');
      const value = rest.join('|');
      const label = kind === 'device' ? (ch?.devices || []).find((d) => d.mac === value)?.name : '';
      return { kind, value, label: label || '' };
    });
    const r = await run(() => put(`/api/vpn/${v.kind}/${v.id}/uses`, { uses }), 'Saved.');
    if (r) {
      (r.warnings || []).forEach((w) => window.alert(w));
      onSaved(); onClose();
    }
  };
  return (
    <Modal title={`Who uses ${v.label}`} onClose={onClose} footer={<><Button onClick={onClose}>Cancel</Button><Button kind="primary" disabled={busy} onClick={save}>Save</Button></>}>
      <div className="stack">
        <p className="sec">Tick the networks and devices whose internet should go through this VPN. Everything you leave unticked keeps using your normal connection.</p>
        <Field label="Whole networks">
          <div className="stack" style={{ gap: 'var(--space-1)' }}>
            {(ch?.networks || []).map((n) => (
              <label key={n.name} className="inline"><input type="checkbox" checked={sel.has(`network|${n.name}`)} onChange={() => toggle(`network|${n.name}`)} /> <span className="name">{n.name}</span> <span className="small muted mono">{n.subnets.join(', ')}</span></label>
            ))}
            {ch && !ch.networks.length && <span className="muted small">No networks yet.</span>}
          </div>
        </Field>
        <Field label="Individual devices" hint="Gatehouse keeps the device's address fixed so it stays in the VPN.">
          <Input value={q} onChange={setQ} placeholder="Search devices" aria-label="Search devices" />
          <div className="stack" style={{ gap: 'var(--space-1)', maxHeight: '14rem', overflow: 'auto', marginTop: 'var(--space-2)' }}>
            {devs.map((d) => (
              <label key={d.mac} className="inline"><input type="checkbox" checked={sel.has(`device|${d.mac}`)} onChange={() => toggle(`device|${d.mac}`)} /> <span className={`dot ${d.online ? '' : 'off'}`} /> <span className="name">{d.name}</span> <span className="small muted mono">{d.ip || 'not seen yet'}</span></label>
            ))}
            {ch && !devs.length && <span className="muted small">No matching devices.</span>}
          </div>
        </Field>
        <Field label="Address or range" hint="For anything else, like 192.168.1.50 or 192.168.20.0/24.">
          <div className="inline">
            <Input value={range} onChange={setRange} placeholder="192.168.1.50" onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); addRange(); } }} />
            <Button onClick={addRange}>Add</Button>
          </div>
          {extra.map((r) => (
            <label key={r} className="inline"><input type="checkbox" checked={sel.has(`cidr|${r}`)} onChange={() => toggle(`cidr|${r}`)} /> <span className="mono">{r}</span></label>
          ))}
        </Field>
      </div>
    </Modal>
  );
}

// ---------------------------------------------------------------- settings

function Settings({ v, onClose, onSaved }) {
  const [run, busy] = useAction();
  const client = v.role === 'client';
  const [f, setF] = useState({ label: v.label, check: v.check_target || '', killswitch: v.killswitch, endpoint: v.endpoint || '' });
  const save = async () => {
    const body = { label: f.label };
    if (client) { body.check = f.check; body.killswitch = f.killswitch; } else body.endpoint = f.endpoint;
    if (await run(() => put(`/api/vpn/${v.kind}/${v.id}`, body), 'Saved.')) { onSaved(); onClose(); }
  };
  return (
    <Modal title={`Settings for ${v.label}`} onClose={onClose} footer={<><Button onClick={onClose}>Cancel</Button><Button kind="primary" disabled={busy} onClick={save}>Save</Button></>}>
      <div className="stack">
        <Field label="Name"><Input value={f.label} onChange={(x) => setF({ ...f, label: x })} /></Field>
        {client ? (
          <>
            <Field label="Test address" hint="Reached through the VPN every 20 seconds to prove it works. If it stops answering you are alerted."><Input value={f.check} onChange={(x) => setF({ ...f, check: x })} placeholder="1.1.1.1" /></Field>
            <ToggleRow title="Kill switch" desc="If the VPN drops, the devices using it lose internet instead of falling back to your normal connection." checked={f.killswitch} onChange={(x) => setF({ ...f, killswitch: x })} />
          </>
        ) : (
          <Field label="Address devices connect to" hint="Your public IP address or a name like home.example.com. Existing device files keep the old address."><Input value={f.endpoint} onChange={(x) => setF({ ...f, endpoint: x })} placeholder="home.example.com" /></Field>
        )}
      </div>
    </Modal>
  );
}

// ---------------------------------------------------------------- add a device to a server

function AddDevice({ v, endpoint, onClose, onSaved }) {
  const [run, busy] = useAction();
  const [name, setName] = useState('');
  const [res, setRes] = useState(null);
  const [copied, setCopied] = useState(false);
  const [host, setHost] = useState(v.endpoint || endpoint || '');
  const needHost = !v.endpoint && !endpoint;
  const create = async () => {
    if (needHost) {
      if (!(await run(() => put(`/api/vpn/${v.kind}/${v.id}`, { endpoint: host })))) return;
    }
    const r = await run(() => post(`/api/vpn/${v.kind}/${v.id}/peers`, { name }));
    if (r) { setRes(r); onSaved(); }
  };
  if (res) {
    return (
      <Modal title={`${res.name} is ready`} onClose={onClose} footer={<Button kind="primary" onClick={onClose}>Done</Button>}>
        <div className="stack">
          <Note tone="warn" title="Shown only once">This contains the device's secret key. Import it now; Gatehouse cannot show it again.</Note>
          {res.qr && (
            <div className="stack" style={{ alignItems: 'center' }}>
              <img src={res.qr} alt="QR code for the WireGuard app" width="240" height="240" style={{ background: '#fff', padding: 8, borderRadius: 8 }} />
              <span className="small muted">On a phone: open the WireGuard app, tap +, then Scan from QR code.</span>
            </div>
          )}
          <div className="actions">
            <Button icon="plus" onClick={() => download(res.filename, res.config)}>Download {res.filename}</Button>
            <Button onClick={() => { navigator.clipboard?.writeText(res.config); setCopied(true); }}>{copied ? 'Copied' : 'Copy'}</Button>
          </div>
          {!res.qr && <span className="small muted">On a computer or phone: open the downloaded file with the OpenVPN app.</span>}
        </div>
      </Modal>
    );
  }
  return (
    <Modal title="Add a device" onClose={onClose} footer={<><Button onClick={onClose}>Cancel</Button><Button kind="primary" disabled={busy || !name.trim()} onClick={create}>Create</Button></>}>
      <div className="stack">
        <Field label="Device name" hint="Like Marvin's iPhone."><Input value={name} onChange={setName} onKeyDown={(e) => { if (e.key === 'Enter' && name.trim()) create(); }} /></Field>
        {needHost && (
          <Field label="Address devices connect to" hint="Your public IP address or a name like home.example.com. You only enter this once.">
            <Input value={host} onChange={setHost} placeholder="home.example.com" />
          </Field>
        )}
      </div>
    </Modal>
  );
}

// ---------------------------------------------------------------- cards

function Header({ v }) {
  const s = STATE[v.state] || STATE.down;
  return (
    <div className="spread">
      <div className="inline"><span className="name" style={{ fontSize: 'var(--font-size-lg)' }}>{v.label}</span><span className="small muted">{v.kind === 'wireguard' ? 'WireGuard' : 'OpenVPN'} · {v.dev}</span></div>
      <Pill tone={s.tone}>{s.label}</Pill>
    </div>
  );
}

function Outbound({ v, reload, setModal }) {
  const [run] = useAction();
  const sources = v.uses || [];
  return (
    <Card>
      <div className="stack">
        <Header v={v} />
        <p className={v.state === 'up' ? 'sec' : ''} style={v.state === 'down' || v.state === 'degraded' ? { color: 'var(--color-status-serious)' } : undefined}>
          {v.message}{v.state === 'up' && v.rtt_ms ? ` · ${v.rtt_ms} ms` : ''}
        </p>
        <div className="grid">
          <Fact label="Internet sees you as">{v.exit_ip || '–'}</Fact>
          <Fact label="Address inside the VPN">{v.address || '–'}</Fact>
          <Fact label="Provider">{v.endpoint || '–'}</Fact>
          {v.kind === 'wireguard' && <Fact label="Last contact">{v.handshake ? fmt.ago(v.handshake) : 'never'}</Fact>}
          {v.kind === 'wireguard' && <Fact label="Traffic">{`↓ ${bytes(v.rx)}  ↑ ${bytes(v.tx)}`}</Fact>}
          <Fact label="Checked">{v.checked_at ? `${fmt.ago(v.checked_at)} via ${v.check_target}` : 'waiting'}</Fact>
        </div>
        <div className="stack" style={{ gap: 'var(--space-1)' }}>
          <span className="small muted">Used by</span>
          {sources.length === 0 ? <span className="muted">Nothing yet. Choose which networks or devices should use this VPN.</span> : (
            <div className="inline">{sources.map((u) => <Pill key={u.id} tone="">{u.kind === 'network' ? `${u.label} network` : u.label}</Pill>)}</div>
          )}
          {v.killswitch && sources.length > 0 && v.state !== 'up' && v.state !== 'off' && <span className="small muted">Kill switch is on: these devices have no internet until the VPN is back.</span>}
        </div>
        <div className="actions">
          <Button kind="primary" icon="edit" onClick={() => setModal({ t: 'uses', v })}>Choose who uses it</Button>
          <Button onClick={() => run(() => put(`/api/vpn/${v.kind}/${v.id}`, { enabled: !v.enabled }), v.enabled ? 'Turned off.' : 'Turned on.').then(reload)}>{v.enabled ? 'Turn off' : 'Turn on'}</Button>
          <Button onClick={() => setModal({ t: 'settings', v })}>Settings</Button>
          <Button kind="danger" icon="trash" onClick={() => window.confirm(`Remove ${v.label}? Devices using it go back to your normal connection.`) && run(() => del(`/api/vpn/${v.kind}/${v.id}`), 'Removed.').then(reload)}>Remove</Button>
        </div>
      </div>
    </Card>
  );
}

const PEER = { connected: { tone: 'good', label: 'Connected' }, idle: { tone: '', label: 'Idle' }, never: { tone: '', label: 'Not connected yet' } };

function Inbound({ v, reload, setModal }) {
  const [run] = useAction();
  return (
    <Card>
      <div className="stack">
        <Header v={v} />
        <p className="sec">{v.message}</p>
        <div className="grid">
          <Fact label="Devices connect to">{v.endpoint ? `${v.endpoint}:${v.port}` : 'not set'}</Fact>
          <Fact label="VPN network">{v.subnet}</Fact>
          <Fact label="They can reach">{v.access === 'full' ? 'Whole network' : 'Internet only'}</Fact>
        </div>
        <div className="list">
          {v.peers.map((p) => {
            const s = PEER[p.state] || PEER.never;
            return (
              <div className="item" key={p.id}>
                <div><div className="name">{p.name}</div><div className="meta">{p.address || ''}{p.real ? ` · from ${p.real}` : ''}{p.state === 'connected' ? ` · ↓ ${bytes(p.rx)} ↑ ${bytes(p.tx)}` : p.last_seen ? ` · last seen ${fmt.ago(p.last_seen)}` : ''}</div></div>
                <div className="inline"><Pill tone={s.tone}>{s.label}</Pill>
                  <Button small kind="danger" icon="trash" onClick={() => window.confirm(`Remove ${p.name}? It will no longer be able to connect.`) && run(() => del(`/api/vpn/${v.kind}/${v.id}/peers/${p.id}`), 'Removed.').then(reload)}>Remove</Button></div>
              </div>
            );
          })}
          {!v.peers.length && <Empty>No devices yet. Add your phone or laptop.</Empty>}
        </div>
        <div className="actions">
          <Button kind="primary" icon="plus" onClick={() => setModal({ t: 'device', v })}>Add a device</Button>
          <Button onClick={() => run(() => put(`/api/vpn/${v.kind}/${v.id}`, { enabled: !v.enabled }), v.enabled ? 'Turned off.' : 'Turned on.').then(reload)}>{v.enabled ? 'Turn off' : 'Turn on'}</Button>
          <Button onClick={() => setModal({ t: 'settings', v })}>Settings</Button>
          <Button kind="danger" icon="trash" onClick={() => window.confirm(`Remove ${v.label}? Every device on it will be disconnected.`) && run(() => del(`/api/vpn/${v.kind}/${v.id}`), 'Removed.').then(reload)}>Remove</Button>
        </div>
      </div>
    </Card>
  );
}

export default function VPN() {
  const { data, reload } = useData('/api/vpn', { topics: ['vpn.status'], every: 15000 });
  const [modal, setModal] = useState(null);
  const vpns = data?.vpns || [];
  const out = vpns.filter((v) => v.role === 'client');
  const inn = vpns.filter((v) => v.role === 'server');
  const bad = out.filter((v) => v.enabled && (v.state === 'down' || v.state === 'degraded')).length;
  const sub = !vpns.length ? 'Reach home from anywhere, or send chosen devices through a VPN provider.'
    : bad ? `${bad} VPN${bad > 1 ? 's need' : ' needs'} attention` : 'Everything is working.';
  const close = () => setModal(null);
  const fresh = async () => { await reload(); };
  return (
    <Page title="VPN" sub={sub} actions={<Button kind="primary" icon="plus" onClick={() => setModal({ t: 'add' })}>Add a VPN</Button>}>
      {data && !data.helper && <Note tone="warn" title="The network helper is not running">VPN changes cannot be applied until it is.</Note>}
      {!vpns.length && data && (
        <Card>
          <Empty>
            <div className="stack" style={{ alignItems: 'center' }}>
              <span>No VPNs yet.</span>
              <Button kind="primary" icon="plus" onClick={() => setModal({ t: 'add' })}>Add a VPN</Button>
            </div>
          </Empty>
        </Card>
      )}
      {out.length > 0 && <h2 className="sec">Through a VPN provider</h2>}
      {out.map((v) => <Outbound key={v.dev} v={v} reload={fresh} setModal={setModal} />)}
      {inn.length > 0 && <h2 className="sec">Connect from outside</h2>}
      {inn.map((v) => <Inbound key={v.dev} v={v} reload={fresh} setModal={setModal} />)}

      {modal?.t === 'add' && <AddVPN endpoint={data?.endpoint} onClose={close} onDone={(id, goal, tech) => { reload(); if (goal === 'out') setTimeout(() => setModal({ t: 'uses-new', id, kind: tech }), 0); }} />}
      {modal?.t === 'uses-new' && <UsesNew id={modal.id} kind={modal.kind} onClose={close} onSaved={reload} />}
      {modal?.t === 'uses' && <Uses v={modal.v} onClose={close} onSaved={reload} />}
      {modal?.t === 'settings' && <Settings v={modal.v} onClose={close} onSaved={reload} />}
      {modal?.t === 'device' && <AddDevice v={modal.v} endpoint={data?.endpoint} onClose={close} onSaved={reload} />}
    </Page>
  );
}

// After adding a provider VPN, go straight to choosing who uses it.
function UsesNew({ id, kind, onClose, onSaved }) {
  const { data } = useData('/api/vpn?fresh=1');
  const v = (data?.vpns || []).find((x) => x.id === id && x.kind === kind && x.role === 'client');
  if (!v) return null;
  return <Uses v={v} onClose={onClose} onSaved={onSaved} />;
}

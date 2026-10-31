import React, { useMemo, useState } from 'react';
import { del, get, post, put, useData } from '../api.js';
import { Button, Card, Empty, Field, Input, Modal, Note, Page, Pill, Segmented, ToggleRow, useAction, fmt, Switch, confirmBox, noticeBox } from '../ui.jsx';

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

// A box that takes a dropped or chosen file and hands back its text. Pasting stays possible.
function FileDrop({ onText, accept, label }) {
  const [over, setOver] = useState(false);
  const [name, setName] = useState('');
  const [err, setErr] = useState('');
  const take = (file) => {
    if (!file) return;
    if (file.size > 200 * 1024) { setErr('That file is too large to be a VPN file.'); return; }
    const rd = new FileReader();
    rd.onload = () => { setErr(''); setName(file.name); onText(String(rd.result)); };
    rd.onerror = () => setErr('That file could not be read.');
    rd.readAsText(file);
  };
  return (
    <div className={`dropzone ${over ? 'over' : ''}`} onDragOver={(e) => { e.preventDefault(); setOver(true); }} onDragLeave={() => setOver(false)}
      onDrop={(e) => { e.preventDefault(); setOver(false); take(e.dataTransfer.files?.[0]); }}>
      <span>{name ? `Loaded ${name}` : label}</span>
      <label className="btn small">Choose a file<input type="file" accept={accept} hidden onChange={(e) => take(e.target.files?.[0])} /></label>
      {err && <span className="small" style={{ color: 'var(--color-status-serious)' }}>{err}</span>}
    </div>
  );
}

function OtherResult({ r, onClose }) {
  const [copied, setCopied] = useState(false);
  const text = [
    '[Interface]',
    '# Use the private key you made on your router. Gatehouse never sees it.',
    `Address = ${r.their_address}`,
    '',
    '[Peer]',
    `PublicKey = ${r.our_public_key}`,
    `PresharedKey = ${r.psk}`,
    r.our_endpoint ? `Endpoint = ${r.our_endpoint}` : '# Endpoint: not needed if this router has a fixed address you gave Gatehouse',
    `AllowedIPs = ${(r.their_allowed_ips || []).join(', ')}`,
    `PersistentKeepalive = ${r.keepalive}`,
  ].join('\n');
  return (
    <Modal title="Enter this on the other router" onClose={onClose} footer={<Button kind="primary" onClick={onClose}>Done</Button>}>
      <div className="stack">
        <Note tone="warn" title="Shown only once">The pre-shared key below protects the link. Copy it now.</Note>
        <p className="sec small">Add a peer on the other router with these details. The names differ between makers, but the values are the same. Gatehouse has set up its side, including the routes and firewall rules.</p>
        <textarea className="in mono" rows={11} readOnly value={text} onFocus={(e) => e.target.select()} />
        <div className="actions">
          <Button onClick={() => { navigator.clipboard?.writeText(text); setCopied(true); }}>{copied ? 'Copied' : 'Copy'}</Button>
          <Button onClick={() => download('gatehouse-peer.conf', text)}>Download</Button>
        </div>
      </div>
    </Modal>
  );
}

function DeviceResult({ res, onClose }) {
  const [copied, setCopied] = useState(false);
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

// ---------------------------------------------------------------- add a VPN

function AddVPN({ endpoint, onClose, onDone }) {
  const [run, busy] = useAction();
  const [goal, setGoal] = useState('');          // out | in | site
  const [tech, setTech] = useState('wireguard');
  const [f, setF] = useState({ label: '', endpoint: endpoint || '', route: 'all', reach: true, scope: 'all', conf: '', username: '', password: '', check: '', killswitch: true,
    remote_lan: '', code: '', siteMode: 'create', device: '', other_key: '', other_lans: '', other_ep: '', other_psk: '' });
  const [first, setFirst] = useState(null);   // the first device made with a new server
  const [other, setOther] = useState(null);    // the answer for a router from another maker
  const [made, setMade] = useState(null);       // site link: the code to take to the other site
  const [warn, setWarn] = useState('');
  const [copied, setCopied] = useState(false);
  const set = (k) => (v) => setF((x) => ({ ...x, [k]: v }));

  const save = async () => {
    let path; let body;
    if (goal === 'site') {
      if (f.siteMode === 'create') {
        const r = await run(() => post('/api/vpn/wireguard/site', { label: f.label, endpoint: f.endpoint, remote_lan: f.remote_lan }));
        if (r) { setMade(r); onDone(r.id, 'site', 'wireguard'); }
      } else {
        const r = await run(() => post('/api/vpn/wireguard/site/join', { code: f.code, label: f.label }), 'Site link added.');
        if (r) { onDone(r.id, 'site', 'wireguard'); if (r.warning) setWarn(r.warning); else onClose(); }
      }
      return;
    }
    if (goal === 'other') {
      const r = await run(() => post('/api/vpn/wireguard/site/other', { label: f.label, public_key: f.other_key, remote_lans: f.other_lans.split(/[\s,]+/).filter(Boolean), endpoint: f.other_ep, psk: f.other_psk, host: f.endpoint }));
      if (r) { setOther(r); onDone(r.id, 'site', 'wireguard'); }
      return;
    }
    if (goal === 'in') {
      path = `/api/vpn/${tech}/server`;
      body = { label: f.label, endpoint: f.endpoint, route: f.route, reach: f.route === 'home' ? true : f.reach, device: f.device.trim() };
    } else if (tech === 'wireguard') {
      path = '/api/vpn/wireguard/client';
      body = { label: f.label, conf: f.conf, check: f.check, killswitch: f.killswitch };
    } else {
      path = '/api/vpn/openvpn/client';
      body = { label: f.label, profile: f.conf, username: f.username, password: f.password, check: f.check, killswitch: f.killswitch };
    }
    const r = await run(() => post(path, body), 'VPN added.');
    if (r && goal === 'in' && (r.device || r.device_error)) {
      onDone(r.id, goal, tech);
      if (r.device_error) noticeBox(`The VPN was created, but the device could not be added: ${r.device_error}`);
      if (r.device) { setFirst(r.device); return; }
    }
    if (r && goal === 'out' && f.scope === 'later') {
      // nobody is chosen yet: the VPN is added and waits; Routes can send traffic through it
      onDone(r.id, 'out-all', tech); onClose();
    } else if (r && goal === 'out' && f.scope === 'all') {
      // everything on the network uses the VPN: tick every network for them
      const ch = await run(() => get('/api/vpn/choices'));
      const uses = (ch?.networks || []).map((n) => ({ kind: 'network', value: n.name, label: '' }));
      if (uses.length) await run(() => put(`/api/vpn/${tech}/${r.id}/uses`, { uses }));
      onDone(r.id, 'out-all', tech); onClose();
    } else if (r) { onDone(r.id, goal, tech); onClose(); }
  };

  if (first) return <DeviceResult res={first} onClose={onClose} />;
  if (other) return <OtherResult r={other} onClose={onClose} />;

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
          <button className="preset" onClick={() => { setGoal('site'); setTech('wireguard'); }}>
            <span className="t">Join two sites together</span>
            <span className="d">Link this network to another Gatehouse, like a second home or an office. Each side can reach the other automatically.</span>
          </button>
          <button className="preset" onClick={() => { setGoal('other'); setTech('wireguard'); }}>
            <span className="t">Join a router from another maker</span>
            <span className="d">Link to a UniFi, pfSense, MikroTik or other router that supports WireGuard. You copy a few details across.</span>
          </button>
        </div>
      </Modal>
    );
  }

  if (goal === 'site') {
    if (made) {
      return (
        <Modal title="Take this code to the other site" onClose={onClose} footer={<Button kind="primary" onClick={onClose}>Done</Button>}>
          <div className="stack">
            <Note tone="warn" title="Shown only once">The code contains the secret keys for the link. Anyone who has it can join the link, so send it privately.</Note>
            <p className="sec">On the other Gatehouse, open VPN, choose Add a VPN, then Join two sites together, pick I have a code, and paste this in. The link then works in both directions with nothing else to set up.</p>
            <textarea className="in mono" rows={6} readOnly value={made.code} onFocus={(e) => e.target.select()} />
            <div className="actions">
              <Button onClick={() => { navigator.clipboard?.writeText(made.code); setCopied(true); }}>{copied ? 'Copied' : 'Copy'}</Button>
              <Button onClick={() => download(made.filename, made.code)}>Download as a file</Button>
            </div>
          </div>
        </Modal>
      );
    }
    if (warn) {
      return (
        <Modal title="Site link added" onClose={onClose} footer={<Button kind="primary" onClick={onClose}>Done</Button>}>
          <Note tone="warn" title="Check the networks">{warn}</Note>
        </Modal>
      );
    }
    const create = f.siteMode === 'create';
    return (
      <Modal title="Join two sites" onClose={onClose}
        footer={<><Button onClick={() => setGoal('')}>Back</Button><Button kind="primary" disabled={busy || (create ? !f.remote_lan.trim() : !f.code.trim())} onClick={save}>{create ? 'Create the link' : 'Join'}</Button></>}>
        <div className="stack">
          <Field label="This is the" hint="Start on one site and finish on the other.">
            <Segmented value={f.siteMode} onChange={set('siteMode')} options={[{ value: 'create', label: 'First site' }, { value: 'join', label: 'Second site: I have a code' }]} />
          </Field>
          <Field label="Name" hint="Anything that helps you recognise it, like Cabin."><Input value={f.label} onChange={set('label')} placeholder="Cabin" /></Field>
          {create ? (
            <>
              <Field label="Network at the other site" hint="The address range used there, like 192.168.50.0/24. It must be different from the ranges used here.">
                <Input value={f.remote_lan} onChange={set('remote_lan')} placeholder="192.168.50.0/24" />
              </Field>
              <Field label="Address the other site connects to" hint="Your public IP address or a name like home.example.com.">
                <Input value={f.endpoint} onChange={set('endpoint')} placeholder="home.example.com" />
              </Field>
              <p className="small muted">Both sites will be able to reach each other's devices. Gatehouse makes the routes and firewall rules on both sides for you.</p>
            </>
          ) : (
            <Field label="Code from the first site" hint="Paste the whole code.">
              <textarea className="in mono" rows={6} value={f.code} onChange={(e) => set('code')(e.target.value)} spellCheck={false} placeholder="GATEHOUSE-SITE-1:…" />
            </Field>
          )}
        </div>
      </Modal>
    );
  }

  if (goal === 'other') {
    return (
      <Modal title="Join a router from another maker" onClose={onClose}
        footer={<><Button onClick={() => setGoal('')}>Back</Button><Button kind="primary" disabled={busy || !f.other_key.trim() || !f.other_lans.trim()} onClick={save}>Create the link</Button></>}>
        <div className="stack">
          <p className="sec small">Create a WireGuard tunnel on the other router first, and copy its public key and networks here. Gatehouse then shows what to enter on that router.</p>
          <Field label="Name"><Input value={f.label} onChange={set('label')} placeholder="Office" /></Field>
          <Field label="Their public key" hint="44 characters, ending in =."><Input value={f.other_key} onChange={set('other_key')} className="mono" spellCheck={false} /></Field>
          <Field label="Networks at the other site" hint="Separate several with commas, like 192.168.50.0/24, 192.168.60.0/24."><Input value={f.other_lans} onChange={set('other_lans')} placeholder="192.168.50.0/24" /></Field>
          <Field label="Address of the other router" hint="Only if it has a fixed address Gatehouse can connect to, like 203.0.113.9:51820. Leave empty if it will connect to you."><Input value={f.other_ep} onChange={set('other_ep')} placeholder="203.0.113.9:51820" /></Field>
          <Field label="Address the other router connects to" hint="Your public IP address or a name like home.example.com."><Input value={f.endpoint} onChange={set('endpoint')} placeholder="home.example.com" /></Field>
          <details className="more"><summary>More options</summary>
            <Field label="Pre-shared key" hint="Only if the other router already has one. Otherwise Gatehouse makes one."><Input value={f.other_psk} onChange={set('other_psk')} className="mono" spellCheck={false} /></Field>
          </details>
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
            <Field label="What goes through the VPN?" hint={f.route === 'all' ? 'All of the device\'s internet and home traffic travels through your router. Best for public Wi-Fi.' : 'Only traffic for your home network goes through your router. Everything else uses the device\'s own connection.'}>
              <Segmented value={f.route} onChange={set('route')} options={[{ value: 'all', label: 'Everything' }, { value: 'home', label: 'Only my home network' }]} />
            </Field>
            <Field label="First device (optional)" hint="Give the first phone or laptop a name and you get its file and QR code straight away, like Marvins iPhone."><Input value={f.device} onChange={set('device')} placeholder="Marvins iPhone" /></Field>
            <ToggleRow title="Can reach devices in my home network" desc={f.route === 'home' ? 'Needed, because the VPN carries only home traffic.' : 'Turn off to give connected devices the internet through your router and nothing else.'} checked={f.reach || f.route === 'home'} disabled={f.route === 'home'} onChange={set('reach')} />
          </>
        ) : (
          <>
            <Field label={tech === 'wireguard' ? 'WireGuard file' : 'OpenVPN file (.ovpn)'} hint="Drop the file from your provider here, or choose it. You can also paste its contents.">
              <FileDrop accept={tech === 'wireguard' ? '.conf,text/plain' : '.ovpn,.conf,text/plain'} label="Drop your file here" onText={set('conf')} />
              <textarea className="in" rows={7} value={f.conf} onChange={(e) => set('conf')(e.target.value)} spellCheck={false} placeholder={tech === 'wireguard' ? '[Interface]\nPrivateKey = …' : 'client\nremote …'} />
            </Field>
            {tech === 'openvpn' && (
              <div className="formgrid">
                <Field label="Username" hint="Only if your provider asks for one."><Input value={f.username} onChange={set('username')} autoComplete="off" /></Field>
                <Field label="Password"><Input type="password" value={f.password} onChange={set('password')} autoComplete="new-password" /></Field>
              </div>
            )}
            <Field label="Test address" hint="Gatehouse checks the VPN is really working by reaching this address through it. Leave empty for 1.1.1.1."><Input value={f.check} onChange={set('check')} placeholder="1.1.1.1" /></Field>
            <Field label="Who uses it?">
              <Segmented value={f.scope} onChange={set('scope')} options={[{ value: 'all', label: 'Everything on my network' }, { value: 'pick', label: 'Only what I pick' }, { value: 'later', label: 'Nobody yet' }]} />
            </Field>
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
      if (r.warnings?.length) noticeBox(r.warnings.join('\n'));
      onSaved(); onClose();
    }
  };
  return (
    <Modal title={`Who uses ${v.label} (optional)`} onClose={onClose} footer={<><Button onClick={onClose}>Skip for now</Button><Button kind="primary" disabled={busy} onClick={save}>Save</Button></>}>
      <div className="stack">
        <p className="sec">Tick the networks and devices whose internet should go through this VPN. Everything you leave unticked keeps using your normal connection. You can skip this and choose later, or use the Routes page to send only some websites or countries through it.</p>
        <Field label="Whole networks">
          <div className="stack" style={{ gap: 'var(--space-1)' }}>
            {(ch?.networks || []).map((n) => (
              <label key={n.name} className="inline"><input type="checkbox" checked={sel.has(`network|${n.name}`)} onChange={() => toggle(`network|${n.name}`)} /> <span className="name">{n.name}</span> <span className="small muted mono">{n.subnets.join(', ')}</span></label>
            ))}
            {ch && !ch.networks.length && <span className="muted small">No networks yet.</span>}
          </div>
        </Field>
        {(ch?.groups || []).length > 0 && (
          <Field label="Device groups" hint="Everyone in the group, now and as the group changes.">
            <div className="stack" style={{ gap: 'var(--space-1)' }}>
              {ch.groups.map((g) => <label key={g} className="inline"><input type="checkbox" checked={sel.has(`group|${g}`)} onChange={() => toggle(`group|${g}`)} /> <span className="name">{g}</span></label>)}
            </div>
          </Field>
        )}
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
            <Input value={range} onChange={setRange} aria-label="Address or range" placeholder="192.168.1.50" onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); addRange(); } }} />
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
  const [f, setF] = useState({ label: v.label, check: v.check_target || '', killswitch: v.killswitch, endpoint: v.endpoint || '', mtu: v.mtu || 0 });
  const save = async () => {
    const body = { label: f.label };
    if (client) { body.check = f.check; body.killswitch = f.killswitch; } else if (!v.site) body.endpoint = f.endpoint;
    if (v.kind === 'wireguard') body.mtu = Number(f.mtu) || 0;
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
        ) : v.site ? null : (
          <Field label="Address devices connect to" hint="Your public IP address or a name like home.example.com. Existing device files keep the old address."><Input value={f.endpoint} onChange={(x) => setF({ ...f, endpoint: x })} placeholder="home.example.com" /></Field>
        )}
        {v.kind === 'wireguard' && (
          <details className="more"><summary>More options</summary>
            <Field label="Packet size (MTU)" hint="Leave at 0 for automatic, which suits nearly every connection. Gatehouse already trims connections to fit. Only set a number between 1280 and 1500 if your provider asks for one.">
              <Input type="number" min="0" max="1500" value={f.mtu} onChange={(x) => setF({ ...f, mtu: x })} />
            </Field>
          </details>
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
  const [host, setHost] = useState(v.endpoint || endpoint || '');
  const needHost = !v.endpoint && !endpoint;
  const create = async () => {
    if (needHost) {
      if (!(await run(() => put(`/api/vpn/${v.kind}/${v.id}`, { endpoint: host })))) return;
    }
    const r = await run(() => post(`/api/vpn/${v.kind}/${v.id}/peers`, { name }));
    if (r) { setRes(r); onSaved(); }
  };
  if (res) return <DeviceResult res={res} onClose={onClose} />;
  return (
    <Modal title="Add a device" onClose={onClose} footer={<><Button onClick={onClose}>Cancel</Button><Button kind="primary" disabled={busy || !name.trim()} onClick={create}>Create</Button></>}>
      <div className="stack">
        <Field label="Device name" hint="Like Marvins iPhone."><Input value={name} onChange={setName} onKeyDown={(e) => { if (e.key === 'Enter' && name.trim()) create(); }} /></Field>
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

function Warnings({ v }) {
  return (v.warnings || []).map((w) => <Note key={w} tone="warn" title="Needs attention">{w}</Note>);
}

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
  const [checked, setChecked] = useState(null);
  const check = (ip) => get(`/api/vpn/check?ip=${encodeURIComponent(ip)}`).then(setChecked).catch(() => setChecked({ sentence: 'The check could not be run.', verdict: 'none' }));
  return (
    <Card>
      <div className="stack">
        <Header v={v} />
        <p className={v.state === 'up' ? 'sec' : ''} style={v.state === 'down' || v.state === 'degraded' ? { color: 'var(--color-status-serious)' } : undefined}>
          {v.message}{v.state === 'up' && v.rtt_ms ? ` · ${v.rtt_ms} ms` : ''}
        </p>
        {(v.log || []).length > 0 && (
          <details>
            <summary className="small muted">Show details (what OpenVPN says)</summary>
            <pre className="small" style={{ whiteSpace: 'pre-wrap', margin: 'var(--space-1) 0 0', overflowWrap: 'anywhere' }}>{v.log.join('\n')}</pre>
          </details>
        )}
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
            <div className="inline">{sources.map((u) => (
              <span key={u.id} className="inline" style={{ gap: 'var(--space-1)' }}>
                <Pill tone="">{u.kind === 'network' ? `${u.label} network` : u.label}</Pill>
                {u.kind === 'device' && (u.ips || []).length > 0 && <button type="button" className="link small" onClick={() => check(u.ips[0].split('/')[0])}>Check</button>}
              </span>
            ))}</div>
          )}
          {checked && (
            <div className="stack" style={{ gap: 'var(--space-1)' }}>
              <span className="small" style={{ color: checked.verdict === 'leak' ? 'var(--color-status-serious)' : undefined }}>{checked.device ? `${checked.device}: ` : ''}{checked.sentence}</span>
              {checked.others?.length > 0 && <span className="small muted">Went out through: {checked.others.map((o) => `${o.label} (${o.n})`).join(', ')}</span>}
              {checked.leaked?.map((l, i) => <span key={i} className="small muted mono">{fmt.ago(l.ts)} · {l.what} · {l.via}</span>)}
              {checked.verdict === 'ok' && <span className="small muted">This looks at the last hour of new connections in the log.</span>}
            </div>
          )}
          {v.killswitch && sources.length > 0 && v.state !== 'up' && v.state !== 'off' && <span className="small muted">Kill switch is on: these devices have no internet until the VPN is back.</span>}
        </div>
        <div className="actions">
          <Button kind="primary" icon="edit" onClick={() => setModal({ t: 'uses', v })}>Choose who uses it</Button>
          <Button onClick={() => run(() => put(`/api/vpn/${v.kind}/${v.id}`, { enabled: !v.enabled }), v.enabled ? 'Turned off.' : 'Turned on.').then(reload)}>{v.enabled ? 'Turn off' : 'Turn on'}</Button>
          <Button onClick={() => setModal({ t: 'settings', v })}>Settings</Button>
          <Button kind="danger" icon="trash" onClick={async () => await confirmBox(`Remove ${v.label}? Devices using it go back to your normal connection.`) && run(() => del(`/api/vpn/${v.kind}/${v.id}`), 'Removed.').then(reload)}>Remove</Button>
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
        <Warnings v={v} />
        <p className="sec">{v.message}</p>
        <div className="grid">
          <Fact label="Devices connect to">{v.endpoint ? `${v.endpoint}:${v.port}` : 'not set'}</Fact>
          <Fact label="VPN network">{v.subnet}</Fact>
          <Fact label="Goes through the VPN">{v.route_all ? 'Everything' : 'Only the home network'}</Fact>
          <Fact label="Can reach home devices">{v.access === 'full' ? 'Yes' : 'No, internet only'}</Fact>
        </div>
        <div className="list">
          {v.peers.map((p) => {
            const s = PEER[p.state] || PEER.never;
            return (
              <div className="item" key={p.id}>
                <div><div className="name">{p.name}</div><div className="meta">{p.address || ''}{p.real ? ` · from ${p.real}` : ''}{p.state === 'connected' ? ` · ↓ ${bytes(p.rx)} ↑ ${bytes(p.tx)}` : p.last_seen ? ` · last seen ${fmt.ago(p.last_seen)}` : ''}</div></div>
                <div className="inline">{p.paused ? <Pill tone="">Paused</Pill> : <Pill tone={s.tone}>{s.label}</Pill>}
                  <Switch checked={!p.paused} label={`${p.name} allowed to connect`} onChange={(on) => run(() => put(`/api/vpn/${v.kind}/${v.id}/peers/${p.id}`, { enabled: on }), on ? 'Allowed again.' : 'Paused.').then(reload)} />
                  <Button small kind="danger" icon="trash" onClick={async () => await confirmBox(`Remove ${p.name}? It will no longer be able to connect.`) && run(() => del(`/api/vpn/${v.kind}/${v.id}/peers/${p.id}`), 'Removed.').then(reload)}>Remove</Button></div>
              </div>
            );
          })}
          {!v.peers.length && <Empty>No devices yet. Add your phone or laptop.</Empty>}
        </div>
        <div className="actions">
          <Button kind="primary" icon="plus" onClick={() => setModal({ t: 'device', v })}>Add a device</Button>
          <Button onClick={() => run(() => put(`/api/vpn/${v.kind}/${v.id}`, { enabled: !v.enabled }), v.enabled ? 'Turned off.' : 'Turned on.').then(reload)}>{v.enabled ? 'Turn off' : 'Turn on'}</Button>
          <Button onClick={() => setModal({ t: 'settings', v })}>Settings</Button>
          <Button kind="danger" icon="trash" onClick={async () => await confirmBox(`Remove ${v.label}? Every device on it will be disconnected.`) && run(() => del(`/api/vpn/${v.kind}/${v.id}`), 'Removed.').then(reload)}>Remove</Button>
        </div>
      </div>
    </Card>
  );
}

function Site({ v, reload, setModal }) {
  const [run] = useAction();
  const p = v.peers[0];
  const s = STATE[v.state] || STATE.down;
  return (
    <Card>
      <div className="stack">
        <div className="spread">
          <div className="inline"><span className="name" style={{ fontSize: 'var(--font-size-lg)' }}>{v.label}</span><span className="small muted">Site link · {v.dev}</span></div>
          <Pill tone={v.connected ? 'good' : s.tone}>{v.connected ? 'Connected' : s.label}</Pill>
        </div>
        <p className="sec">{v.message}. Devices on both sides can reach each other.</p>
        <div className="grid">
          <Fact label="Networks at the other site">{(v.networks || []).join(', ') || '–'}</Fact>
          <Fact label="Link address">{v.subnet}</Fact>
          <Fact label="Last contact">{p?.last_seen ? fmt.ago(p.last_seen) : 'never'}</Fact>
          {p?.state === 'connected' && <Fact label="Traffic">{`↓ ${bytes(p.rx)}  ↑ ${bytes(p.tx)}`}</Fact>}
        </div>
        <div className="actions">
          <Button onClick={() => run(() => put(`/api/vpn/${v.kind}/${v.id}`, { enabled: !v.enabled }), v.enabled ? 'Turned off.' : 'Turned on.').then(reload)}>{v.enabled ? 'Turn off' : 'Turn on'}</Button>
          <Button onClick={() => setModal({ t: 'settings', v })}>Rename</Button>
          <Button kind="danger" icon="trash" onClick={async () => await confirmBox(`Remove the link ${v.label}? The two sites will no longer reach each other.`) && run(() => del(`/api/vpn/${v.kind}/${v.id}`), 'Removed.').then(reload)}>Remove</Button>
        </div>
      </div>
    </Card>
  );
}

import RemoteAccess from './RemoteAccess.jsx';

export default function VPN() {
  const { data, reload } = useData('/api/vpn', { topics: ['vpn.status'], every: 15000 });
  const [modal, setModal] = useState(null);
  const vpns = data?.vpns || [];
  const out = vpns.filter((v) => v.role === 'client');
  const inn = vpns.filter((v) => v.role === 'server' && !v.site);
  const sites = vpns.filter((v) => v.role === 'server' && v.site);
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
          <Empty>No VPNs yet. Add one to reach your home from anywhere, or to send chosen devices through a VPN provider.</Empty>
        </Card>
      )}
      {data && <RemoteAccess onAddVPN={() => setModal({ t: 'add' })} hasDevice={inn.some((v) => (v.peers || []).length > 0)} />}
      {out.length > 0 && <h2 className="sec">Through a VPN provider</h2>}
      {out.map((v) => <Outbound key={v.dev} v={v} reload={fresh} setModal={setModal} />)}
      {inn.length > 0 && <h2 className="sec">Connect from outside</h2>}
      {inn.map((v) => <Inbound key={v.dev} v={v} reload={fresh} setModal={setModal} />)}
      {sites.length > 0 && <h2 className="sec">Linked sites</h2>}
      {sites.map((v) => <Site key={v.dev} v={v} reload={fresh} setModal={setModal} />)}

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

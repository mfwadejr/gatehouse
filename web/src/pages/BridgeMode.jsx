import React, { useEffect, useRef, useState } from 'react';
import { get, post, put, useData } from '../api.js';
import { Button, Card, Field, Input, NumberInput, Note, Pill, Problems, Segmented, Select, ToggleRow, useAction, useToast, confirmBox } from '../ui.jsx';

// Bridge mode: Gatehouse sits between another router and your network and filters, instead of
// routing. Saving only stages the setup. Applying starts a 60-second countdown; unless it is kept,
// the network files and the firewall both go back by themselves.
const blank = { enabled: true, outside: '', inside: [], oob: '', oob_addr: '', mode: 'static', addr: '', gateway: '', dns: '', subnets: [], allow: [], open: false };

export default function BridgeMode() {
  const toast = useToast();
  const { data, reload } = useData('/api/bridge', { topics: ['fw.status', 'net.status'], every: 4000 });
  const [f, setF] = useState(null);
  const [run, busy] = useAction();
  const [left, setLeft] = useState(null);
  const [code, setCode] = useState('');
  const [subnets, setSubnets] = useState('');
  const tried = useRef(0);
  useEffect(() => {
    if (data && !f) {
      const c = data.config?.outside || data.config?.enabled === false ? data.config : blank;
      setF({ ...blank, ...c, inside: c.inside || [], subnets: c.subnets || [], allow: c.allow || [] });
      setSubnets((c.subnets || []).join(', '));
    }
  }, [data, f]);
  const pending = data?.pending;
  useEffect(() => {
    if (!pending) { setLeft(null); return undefined; }
    const tick = () => setLeft(Math.max(0, pending.deadline - Math.floor(Date.now() / 1000)));
    tick();
    const t = setInterval(tick, 500);
    return () => clearInterval(t);
  }, [pending]);
  // Like the other applies: if this browser can still reach the console a moment later, keep the change.
  useEffect(() => {
    if (!pending || tried.current === pending.deadline) return undefined;
    tried.current = pending.deadline;
    let dead = false;
    (async () => {
      try {
        await new Promise((r) => setTimeout(r, 4000));
        if (dead) return;
        await get('/api/auth/me');
        await new Promise((r) => setTimeout(r, 2000));
        if (dead) return;
        await get('/api/auth/me');
        if (dead) return;
        await post('/api/bridge/confirm', {});
        toast('Applied and kept. You can still reach Gatehouse, so the change is permanent.');
        reload();
      } catch { /* cannot reach it: leave the countdown to undo itself */ }
    })();
    return () => { dead = true; };
  }, [pending]); // eslint-disable-line react-hooks/exhaustive-deps
  if (!data || !f) return null;

  const set = (k) => (v) => setF((x) => ({ ...x, [k]: v }));
  const wired = (data.ports || []).filter((p) => !p.Wireless);
  const portOpts = (blankLabel) => [{ value: '', label: blankLabel }, ...wired.map((p) => ({ value: p.Name, label: `${p.Name}${p.UsedFor ? ` (used for ${p.UsedFor === 'internet' ? 'the internet' : 'a network'} now)` : ''}` }))];
  const payload = () => ({ ...f, subnets: subnets.split(/[,\s]+/).filter(Boolean), allow: f.allow.map((a) => ({ ...a, port: Number(a.port) })) });
  const save = () => run(async () => { const r = await put('/api/bridge', payload()); reload(); return r; }, 'Saved. Nothing has changed on the machine yet.');
  const apply = () => run(async () => {
    await put('/api/bridge', payload());
    const r = await post('/api/bridge/apply', { code });
    toast(`Applied. If this page stops responding, open https://${(f.addr || '').split('/')[0] || 'the new address'} within a minute and keep the change; otherwise it undoes itself.`);
    reload();
    return r;
  });
  const live = data.live;
  const addAllow = () => setF((x) => ({ ...x, allow: [...x.allow, { name: '', proto: 'tcp', port: '', to: '' }] }));
  const setAllow = (i, k, v) => setF((x) => ({ ...x, allow: x.allow.map((a, j) => (j === i ? { ...a, [k]: v } : a)) }));

  if (pending) {
    return (
      <Card title="Bridge mode">
        <div className="confirmbar countdown" role="alert">
          <div>
            <div className="count">{left ?? ''}s</div>
            <div className="sec">Checking that you can still reach Gatehouse. If you can, the change is kept on its own. If you cannot, the network and the firewall both go back to how they were. After a change of address, open the new address in the browser to keep it.</div>
          </div>
          <div className="actions">
            <Button disabled={busy} onClick={() => run(async () => { await post('/api/bridge/cancel', {}); reload(); }, 'Undone. The previous settings are back.')}>Undo now</Button>
            <Button kind="primary" disabled={busy} onClick={() => run(async () => { await post('/api/bridge/confirm', {}); reload(); }, 'Kept. Bridge mode is now permanent.')}>Keep this change</Button>
          </div>
        </div>
      </Card>
    );
  }

  return (
    <Card title="Bridge mode" desc="Use Gatehouse as a filter between another router and your network, without routing or changing addresses. Your current networks stay saved and come back when you switch it off."
      actions={live ? <Pill tone="good">On</Pill> : <Pill>Off</Pill>}>
      <div className="stack">
        {data.undone && <Note tone="warn" title="The last change was not kept">It was undone on its own, and your previous settings are back. Your setup is still saved below so you can try again.</Note>}
        <Problems list={data.problems} />
        <ToggleRow title="Use bridge mode" desc="Off means Gatehouse routes as usual." checked={f.enabled} onChange={set('enabled')} />
        {f.enabled && (
          <>
            <div className="formgrid">
              <Field label="Port to your other router" hint="Where the internet comes from."><Select value={f.outside} onChange={set('outside')} options={portOpts('Choose a port')} /></Field>
              <Field label="Ports to your devices" hint="Tick every port your own network plugs into.">
                <div className="stack" style={{ gap: 4 }}>
                  {wired.filter((p) => p.Name !== f.outside && p.Name !== f.oob).map((p) => (
                    <label key={p.Name} className="inline"><input type="checkbox" checked={f.inside.includes(p.Name)} onChange={(e) => set('inside')(e.target.checked ? [...f.inside, p.Name] : f.inside.filter((n) => n !== p.Name))} /> {p.Name}{(data.current || []).includes(p.Name) ? ' (you are connected here)' : ''}</label>
                  ))}
                </div>
              </Field>
            </div>
            <Field label="Gatehouse's own address"><Segmented value={f.mode} onChange={set('mode')} options={[{ value: 'static', label: 'Fixed (recommended)' }, { value: 'dhcp', label: 'From your other router' }]} /></Field>
            {f.mode === 'static' ? (
              <div className="formgrid">
                <Field label="Address" hint="Unused address on your network, with its size."><Input value={f.addr} onChange={set('addr')} placeholder="192.168.1.2/24" className="in mono" /></Field>
                <Field label="Your other router's address"><Input value={f.gateway} onChange={set('gateway')} placeholder="192.168.1.1" className="in mono" /></Field>
                <Field label="DNS server (optional)"><Input value={f.dns} onChange={set('dns')} placeholder="192.168.1.1" className="in mono" /></Field>
              </div>
            ) : <Note tone="warn" title="The address can change">If your other router hands out a different address later, the console moves with it. Reserve this address on that router so it stays the same.</Note>}
            <Note title="Addresses and DHCP">Your other router keeps handing out addresses; Gatehouse does not. The console answers on the address above, from your devices only.</Note>
            <details className="more"><summary>Extra safety and exceptions</summary>
              <div className="stack">
                <div className="formgrid">
                  <Field label="Management-only port (optional)" hint="A spare port that is never part of the bridge. Plug a laptop in here to always reach the console."><Select value={f.oob} onChange={set('oob')} options={portOpts('None')} /></Field>
                  {f.oob && <Field label="Its address"><Input value={f.oob_addr} onChange={set('oob_addr')} placeholder="192.168.99.1/24" className="in mono" /></Field>}
                </div>
                <Field label="Networks on the other router's side that may manage Gatehouse" hint="Leave empty so only your own devices can. For example an office or VPN range: 10.8.0.0/24"><Input value={subnets} onChange={setSubnets} className="in mono" /></Field>
                <div className="stack">
                  <strong>Let a service on your network be reached from outside</strong>
                  {f.allow.map((a, i) => (
                    <div className="formgrid" key={i}>
                      <Input value={a.name} onChange={(v) => setAllow(i, 'name', v)} placeholder="Name" />
                      <Select value={a.proto} onChange={(v) => setAllow(i, 'proto', v)} options={['tcp', 'udp']} />
                      <NumberInput value={a.port} onChange={(v) => setAllow(i, 'port', v)} />
                      <Input value={a.to} onChange={(v) => setAllow(i, 'to', v)} placeholder="192.168.1.20" className="in mono" />
                      <Button small kind="danger" onClick={() => setF((x) => ({ ...x, allow: x.allow.filter((_, j) => j !== i) }))}>Remove</Button>
                    </div>
                  ))}
                  <div><Button small onClick={addAllow}>Add a service</Button></div>
                </div>
                <ToggleRow title="Trial: let everything through" desc="Nothing is filtered. Use it for a few minutes to check the cabling, then turn it off." checked={f.open} onChange={set('open')} />
              </div>
            </details>
          </>
        )}
        {(data.warnings || []).map((w, i) => <Note key={i} tone="warn">{w}</Note>)}
        <div className="inline">
          <Button disabled={busy} onClick={save}>Save</Button>
          <Button kind="primary" disabled={busy} onClick={async () => { if (await confirmBox(f.enabled ? 'Turn on bridge mode? You have 60 seconds to keep it; otherwise everything goes back by itself.' : 'Turn bridge mode off and go back to routing? You have 60 seconds to keep it.', { title: 'Apply this change', yes: 'Apply' })) apply(); }}>Apply</Button>
          {data.dirty && <Button disabled={busy} onClick={() => run(async () => { await post('/api/bridge/discard', {}); setF(null); reload(); }, 'Changes dropped.')}>Drop my changes</Button>}
        </div>
        <span className="small muted">Wi-Fi networks, VPNs and port forwards are paused while bridge mode is on and come back when it is off. If something goes wrong, the 60-second way back restores everything; a reset to defaults from the screen on this machine also switches bridge mode off.</span>
      </div>
    </Card>
  );
}

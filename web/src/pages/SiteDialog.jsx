import React, { useState } from 'react';
import { del, put, useData } from '../api.js';
import { Button, Field, Modal, Note, ToggleRow, useAction, useToast } from '../ui.jsx';

const chip = (on, label, click, key) => <Button key={key || label} small kind={on ? 'primary' : undefined} onClick={click}>{label}</Button>;
const toggle = (list, v) => (list.includes(v) ? list.filter((x) => x !== v) : [...list, v]);

// Allow this site / Block this site, from a name lookup in Logs. It asks what (this name or the whole
// site) and who (everyone, or chosen networks, profiles and devices), says what already covers the site,
// and saves a named exception or block like the Blocks page does. The change waits on the Review page
// the same way, and Undo takes it away again.
export default function SiteDialog({ row, onClose }) {
  const allow = row.verdict === 'blocked';
  const toast = useToast();
  const [run, busy] = useAction();
  const { data: info } = useData(`/api/logs/site?name=${encodeURIComponent(row.what)}&client=${encodeURIComponent(row.addr || '')}`);
  const { data: groups } = useData('/api/dns/groups');
  const { data: ch } = useData('/api/vpn/choices');
  const { data: ov } = useData('/api/fw/overview');
  const [whole, setWhole] = useState(false);
  const [who, setWho] = useState('everyone');
  const [nets, setNets] = useState([]);
  const [profs, setProfs] = useState([]);
  const [devs, setDevs] = useState([]);
  const networks = (ov?.matrix_zones || []).map((z) => z.name);
  const name = info?.name || row.what;
  const site = info?.site || name;
  const value = whole ? site : name;
  const w = info?.who || {};
  const targets = who === 'everyone' ? [{ scope: 'everyone', target: '' }]
    : who === 'device' ? [{ scope: 'device', target: w.mac }]
      : who === 'network' ? [{ scope: 'network', target: w.network }]
        : who === 'profile' ? [{ scope: 'profile', target: w.profile_id }]
          : [...nets.map((t) => ({ scope: 'network', target: t })), ...profs.map((t) => ({ scope: 'profile', target: t })), ...devs.map((t) => ({ scope: 'device', target: t }))];
  const held = allow && (info?.holds || []).length > 0;
  const ready = !!info && targets.length > 0 && targets.every((t) => t.scope === 'everyone' || t.target) && !held;
  const verb = allow ? 'Allow' : 'Block';
  const save = async () => {
    const r = await run(() => put('/api/blocks', { id: 0, name: `${verb} ${value} (from Logs)`.slice(0, 60), allow, source: 'logs', items: [{ kind: 'name', value }], targets, method: 'fast', schedule: '', enabled: true }));
    if (!r || !r.id) return;
    onClose();
    toast(`${verb === 'Allow' ? 'Allowed' : 'Blocked'} ${value}. It is saved on the Blocks page under ${allow ? 'Exceptions' : 'Blocks'}.`, '', { undo: async () => { await del(`/api/blocks/${r.id}`); toast(`That ${allow ? 'exception' : 'block'} is gone.`); } });
  };
  return (
    <Modal title={`${verb} this site`} onClose={onClose}
      footer={<><Button onClick={onClose}>Cancel</Button><Button kind="primary" onClick={save} disabled={busy || !ready}>{verb}</Button></>}>
      <div className="stack">
        <p className="small">{allow
          ? <>NopeWarden stopped <b>{name}</b> because it is on {(info?.lists || []).length ? `the ${info.lists.join(', ')} list${info.lists.length > 1 ? 's' : ''}` : 'a block list'}. Letting it through does not change the list.</>
          : <>NopeWarden answered <b>{name}</b>. Blocking it makes lookups of it fail.</>}</p>
        {allow && info?.threat && <Note tone="warn" title="This is on a threat list">The list that stops it is about malware, phishing or scams. Only allow it if you are sure it is safe.</Note>}
        {held && <Note tone="bad" title="A block you set up covers this">{info.holds.map((h) => h.text).join('. ')}. An exception from Logs never overrides a block you wrote on purpose. Change that block on the Blocks page instead.</Note>}
        {!allow && (info?.allowed || []).length > 0 && <Note tone="warn" title="An exception already allows this">{info.allowed.map((h) => h.text).join('. ')}. Where an exception covers a device, it wins over this block. Remove it on the Blocks page first.</Note>}
        <Field label="What">
          <div className="stack">
            <label className="small" style={{ display: 'flex', gap: 'var(--space-2)' }}><input type="radio" name="sd-what" checked={!whole} onChange={() => setWhole(false)} /> <span><b>{name}</b> and anything under it</span></label>
            {site !== name && <label className="small" style={{ display: 'flex', gap: 'var(--space-2)' }}><input type="radio" name="sd-what" checked={whole} onChange={() => setWhole(true)} /> <span>The whole site, <b>{site}</b> and everything under it</span></label>}
          </div>
        </Field>
        <Field label="Who">
          <div className="stack">
            <label className="small" style={{ display: 'flex', gap: 'var(--space-2)' }}><input type="radio" name="sd-who" checked={who === 'everyone'} onChange={() => setWho('everyone')} /> <span>Everyone</span></label>
            {w.mac && <label className="small" style={{ display: 'flex', gap: 'var(--space-2)' }}><input type="radio" name="sd-who" checked={who === 'device'} onChange={() => setWho('device')} /> <span>Only {w.device || 'this device'}</span></label>}
            {w.network && <label className="small" style={{ display: 'flex', gap: 'var(--space-2)' }}><input type="radio" name="sd-who" checked={who === 'network'} onChange={() => setWho('network')} /> <span>Only the {w.network} network</span></label>}
            {w.profile && <label className="small" style={{ display: 'flex', gap: 'var(--space-2)' }}><input type="radio" name="sd-who" checked={who === 'profile'} onChange={() => setWho('profile')} /> <span>Only the {w.profile} profile</span></label>}
            <label className="small" style={{ display: 'flex', gap: 'var(--space-2)' }}><input type="radio" name="sd-who" checked={who === 'choose'} onChange={() => setWho('choose')} /> <span>Choose networks, profiles and devices</span></label>
            {who === 'choose' && (
              <>
                {networks.length > 0 && <Field label="Networks"><div style={{ display: 'flex', flexWrap: 'wrap', gap: 'var(--space-2)' }}>{networks.map((n) => chip(nets.includes(n), n, () => setNets(toggle(nets, n))))}</div></Field>}
                {(groups || []).length > 0 && <Field label="Profiles"><div style={{ display: 'flex', flexWrap: 'wrap', gap: 'var(--space-2)' }}>{(groups || []).map((g) => chip(profs.includes(String(g.id)), g.name, () => setProfs(toggle(profs, String(g.id))), `g${g.id}`))}</div></Field>}
                {(ch?.devices || []).length > 0 && <Field label="Devices"><div style={{ display: 'flex', flexWrap: 'wrap', gap: 'var(--space-2)', maxHeight: '8rem', overflow: 'auto' }}>{(ch.devices || []).map((d) => chip(devs.includes(d.mac.toLowerCase()), d.name, () => setDevs(toggle(devs, d.mac.toLowerCase())), d.mac))}</div></Field>}
              </>
            )}
          </div>
        </Field>
        <p className="small muted">It applies at once, always. Open it on the Blocks page to give it hours or change who it covers. {allow ? 'It only lets through names that a block list stops.' : ''}</p>
      </div>
    </Modal>
  );
}

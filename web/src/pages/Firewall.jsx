import { hashQuery, setHashQuery, useHashTab } from '../hashtab.js';
import React, { useEffect, useMemo, useState } from 'react';
import { del, holdAnd, put, useData, useStream } from '../api.js';
import { SAVED_LATER, SaveChoice } from '../changes.jsx';
import { confirmRemoval } from '../removal.js';
import AddNetwork from '../AddNetwork.jsx';
import { go } from '../App.jsx';
import { linkState } from '../linkstate.js';
import AlwaysOn from './AlwaysOn.jsx';
import Countries from './Countries.jsx';
import UPnPCard from './UPnPCard.jsx';
import Forwards from './Forwards.jsx';
import Discovery from './Discovery.jsx';
import NatNotice from '../natnotice.jsx';
import { GroupsAndLists, Matrix, PairModal, RuleForm, RulesList } from './FirewallRules.jsx';
import Services from './Services.jsx';
import { Button, Card, Empty, Field, Input, Modal, NumberInput, Note, Page, Pill, Problems, Select, Tabs, ToggleRow, useAction, useToast, fmt, confirmBox, Limited, usePortNames, connLabel } from '../ui.jsx';

function ZoneForm({ initial, presets, ifaces, taken, onClose, onSaved }) {
  const pn = usePortNames();
  const [z, setZ] = useState({ kind: 'trusted', name: '', ifaces: [], internet: true, management: true, intra_allow: true, gateway: true, upnp: false, ssh: false, dns_force: true, description: '', ...initial });
  const [run, busy] = useAction();
  const set = (k) => (v) => setZ((x) => ({ ...x, [k]: v }));
  const pick = (p) => setZ((x) => ({ ...x, kind: p.kind, internet: p.internet, intra_allow: p.intra_allow, management: p.management }));
  return (
    <Modal title={z.id ? `Edit ${z.name}` : 'Add a network'} onClose={onClose}
      footer={<><Button onClick={onClose}>Cancel</Button><SaveChoice busy={busy} disabled={!z.name} onSave={async (later) => { if (await run(() => (later ? holdAnd(() => put('/api/fw/zones', z)) : put('/api/fw/zones', z)), later ? SAVED_LATER : 'Saved.')) { onSaved(); onClose(); } }} /></>}>
      <Field label="Name"><Input value={z.name} onChange={set('name')} placeholder="Guests" /></Field>
      <div className="field"><span className="lab">What kind of network is it?</span>
        <div className="stack">
          {presets.map((p) => (
            <button type="button" key={p.kind} className={`preset ${z.kind === p.kind ? 'on' : ''}`} onClick={() => pick(p)} aria-pressed={z.kind === p.kind}>
              <span className="t">{p.title}</span><span className="d">{p.summary}</span>
            </button>
          ))}
        </div>
      </div>
      <div className="field"><span className="lab">Which ports or VLANs belong to it?</span>
        {ifaces.length === 0 && <span className="hint">Add a port on the Internet page first.</span>}
        {ifaces.map((i) => {
          const other = taken[i.name] && taken[i.name] !== z.name;
          return (
            <label key={i.id} className="inline small">
              <input type="checkbox" checked={z.ifaces.includes(i.name)} disabled={!!other}
                onChange={(e) => set('ifaces')(e.target.checked ? [...z.ifaces, i.name] : z.ifaces.filter((n) => n !== i.name))} />
              <span>{pn(i.name)} <span className="muted">{i.ipv4}{other ? ` · used by ${taken[i.name]}` : ''}</span></span>
            </label>
          );
        })}
      </div>
      <ToggleRow title="Can reach the internet" checked={z.internet} onChange={set('internet')} />
      <ToggleRow title="Devices here can talk to each other" checked={z.intra_allow} onChange={set('intra_allow')} />
      <ToggleRow title="Can manage NopeWarden" desc="Allows the console from this network. Keep at least one network on, so you can always get back in; NopeWarden refuses a change that would leave none." checked={z.management} onChange={set('management')} />
      <details className="more"><summary>Advanced</summary>
        <ToggleRow title="NopeWarden is the gateway" desc="Off if another router on this network does that job." checked={z.gateway} onChange={set('gateway')} />
        <ToggleRow title="Make devices here use the enforced DNS server" desc="Catches gadgets with a DNS server built in, such as smart TVs and cameras. Turn off for a network that needs its own DNS. A different server for this network, and exceptions for single devices, are under Protection." checked={z.dns_force} onChange={set('dns_force')} />
        <ToggleRow title="Allow remote login (SSH)" checked={z.ssh} onChange={set('ssh')} />
      </details>
      <p className="small muted">Letting devices open ports by themselves (UPnP) is set in one place: Firewall, Open ports.</p>
    </Modal>
  );
}

function InternetRow({ ov }) {
  const pn = usePortNames();
  const { data: wd } = useData('/api/net/wans', { topics: ['net.status'] });
  const { data: st } = useData('/api/net/status', { topics: ['net.status'], every: 5000 });
  const wans = (wd?.wans || []).filter((w) => w.enabled);
  const links = st?.links || {};
  let tone = '', text = 'Not set up';
  if (wans.length) {
    const states = wans.map((w) => linkState(links[w.name]));
    const ok = states.filter((x) => x.health === 'working');
    if (ok.length) { tone = 'good'; text = wans.length > 1 ? `${ok.length} of ${wans.length} working` : 'Connected'; }
    else if (states.every((x) => x.health === 'unknown' || x.health === 'none')) { text = 'Checking…'; }
    else { tone = 'bad'; text = states[0].text; }
  }
  return (
    <div className="item">
      <div>
        <div className="name">Internet <Pill>Internet</Pill><Pill tone={tone}>{text}</Pill></div>
        <div className="meta">{wans.length ? wans.map((w) => connLabel(pn, w)).join(', ') : 'no connection yet'} · managed automatically</div>
        <div className="meta">Devices on your networks can go out. Nothing from the internet can come in unless you open a port.</div>
      </div>
      <div className="actions"><Button small icon="edit" onClick={() => go('internet')}>Edit</Button></div>
    </div>
  );
}

function Networks({ ov, presets, reload }) {
  const pn = usePortNames();
  const { data: ifs } = useData('/api/net/interfaces');
  const [edit, setEdit] = useState(null);
  const [addNet, setAddNet] = useState(false);
  const toast = useToast();
  const [run] = useAction();
  // The same order as the "Who can reach whom" grid: your networks as they were made, Internet last.
  const stored = (ov?.zones || []).filter((z) => z.kind !== 'wan');
  const order = (ov?.matrix_zones || []).map((z) => z.name);
  const rank = (z) => { const i = order.indexOf(z.name); return i < 0 ? 999 : i; };
  const zones = [...stored].sort((a, b) => rank(a) - rank(b));
  const taken = {};
  zones.forEach((z) => z.ifaces?.forEach((n) => { taken[n] = z.name; }));
  const kindTitle = (k) => presets.find((p) => p.kind === k)?.title || k;
  // A link such as #/firewall/networks?edit=Guests (from the Security check) opens that network's edit window.
  const [wanted, setWanted] = useState(() => hashQuery('edit'));
  useEffect(() => { const on = () => setWanted(hashQuery('edit')); window.addEventListener('hashchange', on); return () => window.removeEventListener('hashchange', on); }, []);
  useEffect(() => {
    if (!wanted || !ov) return;
    const z = zones.find((x) => x.name.toLowerCase() === wanted.toLowerCase());
    if (z) setEdit(z);
    setHashQuery('edit', '');
    setWanted('');
  });
  return (
    <>
      {ov?.problems && zones.length > 0 && <Problems list={ov.problems} />}
      <Card title="Your networks" desc="Each network has a type. The type decides what it can reach, in plain terms." actions={<Button small icon="plus" onClick={() => setAddNet(true)}>Add a network</Button>}>
        {zones.length === 0 && <Empty>No networks yet. Add your first one so devices are protected.</Empty>}
        <div className="list">
          {zones.map((z) => (
            <div className="item" key={z.id}>
              <div>
                <div className="name">{z.name} <Pill>{kindTitle(z.kind)}</Pill>{z.management && <Pill tone="good">Can manage NopeWarden</Pill>}{!z.ifaces?.length && <Pill tone="warn">Not attached to anything</Pill>}</div>
                <div className="meta">{z.ifaces?.length ? z.ifaces.map(pn).join(', ') : 'Not attached to a port, so nothing is on it. Remove it if you no longer need it.'}</div>
              </div>
              <div className="actions"><Button small icon="edit" onClick={() => setEdit(z)}>Edit</Button>
                <Button small kind="danger" icon="trash" onClick={async () => await confirmRemoval(`/api/fw/zones/${z.id}/removal`, `Remove ${z.name}?`, { zoneRemoval: true }) && run(() => del(`/api/fw/zones/${z.id}`)).then(reload)}>Remove</Button></div>
            </div>
          ))}
          <InternetRow ov={ov} />
        </div>
      </Card>
      {ov?.summary && (
        <Card title="In plain words" desc="What this firewall does right now, once applied.">
          <ul className="stack" style={{ margin: 0, paddingLeft: '1.2rem' }}>{ov.summary.map((s, i) => <li key={i}>{s}</li>)}</ul>
        </Card>
      )}
      {addNet && <AddNetwork onClose={() => setAddNet(false)} onCreated={(n, later) => { setAddNet(false); toast(later ? `${n} saved for later. It waits on the Review page.` : `${n} added. It goes live in a moment.`); reload(); go('networks'); }} />}
      {edit && <ZoneForm initial={edit} presets={presets} ifaces={ifs || []} taken={taken} onClose={() => setEdit(null)} onSaved={reload} />}
    </>
  );
}

function Protected() {
  const { data } = useData('/api/fw/protected', { topics: ['fw.status'] });
  if (!data) return null;
  return (
    <section className="protected" aria-label="Protected rules">
      <div className="spread">
        <div className="inline"><span className="lock" aria-hidden="true">🔒</span><span className="name" style={{ fontSize: 'var(--font-size-lg)' }}>Protected: you can always get back in</span></div>
        <a className="small" href="#/settings/manage">Who may manage and Test my access</a>
      </div>
      <p className="small muted">These rules are built in. They sit above everything you can edit, and no change you make here can remove them. If you are ever locked out, press Alt+F2 on the machine itself. Who may manage this device, and a check that you still can, are under Settings, Managing this device.</p>
      <div className="list">
        {<Limited items={data.rules} noun="rules" render={(r) => (
          <div className="item" key={r.title}>
            <div><div className="name">{r.title}</div><div className="meta">{r.text}</div>{r.note && <div className="meta muted">{r.note}</div>}</div>
            <Pill tone={r.active ? 'good' : ''}>{r.active ? 'Always on' : 'Standing by'}</Pill>
          </div>
        )} />}
      </div>
    </section>
  );
}

function Protection({ ov, reload }) {
  const s = ov?.settings;
  const [run] = useAction();
  if (!s) return null;
  const save = (k) => (v) => run(() => put('/api/fw/settings', { ...s, [k]: v })).then(reload);
  return (
    <>
    <Note tone="info">Forced DNS, which sends devices that use a DNS server of their own to the one you chose, is now under <a href="#/dns/servers">DNS, Servers</a>.</Note>
    <Card title="Protection" desc="Extra safeguards that apply to every network.">
      <ToggleRow title="Block encrypted DNS to other servers" desc="Stops devices bypassing your filters with DNS-over-TLS. Follows the forced DNS settings under DNS, Servers: a device left alone is not blocked." checked={s.block_dot} onChange={save('block_dot')} />
      <ToggleRow title="Also block well-known encrypted-DNS websites" desc="Stops browsers that quietly switch to Cloudflare, Google, Quad9, OpenDNS, AdGuard or CleanBrowsing over HTTPS. This cannot be complete: any website can run such a server, so it stops the common cases, not a determined person." checked={!!s.block_doh} onChange={save('block_doh')} />
      <ToggleRow title="Ask for a code before changing the firewall" desc="Requires your authenticator code to apply network changes." checked={s.step_up} onChange={save('step_up')} />
      <ToggleRow title="Log blocked traffic" desc="Useful when troubleshooting. Uses more disk space." checked={s.log_drops} onChange={save('log_drops')} />
    </Card>
    <Card title="Threat watching, apps and categories" desc="These now have their own page."><p className="small">Block apps and categories, and watch for threats, under <a href="#/protection">Protection</a>.</p></Card>
    </>
  );
}

function History() {
  const { data } = useData('/api/fw/history', { topics: ['fw.status'] });
  return (
    <Card title="Change history" desc="Every time the firewall was changed, and whether you kept it.">
      <div className="scroll"><table className="table">
        <thead><tr><th>#</th><th>When</th><th>Who</th><th>What</th><th>Result</th></tr></thead>
        <tbody>{(data || []).map((h) => (
          <tr key={h.version}><td>{h.version}</td><td>{fmt.dt(h.at)}</td><td>{h.author}</td><td>{h.summary}</td>
            <td><Pill tone={h.status === 'confirmed' ? 'good' : h.status === 'pending' ? 'warn' : ''}>{h.status === 'confirmed' ? 'Kept' : h.status === 'pending' ? 'Waiting' : h.status === 'superseded' ? 'Replaced' : 'Undone'}</Pill></td></tr>
        ))}</tbody></table>
        {!data?.length && <Empty>No changes have been applied yet.</Empty>}
      </div>
    </Card>
  );
}

export default function Firewall({ me }) {
  const { data: ov, reload } = useData('/api/fw/overview', { topics: ['fw.status'] });
  const { data: rules, reload: reloadRules } = useData('/api/fw/rules');
  const { data: pickers, reload: reloadPickers } = useData('/api/fw/pickers');
  // One live connection for both lists, since a browser only keeps a few open per site.
  useStream(['fw.status', 'dhcp.leases'], (topic) => { if (topic === 'fw.status') reloadRules(); else reloadPickers(); });
  const { data: pr } = useData('/api/fw/presets');
  const [tab, setTab] = useHashTab('firewall', ['networks','rules','groups','services','ports','countries','discovery','protection','history'], 'networks');
  const [filter, setFilter] = useState({});
  const [pair, setPair] = useState(null);
  const [newRule, setNewRule] = useState(() => (hashQuery('new') && window.location.hash.includes('/rules') ? {} : null));
  useEffect(() => { if (newRule && hashQuery('new')) setHashQuery('new', ''); }, []); // eslint-disable-line react-hooks/exhaustive-deps
  const countryRules = (rules || []).filter((r) => r.countries);
  const presets = pr?.presets || [];
  const all = () => { reload(); reloadRules(); reloadPickers(); };
  const names = (ov?.matrix_zones || []).map((z) => z.name);
  return (
    <Page title="Firewall" sub="Decide what each network may reach. A saved change goes live in a moment, or waits on the Review page if you choose Save for later.">
      <Protected />
      <Tabs value={tab} onChange={setTab} tabs={[{ value: 'networks', label: 'Networks' }, { value: 'rules', label: `Rules${(rules || []).filter((r) => !r.countries).length ? ` (${(rules || []).filter((r) => !r.countries).length})` : ''}` }, { value: 'groups', label: 'Device groups' }, { value: 'services', label: 'Services' }, { value: 'ports', label: 'Open ports' }, { value: 'countries', label: 'Countries' }, { value: 'discovery', label: 'Discovery' }, { value: 'protection', label: 'Protection' }, { value: 'history', label: 'History' }]} />
      {tab === 'networks' && <><Matrix ov={ov} rules={(rules || []).filter((r) => !r.countries)} onPick={(from, to, p) => setPair({ from, to, p })} /><Networks ov={ov} presets={presets} reload={reload} /></>}
      {tab === 'rules' && <><RulesList ov={ov} rules={(rules || []).filter((r) => !r.countries)} countryRules={countryRules} pickers={pickers} reloadPickers={reloadPickers} reload={all} filter={filter} setFilter={setFilter} startNew={(init) => setNewRule(init)} goPorts={() => setTab('ports')} goTab={setTab} /><AlwaysOn /></>}
      {tab === 'groups' && <GroupsAndLists pickers={pickers} reloadPickers={reloadPickers} reload={all} />}
      {tab === 'services' && <Services part goPorts={() => setTab('ports')} />}
      {tab === 'ports' && <><NatNotice compact /><Forwards reload={reload} /><UPnPCard /></>}
      {tab === 'protection' && <><Note tone="info">The management port, the spare port kept for managing this device, is now under <a href="#/settings/manage">Settings, Managing this device</a>.</Note><Protection ov={ov} reload={reload} /></>}
      {tab === 'countries' && <Countries reload={all} />}
      {tab === 'discovery' && <Discovery reload={all} />}
      {tab === 'history' && <History />}
      {pair && (
        <PairModal pair={pair} ov={ov} onClose={() => setPair(null)} onChanged={all}
          onShowRules={(from, to) => { setFilter({ from, to }); setTab('rules'); }}
          onAddRule={(from, to) => { setTab('rules'); setNewRule({ from, to }); }} />
      )}
      {newRule && names.length > 0 && <RuleForm initial={newRule} zoneNames={names} pickers={pickers} reloadPickers={reloadPickers} onClose={() => setNewRule(null)} onSaved={all} />}
    </Page>
  );
}

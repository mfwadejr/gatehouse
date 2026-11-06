import React, { useEffect, useState } from 'react';
import { hashQuery, setHashQuery, useWantedEdit } from '../hashtab.js';
import { del, holdAnd, post, put, useData } from '../api.js';
import { SAVED_LATER, SaveChoice } from '../changes.jsx';
import { OnToggle, PolicyControls, ServiceField, usePickers, WhenField, WhereField, WhoField, whenText } from '../policykit.jsx';
import { go } from '../App.jsx';
import { Button, Card, Empty, Field, Input, Modal, Note, Page, Pill, Segmented, Select, ToggleRow, useAction, useUndoDelete, Limited } from '../ui.jsx';

const STATE = {
  working: { tone: 'good', label: 'Working' },
  off: { tone: '', label: 'Off' },
  nobody: { tone: 'warn', label: 'Nobody yet' },
  'target-off': { tone: 'bad', label: 'Not working' },
  'one-connection': { tone: 'warn', label: 'Needs two connections' },
  paused: { tone: 'warn', label: 'Paused' },
};

// routeBody is what the server stores for a route: its own fields only. The routes list also carries
// words made for the screen (state, plain, warnings), which must not be sent back.
const routeBody = (r) => ({ id: r.id, name: r.name, enabled: r.enabled, target_kind: r.target_kind, target: r.target, mode: r.mode, kill: r.kill, who: r.who, where: r.where, service: r.service, sched: r.sched, paused_until: r.paused_until });

function RouteForm({ initial, targets, onClose, onSaved }) {
  const { pickers, reload: reloadPickers } = usePickers();
  const [run, busy] = useAction();
  const wans = targets.filter((t) => t.kind === 'wan');
  const vpns = targets.filter((t) => t.kind === 'tunnel');
  const groups = targets.filter((t) => t.kind === 'group');
  // A route saved for one connection is shown as a list of one; saving writes it as a list.
  const start = initial ? { ...initial, target_kind: initial.target_kind === 'wan' ? 'links' : initial.target_kind } : null;
  const firstKind = wans.length ? 'links' : vpns.length ? 'tunnel' : 'group';
  const firstOf = (k) => (k === 'links' ? (wans.find((w) => w.enabled) || wans[0])?.target : (targets.find((t) => t.kind === k && t.enabled) || targets.find((t) => t.kind === k))?.target) || '';
  const [f, setF] = useState(() => ({ name: '', enabled: true, target_kind: firstKind, target: firstOf(firstKind), mode: 'failover', kill: true, who: '', where: '', service: '', sched: null, ...start }));
  const set = (k) => (v) => setF((x) => ({ ...x, [k]: v }));
  const kind = f.target_kind;
  const order = kind === 'links' ? f.target.split(',').filter(Boolean) : [];
  const setOrder = (list) => setF((x) => ({ ...x, target: list.join(',') }));
  const toggleLink = (n) => setOrder(order.includes(n) ? order.filter((x) => x !== n) : [...order, n]);
  const moveLink = (i, d) => { const l = [...order]; const j = i + d; if (j < 0 || j >= l.length) return; [l[i], l[j]] = [l[j], l[i]]; setOrder(l); };
  const chooseKind = (k) => setF((x) => ({ ...x, target_kind: k, target: k === x.target_kind ? x.target : firstOf(k), kill: k === 'tunnel' ? x.kill : false }));
  const tgt = targets.find((t) => t.kind === f.target_kind && t.target === f.target);
  const save = async (later) => {
    const r = await run(() => (later ? holdAnd(() => put('/api/routes', routeBody(f))) : put('/api/routes', routeBody(f))), later ? SAVED_LATER : 'Saved.');
    if (r) { onSaved(); onClose(); }
  };
  return (
    <Modal title={initial ? 'Edit route' : 'Add a route'} onClose={onClose}
      footer={<><Button onClick={onClose}>Cancel</Button><SaveChoice onSave={save} busy={busy} /></>}>
      <div className="stack">
        <Field label="Name"><Input value={f.name} onChange={set('name')} placeholder="Usenet through the backup line" /></Field>
        <Field label="Send the traffic through">
          <Segmented value={kind} onChange={chooseKind} options={[
            { value: 'links', label: 'Internet connections', disabled: !wans.length },
            { value: 'tunnel', label: 'A VPN', disabled: !vpns.length },
            { value: 'group', label: 'A connection group', disabled: !groups.length },
          ]} />
        </Field>
        {kind === 'links' && (
          <Field label="Which connections, in what order" hint="Tick one or several. The first one that is working is used. A connection kept only as the backup can be chosen on its own.">
            <div className="stack gap1">
              {order.map((n, i) => (
                <div className="inline" key={n}>
                  <span className="name">{i + 1}. {n}</span>
                  <Button small aria-label={`Move ${n} up`} disabled={i === 0} onClick={() => moveLink(i, -1)}>↑</Button>
                  <Button small aria-label={`Move ${n} down`} disabled={i === order.length - 1} onClick={() => moveLink(i, 1)}>↓</Button>
                  <Button small onClick={() => toggleLink(n)}>Remove</Button>
                </div>
              ))}
              {wans.filter((w) => !order.includes(w.target)).map((w) => (
                <label key={w.target} className="inline"><input type="checkbox" checked={false} onChange={() => toggleLink(w.target)} /> <span className="name">{w.label}</span>{!w.enabled && <span className="small muted"> (off)</span>}</label>
              ))}
            </div>
            {order.length > 1 && <div className="mt2"><Segmented value={f.mode === 'balance' ? 'balance' : 'failover'} onChange={set('mode')} options={[{ value: 'failover', label: 'Use in this order' }, { value: 'balance', label: 'Share evenly' }]} /></div>}
            {order.length === 0 && <p className="small muted">Choose at least one connection.</p>}
            <p className="small muted">If none of them is working, the traffic uses your normal connection.</p>
          </Field>
        )}
        {kind === 'tunnel' && (
          <>
            <Field label="Which VPN">
              <Select value={f.target} onChange={(v) => setF((x) => ({ ...x, target: v }))} options={vpns.map((t) => ({ value: t.target, label: `${t.label}${t.enabled ? '' : ' (off)'}` }))} />
            </Field>
            <ToggleRow title="Kill switch" desc="If the VPN drops, this traffic stops instead of leaking out through your normal connection." checked={!!f.kill} onChange={set('kill')} />
          </>
        )}
        {kind === 'group' && (
          <>
            <Field label="Which group">
              <Select value={f.target} onChange={(v) => setF((x) => ({ ...x, target: v }))} options={groups.map((t) => ({ value: t.target, label: `${t.target}${t.enabled ? '' : ' (off)'}` }))} />
            </Field>
            <p className="small muted">{tgt?.note ? `This group ${tgt.note}. ` : ''}Change the order or the shares on the Internet page, under connection groups.</p>
          </>
        )}
        <WhoField value={f.who} onChange={set('who')} pickers={pickers} reload={reloadPickers} hint="Any mix of networks, devices, device groups and addresses. You can save now and choose later." />
        <WhereField value={f.where} onChange={set('where')} pickers={pickers} reload={reloadPickers} />
        <ServiceField value={f.service} onChange={set('service')} pickers={pickers} reload={reloadPickers} />
        <p className="small muted">Websites are found from the lookups your devices make through NopeWarden, so a device that uses its own DNS, or already knew the address, may go the normal way at first. Routes carry IPv4 traffic.</p>
        <WhenField value={f.sched} onChange={set('sched')} hint="Always, or only during certain hours. Outside them the traffic takes the normal way." />
        <OnToggle checked={f.enabled} onChange={set('enabled')} what="the route" />
      </div>
    </Modal>
  );
}

export default function Routes() {
  const { data, reload } = useData('/api/routes', { topics: ['vpn.status', 'net.status'], every: 15000 });
  const [run] = useAction();
  const undoDelete = useUndoDelete();
  const [edit, setEdit] = useState(() => (hashQuery('new') ? {} : null));
  useEffect(() => { if (hashQuery('new')) setHashQuery('new', ''); }, []);
  const routes = data?.routes || [];
  useWantedEdit(data && routes, (r, w) => String(r.id) === w, setEdit);
  const targets = data?.targets || [];
  const again = () => { reload(); };
  const move = (i, d) => {
    const j = i + d;
    if (j < 0 || j >= routes.length) return;
    const ids = routes.map((r) => r.id);
    [ids[i], ids[j]] = [ids[j], ids[i]];
    run(() => post('/api/routes/order', { order: ids })).then(again);
  };
  const body = routeBody;
  const pauseFor = (r, m) => run(() => post(`/api/routes/${r.id}/pause`, { minutes: m }), m === 0 ? 'Resumed.' : 'Paused.').then(again);
  return (
    <Page title="Routes" sub="Send chosen devices or networks through a VPN or one internet connection, for everything or only for some destinations."
      actions={<Button kind="primary" small icon="plus" disabled={!targets.length} onClick={() => setEdit({})}>Add a route</Button>}>
      {!targets.length && data && (
        <Note tone="warn" title="Nothing to route to yet">Add a VPN on the VPN page, or a second internet connection on the Internet page, then come back here.</Note>
      )}
      <Card title="Your routes" desc="The first route that matches is used, so put the more specific ones at the top.">
        <div className="list">
          <Limited items={routes} noun="routes" render={(r, i) => {
            const st = STATE[r.state] || STATE.working;
            return (
              <div className={`item ${r.enabled ? '' : 'off'}`} key={r.id}>
                <div className="fill">
                  <div className="name">{r.name} <Pill tone={st.tone}>{st.label}</Pill></div>
                  <div className="meta">{r.plain}</div>
                  {whenText(r.paused_until) && <div className="meta">{whenText(r.paused_until)}</div>}
                  {(r.warnings || []).map((w, k) => <div className="meta warnline" key={k}>⚠ {w}</div>)}
                  {(r.where || '').includes('dom:') && (
                    <details className="meta">
                      <summary>{r.learned_total ? `${r.learned_total} address${r.learned_total === 1 ? '' : 'es'} learned for these websites` : 'No addresses learned yet'}</summary>
                      {r.learned_total ? <div className="mono small" style={{ overflowWrap: 'anywhere' }}>{r.learned.join(', ')}{r.learned_total > r.learned.length ? ` and ${r.learned_total - r.learned.length} more` : ''}</div>
                        : <div className="small">An address is learned when a device looks the website up. Until then there is nothing to send through this route. Addresses are forgotten when the website says they have expired.</div>}
                    </details>
                  )}
                </div>
                <div className="actions">
                  <Button small aria-label="Move up" disabled={i === 0} onClick={() => move(i, -1)}>↑</Button>
                  <Button small aria-label="Move down" disabled={i === routes.length - 1} onClick={() => move(i, 1)}>↓</Button>
                  <PolicyControls name={r.name} enabled={r.enabled} pausedUntil={r.paused_until} busy={false}
                    onPause={(m) => pauseFor(r, m)}
                    onToggle={() => run(() => put('/api/routes', { ...body(r), enabled: !r.enabled }), r.enabled ? 'Turned off.' : 'Turned on.').then(again)}
                    onEdit={() => setEdit(r)}
                    onRemove={() => undoDelete(`The route "${r.name}"`, () => del(`/api/routes/${r.id}`), () => put('/api/routes', { ...body(r), id: undefined }), again)} />
                </div>
              </div>
            );
          }} />
          {data && !routes.length && <Empty>No routes yet. Add one to send a device, a network or a website through a VPN or a chosen connection.</Empty>}
        </div>
      </Card>
      {(data?.legacy || []).length > 0 && (
        <Card title="Set up on the VPN page" desc="These send everything from the devices below through a VPN. They are changed on the VPN page, and they come after the routes above.">
          <div className="list">
            {data.legacy.map((l) => (
              <div className="item" key={l.tunnel}><div><div className="name">{l.uses}</div><div className="meta">everything through {l.label}</div></div><div className="actions"><Button small onClick={() => go('vpn')}>Open VPN</Button></div></div>
            ))}
          </div>
        </Card>
      )}
      {edit && targets.length > 0 && <RouteForm initial={edit.id ? edit : undefined} targets={targets} onClose={() => setEdit(null)} onSaved={again} />}
    </Page>
  );
}

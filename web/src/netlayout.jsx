import React from 'react';
import { useData, useThroughput } from './api.js';
import { fmtBits } from './charts.jsx';
import { Card, Pill, Empty, usePortNames } from './ui.jsx';
import { go } from './App.jsx';
import { linkState, linkMeasure } from './linkstate.js';

// The picture at the top of Networks: the internet at the top, every network hanging off it, each
// with its own live speed, how many devices are on it and anything that needs a look. Every number
// here comes from a feed the console already keeps (internet and network speeds, the connection
// monitor, device counts, address-pool use, VPN speeds), so nothing extra is measured for it.

const KIND = { guest: 'Guest', isolated: 'Isolated', trusted: '', private: '', custom: 'Custom rules' };
const last = (a) => (a && a.length ? a[a.length - 1] : 0);

export function NetworkLayout({ nets, zones, scopes, onOpen }) {
  const { data: wans } = useData('/api/net/wans', { topics: ['net.status'] });
  const { data: grp } = useData('/api/net/groups', { topics: ['net.status'], every: 15000 });
  const { data: net } = useData('/api/net/status', { topics: ['net.status'], every: 4000 });
  const { data: thr } = useThroughput(1000);
  const { data: speed } = useData('/api/net/zone-speed', { every: 1000 });
  const { data: util } = useData('/api/dhcp/utilisation', { topics: ['dhcp.utilisation', 'dhcp.leases'], every: 10000 });
  const { data: vpn } = useData('/api/vpn', { topics: ['vpn.status'], every: 15000 });
  const { data: vsp } = useData('/api/vpn/speed', { every: 2000 });
  const { data: mg } = useData('/api/mgmt/port', { topics: ['fw.status'], every: 2000 });
  const pn = usePortNames();

  const links = net?.links || {};
  const enabled = (wans?.wans || []).filter((w) => w.enabled);
  const states = enabled.map((w) => ({ w, l: links[w.name], s: linkState(links[w.name]) }));
  const active = states.find((x) => x.l?.active) || states.find((x) => x.s.health === 'working');
  // In Balance mode every working link in the default group carries traffic, each in proportion to its
  // share. Only in Failover does one link carry it while the others wait.
  const balance = grp?.default?.mode === 'balance';
  const shareOf = new Map((grp?.default?.members || []).map((m) => [m.name, Math.max(m.weight || 1, 1)]));
  const working = states.filter((x) => x.s.health === 'working');
  const total = working.reduce((n, x) => n + (shareOf.get(x.w.name) || 1), 0);
  // What the box is really doing comes from the monitor (two or more links carrying traffic), not
  // from the saved setting, which only takes effect once the change has been applied.
  const live = working.filter((x) => x.l?.active).length > 1;
  const notYet = balance && working.length > 1 && !live;
  const shared = live ? working.map((x) => ({ ...x, pct: Math.round(((shareOf.get(x.w.name) || 1) / total) * 100) })) : null;
  const standby = shared ? [] : states.filter((x) => x !== active && x.s.health === 'working');
  const down = enabled.reduce((n, w) => n + last(thr?.links?.[w.name]?.rx), 0);
  const up = enabled.reduce((n, w) => n + last(thr?.links?.[w.name]?.tx), 0);
  const tone = !enabled.length ? 'bad' : active ? (active.s.health === 'working' ? 'good' : 'warn') : states.every((x) => x.s.health === 'unknown' || x.s.health === 'none') ? '' : 'bad';

  const byZone = new Map();
  (nets || []).forEach((n) => {
    if (!n.zone || byZone.has(n.zone)) { if (n.zone) { const z = byZone.get(n.zone); z.ports.push(n); } return; }
    byZone.set(n.zone, { name: n.zone, ports: [n], online: n.online, total: n.total });
  });
  const cards = [...byZone.values()].map((z) => {
    const first = z.ports[0];
    const zone = (zones || []).find((x) => x.name === z.name);
    const u = (util || []).find((x) => z.ports.some((p) => p.scope && p.scope.id === x.scope_id));
    const pct = u && u.size ? Math.round((u.active / u.size) * 100) : null;
    return { ...z, first, zone, pct, dhcp: z.ports.some((p) => p.scope && p.scope.enabled !== false), sp: speed?.zones?.[z.name] };
  });
  const tunnels = (vpn?.vpns || []).filter((v) => v.enabled);

  return (
    <Card title="Network layout" desc="The internet at the top and each of your networks below it, with live speeds. Select a network to open it.">
      {!nets?.length && !enabled.length ? <Empty>Nothing to draw yet. Add a network and it appears here.</Empty> : (
        <div className="nl" role="group" aria-label="Network layout">
          <div className="nl-up">
            <div className="nl-cloud">
              <span className={`sdot ${tone}`} aria-hidden="true" />
              <b>{shared ? `Internet · ${grp?.default?.label || 'Default'} group sharing the load` : active ? `Internet · ${active.w.name || 'connection'}` : enabled.length ? 'Internet' : 'No internet connection set up'}</b>
              {shared ? shared.map((x) => <Pill key={x.w.name} tone="good">{x.w.name} in use · {x.pct}%</Pill>) : active && <Pill tone="good">In use</Pill>}
              {notYet && <button type="button" className="pillbtn warn" onClick={() => go('review')}>Sharing is saved, not applied yet</button>}
              {standby.map((x) => <Pill key={x.w.name}>{x.w.name} standing by</Pill>)}
              {enabled.length > 0 && !active && <Pill tone={tone}>{states[0]?.s.text}</Pill>}
            </div>
            {shared ? shared.map((x) => (
              <div className="nl-rate mono" key={x.w.name}><span>{x.w.name}</span><span className="nl-d">▼ {fmtBits(last(thr?.links?.[x.w.name]?.rx))}</span><span className="nl-u">▲ {fmtBits(last(thr?.links?.[x.w.name]?.tx))}</span>{linkMeasure(x.l) && <span>{linkMeasure(x.l)}</span>}</div>
            )) : enabled.length > 0 && <div className="nl-rate mono"><span className="nl-d">▼ {fmtBits(down)}</span><span className="nl-u">▲ {fmtBits(up)}</span>{active && linkMeasure(active.l) && <span>{linkMeasure(active.l)}</span>}</div>}
          </div>
          {cards.length > 0 && (
            <>
              <div className="nl-trunk" aria-hidden="true" />
              <div className={`nl-bus ${tone}`} aria-hidden="true" />
              <div className="nl-drops">
                {cards.map((c) => (
                  <div className="nl-drop" key={c.name}>
                    <button type="button" className="nl-net" onClick={() => onOpen(c.first)} aria-label={`Open ${c.name}`}>
                      <b>{c.name}</b>
                      <span className="nl-sn mono">{c.first.iface.kind === 'vlan' ? `VLAN ${c.first.iface.vlan} · ` : ''}{c.first.iface.ipv4 || 'no address'}</span>
                      <span className="nl-rate mono"><span className="nl-d">▼ {c.sp ? fmtBits(c.sp.down) : '0'}</span><span className="nl-u">▲ {c.sp ? fmtBits(c.sp.up) : '0'}</span></span>
                    </button>
                    <div className="nl-meta">
                      <button type="button" className="pillbtn" onClick={() => go('devices')}>{c.online} online{c.total > c.online ? ` of ${c.total}` : ''}</button>
                      {c.dhcp && <button type="button" className="pillbtn" onClick={() => go('dhcp/overview')}>DHCP</button>}
                      {KIND[c.zone?.kind] && <button type="button" className="pillbtn" onClick={() => go('firewall/networks')}>{KIND[c.zone.kind]}</button>}
                      {c.pct !== null && c.pct >= 80 && <button type="button" className="pillbtn warn" onClick={() => go('dhcp/scopes')}>Pool {c.pct}% full</button>}
                      {!c.first.iface.enabled && <Pill tone="warn">Off</Pill>}
                    </div>
                  </div>
                ))}
              </div>
            </>
          )}
          {mg?.config?.iface && !mg.bridge && (
            <div className="nl-mgmt">
              <button type="button" className="nl-net" onClick={() => go('settings/manage')} aria-label="Open Managing this device">
                <b>Management port · {pn(mg.config.iface)}{mg.address ? ` · ${mg.address}` : ''}</b>
                <span className="nl-sn"><span className={`sdot ${mg.state === 'ok' ? 'good' : mg.state === 'quiet' ? '' : 'warn'}`} aria-hidden="true" /> {mg.status}</span>
                {mg.state === 'ok' && <span className="nl-rate mono"><span className="nl-d">▼ {fmtBits(mg.down || 0)}</span><span className="nl-u">▲ {fmtBits(mg.up || 0)}</span></span>}
                <span className="small muted">{mg.lan_open ? 'The console also answers on the other networks.' : 'The console is closed on the other networks.'} Nothing is routed through this port.</span>
              </button>
            </div>
          )}
          {tunnels.length > 0 && (
            <div className="nl-vpn">
              <b>VPN</b>
              {tunnels.map((v) => {
                const s = vsp?.vpns?.[String(v.id)];
                return <span key={v.id} className="nl-vpnone"><Pill tone={v.state === 'up' ? 'good' : 'warn'}>{v.label || v.dev} · {v.state === 'up' ? 'up' : v.state || 'down'}</Pill>{s && <span className="mono small muted">▼ {fmtBits(s.down)} ▲ {fmtBits(s.up)}</span>}</span>;
              })}
            </div>
          )}
        </div>
      )}
    </Card>
  );
}

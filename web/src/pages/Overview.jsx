import React, { useState } from 'react';
import { useData, useStream, put } from '../api.js';
import { Card, Empty, Icon, Page, Pill, Spark, Trend, Tile, Bar, fmt, Note } from '../ui.jsx';
import { go } from '../App.jsx';

function Status({ title, tone, state, big, small, to }) {
  return (
    <Card className="tile">
      <div className="spread"><span className="lbl sec">{title}</span><Pill tone={tone}>{state}</Pill></div>
      <div className="val" style={{ fontSize: 'var(--font-size-2xl)', fontWeight: 600 }}>{big}</div>
      <div className="small muted">{small}</div>
      <button className="link small" style={{ textAlign: 'left' }} onClick={() => go(to)}>Open →</button>
    </Card>
  );
}

// The "Finish setting up" list. It is worked out from what the machine has, so it is right in any
// order, and it goes away by itself when everything is done.
function SetupChecklist() {
  const { data, reload } = useData('/api/setup/checklist', { topics: ['net.status', 'fw.status'], every: 15000 });
  if (!data || !data.items?.length) return null;
  const hide = async (hidden) => { try { await put('/api/setup/checklist', { hidden }); } catch { /* ignore */ } reload(); };
  if (data.left === 0) return null;
  if (data.hidden) {
    return <p className="small muted"><button type="button" className="link" onClick={() => hide(false)}>Show the setup checklist ({data.left} left)</button></p>;
  }
  const done = data.items.length - data.left;
  return (
    <Card title="Finish setting up" desc={`${done} of ${data.items.length} done. You can do these in any order, and change any of them later.`}
      actions={<button type="button" className="link small" onClick={() => hide(true)}>Hide for now</button>}>
      <ul className="stack" style={{ listStyle: 'none', margin: 0, padding: 0 }}>
        {data.items.map((i) => (
          <li key={i.id} className="spread" style={{ gap: 'var(--space-3)', alignItems: 'flex-start' }}>
            <div>
              <div style={{ fontWeight: 600 }}>{i.done ? '✓ ' : ''}{i.text}</div>
              <div className="small muted">{i.why}</div>
            </div>
            {i.done ? <Pill tone="good">Done</Pill> : <button type="button" className="btn small primary" onClick={() => go(i.to)}>Set up</button>}
          </li>
        ))}
      </ul>
    </Card>
  );
}

export default function Overview() {
  const { data: dns, setData: setDns } = useData('/api/dns/summary', { every: 15000 });
  useStream(['dns.stats'], (t, p) => p && setDns((d) => (d ? { ...d, stats: p } : d)));
  const { data: net } = useData('/api/net/status', { topics: ['net.status'], every: 5000 });
  const { data: wans } = useData('/api/net/wans', { topics: ['net.status'] });
  const { data: util } = useData('/api/dhcp/utilisation', { topics: ['dhcp.utilisation'], every: 15000 });
  const { data: leases } = useData('/api/dhcp/leases', { topics: ['dhcp.leases'] });
  const { data: fw } = useData('/api/fw/overview', { topics: ['fw.status'] });
  const { data: vpn } = useData('/api/vpn', { topics: ['vpn.status'], every: 15000 });
  const { data: top } = useData('/api/dns/top?hours=24', { every: 30000 });
  const { data: upd } = useData('/api/system/update', { every: 300000 });
  const { data: rogue } = useData('/api/dhcp/alerts', { topics: ['dhcp.alerts'] });

  const st = dns?.stats;
  const links = net?.links || {};
  const wanList = wans?.wans || [];
  const wanUp = wanList.filter((w) => w.enabled && links[w.name]?.up !== false).length;
  const wanTone = !wanList.length ? 'bad' : wanUp === wanList.filter((w) => w.enabled).length ? 'good' : wanUp ? 'warn' : 'bad';
  const active = Object.entries(links).find(([, v]) => v.active)?.[0] || wanList.find((w) => w.enabled)?.name;
  const totalLeases = (util || []).reduce((a, s) => a + s.active, 0);
  const totalPool = (util || []).reduce((a, s) => a + s.size, 0);
  const attention = [];
  if (!wanList.length) attention.push({ t: 'No internet connection is set up yet', s: 'Devices cannot reach the internet until you add one.', to: 'internet' });
  if (fw && !fw.zones?.length) attention.push({ t: 'No networks defined in the firewall', s: 'Create your first network so devices are protected.', to: 'firewall' });
  if (fw?.status?.dirty) attention.push({ t: 'Firewall changes are waiting to be applied', s: 'Review and apply them.', to: 'firewall' });
  if (net?.status?.dirty) attention.push({ t: 'Internet settings are waiting to be applied', s: 'Review and apply them.', to: 'internet' });
  (rogue || []).forEach((r) => attention.push({ t: `Another DHCP server is answering on ${r.name}`, s: `${r.found.map((f) => f.server).join(', ')} hands out addresses too. Switch it off, or devices may get the wrong settings.`, to: 'dhcp' }));
  Object.entries(links).forEach(([n, v]) => { if (v.up === false) attention.push({ t: `${n} is down`, s: v.error || 'The connection check is failing.', to: 'internet' }); });
  (vpn?.vpns || []).forEach((v) => {
    if (!v.enabled) return;
    if (v.state === 'down') attention.push({ t: `VPN ${v.label} is down`, s: v.message, to: 'vpn' });
    else if (v.state === 'degraded') attention.push({ t: `VPN ${v.label} is not passing traffic`, s: v.message, to: 'vpn' });
  });
  (util || []).forEach((s) => { if (s.size && s.active / s.size > 0.8) attention.push({ t: `${s.name} is ${Math.round((100 * s.active) / s.size)}% full`, s: 'Consider a larger address range.', to: 'dhcp' }); });
  (dns?.upstreams || []).forEach((u) => { if (u.state !== 'healthy') attention.push({ t: `DNS server ${u.name} is ${u.state}`, s: u.last_error || '', to: 'dns' }); });

  if (upd?.available && upd.mode !== 'off' && upd.state === 'idle') attention.push({ t: 'Updates are ready to install', s: [upd.gatehouse_new && upd.latest ? `Gatehouse ${upd.latest.version}` : '', upd.system_count > 0 ? `${upd.system_count} system update${upd.system_count === 1 ? '' : 's'}` : ''].filter(Boolean).join(' and '), to: 'settings' });
  if (upd?.needs_reboot) attention.push({ t: 'A restart is needed', s: 'The system was updated and needs a restart to finish.', to: 'settings' });

  const outcomes = st ? [
    { k: 'Forwarded', v: st.outcomes.forwarded || 0 },
    { k: 'Blocked', v: st.outcomes.blocked || 0 },
    { k: 'Cached', v: st.outcomes.cached || 0 },
    { k: 'Local', v: st.outcomes.local || 0 },
  ] : [];
  const total = st?.hour_total || 0;

  return (
    <Page title="Overview" sub={`${leases?.length ?? 0} devices have an address · ${attention.length ? `${attention.length} item${attention.length > 1 ? 's' : ''} need attention` : 'all good'}`}>
      <SetupChecklist />
      <section className="grid" aria-label="Status">
        <Status title="Internet" tone={wanTone} state={!wanList.length ? 'Not set up' : wanTone === 'good' ? 'Healthy' : wanTone === 'warn' ? 'Degraded' : 'Down'}
          big={active || '–'} small={wanList.length > 1 ? 'Primary active · others on standby' : wanList.length ? 'Single connection' : 'Add a connection'} to="internet" />
        <Status title="DNS" tone={dns?.upstreams?.some((u) => u.state === 'healthy') ? 'good' : 'bad'} state={dns?.upstreams?.some((u) => u.state === 'healthy') ? 'Healthy' : 'No upstream'}
          big={`${fmt.d1(st?.qps)} qps`} small={`${fmt.d1(st?.blocked_pct)}% blocked · ${fmt.d1(st?.cached_pct)}% cached`} to="dns" />
        <Status title="DHCP" tone={totalPool ? 'good' : 'warn'} state={totalPool ? 'Serving' : 'No scope'} big={`${totalLeases} leases`} small={`${(util || []).length} scope${(util || []).length === 1 ? '' : 's'}`} to="dhcp" />
        <Status title="Firewall" tone={fw?.status?.pending ? 'bad' : fw?.zones?.length ? 'good' : 'warn'} state={fw?.status?.pending ? 'Waiting for you' : fw?.zones?.length ? 'Enforcing' : 'Not set up'}
          big={`${fw?.zones?.length ?? 0} networks`} small={fw?.status?.dirty ? 'Changes not applied yet' : 'Up to date'} to="firewall" />
      </section>

      <div className="row">
        <div className="grow">
          <Card title="DNS queries" desc="The last minute, one point per second">
            <Trend values={st?.history || []} unit="/s" series="Queries per second" height={110} empty="No queries in the last minute" />
            <div className="inline sec small">
              <span>{fmt.n(total)} queries in the last hour</span>
              <span>· average {fmt.d1(st?.avg_ms)} ms</span><span>· 95% under {fmt.d1(st?.p95_ms)} ms</span>
            </div>
          </Card>
        </div>
        <div className="grow">
          <Card title="Needs attention">
            {attention.length === 0 ? <Empty>Nothing needs your attention.</Empty> : (
              <div className="list">
                {attention.map((a, i) => (
                  <div className="item" key={i}>
                    <div className="inline"><Icon name="warn" /><div><div className="name">{a.t}</div><div className="meta">{a.s}</div></div></div>
                    <button className="btn small" onClick={() => go(a.to)}>Open</button>
                  </div>
                ))}
              </div>
            )}
          </Card>
        </div>
      </div>

      <div className="row">
        <div className="grow">
          <Card title="How queries were answered" desc="Last hour">
            {total === 0 ? <Empty>No queries yet.</Empty> : (
              <div className="stack">
                {outcomes.map((o) => (
                  <div key={o.k} className="stack" style={{ gap: 'var(--space-1)' }}>
                    <div className="spread small"><span>{o.k}</span><span className="muted">{fmt.n(o.v)} · {fmt.d1((100 * o.v) / total)}%</span></div>
                    <Bar pct={(100 * o.v) / total} thin />
                  </div>
                ))}
              </div>
            )}
          </Card>
        </div>
        <div className="grow">
          <Card title="Busiest devices" desc="DNS queries, last 24 hours">
            {!top?.clients?.length ? <Empty>No activity yet.</Empty> : (
              <div className="list">
                {top.clients.slice(0, 6).map((c) => (
                  <div className="item" key={c.key}><div><div className="name">{c.label || c.key}</div><div className="meta">{c.key}</div></div><span>{fmt.n(c.n)}</span></div>
                ))}
              </div>
            )}
          </Card>
        </div>
        <div className="grow">
          <Card title="Most blocked" desc="Last 24 hours">
            {!top?.blocked?.length ? <Empty>Nothing has been blocked yet.</Empty> : (
              <div className="list">
                {top.blocked.slice(0, 6).map((c) => (
                  <div className="item" key={c.key}><span className="name" style={{ wordBreak: 'break-all' }}>{c.key}</span><span>{fmt.n(c.n)}</span></div>
                ))}
              </div>
            )}
          </Card>
        </div>
      </div>
    </Page>
  );
}

import React, { createContext, useContext, useMemo } from 'react';
import { useData } from './api.js';
import { linkState } from './linkstate.js';

// What needs the person, worked out once for the whole console. The Overview's status line and
// card read it, and the left menu shows a quiet dot on the page that holds the action. A dot is
// steady (it never blinks); red is something broken, amber needs you, blue is a suggestion.

const RANK = { info: 1, warn: 2, bad: 3 };
const AttentionContext = createContext({ items: [], byPage: {}, loaded: false, worst: '' });

export const useAttention = () => useContext(AttentionContext);

export function AttentionProvider({ children }) {
  const { data: net } = useData('/api/net/status', { topics: ['net.status'], every: 20000 });
  const { data: wans } = useData('/api/net/wans', { topics: ['net.status'] });
  const { data: fw } = useData('/api/fw/overview', { topics: ['fw.status'] });
  const { data: rogue } = useData('/api/dhcp/alerts', { topics: ['dhcp.alerts'] });
  const { data: vpn } = useData('/api/vpn', { topics: ['vpn.status'], every: 30000 });
  const { data: util } = useData('/api/dhcp/utilisation', { topics: ['dhcp.utilisation', 'dhcp.leases'], every: 30000 });
  const { data: dns } = useData('/api/dns/summary', { every: 30000 });
  const { data: upd } = useData('/api/system/update', { every: 300000 });
  const { data: sec } = useData('/api/security/check', { topics: ['fw.status'], every: 60000 });
  const { data: setup } = useData('/api/setup/checklist', { topics: ['net.status', 'fw.status'], every: 30000 });
  const { data: ov } = useData('/api/overview/security', { every: 30000 });
  const value = useMemo(() => {
    const a = [];
    const add = (level, t, s, to) => a.push({ level, t, s, to });
    const wanList = wans?.wans || [];
    const links = net?.links || {};
    if (net && !wanList.length) add('bad', 'No internet connection is set up yet', 'Devices cannot reach the internet until you add one.', 'internet');
    if (fw && !fw.zones?.length) add('warn', 'No networks defined in the firewall', 'Create your first network so devices are protected.', 'networks');
    if (fw?.status?.last_error) add('bad', 'The last firewall change did not apply', `${fw.status.last_error} Open Diagnostics to see what else is wrong.`, 'firewall');
    if (fw?.status?.dirty) add('warn', 'Firewall changes are waiting to be applied', 'Review and apply them.', 'networks');
    if (net?.status?.dirty) add('warn', 'Internet settings are waiting to be applied', 'Review and apply them.', 'internet');
    (rogue || []).forEach((r) => add('warn', `Another DHCP server is answering on ${r.name}`, `${r.found.map((f) => f.server).join(', ')} hands out addresses too. Switch it off, or devices may get the wrong settings.`, 'dhcp'));
    wanList.filter((w) => w.enabled).forEach((w) => {
      const s = linkState(links[w.name]);
      if (s.tone === 'bad') add('bad', `${w.name}: ${s.text}`, s.detail, 'internet');
    });
    (vpn?.vpns || []).forEach((v) => {
      if (!v.enabled) return;
      if (v.state === 'down') add('warn', `VPN ${v.label} is down`, v.message, 'vpn');
      else if (v.state === 'degraded') add('warn', `VPN ${v.label} is not passing traffic`, v.message, 'vpn');
    });
    (util || []).forEach((s) => {
      if (!s.size || s.active / s.size <= 0.8) return;
      const left = Math.max(0, s.size - s.active);
      add(left === 0 ? 'bad' : 'warn', left === 0 ? `${s.name} has no addresses left` : `${s.name} is nearly out of addresses`,
        `${s.active} of ${s.size} in use, ${left} left. ${left === 0 ? 'New devices cannot join until some are freed. ' : ''}Widen the range or shorten how long addresses are kept.`, 'dhcp');
    });
    if (dns?.upstreams?.length && !dns.upstreams.some((u) => u.state === 'healthy')) add('bad', 'No DNS server is answering', 'Devices cannot look up names until one answers. Open DNS and press Test.', 'dns');
    if (upd?.needs_reboot) add('warn', 'A restart is needed', 'The system was updated and needs a restart to finish.', 'settings');
    else if (upd?.available && upd.mode !== 'off' && upd.state === 'idle') add('info', 'Updates are ready to install', [upd.gatehouse_new && upd.latest ? `Gatehouse ${upd.latest.version}` : '', upd.system_count > 0 ? `${upd.system_count} system update${upd.system_count === 1 ? '' : 's'}` : ''].filter(Boolean).join(' and '), 'settings');
    // The security check and threat alerts both live on Protection.
    const bad = (sec?.items || []).filter((i) => i.level === 'bad').length;
    const warn = (sec?.items || []).filter((i) => i.level === 'warn').length;
    if (bad) add('bad', `Security check: ${bad} to fix now${warn ? `, ${warn} worth fixing` : ''}`, 'Open Protection to see what and how to fix it.', 'protection');
    else if (warn) add('warn', `Security check: ${warn} worth fixing`, 'Open Protection to see what and how to fix it.', 'protection');
    if (ov?.threat_alerts_24h > 0) add('warn', `Threat watching raised ${ov.threat_alerts_24h} alert${ov.threat_alerts_24h === 1 ? '' : 's'} in the last day`, `${ov.threat_devices_24h || 1} device${ov.threat_devices_24h === 1 ? '' : 's'} involved. Open Protection to look.`, 'protection');
    if (setup?.items?.length && setup.left > 0) add('info', `Finish setting up: ${setup.items.length - setup.left} of ${setup.items.length} done`, 'You can do these in any order.', 'settings');
    const byPage = {};
    a.forEach((x) => { if (!byPage[x.to] || RANK[x.level] > RANK[byPage[x.to]]) byPage[x.to] = x.level; });
    const worst = a.reduce((m, x) => (RANK[x.level] > (RANK[m] || 0) ? x.level : m), '');
    // What the status line counts: things that need the person, not suggestions.
    const needs = a.filter((x) => x.level !== 'info');
    return { items: a, needs, byPage, worst, loaded: !!(net && wans && fw) };
  }, [net, wans, fw, rogue, vpn, util, dns, upd, sec, setup, ov]);
  return <AttentionContext.Provider value={value}>{children}</AttentionContext.Provider>;
}

export const levelText = { bad: 'Something is broken', warn: 'Needs you', info: 'A suggestion' };

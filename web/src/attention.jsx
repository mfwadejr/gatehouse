import React, { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react';
import { put, useData } from './api.js';
import { linkState } from './linkstate.js';
import { plainNotes } from './whatsnew.jsx';

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
    const add = (level, id, t, s, to) => a.push({ level, id, t, s, to });
    const wanList = wans?.wans || [];
    const links = net?.links || {};
    if (net && !wanList.length) add('bad', 'no-wan', 'No internet connection is set up yet', 'Devices cannot reach the internet until you add one.', 'internet');
    if (fw && !fw.zones?.length) add('warn', 'no-zones', 'No networks defined in the firewall', 'Create your first network so devices are protected.', 'networks');
    if (fw?.status?.last_error) add('bad', 'fw-error', 'The last firewall change did not apply', `${fw.status.last_error} Open Diagnostics to see what else is wrong.`, 'firewall');
    if (fw?.status?.dirty && fw.status.refresh_only) add('info', 'fw-refresh', 'The firewall rules were refreshed by an update', 'NopeWarden writes its rules a little differently now. What is allowed has not changed. It puts them live by itself; you can also commit them on the Review page.', 'review');
    else if (fw?.status?.dirty) add('warn', 'fw-dirty', 'Firewall changes are waiting to be applied', 'Open the Review page and commit them.', 'review');
    if (net?.status?.dirty) add('warn', 'net-dirty', 'Internet settings are waiting to be applied', 'Open the Review page and commit them.', 'review');
    (rogue || []).forEach((r) => add('warn', `rogue:${r.name}`, `Another DHCP server is answering on ${r.name}`, `${r.found.map((f) => f.server).join(', ')} hands out addresses too. Switch it off, or devices may get the wrong settings.`, 'dhcp'));
    wanList.filter((w) => w.enabled).forEach((w) => {
      const s = linkState(links[w.name]);
      if (s.tone === 'bad') add('bad', `wan:${w.name}`, `${w.name}: ${s.text}`, s.detail, 'internet');
    });
    (vpn?.vpns || []).forEach((v) => {
      if (!v.enabled) return;
      if (v.state === 'down') add('warn', `vpn:${v.label}`, `VPN ${v.label} is down`, v.message, 'vpn');
      else if (v.state === 'degraded') add('warn', `vpn:${v.label}`, `VPN ${v.label} is not passing traffic`, v.message, 'vpn');
    });
    (util || []).forEach((s) => {
      if (!s.size || s.active / s.size <= 0.8) return;
      const left = Math.max(0, s.size - s.active);
      add(left === 0 ? 'bad' : 'warn', `dhcp-full:${s.name}`, left === 0 ? `${s.name} has no addresses left` : `${s.name} is nearly out of addresses`,
        `${s.active} of ${s.size} in use, ${left} left. ${left === 0 ? 'New devices cannot join until some are freed. ' : ''}Widen the range or shorten how long addresses are kept.`, 'dhcp');
    });
    // Red only when every server is down and none has answered for 30 seconds. A slow server still works.
    if (dns?.upstreams?.length && dns.upstreams.every((u) => u.state === 'down') && dns.upstreams.every((u) => !u.last_ok || Date.now() / 1000 - u.last_ok > 30)) {
      const names = dns.upstreams.map((u) => u.name).filter(Boolean).join(', ');
      add('bad', 'dns-down', 'No DNS server is answering', `${names ? `${names} did not answer. ` : ''}Devices cannot look up names until one answers. Open DNS and press Test.`, 'dns');
    }
    // Updates have their own page. An update that is ready is shown whatever the update mode is.
    const failedAt = upd?.result && !upd.result.ok ? upd.result.at || 0 : 0;
    if (upd?.state === 'idle' && !upd.working && failedAt && Date.now() / 1000 - failedAt < 3 * 86400) add('bad', 'update-failed', upd.result.rolled_back ? 'The last update did not work and was undone' : 'The last update did not work', `${upd.result.error || 'Open Updates to see what happened.'}`, 'updates');
    if (upd?.needs_reboot) add('warn', 'reboot', 'A restart is needed', 'The system was updated and needs a restart to finish.', 'updates');
    else if (upd?.available && upd.state === 'idle' && !upd.working) {
      const nw = upd.nopewarden_new && upd.latest ? upd.latest.version : '';
      const sys = upd.system_count > 0 ? `${upd.system_count} system update${upd.system_count === 1 ? '' : 's'}` : '';
      const it = { level: 'info', id: 'updates', t: nw ? `NopeWarden ${nw} is available` : 'System updates are ready to install', s: [nw ? `You are on ${upd.current}.` : '', sys && nw ? `Also ${sys}.` : sys].filter(Boolean).join(' '), to: 'updates', sig: nw ? `v${nw}` : 'system', notes: nw ? plainNotes(upd.latest.notes) : '' };
      a.push(it);
    }
    // The security check and threat alerts both live on Protection.
    const bad = (sec?.items || []).filter((i) => i.level === 'bad').length;
    const warn = (sec?.items || []).filter((i) => i.level === 'warn').length;
    if (bad) add('bad', 'security', `Security check: ${bad} to fix now${warn ? `, ${warn} worth fixing` : ''}`, 'Open Protection to see what and how to fix it.', 'protection');
    else if (warn) add('warn', 'security', `Security check: ${warn} worth fixing`, 'Open Protection to see what and how to fix it.', 'protection');
    if (ov?.threat_alerts_24h > 0) add('warn', 'threat', `Threat watching raised ${ov.threat_alerts_24h} alert${ov.threat_alerts_24h === 1 ? '' : 's'} in the last day`, `${ov.threat_devices_24h || 1} device${ov.threat_devices_24h === 1 ? '' : 's'} involved. Open Threats to look.`, 'threats/alerts');
    if (setup?.items?.length && setup.left > 0) add('info', 'setup', `Finish setting up: ${setup.items.length - setup.left} of ${setup.items.length} done`, 'You can do these in any order.', 'settings');
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

// The words for where an item's "Open" goes (the left menu's names).
export const pageLabel = { internet: 'Internet', networks: 'Networks', firewall: 'Firewall', review: 'Review', dhcp: 'DHCP', vpn: 'VPN', dns: 'DNS', settings: 'Settings', updates: 'Updates', protection: 'Protection', threats: 'Threats' };

// Short fingerprint of what an item says, so a dismissed suggestion comes back when its wording changes.
const sigOf = (it) => { let h = 5381; const t = it.sig || `${it.t}|${it.s}`; for (let i = 0; i < t.length; i++) h = ((h * 33) ^ t.charCodeAt(i)) >>> 0; return h.toString(36); };

export const SNOOZES = { day: 24 * 3600 * 1000, week: 7 * 24 * 3600 * 1000 };

// What the person has chosen to do with the Overview box, kept on the machine under their account
// (/api/prefs/attention), so it follows them to every device:
//   open  whether the box is open (default open)
//   hide  by item: a dismissed suggestion (k "d" and the fingerprint of what it said) or a snoozed
//         needs-you item (k "s" and the time it comes back).
// Broken (red) items can never be dismissed or snoozed. The dots in the left menu ignore all of this.
export function useAttentionChoices(items) {
  const { data } = useData('/api/prefs/attention');
  const [mine, setMine] = useState(null);
  useEffect(() => { if (data && mine === null) setMine({ open: data.open !== false, hide: data.hide || {} }); }, [data, mine]);
  const state = mine || { open: true, hide: {} };
  const save = useCallback((next) => {
    const now = Date.now();
    const ids = new Set(items.map((x) => x.id));
    const hide = {};
    for (const [id, h] of Object.entries(next.hide)) if (ids.has(id) && (h.k === 'd' || h.until > now)) hide[id] = h;
    const clean = { open: next.open, hide };
    setMine(clean);
    put('/api/prefs/attention', clean).catch(() => {});
  }, [items]);
  const now = Date.now();
  const view = useMemo(() => {
    const status = (it) => {
      const h = state.hide[it.id];
      if (it.level === 'info') return h?.k === 's' && h.until > now ? 'snoozed' : h?.k === 'd' && h.sig === sigOf(it) ? 'dismissed' : 'shown';
      if (it.level === 'warn') return h?.k === 's' && h.until > now ? 'snoozed' : 'shown';
      return 'shown';
    };
    const tagged = items.map((it) => ({ ...it, state: status(it), until: state.hide[it.id]?.until }));
    return {
      needs: tagged.filter((x) => x.level !== 'info' && x.state === 'shown'),
      snoozed: tagged.filter((x) => x.state === 'snoozed'),
      suggestions: tagged.filter((x) => x.level === 'info' && x.state === 'shown'),
      dismissed: tagged.filter((x) => x.state === 'dismissed'),
    };
  }, [items, state, now]);
  return {
    ...view,
    open: state.open,
    setOpen: (open) => save({ ...state, open }),
    dismiss: (it) => save({ ...state, hide: { ...state.hide, [it.id]: { k: 'd', sig: sigOf(it) } } }),
    snooze: (it, ms) => save({ ...state, hide: { ...state.hide, [it.id]: { k: 's', until: Date.now() + ms } } }),
    bringBack: (it) => { const hide = { ...state.hide }; delete hide[it.id]; save({ ...state, hide }); },
  };
}

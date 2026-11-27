import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { AttentionContext, useAttention } from './attentionctx.js';
import { put, useData } from './api.js';
import { linkState } from './linkstate.js';
import { plainNotes } from './whatsnew.jsx';

// What needs the person, worked out once for the whole console. The Overview's status line and
// card read it, and the left menu shows a quiet dot on the page that holds the action. A dot is
// steady (it never blinks); red is something broken, amber needs you, blue is a suggestion.

const RANK = { info: 1, warn: 2, bad: 3 };
export { useAttention };

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
  const { data: bk } = useData('/api/backups', { every: 300000 });
  const { data: disks } = useData('/api/disk/health', { every: 900000 });
  const { data: power } = useData('/api/power', { every: 30000 });
  const { data: certs } = useData('/api/certs', { every: 3600000 });
  const { data: advData } = useData('/api/prefs/advice');
  const [advHide, setAdvHide] = useState(null);
  useEffect(() => { if (advData && advHide === null) setAdvHide(advData.hide || {}); }, [advData, advHide]);
  const advice = useMemo(() => {
    const hide = advHide || {};
    const save = (next) => { setAdvHide(next); put('/api/prefs/advice', { hide: next }).catch(() => {}); };
    return {
      // Dismissed only while the item still says what it said when it was dismissed.
      hidden: (it) => !!it.advice && hide[it.id] === adviceSig(it),
      dismiss: (it) => save({ ...hide, [it.id]: adviceSig(it) }),
      bringBack: (it) => { const next = { ...hide }; delete next[it.id]; save(next); },
    };
  }, [advHide]);
  const value = useMemo(() => {
    const a = [];
    // stopped: something has actually stopped working (as opposed to a setting that needs a decision).
    const add = (level, id, t, s, to, stopped = false, act = '') => a.push({ level, id, t, s, to, stopped, act });
    const wanList = wans?.wans || [];
    const links = net?.links || {};
    if (net && !wanList.length) add('bad', 'no-wan', 'No internet connection is set up yet', 'Devices cannot reach the internet until you add one.', 'internet/connections', true);
    if (fw && !fw.zones?.length) add('warn', 'no-zones', 'No networks defined in the firewall', 'Create your first network so devices are protected.', 'networks');
    // A network that can manage this device but has no port: nothing can use it, and it makes every later change to the networks fail the check.
    (fw?.zones || []).filter((z) => z.management && z.kind !== 'wan' && z.kind !== 'vpn' && !(z.ifaces || []).length).forEach((z) => add('bad', `zone-noport:${z.name}`, `The network ${z.name} can manage this device but has no port`, 'Nothing can use it, and it stops new networks from being added. Remove it, or give it a port.', 'firewall/networks'));
    if (fw?.status?.last_error) add('bad', 'fw-error', 'The last firewall change did not apply', `${fw.status.last_error} Open Diagnostics to see what else is wrong.`, 'firewall/rules', true);
    if (fw?.status?.dirty && fw.status.refresh_only) add('info', 'fw-refresh', 'The firewall rules were refreshed by an update', 'NopeWarden writes its rules a little differently now. What is allowed has not changed. It puts them live by itself; you can also commit them on the Review page.', 'review');
    else if (fw?.status?.dirty) add('warn', 'fw-dirty', 'Firewall changes are waiting to be applied', 'Open the Review page and commit them.', 'review');
    if (net?.status?.dirty) add('warn', 'net-dirty', 'Internet settings are waiting to be applied', 'Open the Review page and commit them.', 'review');
    (rogue || []).forEach((r) => add('warn', `rogue:${r.name}`, `Another DHCP server is answering on ${r.name}`, `${r.found.map((f) => f.server).join(', ')} hands out addresses too. Switch it off, or devices may get the wrong settings.`, 'dhcp/overview'));
    wanList.filter((w) => w.enabled).forEach((w) => {
      const s = linkState(links[w.name]);
      if (s.tone === 'bad') add('bad', `wan:${w.name}`, `${w.name}: ${s.text}`, s.detail, 'internet/connections', true);
    });
    // A paused connection is on purpose, so it is not broken and raises no alert, but it stays on the list so it is not forgotten.
    wanList.filter((w) => w.enabled && links[w.name]?.paused).forEach((w) => add('warn', `wan-paused:${w.name}`, `${w.name} is paused`, linkState(links[w.name]).detail, 'internet/connections'));
    if (dns?.pause?.paused) add('warn', 'dns-paused', 'Blocking is paused', dns.pause.until ? `Every name is let through until ${new Date(dns.pause.until * 1000).toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' })}.` : 'Every name is let through until you resume.', 'dns');
    (vpn?.vpns || []).forEach((v) => {
      if (!v.enabled) return;
      if (v.state === 'down') add('warn', `vpn:${v.label}`, `VPN ${v.label} is down`, v.message, 'vpn');
      else if (v.state === 'degraded') add('warn', `vpn:${v.label}`, `VPN ${v.label} is not passing traffic`, v.message, 'vpn');
    });
    (util || []).forEach((s) => {
      if (!s.size || s.active / s.size <= 0.8) return;
      const left = Math.max(0, s.size - s.active);
      add(left === 0 ? 'bad' : 'warn', `dhcp-full:${s.name}`, left === 0 ? `${s.name} has no addresses left` : `${s.name} is nearly out of addresses`,
        `${s.active} of ${s.size} in use, ${left} left. ${left === 0 ? 'New devices cannot join until some are freed. ' : ''}Widen the range or shorten how long addresses are kept.`, 'dhcp/scopes', left === 0);
    });
    // Red only when every server is down and none has answered for 30 seconds. A slow server still works.
    if (dns?.upstreams?.length && dns.upstreams.every((u) => u.state === 'down') && dns.upstreams.every((u) => !u.last_ok || Date.now() / 1000 - u.last_ok > 30)) {
      const names = dns.upstreams.map((u) => u.name).filter(Boolean).join(', ');
      add('bad', 'dns-down', 'No DNS server is answering', `${names ? `${names} did not answer. ` : ''}Devices cannot look up names until one answers. Open DNS and press Test.`, 'dns/servers', true);
    }
    // Updates have their own page. An update that is ready is shown whatever the update mode is.
    const failedAt = upd?.result && !upd.result.ok ? upd.result.at || 0 : 0;
    if (upd?.state === 'idle' && !upd.working && failedAt && Date.now() / 1000 - failedAt < 3 * 86400) add('bad', 'update-failed', upd.result.rolled_back ? 'The last update did not work and was undone' : 'The last update did not work', `${upd.result.error || 'Open Updates to see what happened.'}`, 'updates/updates', true);
    if (upd?.needs_reboot) add('warn', 'reboot', 'A restart is needed', 'The system was updated and needs a restart to finish.', 'updates/updates');
    else if (upd?.available && upd.state === 'idle' && !upd.working) {
      const nw = upd.nopewarden_new && upd.latest ? upd.latest.version : '';
      const sys = upd.system_count > 0 ? `${upd.system_count} system update${upd.system_count === 1 ? '' : 's'}` : '';
      const it = { level: 'info', id: 'updates', t: nw ? `NopeWarden ${nw} is available` : 'System updates are ready to install', s: [nw ? `You are on ${upd.current}.` : '', sys && nw ? `Also ${sys}.` : sys].filter(Boolean).join(' '), to: 'updates/updates', sig: nw ? `v${nw}` : 'system', notes: nw ? plainNotes(upd.latest.notes) : '' };
      a.push(it);
    }
    // The security check and threat alerts both live on Protection.
    // Advice the person has dismissed is left out here, so it no longer counts, shows a dot or appears in the summary.
    const shown = (sec?.items || []).filter((i) => !advice.hidden(i));
    const bad = shown.filter((i) => i.level === 'bad').length;
    const warn = shown.filter((i) => i.level === 'warn').length;
    if (bad) add('bad', 'security', `Security check: ${bad} to fix now${warn ? `, ${warn} worth fixing` : ''}`, 'Open Protection to see what and how to fix it.', 'protection/overview');
    else if (warn) add('warn', 'security', `Security check: ${warn} worth fixing`, 'Open Protection to see what and how to fix it.', 'protection/overview');
    if (ov?.inline?.on && ov.inline.state === 'stopped') add('bad', 'inline-stopped', 'Blocking as it happens is not checking right now', ov.inline.text, 'threats/settings', true, 'inline-off');
    else if (ov?.inline?.on && ov.inline.state === 'falling_behind') add('warn', 'inline-behind', 'Blocking as it happens is falling behind', ov.inline.text, 'threats/settings', false, 'inline-off');
    if (ov?.threat_alerts_24h > 0) add('warn', 'threat', `Threat watching raised ${ov.threat_alerts_24h} alert${ov.threat_alerts_24h === 1 ? '' : 's'} in the last day`, `${ov.threat_devices_24h || 1} device${ov.threat_devices_24h === 1 ? '' : 's'} involved. Open Threats to look.`, 'threats/alerts');
    if (setup?.items?.length && setup.left > 0) add('info', 'setup', `Finish setting up: ${setup.items.length - setup.left} of ${setup.items.length} done`, 'You can do these in any order.', 'settings/general');
    // A backup is only worth having if it restores: say so when none has been tried in 90 days.
    const copies = bk?.backups || [];
    if (copies.length) {
      const good = copies.find((b) => b.report && b.report.ok);
      const tried = copies.find((b) => b.hand_checked);
      if (!good) add('warn', 'backup-none-good', 'No backup has passed its check', 'Open Backup & recovery and make a new one.', 'settings/backup');
      else if (!tried || Date.now() / 1000 - tried.hand_checked > 90 * 86400) add('info', 'backup-restore-check', 'Try a restore check on a backup', 'No backup has been checked by hand in 90 days. Press Check on one to be sure it will work.', 'settings/backup');
    }
    // Copies off this machine: tell the person when none is set up, when one is failing, or when it is time to download one by hand.
    if (copies.length && bk?.offsite) {
      const o = bk.offsite;
      if (o.failing?.length) add('warn', 'offsite-failing', `A backup copy could not be sent to ${o.failing.join(' and ')}`, 'Open Backup & recovery to see why. It tries again every hour.', 'settings/backup');
      else if (!o.enabled && o.remind_on && (!o.last_download || Date.now() / 1000 - o.last_download > o.remind_days * 86400)) add('info', 'offsite-reminder', o.last_download ? `No backup has been downloaded in ${o.remind_days} days` : 'No backup is kept anywhere but this machine', 'A backup on this machine is lost with it. Send copies to a cloud or file server, or download one to another computer.', 'settings/backup');
    }
    // A drive is shown only when it reports a problem or is nearly worn out.
    (disks?.drives || []).filter((d) => d.level).forEach((d) => add(d.level, `disk:${d.name}`, d.gone ? `The drive ${d.model} has gone` : d.word === 'Worn' ? `The drive ${d.model} is wearing out` : `The drive ${d.model} reports a problem`, d.reason, 'diagnostics/services', d.level === 'bad'));
    // The UPS: what is true right now, for the events set to show on the Overview.
    (power?.lines || []).forEach((l) => add(l.level, `power:${l.id}`, l.title, l.detail, 'diagnostics/services', false));
    // Certificates: blue from 60 days out, amber from 30, red once expired.
    (certs?.certs || []).filter((c) => c.level).forEach((c) => {
      const when = c.days < 0 ? `ran out ${-c.days} day${c.days === -1 ? '' : 's'} ago` : c.days === 0 ? 'runs out today' : `has ${c.days} day${c.days === 1 ? '' : 's'} left`;
      add(c.level, `cert:${c.id}`, `Certificate for ${c.label} ${when}`, c.advice, 'diagnostics/services', c.level === 'bad');
    });
    const byPage = {};
    const byTab = {};
    const top = (to) => String(to || '').split('/')[0];
    a.forEach((x) => {
      const pg = top(x.to);
      if (!byPage[pg] || RANK[x.level] > RANK[byPage[pg]]) byPage[pg] = x.level;
      if (String(x.to).includes('/')) {
        const t = byTab[x.to] || (byTab[x.to] = { level: x.level, titles: [] });
        if (RANK[x.level] > RANK[t.level]) t.level = x.level;
        t.titles.push(x.t);
      }
    });
    const worst = a.reduce((m, x) => (RANK[x.level] > (RANK[m] || 0) ? x.level : m), '');
    // What the status line counts: things that need the person, not suggestions.
    const needs = a.filter((x) => x.level !== 'info');
    return { items: a, needs, byPage, byTab, worst, advice, loaded: !!(net && wans && fw) };
  }, [net, wans, fw, rogue, vpn, util, dns, upd, sec, setup, ov, bk, certs, disks, power, advice]);
  return <AttentionContext.Provider value={value}>{children}</AttentionContext.Provider>;
}

export const levelText = { bad: 'Needs you now', warn: 'Needs you', info: 'A suggestion' };

// The words for where an item's "Open" goes (the left menu's names).
export const pageLabel = { internet: 'Internet', networks: 'Networks', firewall: 'Firewall', review: 'Review', dhcp: 'DHCP', vpn: 'VPN', dns: 'DNS', settings: 'Settings', diagnostics: 'Diagnostics', updates: 'Updates', protection: 'Protection', threats: 'Threats' };

// Short fingerprint of what an item says, so a dismissed suggestion comes back when its wording changes.
const adviceSig = (it) => { let h = 5381; const t = `${it.title}|${it.text}`; for (let i = 0; i < t.length; i++) h = ((h * 33) ^ t.charCodeAt(i)) >>> 0; return h.toString(36); };
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
  useEffect(() => { if (data && mine === null) setMine({ open: data.open !== false, hide: data.hide || {}, morning: data.morning }); }, [data, mine]);
  const state = mine || { open: true, hide: {}, morning: undefined };
  // The morning summary folds on its own. Before it has been chosen here it follows what this browser remembered.
  const morningOpen = state.morning !== undefined ? state.morning !== false : (() => { try { return localStorage.getItem('nw.morning.open') !== '0'; } catch { return true; } })();
  const save = useCallback((next) => {
    const now = Date.now();
    const ids = new Set(items.map((x) => x.id));
    const hide = {};
    for (const [id, h] of Object.entries(next.hide)) if (ids.has(id) && (h.k === 'd' || h.until > now)) hide[id] = h;
    const clean = { open: next.open, hide };
    if (next.morning !== undefined) clean.morning = next.morning;
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
    morningOpen,
    setMorningOpen: (m) => save({ ...state, morning: m }),
    dismiss: (it) => save({ ...state, hide: { ...state.hide, [it.id]: { k: 'd', sig: sigOf(it) } } }),
    snooze: (it, ms) => save({ ...state, hide: { ...state.hide, [it.id]: { k: 's', until: Date.now() + ms } } }),
    bringBack: (it) => { const hide = { ...state.hide }; delete hide[it.id]; save({ ...state, hide }); },
  };
}

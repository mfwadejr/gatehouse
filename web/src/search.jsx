import React, { useEffect, useMemo, useRef, useState } from 'react';
import { get } from './api.js';
import { Icon } from './ui.jsx';

// Things that always exist. "words" are extra ways people might describe them.
const STATIC = [
  ['Overview', 'overview', 'The state of everything at a glance', 'home dashboard status'],
  ['Internet connection', 'internet', 'Your internet connections and speed tests', 'wan speed test failover isp modem'],
  ['Networks', 'networks', 'Ports, networks and VLANs', 'lan vlan port interface add network guest iot'],
  ['Wi-Fi', 'wifi', 'Wireless networks', 'wireless ssid password access point'],
  ['DNS', 'dns', 'Names, blocking and DNS settings', 'domain resolver encrypted doh'],
  ['Block ads and trackers', 'dns', 'Blocklists on the DNS page', 'adblock blocklist ads tracking malware'],
  ['Your own names', 'dns', 'Give devices names like nas.lan', 'records hostname firewall.lan local'],
  ['DHCP', 'dhcp', 'Address pools and reservations', 'address pool lease range'],
  ['Reserve an address', 'dhcp', 'Keep the same address for a device', 'reservation static ip'],
  ['Firewall', 'firewall', 'Networks, rules and protection', 'rules allow block zone'],
  ['Block or allow countries', 'firewall', 'Stop traffic to or from whole countries', 'country region geo block russia china'],
  ['Test my internet', 'internet', 'Check the cable, modem, provider and DNS', 'diagnose troubleshoot not working down slow ping traceroute dns lookup tools'],
  ['Devices finding each other', 'firewall', 'Let printers, Chromecast and AirPlay work across networks', 'mdns bonjour airplay chromecast printer discovery avahi reflector'],
  ['Profiles', 'profiles', 'Kids, Guests, Smart home and Work: filtering level, safe search and bedtime', 'kids parental controls bedtime schedule safe search categories adult gambling filter'],
  ['Pause the internet', 'profiles', 'Pause a device or a whole profile for a while', 'pause stop internet bedtime off hours resume'],
  ["Why can't it reach that?", 'internet', 'Find out what is blocking a device from a website', 'blocked cannot reach not working why tester troubleshoot'],
  ['Security check', 'overview', 'What to fix to make your network safer', 'security risk safe hardening audit'],
  ['Undo a change', 'settings', 'Go back to how things were before a change', 'undo revert restore point rollback changes timeline'],
  ['Common setups', 'overview', 'Guest Wi-Fi, cameras, Plex from outside, UniFi', 'recipe guest cameras plex unifi how to'],
  ['Wi-Fi QR code', 'wifi', 'A code that joins the Wi-Fi when scanned, and a card to print', 'qr guest password join print card'],
  ['My network (printable)', 'settings', 'A printable summary of the whole network', 'summary handover print document records'],
  ['Provider report card', 'internet', 'Outages and speed for a month to show your provider', 'isp outage uptime report complaint'],
  ['Data use and limit', 'internet', 'How much data was used and an optional monthly limit', 'cap bandwidth usage metered billing'],
  ['Reach my home from anywhere', 'vpn', 'A web name and VPN to connect from outside', 'ddns duckdns noip dynamic dns remote access'],
  ['Encrypted DNS', 'dns', 'One switch for encrypted lookups', 'doh dot private dns cloudflare quad9'],
  ['High availability', 'settings', 'Run two machines as a pair with automatic failover', 'ha failover backup standby keepalived pair redundancy'],
  ['Traffic labels', 'settings', 'Name the app behind each connection by looking at packets', 'ndpi dpi deep packet inspection application youtube zoom bittorrent labels'],
  ['Bridge mode', 'settings', 'Filter between another router and your network without routing', 'bridge transparent filtering inline passthrough'],
  ['Send logs elsewhere', 'settings', 'Copy logs to a syslog server', 'syslog export siem'],
  ['Pair: cable between the two units', 'settings', 'Optional cable for the pair to talk over', 'heartbeat ha high availability cable link'],
  ['Pair: keep the spare internet ready', 'settings', 'Silent warm standby for faster takeover', 'warm standby ha failover mac'],
  ['Guest sign-in page', 'settings', 'Make guests tap Connect before they get internet', 'captive portal guest wifi terms splash sign in hotspot'],
  ['Send traffic records (NetFlow / IPFIX)', 'settings', 'Tell a traffic analysis tool about connections', 'netflow ipfix flow export collector'],
  ['Let a monitoring tool read this device (SNMP)', 'settings', 'Read only status for monitoring tools', 'snmp monitoring librenms zabbix prometheus'],
  ['Keep delay low when the line is busy', 'internet', 'Smart queues: hold the connection just under its speed', 'bufferbloat sqm cake fq_codel lag latency speed limit'],
  ['Alerts', 'settings', 'Be told by email or push when something happens', 'notify notification ntfy email push quiet hours new device offline'],
  ['Open a port', 'firewall', 'Let something in from the internet', 'port forward forwarding nat open ports'],
  ['UPnP', 'firewall', 'Let devices open ports themselves', 'upnp natpmp'],
  ['Services', 'services', 'Extra services on this machine', 'ntp time ssh'],
  ['VPN', 'vpn', 'VPN server and VPN providers', 'openvpn wireguard tunnel expressvpn remote access'],
  ['Routes', 'routes', 'Send devices or websites through a VPN or one connection', 'route vpn policy kill switch tunnel pin'],
  ['Devices & access', 'devices', 'Devices and who can sign in', 'users admin password two-step 2fa mfa'],
  ['Logs', 'logs', 'What happened and when', 'events history blocked connections map countries'],
  ['Settings', 'settings', 'Updates, backups, restart and reset', 'update backup restore restart reboot reset power console port'],
  ['Check for updates', 'settings', 'Install a new version of Gatehouse', 'upgrade version install'],
  ['Backups', 'settings', 'Save and restore your settings', 'backup restore export import'],
  ['Restart or shut down', 'settings', 'Power controls', 'reboot power off shutdown'],
  ['Remote login', 'devices', 'Turn SSH on or off', 'ssh'],
].map(([label, page, hint, words]) => ({ label, page, hint, words, kind: 'Page' }));

export function useSearchShortcut(open) {
  useEffect(() => {
    const on = (e) => {
      const t = e.target;
      const typing = t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.tagName === 'SELECT' || t.isContentEditable);
      if ((e.key === 'k' || e.key === 'K') && (e.metaKey || e.ctrlKey)) { e.preventDefault(); open(); }
      else if (e.key === '/' && !typing && !e.metaKey && !e.ctrlKey) { e.preventDefault(); open(); }
    };
    document.addEventListener('keydown', on);
    return () => document.removeEventListener('keydown', on);
  }, [open]);
}

export function SearchBox({ onClose, go }) {
  const [q, setQ] = useState('');
  const [sel, setSel] = useState(0);
  const [extra, setExtra] = useState([]);
  const input = useRef(null);
  useEffect(() => {
    input.current?.focus();
    let live = true;
    (async () => {
      const out = [];
      try {
        const zones = await get('/api/fw/zones');
        (zones || []).forEach((z) => out.push({ label: z.name, page: 'firewall', hint: 'Network', words: '', kind: 'Network' }));
      } catch { /* search still works without it */ }
      try {
        const leases = await get('/api/dhcp/leases');
        const seen = new Set();
        (leases || []).forEach((l) => {
          if (seen.has(l.mac)) return;
          seen.add(l.mac);
          out.push({ label: l.hostname || l.mac, page: 'dhcp', hint: `${l.ip} · ${l.mac} · ${l.scope || ''}`, words: `${l.ip} ${l.mac}`, kind: 'Device' });
        });
      } catch { /* same */ }
      if (live) setExtra(out);
    })();
    return () => { live = false; };
  }, []);
  const results = useMemo(() => {
    const words = q.toLowerCase().split(/\s+/).filter(Boolean);
    const all = [...STATIC, ...extra];
    if (!words.length) return STATIC.slice(0, 8);
    return all.map((r) => {
      const hay = `${r.label} ${r.hint} ${r.words}`.toLowerCase();
      if (!words.every((w) => hay.includes(w))) return null;
      const lab = r.label.toLowerCase();
      return { r, score: (words.every((w) => lab.startsWith(w)) ? 0 : words.every((w) => lab.includes(w)) ? 1 : 2) };
    }).filter(Boolean).sort((a, b) => a.score - b.score).slice(0, 12).map((x) => x.r);
  }, [q, extra]);
  useEffect(() => setSel(0), [q]);
  const pick = (r) => { onClose(); go(r.page); };
  const key = (e) => {
    if (e.key === 'Escape') onClose();
    else if (e.key === 'ArrowDown') { e.preventDefault(); setSel((s) => Math.min(results.length - 1, s + 1)); }
    else if (e.key === 'ArrowUp') { e.preventDefault(); setSel((s) => Math.max(0, s - 1)); }
    else if (e.key === 'Enter' && results[sel]) { e.preventDefault(); pick(results[sel]); }
  };
  return (
    <div className="srch-back" onMouseDown={(e) => { if (e.target === e.currentTarget) onClose(); }} role="presentation">
      <div className="srch" role="dialog" aria-label="Search">
        <div className="srch-in"><Icon name="search" />
          <input ref={input} type="search" data-noenter value={q} onChange={(e) => setQ(e.target.value)} onKeyDown={key} placeholder="Search pages, settings, devices, networks…" aria-label="Search" />
        </div>
        <div className="srch-list" role="listbox">
          {results.map((r, i) => (
            <button type="button" key={`${r.kind}-${r.label}-${i}`} role="option" aria-selected={i === sel} className={i === sel ? 'on' : ''} onMouseEnter={() => setSel(i)} onClick={() => pick(r)}>
              <span><b>{r.label}</b><span className="small muted"> {r.hint}</span></span>
              <span className="small muted">{r.kind}</span>
            </button>
          ))}
          {!results.length && <div className="srch-none small muted">Nothing matches "{q}". Try a shorter word.</div>}
        </div>
      </div>
    </div>
  );
}

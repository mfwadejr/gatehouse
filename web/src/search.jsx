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
  ['Open a port', 'firewall', 'Let something in from the internet', 'port forward forwarding nat open ports'],
  ['UPnP', 'firewall', 'Let devices open ports themselves', 'upnp natpmp'],
  ['Services', 'services', 'Extra services on this machine', 'ntp time ssh'],
  ['VPN', 'vpn', 'VPN server and VPN providers', 'openvpn wireguard tunnel expressvpn remote access'],
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

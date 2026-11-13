import React, { useEffect, useMemo, useRef, useState } from 'react';
import { get } from './api.js';
import { Icon } from './ui.jsx';

// Things that always exist. Each row: [label, where it opens, hint, extra words, text to scroll to].
// "where" is a page, or page/tab (or page/tab/inner tab). The last column is the title of the card
// to scroll to and flash once the page is open. check-search.mjs makes sure every target is real.
const ROWS = [
  ['Overview', 'overview', 'The state of everything at a glance', 'home dashboard status map what happened'],
  ['Internet connection', 'internet/connections', 'Your internet connections', 'wan isp modem failover port pppoe dhcp fixed address', 'Internet connections'],
  ['Test my internet', 'internet/troubleshoot', 'Check the cable, modem, provider and DNS', 'diagnose troubleshoot not working down slow ping traceroute dns lookup tools', 'Test my internet'],
  ["Why can't it reach that?", 'internet/troubleshoot', 'Find out what is blocking a device from a website', 'blocked cannot reach not working why tester troubleshoot', "Why can't it reach that?"],
  ['Speed test', 'internet/speed', 'Measure the speed of each connection', 'speed test download upload mbps', 'Speed test'],
  ['Keep delay low when the line is busy', 'internet/speed', 'Smart queues: hold the connection just under its speed', 'bufferbloat sqm cake fq_codel lag latency speed limit', 'Keep delay low'],
  ['Speed control: limit speed or give priority', 'internet/control', 'Hold devices to a speed, or let some traffic go first', 'speed limit throttle bandwidth cap priority qos mbps guest device app', 'Speed control'],
  ['Provider report card', 'internet/usage', 'Outages and speed for a month to show your provider', 'isp outage uptime report complaint', 'Provider report card'],
  ['Data use and limit', 'internet/usage', 'How much data was used and an optional monthly limit', 'cap bandwidth usage metered billing', 'Data use'],
  ['Networks', 'networks', 'Ports, networks and VLANs', 'lan vlan port interface add network guest iot', 'Your networks'],
  ['Network ports', 'networks', 'The ports on this device and what each is for', 'ethernet nic interface cable link speed', 'Network ports on this device'],
  ['DNS', 'dns/overview', 'Lookups, busiest devices and what was blocked', 'domain resolver queries'],
  ['Block ads and trackers', 'dns/lists', 'Blocklists on the DNS page', 'adblock blocklist ads tracking malware', 'Blocklists'],
  ['Why is this blocked?', 'dns/lists', 'Check whether a name is on a blocklist', 'blocked domain lookup allow', 'Why is this blocked?'],
  ['Your own names', 'dns/records', 'Give devices names like nas.lan', 'records hostname firewall.lan local'],
  ['Filtering per network', 'devices/profiles', 'Choose how much each network filters', 'profiles groups filtering level'],
  ['Encrypted DNS', 'dns/overview', 'One switch for encrypted lookups', 'doh dot private dns cloudflare quad9', 'Encrypted DNS'],
  ['Upstream DNS servers', 'dns/overview', 'Where NopeWarden asks when it does not know the answer', 'upstream resolver cloudflare google quad9 provider', 'Upstream DNS servers'],
  ['Forced DNS', 'dns/overview', 'Make devices use this DNS even if they try another', 'force redirect port 53 hardcoded dns', 'Forced DNS'],
  ['DHCP', 'dhcp/overview', 'How DHCP is doing right now', 'overview status address use pool full devices lately'],
  ['How full the address pools are', 'dhcp/overview', 'Which pools are nearly out of addresses', 'pool utilisation full out of addresses range exhausted', 'Address use'],
  ['Newest devices on the network', 'dhcp/overview', 'The devices that asked for an address most lately', 'recent joined new device lease', 'Most recently active'],
  ['Address pools', 'dhcp/scopes', 'Each network gets its own pool of addresses', 'address pool lease range', 'Address pools'],
  ['Devices with an address', 'dhcp/leases', 'Every device that has been given an address', 'leases clients hostname mac ip', 'Devices with an address'],
  ['Reserve an address', 'dhcp/resv', 'Keep the same address for a device', 'reservation static ip', 'Reserved addresses'],
  ['Firewall', 'firewall/networks', 'Networks, rules and protection', 'rules allow block zone', 'Your networks'],
  ['Who can reach whom', 'firewall/networks', 'Rules between your networks', 'rules allow deny zone lan guest iot', 'Who can reach whom'],
  ['Device groups', 'firewall/groups', 'Name a set of devices to use in rules', 'groups lists aliases', 'Device groups'],
  ['Block or allow countries', 'firewall/countries', 'Stop traffic to or from whole countries', 'country region geo block russia china', 'Block or allow countries'],
  ['Devices finding each other', 'firewall/discovery', 'Let printers, Chromecast and AirPlay work across networks', 'mdns bonjour airplay chromecast printer discovery avahi reflector', "Let networks see each other's devices"],
  ['Open a port', 'firewall/ports', 'Let something in from the internet', 'port forward forwarding nat open ports', 'Open a port to the internet'],
  ['UPnP', 'firewall/ports', 'Let devices open ports themselves', 'upnp natpmp', 'Let devices open ports by themselves'],
  ['Change history', 'firewall/history', 'What changed in the firewall and when', 'history changes audit', 'Change history'],
  ['Profiles', 'devices/profiles', 'Kids, Guests, Smart home and Work: filtering level, safe search and bedtime', 'kids parental controls bedtime schedule safe search categories adult gambling filter', 'Profiles'],
  ['Pause the internet', 'devices/profiles', 'Pause a device or a whole profile for a while', 'pause stop internet bedtime off hours resume'],
  ['Protection', 'protection/overview', 'Security check, block apps and categories, threat watching', 'ids ips suricata threat intrusion protect safe', 'Also protecting you'],
  ['Where do I block things?', 'protection/blocks', 'Which page blocks apps, lists, countries and ports', 'block where which page dns profiles firewall country port', 'Where do I block things?'],
  ['Security check', 'protection/overview', 'What to fix to make your network safer', 'security risk safe hardening audit', 'Security check'],
  ['Block apps and categories', 'protection/blocks', 'Stop an app or a kind of website for a device or hours', 'block youtube tiktok games social adult gambling parental', 'Block apps, categories and websites'],
  ['Install threat watching', 'threats/overview', 'What it does, what it costs, and the install button', 'suricata ids ips install download extra program', 'What threat watching does'],
  ['Threat watching', 'threats/alerts', 'Alerts about infected devices and break-in attempts', 'suricata ids ips intrusion detection malware alert install', 'Threat watching'],
  ['Threat settings', 'threats/settings', 'Light, Balanced, Strict or your own, and the limits', 'preset custom strict light balanced cpu memory limit restore defaults', 'How closely to watch'],
  ['Groups of threat checks', 'threats/rules', 'Turn whole kinds of checks on or off', 'rules groups scan malware policy suricata', 'Groups of checks'],
  ['Allowed threat checks', 'threats/rules', 'Things you let through, and devices never checked', 'allow trusted ignore exclude', 'Allowed checks'],
  ['Reset all threat settings', 'threats/advanced', 'Put threat watching back to how it started', 'restore defaults reset', 'Reset all threat settings'],
  ['Undo a change', 'settings/backup', 'Go back to how things were before a change', 'undo revert restore point rollback changes timeline', 'Changes'],
  ['Common setups', 'settings/general', 'Guest network, cameras, Plex from outside, UniFi', 'recipe guest cameras plex unifi how to get started finish setting up', 'Common setups'],
  ['Finish setting up', 'settings/general', 'The first-run list', 'get started checklist first run', 'Finish setting up'],
  ['My network (printable)', 'diagnostics/services', 'A printable summary of the whole network', 'summary handover print document records', 'My network'],
  ['Reach my home from anywhere', 'vpn', 'A web name and VPN to connect from outside', 'ddns duckdns noip dynamic dns remote access', 'Reach my home from anywhere'],
  ['High availability', 'settings/advanced', 'Run two machines as a pair with automatic failover', 'ha failover backup standby keepalived pair redundancy pair cable warm standby spare internet', 'High availability'],
  ['Traffic labels', 'protection/lists', 'Name the app behind each connection by looking at packets', 'ndpi dpi deep packet inspection application youtube zoom bittorrent labels settings advanced moved', 'Traffic labels'],
  ['Device mode', 'settings/general', 'Choose Router or Bridge and see which cable goes where', 'router bridge mode ports cables internet port change walkthrough', 'Device mode'],
  ['Bridge mode', 'settings/advanced', 'Filter between another router and your network without routing', 'bridge transparent filtering inline passthrough', 'Bridge mode'],
  ['Lists used for blocking', 'protection/lists', 'Which version of each blocklist is in use, and going back', 'blocklist version rollback settings advanced moved', 'Lists used for blocking'],
  ['Guest sign-in page', 'settings/advanced', 'Make guests tap Connect before they get internet', 'captive portal guest terms splash sign in hotspot', 'Guest sign-in page'],
  ['Admin activity', 'logs/admin', 'Who signed in and who changed what', 'admin activity audit sign in failed login who changed', 'Admin activity'],
  ['System log', 'diagnostics/log', 'Messages from NopeWarden and the machine it runs on', 'system log journal errors warnings messages follow', 'System log'],
  ['Is NopeWarden itself working?', 'diagnostics/services', 'One sentence on whether NopeWarden is healthy, with problems first', 'health verdict self health uptime restart disk clock', 'Is NopeWarden itself working?'],
  ['Console connections', 'diagnostics/services', 'When the console live connection opened and closed, and why', 'console connection dropped stream network error', 'Console connections'],
  ['Storage and retention', 'settings/alerts', 'How much disk the logs and history use, and when each is removed', 'storage disk space retention keep logs delete old size limit', 'Storage'],
  ['Send logs elsewhere', 'settings/alerts', 'Copy logs to a syslog server', 'syslog export siem', 'Send logs elsewhere'],
  ['Send traffic records (NetFlow / IPFIX)', 'settings/alerts', 'Tell a traffic analysis tool about connections', 'netflow ipfix flow export collector', 'Send traffic records'],
  ['Let a monitoring tool read this device (SNMP)', 'settings/alerts', 'Read only status for monitoring tools', 'snmp monitoring librenms zabbix prometheus', 'Let a monitoring tool read this device'],
  ['Alerts', 'settings/alerts', 'Be told by email or push when something happens', 'notify notification ntfy email push quiet hours new device offline', 'Alerts'],
  ['Services', 'firewall/services', 'Extra services on this machine', 'ntp time ssh'],
  ['VPN', 'vpn', 'VPN server and VPN providers', 'openvpn wireguard tunnel expressvpn remote access'],
  ['Routes', 'policies/routes', 'Send devices, websites or ports through a VPN or one or several connections', 'route vpn policy kill switch tunnel pin wan failover backup balance ntp nntps port service schedule pause', 'Your routes'],
  ['Create policy', 'policies', 'One button to make a route, firewall rule, block or port forward', 'create new policy rule route block forward add', 'Your policies'],
  ['Policies', 'policies', 'Every rule, route, block and open port in one list, in plain words', 'policies policy list all rules routes blocks ports plain words pause turn off', 'Your policies'],
  ['Pause a rule, route, block or port forward', 'policies/routes', 'Pause for a set time or until you resume; the same on every page', 'pause resume timer temporarily disable policy rule route block forward schedule', 'Your routes'],
  ['Keep a device on the same address', 'devices', 'Keep address on a device row gives it a DHCP reservation', 'dhcp reservation fixed static ip keep address release', 'Devices'],
  ['Devices & access', 'devices', 'Devices and who can sign in', 'users admin devices names approve'],
  ['Two-factor sign-in', 'settings/account', 'Ask for a code from an authenticator app', 'users admin password two-step 2fa mfa authenticator', 'Two-factor sign-in'],
  ['Where you are signed in', 'settings/account', 'See and end sign-ins', 'sessions sign out', 'Where you are signed in'],
  ['Remote login (SSH)', 'settings/account', 'Turn SSH on or off', 'ssh remote login', 'Remote login'],
  ['Logs', 'logs/log', 'What happened and when', 'events history blocked connections'],
  ['What is using the disk', 'diagnostics/services', 'The biggest places on the disk', 'disk space full storage using clear', 'What is using the disk'],
  ['Data use', 'logs/activity', 'Data used by devices, apps and sites over the last hours', 'data use activity bandwidth devices apps sites'],
  ['Map of connections', 'logs/summary', 'Where connections go, and the busiest devices and sites', 'map countries summary data used', 'Where connections go'],
  ['Settings', 'settings/general', 'Restart and reset', 'backup restore restart reboot reset power console port'],
  ['Updates', 'updates', 'Install a new version of NopeWarden and the system updates', 'upgrade version install update available restart reboot'],
  ['Update schedule', 'updates/schedule', 'When NopeWarden looks for updates and whether it installs them', 'update automatic notify schedule check hour how often to check settings advanced', 'When updates happen'],
  ['What is new', 'updates/whatsnew', 'What changed in each version', 'release notes changelog new features version', "What's new"],
  ['Where releases come from', 'updates/advanced', 'The release page updates are fetched from', 'repository repo release source github', 'Where releases come from'],
  ['Console address (port)', 'settings/general', 'Change the port this console answers on', 'port 443 https address', 'Console address'],
  ['Your place on the map', 'settings/general', 'Set where the lines on the Overview map start', 'map location home pin country zip postal code city address find lookup', 'Your place on the map'],
  ['Dark or light', 'settings/general', 'Choose the look of the console', 'appearance theme dark light', 'Appearance'],
  ['Backups', 'settings/backup', 'Save and restore your settings', 'backup restore export import', 'Backups'],
  ['Reset to default', 'settings/backup', 'Erase the settings and start again', 'factory reset wipe', 'Reset to default'],
  ['Restart or shut down', 'settings/general', 'Power controls', 'reboot power off shutdown', 'Power'],
  ['Diagnostics', 'diagnostics/services', 'How each service is doing, its log, and a terminal', 'logs services status restart support bundle troubleshoot failing', 'Services'],
  ['Network tools', 'diagnostics/tools', 'Ping, trace a route, look up a name', 'ping traceroute lookup dig tools', 'Tools'],
  ['Terminal', 'diagnostics/terminal', 'A command line on the firewall, if you switch it on', 'shell console command line ssh root', 'Terminal'],
  ['Support bundle', 'diagnostics/services', 'Save the logs and state to send when asking for help', 'logs report troubleshoot help bundle'],
  ['Management port', 'settings/general', 'Set one spare port aside for managing this device', 'management port out of band oob spare port console only lockout manage', 'Management port'],
  ['Hold new devices until you approve them', 'devices', 'A device that has never joined waits on a holding network', 'approve approval new device unknown quarantine holding network allow join', 'New devices'],
  ['Choose which lists apply to which devices', 'devices/profiles', 'Profiles: pick block lists for a profile under Advanced', 'filtering profile lists assign device group strict kids advanced', 'Profiles'],
  ['DNS settings', 'dns/settings', 'Where names are looked up and how answers are kept', 'cache ttl resolver forwarder settings answers', 'DNS settings'],
  ['Resolve directly (no forwarder)', 'dns/overview', 'Look names up yourself instead of asking a forwarder', 'recursive unbound root servers forwarder privacy resolve directly', 'How names are looked up'],
  ['Monthly data limit', 'internet/usage', 'A plan allowance for one connection, with its own billing day and warnings', 'data cap limit allowance billing day metered quota warn', 'Data limit'],
  ['Speed test history', 'internet/speed', 'Every test for the last year, one dot each', 'history results past tests graph download upload', 'Speed test history'],
  ['Speed test servers', 'internet/speed', 'Choose where the speed test sends its data', 'server ookla iperf location pick test server', 'Test servers'],
  ['Connection groups', 'internet/connections', 'Send chosen networks or devices through specific connections', 'wan group failover load balance default group pin network connection', 'Connection groups'],
  ['Traffic on each connection', 'internet/connections', 'What is flowing through each internet connection right now', 'live traffic bandwidth chart wan throughput', 'Traffic on each connection'],
  ['Add a profile', 'devices/profiles', 'Start from a preset, then change anything', 'new profile kids guests preset create', 'Add a profile'],
  ['Filtering for a whole network', 'devices/profiles', 'Choose a profile for an entire network', 'network profile default filtering lan guest', 'Filtering per network'],
  ['Your own services (ports by name)', 'firewall/services', 'Name a set of ports and use it in rules', 'custom service port tcp udp range name rule application', 'Your services'],
  ['Built-in services', 'firewall/services', 'The services NopeWarden already knows by name', 'known ports list http https ssh dns ntp builtin', 'Built in'],
  ['VPN test addresses', 'vpn', 'The addresses every VPN is checked with', 'mtu packet size probe check ping tunnel health address', 'Test addresses'],
  ['Other recurring jobs', 'updates/schedule', 'When block lists, country lists and other jobs refresh', 'schedule jobs lists countries refresh automatic daily', 'Other recurring jobs'],
  ['Software updates', 'updates', 'NopeWarden and the system update together from one button', 'update now system packages debian upgrade install', 'Software updates'],
  ['Limits on threat watching', 'threats/settings', 'Processor and memory limits for the watching program', 'cpu memory limit switch off automatically ten minutes', 'Limits'],
  ['Single threat checks', 'threats/rules', 'Find one check by a word or number and switch it off', 'sid rule signature disable enable search suricata check', 'Single checks'],
  ['Threat program log', 'threats/advanced', 'The last lines the watching program wrote', 'suricata log errors not running failed start', 'Program log'],
  ['Save or load threat tuning', 'threats/advanced', 'Your threat settings as one file', 'export import backup tuning preset file', 'Save or load your tuning'],
  ['What the firewall always does', 'firewall/rules', 'Built-in behaviour that applies before your rules, with counts', 'always on built in default drop invalid replies ping dhcp dns icmp hidden automatic rules', 'Always on'],
  ['What the firewall is doing right now', 'firewall/networks', 'The firewall described in plain words, once applied', 'summary plain words explain current rules what does it do', 'In plain words'],
  ['Find a service by name', 'firewall/services', 'See which ports a service uses', 'port number lookup which port search service name', 'Search'],
  ['Where threat watching writes its results', 'threats/advanced', 'Where NopeWarden reads what the engine noticed', 'eve json log file path suricata output', 'Where it writes'],
  ['Ports devices opened by themselves', 'firewall/ports', 'The ports devices asked for through UPnP', 'upnp open now leases mapped ports game console', 'Open now'],
];
export const STATIC = ROWS.map(([label, page, hint, words, find]) => ({ label, page, hint, words: words || '', find: find || '', kind: page.includes('/') ? 'Setting' : 'Page' }));

// After a result opens its page, scroll to the card it named and flash it so the eye lands there.
function flash(find) {
  if (!find) return;
  const want = find.toLowerCase();
  let tries = 0;
  const look = () => {
    const els = document.querySelectorAll('.main .ch, .main h2, .main h3, .main .name, .main legend');
    const hit = [...els].find((e) => (e.textContent || '').trim().toLowerCase().startsWith(want)) || [...els].find((e) => (e.textContent || '').toLowerCase().includes(want));
    if (hit) {
      const box = hit.closest('.card') || hit;
      let quiet = false;
      try { quiet = window.matchMedia('(prefers-reduced-motion: reduce)').matches; } catch { /* default */ }
      box.scrollIntoView({ block: 'center', behavior: quiet ? 'auto' : 'smooth' });
      box.classList.add('srch-hit');
      setTimeout(() => box.classList.remove('srch-hit'), 2800);
    } else if (++tries < 20) setTimeout(look, 150);
  };
  setTimeout(look, 150);
}

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

const SECTIONS = [['Page', 'Pages', 5], ['Setting', 'Settings', 8], ['Device', 'Devices', 5], ['Network', 'Networks', 4], ['Help', 'Help', 5]];

export function SearchBox({ onClose, go }) {
  const [q, setQ] = useState('');
  const [sel, setSel] = useState(0);
  const [extra, setExtra] = useState([]);
  const [help, setHelp] = useState([]);
  const input = useRef(null);
  useEffect(() => {
    input.current?.focus();
    let live = true;
    (async () => {
      const out = [];
      try {
        const zones = await get('/api/fw/zones');
        (zones || []).forEach((z) => out.push({ label: z.name, page: 'networks', hint: 'Your network', words: '', find: z.name, kind: 'Network' }));
      } catch { /* search still works without it */ }
      try {
        const d = await get('/api/devices');
        (d?.devices || []).forEach((x) => out.push({ label: x.name || x.hostname || x.mac, page: 'devices', hint: `${x.ip || ''} · ${x.mac || ''}${x.network ? ` · ${x.network}` : ''}`, words: `${x.ip || ''} ${x.mac || ''} ${x.hostname || ''} ${x.vendor || ''}`, find: '', device: x.name || x.hostname || x.mac, kind: 'Device' }));
      } catch { /* same */ }
      try {
        const h = await fetch('/help/search.json', { credentials: 'same-origin' });
        if (h.ok) {
          const list = await h.json();
          if (live) setHelp(list.map((x) => ({ label: /^(What it is for|What is on the page|Common tasks|If something goes wrong)$/.test(x.heading) ? `${x.chapter}: ${x.heading}` : x.heading, page: '', hint: x.chapter, words: x.text, href: `/help/${x.file}.html#${x.slug}`, kind: 'Help' })));
        }
      } catch { /* help search is a bonus */ }
      if (live) setExtra(out);
    })();
    return () => { live = false; };
  }, []);
  const groups = useMemo(() => {
    const words = q.toLowerCase().split(/\s+/).filter(Boolean);
    if (!words.length) return [['Pages', STATIC.filter((r) => r.kind === 'Page').slice(0, 8)]];
    const all = [...STATIC, ...extra, ...help];
    const scored = all.map((r) => {
      const hay = `${r.label} ${r.hint} ${r.words}`.toLowerCase();
      if (!words.every((w) => hay.includes(w))) return null;
      const lab = r.label.toLowerCase();
      return { r, score: (words.every((w) => lab.startsWith(w)) ? 0 : words.every((w) => lab.includes(w)) ? 1 : 2) };
    }).filter(Boolean).sort((a, b) => a.score - b.score);
    return SECTIONS.map(([kind, title, max]) => [title, scored.filter((x) => x.r.kind === kind).slice(0, max).map((x) => x.r)]).filter(([, rows]) => rows.length);
  }, [q, extra, help]);
  const results = useMemo(() => groups.flatMap(([, rows]) => rows), [groups]);
  useEffect(() => setSel(0), [q]);
  const pick = (r) => {
    onClose();
    if (r.href) { window.open(r.href, '_blank', 'noopener'); return; }
    if (r.device) { try { sessionStorage.setItem('gh.find.device', r.device); } catch { /* the page opens unfiltered */ } window.dispatchEvent(new Event('gh-find-device')); }
    go(r.page);
    flash(r.find);
  };
  const key = (e) => {
    if (e.key === 'Escape') onClose();
    else if (e.key === 'ArrowDown') { e.preventDefault(); setSel((s) => Math.min(results.length - 1, s + 1)); }
    else if (e.key === 'ArrowUp') { e.preventDefault(); setSel((s) => Math.max(0, s - 1)); }
    else if (e.key === 'Enter' && results[sel]) { e.preventDefault(); pick(results[sel]); }
  };
  let n = -1;
  return (
    <div className="srch-back" onMouseDown={(e) => { if (e.target === e.currentTarget) onClose(); }} role="presentation">
      <div className="srch" role="dialog" aria-label="Search">
        <div className="srch-in"><Icon name="search" />
          <input ref={input} type="search" data-noenter value={q} onChange={(e) => setQ(e.target.value)} onKeyDown={key} placeholder="Search pages, settings, devices, networks, help…" aria-label="Search" />
        </div>
        <div className="srch-list" role="listbox">
          {groups.map(([title, rows]) => (
            <div key={title}>
              <div className="srch-head small muted">{title}</div>
              {rows.map((r) => {
                n += 1;
                const i = n;
                return (
                  <button type="button" key={`${r.kind}-${r.label}-${r.page}-${r.href || ''}-${i}`} role="option" aria-selected={i === sel} className={i === sel ? 'on' : ''} onMouseEnter={() => setSel(i)} onClick={() => pick(r)}>
                    <span><b>{r.label}</b><span className="small muted"> {r.hint}</span></span>
                    <span className="small muted">{r.kind === 'Setting' ? where(r.page) : r.kind === 'Help' ? 'Opens help' : r.kind}</span>
                  </button>
                );
              })}
            </div>
          ))}
          {!results.length && <div className="srch-none small muted">Nothing matches "{q}". Try a shorter word.</div>}
        </div>
      </div>
    </div>
  );
}

// "dns/lists" reads as "DNS" in the list.
const NAMES = { internet: 'Internet', networks: 'Networks', dns: 'DNS', dhcp: 'DHCP', firewall: 'Firewall', protection: 'Protection', threats: 'Threats', services: 'Services', vpn: 'VPN', policies: 'Policies', profiles: 'Profiles', devices: 'Devices', logs: 'Logs', diagnostics: 'Diagnostics', settings: 'Settings', overview: 'Overview' };
const where = (p) => NAMES[p.split('/')[0]] || p;

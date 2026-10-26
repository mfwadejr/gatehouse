package store

// Each entry is one migration. Never edit an applied migration; append a new one.
var configMigrations = []string{`
CREATE TABLE users(
  id INTEGER PRIMARY KEY,
  username TEXT NOT NULL UNIQUE COLLATE NOCASE,
  pass_hash TEXT NOT NULL,
  role TEXT NOT NULL DEFAULT 'admin',
  totp_secret TEXT NOT NULL DEFAULT '',
  totp_enabled INTEGER NOT NULL DEFAULT 0,
  totp_last_step INTEGER NOT NULL DEFAULT 0,
  failed_count INTEGER NOT NULL DEFAULT 0,
  locked_until INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL,
  last_login INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE recovery_codes(
  id INTEGER PRIMARY KEY,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  code_hash TEXT NOT NULL,
  used_at INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE sessions(
  id TEXT PRIMARY KEY,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at INTEGER NOT NULL,
  last_seen INTEGER NOT NULL,
  expires_at INTEGER NOT NULL,
  ip TEXT NOT NULL DEFAULT '',
  ua TEXT NOT NULL DEFAULT '',
  mfa INTEGER NOT NULL DEFAULT 0,
  csrf TEXT NOT NULL DEFAULT ''
);
CREATE TABLE settings(key TEXT PRIMARY KEY, value TEXT NOT NULL);

CREATE TABLE zones(
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  kind TEXT NOT NULL DEFAULT 'lan',
  gateway_mode TEXT NOT NULL DEFAULT 'gateway',
  default_action TEXT NOT NULL DEFAULT 'drop',
  intra_allow INTEGER NOT NULL DEFAULT 1,
  isolation INTEGER NOT NULL DEFAULT 0,
  management INTEGER NOT NULL DEFAULT 0,
  upnp INTEGER NOT NULL DEFAULT 0,
  internet INTEGER NOT NULL DEFAULT 1,
  ssh INTEGER NOT NULL DEFAULT 0,
  description TEXT NOT NULL DEFAULT ''
);
CREATE TABLE interfaces(
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  kind TEXT NOT NULL DEFAULT 'phys',
  parent TEXT NOT NULL DEFAULT '',
  vlan_id INTEGER NOT NULL DEFAULT 0,
  zone_id INTEGER REFERENCES zones(id) ON DELETE SET NULL,
  ipv4_cidr TEXT NOT NULL DEFAULT '',
  ipv6_cidr TEXT NOT NULL DEFAULT '',
  ipv6_pd INTEGER NOT NULL DEFAULT 0,
  mtu INTEGER NOT NULL DEFAULT 0,
  enabled INTEGER NOT NULL DEFAULT 1
);
CREATE TABLE wan_links(
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  iface TEXT NOT NULL,
  type TEXT NOT NULL DEFAULT 'dhcp',
  vlan_id INTEGER NOT NULL DEFAULT 0,
  ipv4_addr TEXT NOT NULL DEFAULT '',
  ipv4_gw TEXT NOT NULL DEFAULT '',
  dns TEXT NOT NULL DEFAULT '',
  pppoe_user TEXT NOT NULL DEFAULT '',
  pppoe_pass TEXT NOT NULL DEFAULT '',
  ipv6_mode TEXT NOT NULL DEFAULT 'none',
  ipv6_addr TEXT NOT NULL DEFAULT '',
  ipv6_gw TEXT NOT NULL DEFAULT '',
  role TEXT NOT NULL DEFAULT 'primary',
  priority INTEGER NOT NULL DEFAULT 1,
  weight INTEGER NOT NULL DEFAULT 1,
  check_kind TEXT NOT NULL DEFAULT 'icmp',
  check_target TEXT NOT NULL DEFAULT '1.1.1.1',
  check_interval INTEGER NOT NULL DEFAULT 5,
  check_failures INTEGER NOT NULL DEFAULT 3,
  mtu INTEGER NOT NULL DEFAULT 0,
  nat INTEGER NOT NULL DEFAULT 1,
  upnp INTEGER NOT NULL DEFAULT 0,
  enabled INTEGER NOT NULL DEFAULT 1
);

CREATE TABLE groups(
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  filtering INTEGER NOT NULL DEFAULT 1,
  description TEXT NOT NULL DEFAULT ''
);
CREATE TABLE devices(
  id INTEGER PRIMARY KEY,
  mac TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL DEFAULT '',
  group_id INTEGER REFERENCES groups(id) ON DELETE SET NULL,
  policy TEXT NOT NULL DEFAULT 'filtered',
  internet INTEGER NOT NULL DEFAULT 1,
  schedule TEXT NOT NULL DEFAULT '',
  upnp_allowed INTEGER NOT NULL DEFAULT 0,
  notes TEXT NOT NULL DEFAULT '',
  first_seen INTEGER NOT NULL DEFAULT 0,
  last_seen INTEGER NOT NULL DEFAULT 0,
  last_ip TEXT NOT NULL DEFAULT ''
);

CREATE TABLE blocklists(
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  url TEXT NOT NULL DEFAULT '',
  format TEXT NOT NULL DEFAULT 'auto',
  enabled INTEGER NOT NULL DEFAULT 1,
  interval_hours INTEGER NOT NULL DEFAULT 24,
  last_fetch INTEGER NOT NULL DEFAULT 0,
  last_ok INTEGER NOT NULL DEFAULT 0,
  entries INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL DEFAULT 'new',
  error TEXT NOT NULL DEFAULT '',
  local INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE group_blocklists(
  group_id INTEGER NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
  blocklist_id INTEGER NOT NULL REFERENCES blocklists(id) ON DELETE CASCADE,
  PRIMARY KEY(group_id, blocklist_id)
);
CREATE TABLE custom_rules(
  id INTEGER PRIMARY KEY,
  domain TEXT NOT NULL,
  action TEXT NOT NULL,
  note TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL DEFAULT 0,
  UNIQUE(domain, action)
);
CREATE TABLE dns_records(
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  type TEXT NOT NULL,
  value TEXT NOT NULL,
  ttl INTEGER NOT NULL DEFAULT 300,
  UNIQUE(name, type, value)
);
CREATE TABLE dns_upstreams(
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL DEFAULT '',
  address TEXT NOT NULL UNIQUE,
  proto TEXT NOT NULL DEFAULT 'udp',
  tls_name TEXT NOT NULL DEFAULT '',
  enabled INTEGER NOT NULL DEFAULT 1
);

CREATE TABLE dhcp_scopes(
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  iface TEXT NOT NULL,
  mode TEXT NOT NULL DEFAULT 'serve',
  subnet TEXT NOT NULL,
  range_start TEXT NOT NULL DEFAULT '',
  range_end TEXT NOT NULL DEFAULT '',
  router TEXT NOT NULL DEFAULT '',
  dns TEXT NOT NULL DEFAULT '',
  domain TEXT NOT NULL DEFAULT '',
  ntp TEXT NOT NULL DEFAULT '',
  lease_seconds INTEGER NOT NULL DEFAULT 86400,
  relay_addr TEXT NOT NULL DEFAULT '',
  enabled INTEGER NOT NULL DEFAULT 1
);
CREATE TABLE dhcp_reservations(
  id INTEGER PRIMARY KEY,
  scope_id INTEGER NOT NULL REFERENCES dhcp_scopes(id) ON DELETE CASCADE,
  mac TEXT NOT NULL,
  ip TEXT NOT NULL,
  hostname TEXT NOT NULL DEFAULT '',
  UNIQUE(scope_id, mac),
  UNIQUE(scope_id, ip)
);
CREATE TABLE dhcp_leases(
  scope_id INTEGER NOT NULL REFERENCES dhcp_scopes(id) ON DELETE CASCADE,
  ip TEXT NOT NULL,
  mac TEXT NOT NULL,
  hostname TEXT NOT NULL DEFAULT '',
  client_id TEXT NOT NULL DEFAULT '',
  start INTEGER NOT NULL,
  expires INTEGER NOT NULL,
  state TEXT NOT NULL DEFAULT 'active',
  PRIMARY KEY(scope_id, ip)
);
CREATE INDEX idx_leases_mac ON dhcp_leases(mac);

CREATE TABLE fw_objects(
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  kind TEXT NOT NULL,
  members TEXT NOT NULL DEFAULT '[]',
  source_url TEXT NOT NULL DEFAULT '',
  refresh_hours INTEGER NOT NULL DEFAULT 0,
  last_refresh INTEGER NOT NULL DEFAULT 0,
  protected INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE fw_rules(
  id INTEGER PRIMARY KEY,
  from_zone TEXT NOT NULL,
  to_zone TEXT NOT NULL,
  position INTEGER NOT NULL,
  name TEXT NOT NULL,
  src TEXT NOT NULL DEFAULT 'any',
  dst TEXT NOT NULL DEFAULT 'any',
  service TEXT NOT NULL DEFAULT 'any',
  action TEXT NOT NULL DEFAULT 'drop',
  limit_rate INTEGER NOT NULL DEFAULT 0,
  log INTEGER NOT NULL DEFAULT 0,
  enabled INTEGER NOT NULL DEFAULT 1,
  UNIQUE(from_zone, to_zone, name)
);
CREATE TABLE fw_revisions(
  version INTEGER PRIMARY KEY,
  created_at INTEGER NOT NULL,
  author TEXT NOT NULL DEFAULT '',
  summary TEXT NOT NULL DEFAULT '',
  ruleset TEXT NOT NULL,
  model TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'pending'
);
CREATE TABLE nat_forwards(
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  wan TEXT NOT NULL DEFAULT '',
  proto TEXT NOT NULL DEFAULT 'tcp',
  ext_port TEXT NOT NULL,
  int_addr TEXT NOT NULL,
  int_port TEXT NOT NULL DEFAULT '',
  enabled INTEGER NOT NULL DEFAULT 1
);
CREATE TABLE upnp_allow(
  mac TEXT PRIMARY KEY,
  note TEXT NOT NULL DEFAULT ''
);
`, `
CREATE TABLE vpn_wg(
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  mode TEXT NOT NULL DEFAULT 'server',
  listen_port INTEGER NOT NULL DEFAULT 51820,
  address TEXT NOT NULL,
  private_key TEXT NOT NULL,
  public_key TEXT NOT NULL,
  endpoint_host TEXT NOT NULL DEFAULT '',
  dns TEXT NOT NULL DEFAULT '',
  mtu INTEGER NOT NULL DEFAULT 0,
  enabled INTEGER NOT NULL DEFAULT 1
);
CREATE TABLE vpn_wg_peers(
  id INTEGER PRIMARY KEY,
  wg_id INTEGER NOT NULL REFERENCES vpn_wg(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  public_key TEXT NOT NULL,
  psk TEXT NOT NULL DEFAULT '',
  address TEXT NOT NULL DEFAULT '',
  allowed_ips TEXT NOT NULL DEFAULT '',
  endpoint TEXT NOT NULL DEFAULT '',
  keepalive INTEGER NOT NULL DEFAULT 25,
  enabled INTEGER NOT NULL DEFAULT 1,
  created_at INTEGER NOT NULL DEFAULT 0,
  UNIQUE(wg_id, name),
  UNIQUE(wg_id, public_key)
);
`, `
ALTER TABLE zones ADD COLUMN dns_force INTEGER NOT NULL DEFAULT 1;
`, `
ALTER TABLE interfaces ADD COLUMN dhcp INTEGER NOT NULL DEFAULT 0;
ALTER TABLE interfaces ADD COLUMN gateway TEXT NOT NULL DEFAULT '';
ALTER TABLE interfaces ADD COLUMN dns TEXT NOT NULL DEFAULT '';
ALTER TABLE interfaces ADD COLUMN members TEXT NOT NULL DEFAULT '';
`, `
ALTER TABLE vpn_wg ADD COLUMN label TEXT NOT NULL DEFAULT '';
ALTER TABLE vpn_wg ADD COLUMN zone_id INTEGER;
ALTER TABLE vpn_wg ADD COLUMN route_all INTEGER NOT NULL DEFAULT 0;
ALTER TABLE vpn_wg ADD COLUMN killswitch INTEGER NOT NULL DEFAULT 1;
ALTER TABLE vpn_wg ADD COLUMN check_target TEXT NOT NULL DEFAULT '';
ALTER TABLE vpn_wg ADD COLUMN created_at INTEGER NOT NULL DEFAULT 0;
CREATE TABLE vpn_ovpn(
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,            -- the device, tun0, tun1, ...
  label TEXT NOT NULL DEFAULT '',
  mode TEXT NOT NULL DEFAULT 'server',  -- server or client
  enabled INTEGER NOT NULL DEFAULT 1,
  proto TEXT NOT NULL DEFAULT 'udp',
  port INTEGER NOT NULL DEFAULT 1194,
  subnet TEXT NOT NULL DEFAULT '',
  endpoint_host TEXT NOT NULL DEFAULT '',
  route_all INTEGER NOT NULL DEFAULT 0,
  dns TEXT NOT NULL DEFAULT '',
  zone_id INTEGER,
  killswitch INTEGER NOT NULL DEFAULT 1,
  check_target TEXT NOT NULL DEFAULT '',
  ca_cert TEXT NOT NULL DEFAULT '',
  ca_key TEXT NOT NULL DEFAULT '',      -- sealed
  srv_cert TEXT NOT NULL DEFAULT '',
  srv_key TEXT NOT NULL DEFAULT '',     -- sealed
  tls_crypt TEXT NOT NULL DEFAULT '',   -- sealed
  crl TEXT NOT NULL DEFAULT '',
  crl_number INTEGER NOT NULL DEFAULT 0,
  profile TEXT NOT NULL DEFAULT '',     -- client mode: the imported file, sealed
  auth_user TEXT NOT NULL DEFAULT '',
  auth_pass TEXT NOT NULL DEFAULT '',   -- sealed
  created_at INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE vpn_ovpn_users(
  id INTEGER PRIMARY KEY,
  ovpn_id INTEGER NOT NULL REFERENCES vpn_ovpn(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  serial TEXT NOT NULL,
  cert TEXT NOT NULL,
  created_at INTEGER NOT NULL DEFAULT 0,
  revoked_at INTEGER NOT NULL DEFAULT 0,
  UNIQUE(ovpn_id, name)
);
-- What each outbound tunnel carries: a whole network, one device, or an address range.
CREATE TABLE vpn_routes(
  id INTEGER PRIMARY KEY,
  tunnel TEXT NOT NULL,                 -- wg1, tun1, ...
  kind TEXT NOT NULL,                   -- network, device, cidr
  value TEXT NOT NULL,                  -- zone name, MAC address, or prefix
  label TEXT NOT NULL DEFAULT '',
  UNIQUE(tunnel, kind, value)
);
`, `
-- Named sets of internet connections. The Default set is implicit: every connection, in the
-- order and shares kept on wan_links. These are the extra sets that chosen sources use instead.
CREATE TABLE wan_groups(
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  mode TEXT NOT NULL DEFAULT 'failover',     -- failover or balance
  fallback TEXT NOT NULL DEFAULT 'default',  -- when nothing in the set works: default or block
  position INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE wan_group_members(
  group_id INTEGER NOT NULL REFERENCES wan_groups(id) ON DELETE CASCADE,
  wan_id INTEGER NOT NULL REFERENCES wan_links(id) ON DELETE CASCADE,
  position INTEGER NOT NULL DEFAULT 0,
  weight INTEGER NOT NULL DEFAULT 50,
  PRIMARY KEY(group_id, wan_id)
);
CREATE TABLE wan_group_routes(
  id INTEGER PRIMARY KEY,
  group_id INTEGER NOT NULL REFERENCES wan_groups(id) ON DELETE CASCADE,
  kind TEXT NOT NULL,   -- network, device, cidr
  value TEXT NOT NULL,
  label TEXT NOT NULL DEFAULT '',
  UNIQUE(group_id, kind, value)
);
`, `
-- Wireless cards: each one either joins a network (client) or broadcasts one (ap).
CREATE TABLE wifi_radios(
  iface TEXT PRIMARY KEY,
  mode TEXT NOT NULL DEFAULT 'client',
  ssid TEXT NOT NULL DEFAULT '',
  psk TEXT NOT NULL DEFAULT '',          -- sealed
  country TEXT NOT NULL DEFAULT '',
  band TEXT NOT NULL DEFAULT '5',
  channel INTEGER NOT NULL DEFAULT 0,
  hidden INTEGER NOT NULL DEFAULT 0,
  wpa3 INTEGER NOT NULL DEFAULT 0,
  fast INTEGER NOT NULL DEFAULT 0,
  enabled INTEGER NOT NULL DEFAULT 1
);
`, `
-- A WireGuard link between two sites: both ends route to each other's networks.
ALTER TABLE vpn_wg ADD COLUMN site INTEGER NOT NULL DEFAULT 0;
`, `
-- Firewall rules: who and what they cover, and when. Devices and groups are keyed by MAC.
ALTER TABLE fw_rules ADD COLUMN ipver TEXT NOT NULL DEFAULT '';      -- '', '4' or '6'
ALTER TABLE fw_rules ADD COLUMN ctstate TEXT NOT NULL DEFAULT '';    -- '' (any) or 'new'
ALTER TABLE fw_rules ADD COLUMN sched TEXT NOT NULL DEFAULT '';      -- JSON, empty for always
ALTER TABLE fw_rules ADD COLUMN descr TEXT NOT NULL DEFAULT '';
ALTER TABLE fw_rules ADD COLUMN also_mac INTEGER NOT NULL DEFAULT 0;
CREATE TABLE device_groups(
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL UNIQUE COLLATE NOCASE,
  reserved_only INTEGER NOT NULL DEFAULT 0,
  description TEXT NOT NULL DEFAULT ''
);
CREATE TABLE device_group_members(
  group_id INTEGER NOT NULL REFERENCES device_groups(id) ON DELETE CASCADE,
  mac TEXT NOT NULL,
  PRIMARY KEY(group_id, mac)
);
-- How often each rule matched, kept across firewall reloads (which restart the kernel counters).
CREATE TABLE fw_hits(
  key TEXT PRIMARY KEY,
  hits INTEGER NOT NULL DEFAULT 0,
  bytes INTEGER NOT NULL DEFAULT 0,
  last_hit INTEGER NOT NULL DEFAULT 0
);
`, `
-- A device's icon: a built-in name, or an uploaded picture.
ALTER TABLE devices ADD COLUMN icon TEXT NOT NULL DEFAULT '';
ALTER TABLE devices ADD COLUMN icon_id INTEGER NOT NULL DEFAULT 0;
CREATE TABLE device_icons(
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL DEFAULT '',
  mime TEXT NOT NULL,
  data BLOB NOT NULL,
  created_at INTEGER NOT NULL DEFAULT 0
);
`, `
-- The order the person wants DNS servers asked in (1 is first).
ALTER TABLE dns_upstreams ADD COLUMN position INTEGER NOT NULL DEFAULT 0;
UPDATE dns_upstreams SET position = id;
`, `
-- Forced DNS, finer than on or off. A network can send its lookups to its own server instead of
-- the router-wide one; a device or group can be left alone, forced to this router, or sent to a server.
CREATE TABLE dns_zone_targets(
  zone_id INTEGER PRIMARY KEY REFERENCES zones(id) ON DELETE CASCADE,
  target TEXT NOT NULL DEFAULT ''
);
CREATE TABLE dns_exceptions(
  id INTEGER PRIMARY KEY,
  who TEXT NOT NULL UNIQUE,            -- dev:aa:bb:cc:dd:ee:ff or grp:Name
  mode TEXT NOT NULL,                  -- off, gatehouse or server
  target TEXT NOT NULL DEFAULT ''
);
`, `
-- An OpenVPN device can be switched off without being deleted: the server's revocation list holds it
-- out while it is paused, and lets it back in when it is not.
ALTER TABLE vpn_ovpn_users ADD COLUMN paused INTEGER NOT NULL DEFAULT 0;
`}

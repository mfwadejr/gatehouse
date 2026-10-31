package store

var logMigrations = []string{`
CREATE TABLE dns_queries(
  id INTEGER PRIMARY KEY,
  ts INTEGER NOT NULL,
  client TEXT NOT NULL,
  client_name TEXT NOT NULL DEFAULT '',
  qname TEXT NOT NULL,
  qtype TEXT NOT NULL,
  result TEXT NOT NULL,
  rcode TEXT NOT NULL DEFAULT '',
  upstream TEXT NOT NULL DEFAULT '',
  latency_us INTEGER NOT NULL DEFAULT 0,
  rule TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_dnsq_ts ON dns_queries(ts);
CREATE INDEX idx_dnsq_client ON dns_queries(client, ts);
CREATE INDEX idx_dnsq_name ON dns_queries(qname, ts);
CREATE TABLE dns_daily(
  day INTEGER NOT NULL,
  key TEXT NOT NULL,
  kind TEXT NOT NULL,
  n INTEGER NOT NULL,
  PRIMARY KEY(day, kind, key)
);
CREATE TABLE fw_events(
  id INTEGER PRIMARY KEY,
  ts INTEGER NOT NULL,
  action TEXT NOT NULL,
  from_zone TEXT NOT NULL DEFAULT '',
  from_addr TEXT NOT NULL DEFAULT '',
  to_zone TEXT NOT NULL DEFAULT '',
  to_addr TEXT NOT NULL DEFAULT '',
  proto TEXT NOT NULL DEFAULT '',
  dport INTEGER NOT NULL DEFAULT 0,
  rule TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_fw_ts ON fw_events(ts);
CREATE TABLE events(
  id INTEGER PRIMARY KEY,
  ts INTEGER NOT NULL,
  area TEXT NOT NULL,
  severity TEXT NOT NULL DEFAULT 'info',
  actor TEXT NOT NULL DEFAULT '',
  title TEXT NOT NULL,
  detail TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_events_ts ON events(ts);
CREATE TABLE metrics(
  ts INTEGER NOT NULL,
  key TEXT NOT NULL,
  value REAL NOT NULL,
  PRIMARY KEY(key, ts)
);
`, `
ALTER TABLE fw_events ADD COLUMN sport INTEGER NOT NULL DEFAULT 0;
ALTER TABLE fw_events ADD COLUMN src_name TEXT NOT NULL DEFAULT '';
ALTER TABLE fw_events ADD COLUMN dst_name TEXT NOT NULL DEFAULT '';
ALTER TABLE fw_events ADD COLUMN reason TEXT NOT NULL DEFAULT '';
CREATE INDEX idx_fw_src ON fw_events(from_addr, ts);
CREATE INDEX idx_fw_dst ON fw_events(to_addr, ts);
`, `
-- Data used, in buckets. mac is empty for the whole internet connection (one-minute buckets);
-- devices are kept in five-minute buckets. up is from the device, down is to it.
CREATE TABLE traffic(
  ts INTEGER NOT NULL,
  mac TEXT NOT NULL,
  up INTEGER NOT NULL DEFAULT 0,
  down INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY(ts, mac)
);
CREATE INDEX idx_traffic_mac ON traffic(mac, ts);
`, `
-- Speed tests, one row per run. A test that could not finish is kept too, with the reason.
CREATE TABLE speedtests(
  id INTEGER PRIMARY KEY,
  ts INTEGER NOT NULL,
  link TEXT NOT NULL,
  source TEXT NOT NULL DEFAULT 'manual',
  quick INTEGER NOT NULL DEFAULT 0,
  server TEXT NOT NULL DEFAULT '',
  down_bps REAL NOT NULL DEFAULT 0,
  up_bps REAL NOT NULL DEFAULT 0,
  delay_ms REAL NOT NULL DEFAULT 0,
  jitter_ms REAL NOT NULL DEFAULT 0,
  loaded_ms REAL NOT NULL DEFAULT 0,
  grade TEXT NOT NULL DEFAULT '',
  bytes INTEGER NOT NULL DEFAULT 0,
  error TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_speed_link ON speedtests(link, ts);
`, `
-- Data sent to and received from each country, in five-minute buckets.
CREATE TABLE country_traffic(
  ts INTEGER NOT NULL,
  cc TEXT NOT NULL,
  up INTEGER NOT NULL DEFAULT 0,
  down INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY(ts, cc)
);
`, `
-- Times the internet was down, for the provider report. end is 0 while it still is.
CREATE TABLE outages(
  id INTEGER PRIMARY KEY,
  start INTEGER NOT NULL,
  end INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_outages_start ON outages(start);
`, `
-- The port a new connection left through, so the log can say which connection it used.
ALTER TABLE fw_events ADD COLUMN out_if TEXT NOT NULL DEFAULT '';
`}

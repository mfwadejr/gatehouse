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
`}

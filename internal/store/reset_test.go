package store

import (
	"path/filepath"
	"testing"
)

func countOf(t *testing.T, db *DB, table string) int {
	var n int
	if err := db.QueryRow("SELECT count(*) FROM " + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func filled(t *testing.T) *DB {
	db, err := OpenConfig(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	for _, q := range []string{
		`INSERT INTO users(username,pass_hash,role,created_at) VALUES('admin','x','admin',1)`,
		`INSERT INTO zones(name,kind) VALUES('Home','trusted')`,
		`INSERT INTO interfaces(name,kind,zone_id) VALUES('eth1','phys',1)`,
		`INSERT INTO dhcp_scopes(name,iface,subnet,range_start,range_end,router) VALUES('Home','eth1','192.168.1.0/24','192.168.1.100','192.168.1.200','192.168.1.1')`,
		`INSERT INTO dns_upstreams(name,address) VALUES('x','1.1.1.1:53')`,
		`INSERT INTO groups(name) VALUES('Default')`,
		`INSERT INTO devices(mac,name) VALUES('aa:bb:cc:dd:ee:ff','Laptop')`,
		`INSERT INTO settings(key,value) VALUES('update.repo','a/b'),('update.mode','auto'),('system.timezone','UTC'),('fw.stepup','1'),('dns.anonymize','1'),('mgmt.allow','10.0.0.0/8'),('net.applied','x'),('setup.wizard','done'),('console.port','8443'),('auth.mfa_required','1')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	return db
}

func settingsOf(t *testing.T, db *DB) map[string]bool {
	rows, err := db.Query(`SELECT key FROM settings`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var k string
		rows.Scan(&k)
		out[k] = true
	}
	return out
}

func TestNetworkResetKeepsAccountsAndUpdateSettings(t *testing.T) {
	db := filled(t)
	if err := ResetConfig(db, ResetNetwork); err != nil {
		t.Fatal(err)
	}
	for _, tb := range []string{"zones", "interfaces", "dhcp_scopes", "dns_upstreams", "groups"} {
		if n := countOf(t, db, tb); n != 0 {
			t.Errorf("%s still has %d rows", tb, n)
		}
	}
	if countOf(t, db, "users") != 1 || countOf(t, db, "devices") != 1 {
		t.Fatal("a network reset removed the account or the device names")
	}
	s := settingsOf(t, db)
	for _, k := range []string{"update.repo", "update.mode", "system.timezone", "fw.stepup", "console.port", "auth.mfa_required"} {
		if !s[k] {
			t.Errorf("%s was removed", k)
		}
	}
	for _, k := range []string{"dns.anonymize", "mgmt.allow", "net.applied", "setup.wizard"} {
		if s[k] {
			t.Errorf("%s survived the reset", k)
		}
	}
}

func TestFactoryResetKeepsOnlyTheReleaseSource(t *testing.T) {
	db := filled(t)
	if err := ResetConfig(db, ResetFactory); err != nil {
		t.Fatal(err)
	}
	for _, tb := range []string{"users", "devices", "zones", "groups", "dhcp_scopes"} {
		if n := countOf(t, db, tb); n != 0 {
			t.Errorf("%s still has %d rows", tb, n)
		}
	}
	s := settingsOf(t, db)
	if len(s) != 1 || !s["update.repo"] {
		t.Fatalf("settings left: %v", s)
	}
}

func TestResetRejectsUnknownLevel(t *testing.T) {
	if err := ResetConfig(filled(t), "everything"); err == nil {
		t.Fatal("accepted an unknown level")
	}
}

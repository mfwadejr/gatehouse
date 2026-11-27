package app

import (
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
)

// A network is stored by name in several places. Renaming it from the console must move every one of
// them, and a scan of the whole database must then find no trace of the old name anywhere except in
// the places that are history on purpose.
func TestRenamingANetworkUpdatesEveryReference(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	a := c.app

	if rc, _, raw := c.call("POST", "/api/networks/add", map[string]any{"name": "Zorbify", "port": "eth2", "internet": true}); rc != 200 {
		t.Fatalf("add: %d %s", rc, raw)
	}
	zones, _ := fwZones(t, c)
	var id int64
	for _, z := range zones {
		if z["name"] == "Zorbify" {
			id = int64(z["id"].(float64))
		}
	}
	if id == 0 {
		t.Fatal("the new network has no firewall network")
	}

	exec := func(db *sql.DB, q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatalf("seed %q: %v", q, err)
		}
	}
	cfg, logs := a.Config.DB, a.Logs.DB
	// Firewall rules in both directions.
	exec(cfg, `INSERT INTO fw_rules(from_zone,to_zone,position,name) VALUES('Zorbify','Internet',1,'out'),('Internet','Zorbify',2,'in')`)
	// What a VPN tunnel and a route carry, a DNS profile, blocks, speed control, port-group routes.
	exec(cfg, `INSERT INTO vpn_routes(tunnel,kind,value) VALUES('wg1','network','Zorbify')`)
	exec(cfg, `INSERT INTO groups(id,name) VALUES(77,'Kids2')`)
	exec(cfg, `INSERT INTO network_profiles(zone,group_id) VALUES('Zorbify',77)`)
	exec(cfg, `INSERT INTO block_sets(name,targets) VALUES('b','[{"scope":"network","target":"Zorbify"},{"scope":"device","target":"aa:bb"}]')`)
	exec(cfg, `INSERT INTO speed_rules(name,targets) VALUES('s','[{"scope":"network","target":"Zorbify"}]')`)
	exec(cfg, `INSERT INTO routes(name,target_kind,target,srcs) VALUES('r','tunnel','wg1','[{"kind":"network","value":"Zorbify"},{"kind":"device","value":"aa:bb"}]')`)
	exec(cfg, `INSERT INTO wan_groups(id,name) VALUES(88,'wg')`)
	exec(cfg, `INSERT INTO wan_group_routes(group_id,kind,value) VALUES(88,'network','Zorbify')`)
	exec(cfg, `INSERT INTO scope_rules(kind,value,scope,target) VALUES('name','x.test','network','Zorbify')`)
	exec(cfg, `INSERT INTO settings(key,value) VALUES('fw.mdns','{"pairs":[{"a":"Zorbify","b":"Other"}]}') ON CONFLICT(key) DO UPDATE SET value=excluded.value`)
	exec(cfg, `INSERT INTO settings(key,value) VALUES('fw.mdns_ui','{"pairs":[{"a":"Other","b":"Zorbify"}],"kinds":[]}') ON CONFLICT(key) DO UPDATE SET value=excluded.value`)
	// History: the firewall log and the network's graph.
	exec(logs, `INSERT INTO fw_events(ts,action,from_zone,to_zone) VALUES(1,'drop','Zorbify','Internet'),(2,'drop','Internet','Zorbify')`)
	exec(logs, `INSERT INTO metrics(ts,key,value) VALUES(1,'net.Zorbify.down',5),(1,'net.Zorbify.up',6)`)

	var z map[string]any
	for _, x := range zones {
		if x["name"] == "Zorbify" {
			z = x
		}
	}
	z["name"] = "Gizmoplex"
	if rc, _, raw := c.call("PUT", "/api/fw/zones", z); rc != 200 {
		t.Fatalf("rename: %d %s", rc, raw)
	}

	one := func(db *sql.DB, q string, args ...any) string {
		var v string
		db.QueryRow(q, args...).Scan(&v)
		return v
	}
	if got := one(cfg, `SELECT group_concat(from_zone||'>'||to_zone) FROM fw_rules WHERE name IN ('out','in') ORDER BY id`); got != "Gizmoplex>Internet,Internet>Gizmoplex" {
		t.Errorf("firewall rules: %s", got)
	}
	if one(cfg, `SELECT value FROM vpn_routes`) != "Gizmoplex" {
		t.Error("VPN route not moved")
	}
	if one(cfg, `SELECT zone FROM network_profiles`) != "Gizmoplex" {
		t.Error("DNS profile not moved")
	}
	for _, q := range []string{`SELECT targets FROM block_sets`, `SELECT targets FROM speed_rules`, `SELECT srcs FROM routes`} {
		if v := one(cfg, q); !strings.Contains(v, "Gizmoplex") || strings.Contains(v, "Zorbify") {
			t.Errorf("%s: %s", q, v)
		}
	}
	// Entries for other things in the same lists are left alone.
	if v := one(cfg, `SELECT targets FROM block_sets`); !strings.Contains(v, "aa:bb") {
		t.Errorf("a device entry in the same block was lost: %s", v)
	}
	if one(cfg, `SELECT value FROM wan_group_routes`) != "Gizmoplex" || one(cfg, `SELECT target FROM scope_rules`) != "Gizmoplex" {
		t.Error("group route or scoped rule not moved")
	}
	for _, k := range []string{"fw.mdns", "fw.mdns_ui"} {
		if v := one(cfg, `SELECT value FROM settings WHERE key=?`, k); !strings.Contains(v, "Gizmoplex") || strings.Contains(v, "Zorbify") || !strings.Contains(v, "Other") {
			t.Errorf("%s: %s", k, v)
		}
	}
	if one(cfg, `SELECT name FROM dhcp_scopes WHERE iface='eth2'`) != "Gizmoplex" {
		t.Error("the DHCP address pool made with the network keeps the old name")
	}
	if one(logs, `SELECT group_concat(from_zone||'>'||to_zone) FROM fw_events`) != "Gizmoplex>Internet,Internet>Gizmoplex" {
		t.Error("firewall log not moved")
	}
	if one(logs, `SELECT group_concat(key) FROM (SELECT key FROM metrics ORDER BY key)`) != "net.Gizmoplex.down,net.Gizmoplex.up" {
		t.Error("graph history not moved")
	}

	// The safety net: nothing anywhere in the settings database still holds the old name, apart from
	// the records that are history by design (saved firewall versions and the change log).
	history := map[string]bool{"fw_revisions": true, "changes": true, "audit": true, "events": true}
	tabs, _ := cfg.Query(`SELECT name FROM sqlite_master WHERE type='table'`)
	var names []string
	for tabs.Next() {
		var n string
		tabs.Scan(&n)
		names = append(names, n)
	}
	tabs.Close()
	for _, tb := range names {
		if history[tb] {
			continue
		}
		cols, err := cfg.Query(`SELECT name FROM pragma_table_info('` + tb + `') WHERE type LIKE '%TEXT%' OR type=''`)
		if err != nil {
			continue
		}
		var cs []string
		for cols.Next() {
			var n string
			cols.Scan(&n)
			cs = append(cs, n)
		}
		cols.Close()
		for _, col := range cs {
			var n int
			cfg.QueryRow(`SELECT count(*) FROM "`+tb+`" WHERE "`+col+`" LIKE ?`, "%Zorbify%").Scan(&n)
			if n > 0 {
				var sample string
				cfg.QueryRow(`SELECT "`+col+`" FROM "`+tb+`" WHERE "`+col+`" LIKE ? LIMIT 1`, "%Zorbify%").Scan(&sample)
				t.Logf("sample: %s", sample)
				t.Errorf("%s.%s still holds the old network name: a place that stores a network by name was not moved by renameNetworkRefs", tb, col)
			}
		}
	}

	// A DHCP pool that already has the new name stops the rename before anything changes.
	if rc, _, _ := c.call("POST", "/api/networks/add", map[string]any{"name": "Taken", "port": "eth3", "internet": true}); rc != 200 {
		t.Skip("could not add the second network")
	}
	zones, _ = fwZones(t, c)
	for _, x := range zones {
		if x["name"] == "Gizmoplex" {
			z = x
		}
	}
	exec(cfg, `UPDATE dhcp_scopes SET name='Wanted' WHERE iface='eth3'`)
	z["name"] = "Wanted"
	if rc, _, raw := c.call("PUT", "/api/fw/zones", z); rc == 200 || !strings.Contains(string(raw), "address pool") {
		t.Errorf("a clash with another pool name should be refused plainly, got %d %s", rc, raw)
	}
	if one(cfg, `SELECT name FROM zones WHERE id=?`, id) != "Gizmoplex" {
		t.Error("a refused rename must change nothing")
	}
}

func fwZones(t *testing.T, c *client) ([]map[string]any, []byte) {
	t.Helper()
	rc, _, raw := c.call("GET", "/api/fw/zones", nil)
	if rc != 200 {
		t.Fatalf("zones: %d %s", rc, raw)
	}
	var out []map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out, raw
}

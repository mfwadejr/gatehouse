package store

import "testing"

// A route, rule or forward made before the pause, schedule and service columns existed keeps
// working: it comes up with no pause, no schedule, every service and the failover mode.
func TestOldPoliciesGainTheNewColumnsUnchanged(t *testing.T) {
	db, err := open(t.TempDir()+"/c.db", 1)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := migrate(db, "config", configMigrations[:len(configMigrations)-1]); err != nil {
		t.Fatal(err)
	}
	mustExec := func(q string, a ...any) {
		t.Helper()
		if _, err := db.Exec(q, a...); err != nil {
			t.Fatal(err)
		}
	}
	mustExec(`INSERT INTO routes(name,enabled,target_kind,target,kill,srcs,dests,position) VALUES('Old',1,'tunnel','wg0',1,'[{"kind":"network","value":"Home"}]','[]',1)`)
	mustExec(`INSERT INTO nat_forwards(name,wan,proto,ext_port,int_addr,int_port,enabled,grp,app,mac,src,countries) VALUES('Web','','tcp','443','192.168.1.2','',1,'','','','','')`)
	if err := migrate(db, "config", configMigrations); err != nil {
		t.Fatal(err)
	}
	var service, sched, mode string
	var paused int64
	if err := db.QueryRow(`SELECT service,sched,paused_until,mode FROM routes WHERE name='Old'`).Scan(&service, &sched, &paused, &mode); err != nil {
		t.Fatal(err)
	}
	if service != "" || sched != "" || paused != 0 || mode != "failover" {
		t.Fatalf("route: %q %q %d %q", service, sched, paused, mode)
	}
	if err := db.QueryRow(`SELECT paused_until,sched FROM nat_forwards WHERE name='Web'`).Scan(&paused, &sched); err != nil || paused != 0 || sched != "" {
		t.Fatalf("forward: %v %d %q", err, paused, sched)
	}
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('fw_rules') WHERE name='paused_until'`).Scan(&n)
	if n != 1 {
		t.Fatal("firewall rules have no pause column")
	}
}

package app

import (
	"database/sql"
	"testing"

	"nopewarden/internal/fw"
)

// Backups carry the route's ports, schedule, pause and its own list of connections, and the
// pauses and schedules on rules and port forwards, and a restore puts them back.
func TestBackupKeepsPolicyDetailsAndRestoreBringsThemBack(t *testing.T) {
	a, _ := newSpeedApp(t)
	// Saved straight to the tables: the box in this test has no networks for the firewall to check against.
	tx, err := a.Config.Begin()
	if err != nil {
		t.Fatal(err)
	}
	err = func(tx *sql.Tx) error {
		if _, err := fw.SaveRoute(tx, fw.RouteRow{Name: "News", Enabled: true, TargetKind: "links", Target: "LTE,Fibre", Mode: "balance",
			Who: "net:Home,192.168.1.77", Where: "dom:news.example.com,203.0.113.0/24", Service: "tcp/563,ntp",
			Sched: &fw.Schedule{Mode: "weekly", Days: []int{1, 2}, From: "08:00", To: "17:00"}, PausedUntil: 4102444800}); err != nil {
			return err
		}
		if _, err := fw.SaveRule(tx, fw.RuleRow{Name: "Kids off", From: "Home", To: "wan", Action: "drop", Enabled: true, PausedUntil: 4102444800}); err != nil {
			return err
		}
		_, err := fw.SaveForward(tx, fw.ForwardRow{Name: "Web", Proto: "tcp", ExtPort: "443", IntAddr: "192.168.1.2", Enabled: true, PausedUntil: 4102444800,
			Sched: &fw.Schedule{Mode: "daily", From: "08:00", To: "20:00"}})
		return err
	}(tx)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	p, err := a.saveBackup("manual")
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{`DELETE FROM routes`, `DELETE FROM nat_forwards`, `DELETE FROM fw_rules`} {
		if _, err := a.Config.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := a.restoreBackup(p, "", "tester"); err != nil {
		t.Fatal(err)
	}
	rs, err := fw.Routes(a.Config)
	if err != nil || len(rs) != 1 {
		t.Fatalf("routes: %v %d", err, len(rs))
	}
	r := rs[0]
	if r.TargetKind != "links" || r.Target != "LTE,Fibre" || r.Mode != "balance" || r.Service != "tcp/563,ntp" || r.PausedUntil != 4102444800 ||
		r.Sched == nil || r.Sched.Mode != "weekly" || r.Who != "net:Home,192.168.1.77" || r.Where != "dom:news.example.com,203.0.113.0/24" {
		t.Fatalf("route after restore: %+v", r)
	}
	rules, _ := fw.Rules(a.Config)
	if len(rules) != 1 || rules[0].PausedUntil != 4102444800 {
		t.Fatalf("rule after restore: %+v", rules)
	}
	fs, _ := fw.Forwards(a.Config)
	if len(fs) != 1 || fs[0].PausedUntil != 4102444800 || fs[0].Sched == nil || fs[0].Sched.Mode != "daily" {
		t.Fatalf("forward after restore: %+v", fs)
	}
}

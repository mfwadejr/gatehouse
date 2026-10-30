package fw

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gatehouse/internal/store"
)

func TestHoursCrossMidnightAndDays(t *testing.T) {
	h := Hours{On: true, Days: []int{5}, From: "22:00", To: "07:00"}                                 // Friday night
	at := func(d, hh, mm int) time.Time { return time.Date(2026, 10, 25+d, hh, mm, 0, 0, time.UTC) } // 25 Oct 2026 is a Sunday
	if !h.Active(at(5, 23, 0)) {
		t.Error("Friday 23:00 must be off")
	}
	if !h.Active(at(6, 6, 59)) {
		t.Error("early Saturday belongs to Friday's window")
	}
	if h.Active(at(6, 7, 0)) || h.Active(at(5, 21, 59)) || h.Active(at(6, 23, 0)) {
		t.Error("outside the window")
	}
	if (Hours{On: true, Days: []int{1}, From: "07:00", To: "07:00"}).Valid() || (Hours{On: true, From: "22:00", To: "07:00"}).Valid() || (Hours{On: true, Days: []int{1}, From: "25:00", To: "07:00"}).Valid() {
		t.Error("bad hours accepted")
	}
	if (Hours{}).Active(at(5, 23, 0)) {
		t.Error("off must never be active")
	}
}

func TestPausedDevicesAndRule(t *testing.T) {
	db, err := store.OpenConfig(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Now()
	for _, q := range []string{
		`INSERT INTO zones(id,name,kind,gateway_mode,intra_allow,management,internet) VALUES(1,'Home','trusted','gateway',1,1,1)`,
		`INSERT INTO interfaces(name,zone_id) VALUES('eth1',1)`,
		`INSERT INTO wan_links(name,iface,type) VALUES('Fibre','eth0','dhcp')`,
		`INSERT INTO groups(id,name,filtering) VALUES(50,'Kids',1)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	db.Exec(`INSERT INTO devices(mac,name,group_id,paused_until) VALUES('aa:bb:cc:00:00:01','Tablet',50,0),('aa:bb:cc:00:00:02','Laptop',NULL,?),('aa:bb:cc:00:00:03','Old pause',NULL,?)`, now.Add(time.Hour).Unix(), now.Add(-time.Hour).Unix())
	if got := PausedDevices(db, now); len(got) != 1 || got[0] != "aa:bb:cc:00:00:02" {
		t.Fatalf("manual pause: %v", got)
	}
	// A profile paused as a whole pauses its devices.
	db.Exec(`UPDATE groups SET paused_until=? WHERE id=50`, now.Add(time.Hour).Unix())
	if got := PausedDevices(db, now); len(got) != 2 {
		t.Fatalf("profile pause: %v", got)
	}
	db.Exec(`UPDATE groups SET paused_until=0, hours=? WHERE id=50`, `{"on":true,"days":[0,1,2,3,4,5,6],"from":"00:00","to":"23:59"}`)
	noon := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	db.Exec(`UPDATE devices SET paused_until=0`)
	got := PausedDevices(db, noon)
	if len(got) != 1 || got[0] != "aa:bb:cc:00:00:01" {
		t.Fatalf("hours must pause only the profile's device: %v", got)
	}
	m, err := Load(db)
	if err != nil {
		t.Fatal(err)
	}
	script, err := Compile(m)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(script, `comment "rule:Home:Internet:Paused"`) && !strings.Contains(script, "Paused") {
		t.Fatalf("no pause rule:\n%s", script)
	}
	if !strings.Contains(script, "set dg_") {
		t.Fatalf("no device set:\n%s", script)
	}
}

package devices

import (
	"net"
	"path/filepath"
	"testing"

	"gatehouse/internal/dhcp"
	dnssrv "gatehouse/internal/dns"
	"gatehouse/internal/store"
)

func TestDirectory(t *testing.T) {
	db, err := store.OpenConfig(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.Exec(`INSERT INTO groups(id,name,filtering) VALUES(1,'Default',1),(2,'Trusted',0)`)
	db.Exec(`INSERT INTO dhcp_scopes(id,name,iface,mode,subnet,range_start,range_end) VALUES(1,'LAN','eth1','serve','192.168.1.0/24','192.168.1.10','192.168.1.20')`)
	db.Exec(`INSERT INTO dhcp_leases(scope_id,ip,mac,hostname,start,expires,state) VALUES(1,'192.168.1.12','aa:bb:cc:00:00:01','phone',1,9999999999,'active')`)
	e := dhcp.NewEngine(db)
	e.Reload()
	d := NewDirectory(db, e)
	d.Rebuild()

	var _ dnssrv.Directory = d
	ci := d.Client("192.168.1.12")
	if ci.Name != "phone" || !ci.Filtering || ci.GroupID != 1 {
		t.Fatalf("unknown device should get the default group: %+v", ci)
	}
	d.Seen("AA:BB:CC:00:00:01", net.ParseIP("192.168.1.12"), "phone")
	list, _ := d.List()
	if len(list) != 1 || !list[0].Online || list[0].Group != "Default" {
		t.Fatalf("list: %+v", list)
	}
	g := int64(2)
	if err := d.Update(list[0].ID, Update{GroupID: &g}); err != nil {
		t.Fatal(err)
	}
	if ci := d.Client("192.168.1.12"); ci.Filtering || ci.GroupID != 2 {
		t.Fatalf("group change not applied: %+v", ci)
	}
	name := "Marvin's phone"
	d.Update(list[0].ID, Update{Name: &name})
	if ci := d.Client("192.168.1.12"); ci.Name != name {
		t.Fatalf("name: %q", ci.Name)
	}
	if err := d.Forget(list[0].ID); err != nil {
		t.Fatal(err)
	}
	if l, _ := d.List(); len(l) != 0 {
		t.Fatal("forget failed")
	}
}

package fw

import (
	"path/filepath"
	"testing"

	"gatehouse/internal/store"
)

func TestTunnelSourcesForGroup(t *testing.T) {
	db, err := store.OpenConfig(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, q := range []string{
		`INSERT INTO dhcp_scopes(id,name,iface,subnet,range_start,range_end,router) VALUES(1,'Home','eth1','192.168.1.0/24','192.168.1.100','192.168.1.200','192.168.1.1')`,
		`INSERT INTO dhcp_reservations(scope_id,mac,ip) VALUES(1,'aa:bb:cc:00:00:01','192.168.1.50')`,
		`INSERT INTO device_groups(id,name) VALUES(1,'Kids')`,
		`INSERT INTO device_group_members(group_id,mac) VALUES(1,'AA:BB:CC:00:00:01'),(1,'aa:bb:cc:00:00:99')`,
		`INSERT INTO vpn_routes(tunnel,kind,value,label) VALUES('wg1','group','Kids','')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	got, err := TunnelSources(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(got["wg1"]) != 1 || got["wg1"][0] != "192.168.1.50/32" {
		t.Fatalf("group sources: %v", got)
	}
}

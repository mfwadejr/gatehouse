package fw

import (
	"path/filepath"
	"strings"
	"testing"

	"gatehouse/internal/store"
)

func TestLoadAndCompile(t *testing.T) {
	db, err := store.OpenConfig(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, q := range []string{
		`INSERT INTO zones(id,name,kind,gateway_mode,intra_allow,management,internet) VALUES(1,'Home','trusted','gateway',1,1,1),(2,'Guests','guest','gateway',0,0,1),(3,'Cams','isolated','gateway',1,0,0)`,
		`INSERT INTO interfaces(name,zone_id) VALUES('eth1',1),('eth2.20',2),('eth3',3)`,
		`INSERT INTO wan_links(name,iface,type,vlan_id) VALUES('Fibre','eth0','dhcp',100),('Backup','eth4','pppoe',0)`,
		`INSERT INTO fw_rules(from_zone,to_zone,position,name,dst,service,action) VALUES('Guests','Home',1,'Print','192.168.1.50','print','accept')`,
		`INSERT INTO fw_objects(name,kind,members) VALUES('printers','address','["192.168.1.50"]')`,
		`INSERT INTO nat_forwards(name,proto,ext_port,int_addr) VALUES('Web','tcp','443','192.168.1.20')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	m, err := Load(db)
	if err != nil {
		t.Fatal(err)
	}
	out, err := Compile(m)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"eth0.100"`, `"ppp-backup"`, `"eth2.20"`, "protected: management console"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s in:\n%s", want, out)
		}
	}
	rec, err := Compile(RecoveryModel(m))
	if err != nil {
		t.Fatalf("recovery model must always compile: %v", err)
	}
	if strings.Contains(rec, "dnat") || strings.Contains(rec, "rule:") {
		t.Error("recovery ruleset kept exceptions or forwards")
	}
}

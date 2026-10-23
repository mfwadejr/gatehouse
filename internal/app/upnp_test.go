package app

import (
	"net/netip"
	"testing"

	"gatehouse/internal/netd"
)

func TestUPnPPolicyAndPush(t *testing.T) {
	fn := &fakeNet{}
	a, err := New(Config{DataDir: t.TempDir(), WebAddr: "127.0.0.1:" + "8443", DNSAddrs: []string{freeAddr(t)}, Net: fn, FW: &fakeFW{}})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.upnpInit()
	a.Config.Exec(`INSERT INTO nat_forwards(name,proto,ext_port,int_addr,enabled) VALUES('web','tcp','40000-40002','192.168.1.5',1)`)
	pol := a.upnpPolicy()
	for _, p := range []int{8443, 5000, 40001} {
		if !pol.Reserved("tcp", p) {
			t.Errorf("port %d should be reserved", p)
		}
	}
	if pol.Reserved("udp", 27015) {
		t.Error("an ordinary game port is reserved")
	}
	me := netip.MustParseAddr("192.168.1.20")
	if err := a.upnp.tbl.Add(me, me, "udp", 27015, 27015, "game", 0); err != nil {
		t.Fatal(err)
	}
	// Nothing is applied while no internet connection is selected for UPnP.
	if len(fn.upnp) != 0 {
		t.Fatalf("mappings applied with no connection: %+v", fn.upnp)
	}
	st := a.upnpStatus()
	if st.Active || len(st.Mappings) != 1 || st.Why == "" {
		t.Fatalf("%+v", st)
	}
	_ = netd.UPnPMapping{}
}

package ha

import (
	"strings"
	"testing"
)

func TestHeartAddressesAgreeOnBothUnits(t *testing.T) {
	po, pp, err := HeartOwn("pair-1", Primary)
	if err != nil {
		t.Fatal(err)
	}
	so, sp, _ := HeartOwn("pair-1", Standby)
	if po.Addr() != sp || so.Addr() != pp || po.Addr() == so.Addr() {
		t.Fatalf("units disagree: %v %v / %v %v", po, pp, so, sp)
	}
	if po.Bits() != 30 || !strings.HasPrefix(po.Addr().String(), "10.255.") {
		t.Fatalf("bad network %v", po)
	}
	if HeartNet("a") == HeartNet("b") && HeartNet("a") == HeartNet("c") {
		t.Fatal("the network should depend on the pair")
	}
	if _, _, err := HeartOwn("x", "nobody"); err == nil {
		t.Fatal("unknown role accepted")
	}
}

func TestHeartConntrackdUsesTheCable(t *testing.T) {
	p := Pairing{V: 1, ID: "pair-1", Secret: strings.Repeat("s", 30), Primary: "192.168.1.2", Standby: "192.168.1.3",
		VIPs: []VIP{{Iface: "eth1", CIDR: "192.168.1.1/24"}}}
	own, peer, _ := HeartOwn(p.ID, Primary)
	out, err := HeartConntrackdConf(p, Primary, "eth2")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"IPv4_address " + own.Addr().String(), "IPv4_Destination_Address " + peer.String(), "Interface eth2"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "192.168.1.2\n") && strings.Contains(out, "IPv4_address 192.168.1.2\n        IPv4_Destination") {
		t.Fatal("the shared network was used")
	}
}

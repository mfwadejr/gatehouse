package app

import (
	"net/netip"
	"strings"
	"testing"

	"gatehouse/internal/fw"
)

func TestNatClass(t *testing.T) {
	for ip, want := range map[string]string{"8.8.8.8": "public", "100.72.1.9": "cgnat", "192.168.1.5": "private", "10.0.0.2": "private", "172.20.1.1": "private", "": "unknown"} {
		a, _ := netip.ParseAddr(ip)
		if got := natClass(a); got != want {
			t.Errorf("%q: got %s want %s", ip, got, want)
		}
	}
}

func TestDoubleNatListsEachForward(t *testing.T) {
	out := buildNat([]natInput{{Name: "Fibre", Address: "192.168.1.50", Gateway: "192.168.1.1", Up: true}, {Name: "LTE", Address: "1.2.3.4", Up: true}},
		[]fw.ForwardRow{{Name: "Plex", Proto: "tcp", ExtPort: "32400", Enabled: true}, {Name: "Off", Proto: "tcp", ExtPort: "81"}, {Name: "OnlyLTE", WAN: "LTE", Proto: "udp", ExtPort: "5000", Enabled: true}})
	if len(out) != 2 || out[0].Class != "private" || !out[0].Blocked || out[1].Blocked {
		t.Fatalf("%+v", out)
	}
	if len(out[0].Forwards) != 1 || !strings.Contains(out[0].Forwards[0].Text, "TCP 32400") || out[0].Forwards[0].Target != "192.168.1.50" {
		t.Fatalf("%+v", out[0].Forwards)
	}
	c := buildNat([]natInput{{Name: "Mobile", Address: "100.64.3.3"}}, nil)
	if c[0].Class != "cgnat" || len(c[0].Options) < 2 {
		t.Fatalf("%+v", c)
	}
}

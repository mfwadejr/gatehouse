package netcfg

import (
	"strings"
	"testing"
)

func TestTunnelMarksBecomePolicyRules(t *testing.T) {
	c := Config{Tunnels: []Tunnel{{Name: "tun1", Table: 300, Enabled: true, Marks: []int{0x1000, 0x1002}}}}
	if errs := c.validateTunnels(); len(errs) > 0 {
		t.Fatal(errs)
	}
	out := &Output{Networkd: map[string]string{}, Private: map[string]string{}}
	c.generateTunnels(out)
	f := out.Networkd["gatehouse-tun1.network"]
	for _, want := range []string{"FirewallMark=0x1000\nTable=main\nSuppressPrefixLength=0\nPriority=20", "FirewallMark=0x1000\nTable=300\nPriority=21", "FirewallMark=0x1002\nTable=300\nPriority=25"} {
		if !strings.Contains(f, want) {
			t.Errorf("missing %q in:\n%s", want, f)
		}
	}
	c.Tunnels[0].Marks = []int{5}
	if errs := c.validateTunnels(); len(errs) == 0 {
		t.Error("a mark outside the route range was accepted")
	}
}

// networkd must leave the address and route OpenVPN set on its own tun device alone, or the
// tunnel connects and then carries nothing.
func TestTunnelFileKeepsTheProgramsOwnAddress(t *testing.T) {
	c := Config{Tunnels: []Tunnel{{Name: "tun0", Table: 300, Enabled: true}}}
	out := &Output{Networkd: map[string]string{}}
	c.generateTunnels(out)
	f := out.Networkd["gatehouse-tun0.network"]
	if !strings.Contains(f, "KeepConfiguration=static") || !strings.Contains(f, "[Match]\nName=tun0") {
		t.Fatalf("tunnel file:\n%s", f)
	}
}

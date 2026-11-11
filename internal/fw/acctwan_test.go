package fw

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDevicesAreCountedPerConnectionOnlyWithSeveralConnections(t *testing.T) {
	m := sample()
	one, err := Compile(m)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(one, "aw_up_") {
		t.Fatal("one connection needs no per-connection sets")
	}
	for i := range m.Zones {
		if m.Zones[i].Kind == KindWan {
			m.Zones[i].Ifaces = append(m.Zones[i].Ifaces, "ppp0.5")
		}
	}
	two, err := Compile(m)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"set aw_up_ppp0_5 { type ipv4_addr; flags dynamic, timeout; timeout 1h; size 8192; counter; }",
		"set aw_dn_ppp0_5 {",
		`oifname "ppp0.5" add @aw_up_ppp0_5 { ip saddr }`,
		`iifname "ppp0.5" add @aw_dn_ppp0_5 { ip daddr }`,
	} {
		if !strings.Contains(two, want) {
			t.Errorf("missing %q in:\n%s", want, two)
		}
	}
	if _, err := exec.LookPath("nft"); err != nil || exec.Command("unshare", "-n", "true").Run() != nil {
		t.Skip("cannot check with nft here")
	}
	f := filepath.Join(t.TempDir(), "r.nft")
	os.WriteFile(f, []byte(two), 0o600)
	if b, err := exec.Command("unshare", "-n", "nft", "-c", "-f", f).CombinedOutput(); err != nil {
		t.Fatalf("nft rejected the ruleset: %v\n%s", err, b)
	}
}

func TestWanKeyKeepsSetNamesLegal(t *testing.T) {
	if WanKey("eth0.100") != "eth0_100" || WanKey("ppp-1") != "ppp_1" {
		t.Fatal(WanKey("eth0.100"), WanKey("ppp-1"))
	}
}

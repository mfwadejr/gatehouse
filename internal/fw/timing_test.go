package fw

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

// A pause or a schedule on a firewall rule, a port forward or a route is a clock test the firewall
// makes itself.
func TestPauseAndScheduleCompile(t *testing.T) {
	m := sample()
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	m.Now, m.Loc = now, time.UTC
	until := now.Add(time.Hour).Unix()
	m.Rules[0].PausedUntil = until
	m.Forwards[0].PausedUntil = until
	m.Forwards[0].Sched = `{"mode":"weekly","days":[1,2,3],"from":"08:00","to":"17:00"}`
	m.Forwards = append(m.Forwards, Forward{Name: "Old pause", Proto: "udp", ExtPort: "1194", IntAddr: "192.168.1.21", Enabled: true, PausedUntil: now.Add(-time.Hour).Unix()})
	script, err := Compile(m)
	if err != nil {
		t.Fatal(err)
	}
	want := "meta time >= " + strconv.FormatInt(until, 10)
	for _, w := range []string{
		`ip daddr { 192.168.1.50, 192.168.1.51 } ` + want + ` tcp dport 631`,
		`tcp dport 443 meta day`,
		want + ` counter dnat ip to 192.168.1.20`,
		`udp dport 1194 counter dnat ip to 192.168.1.21`,
	} {
		if !strings.Contains(script, w) {
			t.Errorf("missing %q in:\n%s", w, script)
		}
	}
	// A pause that has ended leaves no trace in the ruleset.
	if strings.Contains(script, "dport 1194 meta time") {
		t.Error("an expired pause was still compiled")
	}
	if _, err := exec.LookPath("nft"); err != nil || os.Geteuid() != 0 {
		t.Skip("needs nft and root")
	}
	f := t.TempDir() + "/r.nft"
	os.WriteFile(f, []byte(script), 0o600)
	if out, err := exec.Command("nft", "-c", "-f", f).CombinedOutput(); err != nil {
		t.Fatalf("nft refused it: %v\n%s", err, out)
	}
}

func TestRouteServiceAndPauseCompile(t *testing.T) {
	m := sample()
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	m.Now, m.Loc = now, time.UTC
	m.PortLists = map[string][]string{}
	m.PortLists["Usenet"] = []string{"tcp/563", "tcp/119"}
	m.Tunnels = []Tunnel{{Name: "Mullvad", Iface: "wg1"}}
	until := now.Add(30 * time.Minute).Unix()
	m.Routes = []RouteSpec{{Name: "News", Mark: RouteMarkBase, TunnelIface: "wg1", Srcs: []string{"192.168.1.0/24"}, Dst: "203.0.113.0/24,198.51.100.7",
		Service: "list:Usenet,ntp", PausedUntil: until, Sched: &Schedule{Mode: "weekly", Days: []int{1}, From: "08:00", To: "17:00"}}}
	script, err := Compile(m)
	if err != nil {
		t.Fatal(err)
	}
	i := strings.Index(script, "chain routemark")
	chain := script[i : i+strings.Index(script[i:], "\n\t}")]
	for _, w := range []string{"tcp dport 563", "tcp dport 119", "udp dport 123", "meta day", "meta time >= " + strconv.FormatInt(until, 10)} {
		if !strings.Contains(chain, w) {
			t.Errorf("route chain lacks %q:\n%s", w, chain)
		}
	}
	if _, err := exec.LookPath("nft"); err != nil || os.Geteuid() != 0 {
		t.Skip("needs nft and root")
	}
	f := t.TempDir() + "/r.nft"
	os.WriteFile(f, []byte(script), 0o600)
	if out, err := exec.Command("nft", "-c", "-f", f).CombinedOutput(); err != nil {
		t.Fatalf("nft refused it: %v\n%s", err, out)
	}
}

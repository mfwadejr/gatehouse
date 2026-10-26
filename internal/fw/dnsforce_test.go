package fw

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func dnsModel() *Model {
	m := sample()
	m.DNSRedirect = true
	m.BlockDoT = true
	m.Devices = map[string]bool{"aa:bb:cc:00:00:01": true, "aa:bb:cc:00:00:02": true, "aa:bb:cc:00:00:03": true}
	m.DevGroups = map[string]DevGroup{"Lab": {MACs: []string{"aa:bb:cc:00:00:02", "aa:bb:cc:00:00:03"}}}
	return m
}

func TestForcedDNSPerNetworkServer(t *testing.T) {
	m := dnsModel()
	for i := range m.Zones {
		if m.Zones[i].Name == "Guests" {
			m.Zones[i].DNSForce, m.Zones[i].Internet, m.Zones[i].DNSTarget = true, true, "9.9.9.9"
		}
		if m.Zones[i].Name == "Cameras" {
			m.Zones[i].DNSForce = false
		}
	}
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
	out, err := Compile(m)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`iifname "eth2" meta nfproto ipv4 ip saddr != 9.9.9.9 ip daddr != 9.9.9.9 meta l4proto { tcp, udp } th dport 53 counter dnat ip to 9.9.9.9:53`,
		`iifname "eth1" meta l4proto { tcp, udp } th dport 53 counter redirect to :53 comment "rule:dns:force:local"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, `"eth3"`+" meta l4proto { tcp, udp } th dport 53") {
		t.Error("a network left alone was forced")
	}
}

func TestForcedDNSExceptions(t *testing.T) {
	m := dnsModel()
	m.DNSExceptions = []DNSException{
		{Who: "dev:aa:bb:cc:00:00:01", Mode: DNSOff},
		{Who: "grp:Lab", Mode: DNSServer, Target: "1.1.1.1"},
		{Who: "grp:Gone", Mode: DNSOff}, // a removed group does nothing
	}
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
	out, err := Compile(m)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`ether saddr aa:bb:cc:00:00:01 meta l4proto { tcp, udp } th dport 53 counter accept comment "rule:dns:exempt"`,
		`ether saddr { aa:bb:cc:00:00:02, aa:bb:cc:00:00:03 } meta nfproto ipv4 ip saddr != 1.1.1.1 ip daddr != 1.1.1.1`,
		`counter dnat ip to 1.1.1.1:53`,
		`ether saddr { aa:bb:cc:00:00:02, aa:bb:cc:00:00:03 } ip daddr 1.1.1.1 meta l4proto { tcp, udp } th dport 53 ct status dnat accept`,
		// the device left alone is left alone by the encrypted-DNS block too
		`ether saddr != aa:bb:cc:00:00:01 tcp dport 853 counter drop`,
		// the group sent to a server is blocked from other encrypted DNS even on a network left alone
		`ether saddr { aa:bb:cc:00:00:02, aa:bb:cc:00:00:03 } tcp dport 853 counter drop`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	// The exception comes before the network-wide rule, so it wins.
	if i, j := strings.Index(out, `rule:dns:exempt`), strings.Index(out, `rule:dns:force:local`); i < 0 || j < 0 || i > j {
		t.Errorf("exception must precede network rules (%d, %d)", i, j)
	}
}

func TestForcedDNSExceptionWorksWithTheSwitchOff(t *testing.T) {
	m := dnsModel()
	m.DNSRedirect = false
	m.DNSExceptions = []DNSException{{Who: "dev:aa:bb:cc:00:00:01", Mode: DNSGatehouse}}
	out, err := Compile(m)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `ether saddr aa:bb:cc:00:00:01 meta l4proto { tcp, udp } th dport 53 counter redirect to :53`) {
		t.Errorf("a forced device was not redirected:\n%s", out)
	}
	if strings.Contains(out, "rule:dns:force:local") {
		t.Error("networks were forced with the switch off")
	}
}

func TestForcedDNSRejectsBadServer(t *testing.T) {
	m := dnsModel()
	m.Zones[0].DNSTarget = "not an address"
	if m.Validate() == nil {
		t.Error("a bad network server was accepted")
	}
	m = dnsModel()
	m.DNSExceptions = []DNSException{{Who: "dev:aa:bb:cc:00:00:01", Mode: DNSServer, Target: "127.0.0.1"}}
	if m.Validate() == nil {
		t.Error("a loopback server was accepted")
	}
}

func TestForcedDNSNftAcceptsRuleset(t *testing.T) {
	if _, err := exec.LookPath("nft"); err != nil {
		t.Skip("nft not installed")
	}
	if exec.Command("unshare", "-n", "true").Run() != nil {
		t.Skip("cannot create a network namespace here")
	}
	m := dnsModel()
	for i := range m.Zones {
		if m.Zones[i].Name == "Guests" {
			m.Zones[i].DNSForce, m.Zones[i].Internet, m.Zones[i].DNSTarget = true, true, "9.9.9.9"
		}
	}
	m.DNSExceptions = []DNSException{
		{Who: "dev:aa:bb:cc:00:00:01", Mode: DNSOff},
		{Who: "grp:Lab", Mode: DNSServer, Target: "1.1.1.1"},
	}
	out, err := Compile(m)
	if err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(t.TempDir(), "r.nft")
	os.WriteFile(f, []byte(out), 0o600)
	if b, err := exec.Command("unshare", "-n", "nft", "-c", "-f", f).CombinedOutput(); err != nil {
		t.Fatalf("nft rejected the ruleset: %v\n%s\n%s", err, b, out)
	}
}

func TestSSHIsClosedUntilTheMasterSwitchIsOnAndIsRateLimited(t *testing.T) {
	m := dnsModel()
	for i := range m.Zones {
		if m.Zones[i].Management {
			m.Zones[i].SSH = true
		}
	}
	out, err := Compile(m)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "protected: management ssh") {
		t.Fatalf("SSH was open with the master switch off:\n%s", out)
	}
	m.SSHEnabled = true
	out, err = Compile(m)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"set ssh_rate4", "@ssh_rate4 { ip saddr limit rate over 8/minute", `tcp dport 22 accept comment "protected: management ssh"`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if i, j := strings.Index(out, "ssh limit"), strings.Index(out, "protected: management ssh\""); i < 0 || i > j {
		t.Error("the rate limit must come before the accept")
	}
	if _, err := exec.LookPath("nft"); err != nil {
		t.Skip("nft not installed")
	}
	f := filepath.Join(t.TempDir(), "r.nft")
	os.WriteFile(f, []byte(out), 0o600)
	if b, err := exec.Command("nft", "-c", "-f", f).CombinedOutput(); err != nil {
		t.Fatalf("nft rejected the rules: %v\n%s", err, b)
	}
}

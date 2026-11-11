package fw

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagementPortRulesAreAlwaysThereAndNeverRouted(t *testing.T) {
	m := sample()
	m.SSHEnabled = true
	m.MgmtAllow = []string{"10.9.0.0/24"}
	out, err := Compile(m)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"set mgmt_if { type ifname; }",
		`iifname @mgmt_if ip saddr 10.9.0.0/24 tcp dport 443 counter accept comment "protected: management port console"`,
		`iifname @mgmt_if ip saddr 10.9.0.0/24 tcp dport 22 accept comment "protected: management port ssh"`,
		`iifname @mgmt_if icmp type echo-request`,
		`iifname @mgmt_if udp sport 67 udp dport 68 accept`,
		`iifname @mgmt_if counter drop comment "protected: the management port is not routed"`,
		`oifname @mgmt_if counter drop comment "protected: the management port is not routed"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	// The forward rules come before every editable rule, and nothing but the console, ssh and ping is allowed.
	fwd := strings.SplitN(out, "chain forward", 2)[1]
	if strings.Index(fwd, "@mgmt_if") > strings.Index(fwd, "accept") && strings.Contains(fwd, "accept") {
		t.Fatalf("the management port must be refused before any accept:\n%s", fwd)
	}
	if strings.Contains(out, "lan_console") {
		t.Fatal("without the close switch there is no gate set")
	}
	m.SSHEnabled = false
	out, _ = Compile(m)
	if strings.Contains(out, "management port ssh") {
		t.Fatal("ssh must not be open on the management port while remote login is off")
	}
}

func TestClosingTheLANConsoleGoesThroughAGate(t *testing.T) {
	m := sample()
	m.RescueIface = "eth2"
	m.CloseLAN = true
	out, err := Compile(m)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"set lan_console { type inet_service; }",
		`tcp dport @lan_console counter accept comment "protected: management console"`,
		`iifname "eth2" tcp dport @lan_console counter accept comment "protected: setup port"`,
		`protected: management port console`, // the management port itself is never gated
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, `management port console"`) && strings.Contains(strings.SplitN(strings.SplitN(out, "management port console", 2)[0], "\n", -1)[len(strings.Split(strings.SplitN(out, "management port console", 2)[0], "\n"))-1], "lan_console") {
		t.Fatal("the management port must not depend on the LAN gate")
	}
	if _, err := exec.LookPath("nft"); err != nil || exec.Command("unshare", "-n", "true").Run() != nil {
		t.Skip("cannot check with nft here")
	}
	f := filepath.Join(t.TempDir(), "r.nft")
	os.WriteFile(f, []byte(out), 0o600)
	if b, err := exec.Command("unshare", "-n", "nft", "-c", "-f", f).CombinedOutput(); err != nil {
		t.Fatalf("nft rejected the ruleset: %v\n%s", err, b)
	}
}

func TestManagementSetsTakeOnlyPortNamesAndPorts(t *testing.T) {
	s, err := BuildSetScript(map[string]SetFill{
		MgmtIfSet:  {Replace: true, Elems: []string{"eth3"}},
		LanGateSet: {Replace: true, Elems: []string{"443", "8443"}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`flush set inet ` + tableName + ` mgmt_if`, `add element inet ` + tableName + ` mgmt_if { "eth3" }`, `add element inet ` + tableName + ` lan_console { 443, 8443 }`} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in:\n%s", want, s)
		}
	}
	for _, bad := range []map[string]SetFill{
		{MgmtIfSet: {Elems: []string{`eth0" } ; flush ruleset ; "`}}},
		{LanGateSet: {Elems: []string{"0"}}},
		{LanGateSet: {Elems: []string{"https"}}},
	} {
		if _, err := BuildSetScript(bad, nil); err == nil {
			t.Errorf("accepted %v", bad)
		}
	}
}

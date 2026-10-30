package fw

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestCountryRulesBlockBothWays(t *testing.T) {
	m := sample()
	m.Rules = append(m.Rules, CountryRules(m.Zones, CountryPolicy{Enabled: true, Mode: "block", Dir: "both", Countries: []string{"ru", "CN", "ru", "xx1"}})...)
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
	script, err := Compile(m)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"set gc_ru_4", "set gc_cn_6", `comment "rule:Home:Internet:Countries out"`, `comment "rule:Internet:self:Countries in"`, "ip saddr @gc_ru_4"} {
		if !strings.Contains(script, want) {
			t.Errorf("missing %q in:\n%s", want, script)
		}
	}
	// What comes in from the internet is dropped before port forwards and VPN servers are let in.
	in := strings.Index(script, "Countries in")
	dnat := strings.Index(script, `comment "forward"`)
	if dnat >= 0 && in > dnat {
		t.Error("the inbound country rule comes after the port-forward accept")
	}
	if strings.Count(script, "gc_xx1") > 0 {
		t.Error("a bad country code reached the script")
	}
}

func TestCountryRulesAllowOnlyUsesNegation(t *testing.T) {
	m := sample()
	m.Rules = append(m.Rules, CountryRules(m.Zones, CountryPolicy{Enabled: true, Mode: "allow", Dir: "out", Countries: []string{"US", "CA"}})...)
	script, err := Compile(m)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(script, "ip daddr != @gc_ca_4 ip daddr != @gc_us_4 ip daddr != {") {
		t.Errorf("allow-only did not become a negated match:\n%s", script)
	}
	if strings.Contains(script, "Countries in") {
		t.Error("an outgoing-only policy made inbound rules")
	}
}

func TestCountryRulesOffMakesNothing(t *testing.T) {
	m := sample()
	if n := len(CountryRules(m.Zones, CountryPolicy{Enabled: false, Countries: []string{"RU"}})); n != 0 {
		t.Errorf("a switched-off policy made %d rules", n)
	}
	if n := len(CountryRules(m.Zones, CountryPolicy{Enabled: true})); n != 0 {
		t.Errorf("a policy with no countries made %d rules", n)
	}
}

func TestCountryRulesLoadIntoKernel(t *testing.T) {
	if _, err := exec.LookPath("nft"); err != nil || os.Geteuid() != 0 {
		t.Skip("needs nft and root")
	}
	m := sample()
	m.Rules = append(m.Rules, CountryRules(m.Zones, CountryPolicy{Enabled: true, Mode: "allow", Dir: "both", Countries: []string{"US"}})...)
	script, err := Compile(m)
	if err != nil {
		t.Fatal(err)
	}
	f := t.TempDir() + "/r.nft"
	os.WriteFile(f, []byte(script), 0o600)
	if out, err := exec.Command("nft", "-c", "-f", f).CombinedOutput(); err != nil {
		t.Fatalf("nft refused it: %v\n%s", err, out)
	}
}

func TestRouteMarksCompile(t *testing.T) {
	m := sample()
	m.Tunnels = []Tunnel{{Name: "Mullvad", Iface: "wg1"}}
	m.Routes = []RouteSpec{{Name: "Streaming", Mark: RouteMarkBase, TunnelIface: "wg1", Srcs: []string{"192.168.1.0/24"}, Dst: "dom:example.com,203.0.113.0/24,geo:NL", Kill: true}}
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
	script, err := Compile(m)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"chain routemark", "ip saddr 192.168.1.0/24 ip daddr 203.0.113.0/24 ct state new meta mark 0 meta mark set 0x1000", "set dm_", "set gc_nl_4", `comment "killswitch:Streaming"`, "ct mark >= 0x1000 meta mark set ct mark"} {
		if !strings.Contains(script, want) {
			t.Errorf("missing %q in:\n%s", want, script)
		}
	}
	if strings.Contains(script, "ip6 daddr @dm_") && strings.Contains(script, "routemark") {
		// IPv6 sets exist for domains, but the route chain must not use them
		i := strings.Index(script, "chain routemark")
		j := strings.Index(script[i:], "\n\t}")
		if strings.Contains(script[i:i+j], "ip6") {
			t.Error("a route used IPv6")
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

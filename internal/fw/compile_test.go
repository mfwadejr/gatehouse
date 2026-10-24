package fw

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func sample() *Model {
	m := DefaultModel([]string{"eth0"}, []string{"eth1"})
	guest := Zone{Name: "Guests", Kind: KindGuest, Ifaces: []string{"eth2"}}
	ApplyPreset(&guest)
	iot := Zone{Name: "Cameras", Kind: KindIsolated, Ifaces: []string{"eth3", "eth3.20"}}
	ApplyPreset(&iot)
	m.Zones = append(m.Zones, guest, iot)
	m.Groups["printers"] = []string{"192.168.1.50", "192.168.1.51"}
	m.Rules = []Rule{
		{Name: "Guests print", From: "Guests", To: "Home", Dst: "printers", Service: "print", Action: "accept", Enabled: true},
		{Name: "Cameras to NVR", From: "Cameras", To: "Home", Dst: "192.168.1.9", Service: "tcp/554", Action: "accept", Enabled: true, Log: true},
		{Name: "Guests ssh", From: "Guests", To: "self", Service: "ssh", Action: "drop", Enabled: true},
		{Name: "Disabled", From: "Guests", To: "wan", Service: "any", Action: "drop", Enabled: false},
	}
	m.Forwards = []Forward{{Name: "Web server", Proto: "tcp", ExtPort: "443", IntAddr: "192.168.1.20", Enabled: true}}
	return m
}

func TestCompileShape(t *testing.T) {
	out, err := Compile(sample())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"delete table inet gatehouse",
		`iifname "eth1" tcp dport 443 accept comment "protected: management console"`,
		`iifname "eth1" oifname "eth0" counter accept comment "internet:Home"`,
		`iifname "eth2" oifname "eth1" ip daddr { 192.168.1.50, 192.168.1.51 } tcp dport 631 counter accept`,
		`iifname { "eth3", "eth3.20" } oifname "eth1" ip daddr 192.168.1.9 tcp dport 554`,
		"redirect to :53",
		"tcp dport 853 counter drop",
		"dnat ip to 192.168.1.20 comment",
		"hairpin:",
		"meta nfproto ipv4 oifname \"eth0\" masquerade",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	// Guests have the internet, but the isolated network does not.
	if strings.Contains(out, `internet:Cameras`) {
		t.Error("isolated network was given internet access")
	}
	if !strings.Contains(out, `internet:Guests`) {
		t.Error("guest network lost internet access")
	}
	if strings.Contains(out, "Disabled") {
		t.Error("disabled rule compiled")
	}
	// The protected rule comes before every editable rule.
	if strings.Index(out, "protected: management console") > strings.Index(out, "rule:Guests:self:Guests ssh") {
		t.Error("management rule is not first")
	}
}

func TestCompileIsDeterministic(t *testing.T) {
	a, _ := Compile(sample())
	for i := 0; i < 20; i++ {
		b, _ := Compile(sample())
		if a != b {
			t.Fatal("output changes between runs")
		}
	}
}

func TestValidationBlocksLockout(t *testing.T) {
	m := sample()
	m.Zones[0].Management = false
	if _, err := Compile(m); err == nil || !strings.Contains(err.Error(), "locked out") {
		t.Fatalf("no management network accepted: %v", err)
	}
	m = sample()
	m.Zones[0].Ifaces = nil
	if _, err := Compile(m); err == nil {
		t.Fatal("management network without ports accepted")
	}
	m = sample()
	m.Zones[1].Management = true // the internet
	if _, err := Compile(m); err == nil {
		t.Fatal("internet side allowed to manage the device")
	}
	m = sample()
	m.ConsolePorts = nil
	if _, err := Compile(m); err == nil {
		t.Fatal("missing console port accepted")
	}
}

func TestValidationRejectsInjection(t *testing.T) {
	bad := []func(*Model){
		func(m *Model) { m.Zones[0].Name = "Home\" } ; flush ruleset ; #" },
		func(m *Model) { m.Zones[0].Ifaces = []string{`eth1" accept #`} },
		func(m *Model) { m.Rules[0].Dst = "1.2.3.4; drop" },
		func(m *Model) { m.Rules[0].Service = "tcp/80; accept" },
		func(m *Model) { m.Rules[0].Name = "x\ny" },
		func(m *Model) { m.Rules[0].Action = "accept; flush ruleset" },
		func(m *Model) { m.Forwards[0].IntAddr = "1.2.3.4 \n" },
		func(m *Model) { m.Forwards[0].ExtPort = "80 accept" },
		func(m *Model) { m.Rules[0].From = "Nowhere" },
		func(m *Model) { m.Groups["a b\"c"] = []string{"1.1.1.1"} },
		func(m *Model) { m.Groups["x"] = []string{"1.1.1.1\n}"} },
	}
	for i, f := range bad {
		m := sample()
		f(m)
		if out, err := Compile(m); err == nil {
			t.Errorf("case %d accepted:\n%s", i, out)
		}
	}
}

func TestAddressFamilies(t *testing.T) {
	m := sample()
	m.Rules = []Rule{{Name: "mixed", From: "Home", To: "Guests", Src: "10.0.0.0/8,fd00::/8", Dst: "any", Service: "https", Action: "accept", Enabled: true}}
	out, err := Compile(m)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "ip saddr 10.0.0.0/8 tcp dport 443") || !strings.Contains(out, "ip6 saddr fd00::/8 tcp dport 443") || !strings.Contains(out, "udp dport 443") {
		t.Fatalf("family or service expansion wrong:\n%s", out)
	}
	m.Rules[0].Dst = "192.168.1.1"
	out, _ = Compile(m)
	if strings.Contains(out, "ip6 saddr") {
		t.Fatal("v6 source paired with v4 destination")
	}
}

func TestDescribe(t *testing.T) {
	d := strings.Join(Describe(sample()), "\n")
	for _, want := range []string{"Home: devices can reach the internet, every other network and each other; can manage this device.",
		"Guests: devices can reach the internet; cannot manage this device.", "except 1 port you opened on purpose"} {
		if !strings.Contains(d, want) {
			t.Errorf("missing %q in:\n%s", want, d)
		}
	}
}

// The generated script must be accepted by the real nft parser.
func TestNftAcceptsRuleset(t *testing.T) {
	if _, err := exec.LookPath("nft"); err != nil {
		t.Skip("nft not installed")
	}
	if exec.Command("unshare", "-n", "true").Run() != nil {
		t.Skip("cannot create a network namespace here")
	}
	m := sample()
	m.LogAllowed, m.LogDrops = true, true
	out, err := Compile(m)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `log prefix "gh|allow|internet:`) || !strings.Contains(out, `log prefix "gh|drop|default:fwd `) {
		t.Fatalf("decisions are not logged with their reasons:\n%s", out)
	}
	f := filepath.Join(t.TempDir(), "r.nft")
	os.WriteFile(f, []byte(out), 0o600)
	cmd := exec.Command("unshare", "-n", "nft", "-c", "-f", f)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("nft rejected the ruleset: %v\n%s\n%s", err, b, out)
	}
}

func TestUPnPLetsMappedConnectionsThrough(t *testing.T) {
	m := sample()
	m.Forwards = nil
	out, err := Compile(m)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "ct status dnat") && strings.Contains(strings.SplitN(out, "chain forward", 2)[1], "ct status dnat accept") {
		t.Fatal("nothing should be let through by default")
	}
	m.UPnP = true
	out, _ = Compile(m)
	if !strings.Contains(strings.SplitN(out, "chain forward", 2)[1], "ct status dnat") {
		t.Fatalf("UPnP mappings are not accepted:\n%s", out)
	}
}

func TestSetupPortAndManagementLimit(t *testing.T) {
	m := sample()
	m.RescueIface = "eth2" // a port that is not a management network
	m.MgmtAllow = []string{"192.168.1.0/24", "10.8.0.0/24"}
	out, err := Compile(m)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`iifname "eth1" ip saddr { 192.168.1.0/24, 10.8.0.0/24 } tcp dport 443 accept comment "protected: management console"`,
		`iifname "eth2" tcp dport 443 accept comment "protected: setup port"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	// no limit saved: the plain protected rule, and no rule for a setup port that is already managing
	m = sample()
	m.RescueIface = "eth1"
	out, _ = Compile(m)
	if strings.Contains(out, "ip saddr {") && strings.Contains(out, "management console\"") && strings.Contains(out, "protected: setup port") {
		t.Errorf("unexpected rules:\n%s", out)
	}
	if strings.Contains(out, "setup port") {
		t.Errorf("a setup port that already manages got a second rule:\n%s", out)
	}
	// an internet-facing port is never a setup port
	m = sample()
	m.RescueIface = "eth0"
	if out, _ = Compile(m); strings.Contains(out, "setup port") {
		t.Errorf("internet port got the setup rule:\n%s", out)
	}
	m = sample()
	m.MgmtAllow = []string{"not-a-range"}
	if _, err := Compile(m); err == nil {
		t.Fatal("bad management range accepted")
	}
}

func TestStartingRulesFollowPort(t *testing.T) {
	if got := FirstBootBaselineFor(8443); !strings.Contains(got, "tcp dport 8443 ct state new") || strings.Contains(got, "dport 443") {
		t.Fatal(got)
	}
}

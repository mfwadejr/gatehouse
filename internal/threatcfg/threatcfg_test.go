package threatcfg

import (
	"strings"
	"testing"
)

func TestEveryGroupHasAPlainDescription(t *testing.T) {
	for _, g := range allGroups {
		if strings.TrimSpace(groupText[g]) == "" || groupText[g] == "Other checks" {
			t.Errorf("group %q has no plain description", g)
		}
	}
	for g := range groupText {
		if !KnownGroup(g) {
			t.Errorf("a description for %q, which is not a group", g)
		}
	}
	gs := Groups()
	if len(gs) != len(allGroups) || !gs[0].Common || gs[len(gs)-1].Common {
		t.Fatalf("groups: %d, common first expected", len(gs))
	}
}

func TestACustomSetupIsThePresetPlusChanges(t *testing.T) {
	p := Plan{Preset: "balanced"}
	base := EffectiveOff(p)
	has := func(list []string, g string) bool {
		for _, x := range list {
			if x == g {
				return true
			}
		}
		return false
	}
	if !has(base, "scan") || has(base, "malware") {
		t.Fatalf("balanced: %v", base)
	}
	// the person switches scan on and malware off
	p.GroupsOn, p.GroupsOff = []string{"scan"}, []string{"malware"}
	eff := EffectiveOff(p)
	if has(eff, "scan") || !has(eff, "malware") {
		t.Fatalf("effective: %v", eff)
	}
	if dl := DisableList(p); !strings.Contains(dl, "group:emerging-malware.rules") || strings.Contains(dl, "group:emerging-scan.rules") {
		t.Fatalf("disable list:\n%s", dl)
	}
	// changes are worked out against the preset, so only differences are kept
	want := map[string]bool{}
	for _, g := range eff {
		want[g] = true
	}
	on, off := Changes("balanced", want)
	if len(on) != 1 || on[0] != "scan" || len(off) != 1 || off[0] != "malware" {
		t.Fatalf("changes: %v %v", on, off)
	}
	// the same wishes against another preset give other changes: the preset stays the base
	on2, off2 := Changes("strict", want)
	if len(on2)+len(off2) <= 2 {
		t.Fatalf("strict base should differ more: %v %v", on2, off2)
	}
	// an unknown group is refused
	bad := Plan{Preset: "balanced", GroupsOn: []string{"nonsense"}}
	if bad.Check() == nil {
		t.Fatal("unknown group accepted")
	}
}

// A setting in the include file replaces the engine's whole block of that name. Setting "vars" there
// once removed HTTP_PORTS and every other port group, so the real rules failed to load and the engine
// exited. The include must not define it.
func TestIncludeDoesNotReplaceTheEnginesVariables(t *testing.T) {
	for _, l := range strings.Split(Include(), "\n") {
		if strings.HasPrefix(l, "vars:") || strings.HasPrefix(l, "address-groups:") || strings.HasPrefix(l, "port-groups:") {
			t.Fatalf("the include must not define the engine's variables: %q", l)
		}
	}
}

// A pid file left by a failed start made the engine refuse every later start.
func TestUnitAsksForNoPidFile(t *testing.T) {
	u := Unit(Plan{Ifaces: []string{"lan0"}, CPUPercent: 50, MemoryMB: 2048})
	if strings.Contains(u, "pidfile") {
		t.Fatalf("the unit must not ask the engine for a pid file:\n%s", u)
	}
}

func inlinePlan() Plan {
	return Plan{Enabled: true, Inline: true, InlineIfaces: []string{"eth1", "eth2"}, WANIfaces: []string{"eth0"},
		BlockGroups: []string{"malware", "exploit"}, AlertOnlySIDs: []int{2019401}, NeverCheck: []string{"192.168.1.50", "10.0.0.0/24", "fd00::1"}}
}

func TestInlineScriptQueuesOnlyAcceptedTrafficBetweenChosenPortsAndTheInternet(t *testing.T) {
	p := inlinePlan()
	if err := p.Check(); err != nil {
		t.Fatal(err)
	}
	s := NftScript(p)
	for _, want := range []string{
		"hook forward priority 10; policy accept",
		`iifname { "eth1", "eth2" } oifname { "eth0" } queue flags bypass to 100`,
		`iifname { "eth0" } oifname { "eth1", "eth2" } queue flags bypass to 100`,
		"ip saddr { 10.0.0.0/24, 192.168.1.50/32 } return",
		"ip6 daddr { fd00::1/128 } return",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in\n%s", want, s)
		}
	}
	if strings.Contains(s, "input") {
		t.Fatal("traffic to the box itself is never queued")
	}
	// A never-check line must come before the queue lines, or it would not skip anything.
	if strings.Index(s, "return") > strings.Index(s, "queue") {
		t.Fatal("never-check rules come first")
	}
}

func TestInlineFailsOpenEverywhere(t *testing.T) {
	p := inlinePlan()
	u := Unit(p)
	if !strings.Contains(u, " -q 100") || !strings.Contains(u, "nft -f /etc/nopewarden/ips.nft") || !strings.Contains(u, "nft delete table inet nopewarden_ips") {
		t.Fatalf("the queue rules live and die with the engine:\n%s", u)
	}
	if !strings.Contains(u, "gro off lro off") || !strings.Contains(u, "eth0") {
		t.Fatalf("card offloading is switched off on every checked port:\n%s", u)
	}
	if !strings.Contains(IncludeFor(p), "fail-open: yes") || !strings.Contains(NftScript(p), "bypass") {
		t.Fatal("fail open on both sides")
	}
	if strings.Contains(Unit(Plan{Enabled: true, Ifaces: []string{"eth1"}}), "-q ") {
		t.Fatal("watch only never queues")
	}
}

func TestDropAndAlertOnlyFiles(t *testing.T) {
	p := inlinePlan()
	d := DropConfFor(p)
	if !strings.Contains(d, "group:emerging-malware.rules") || !strings.Contains(d, "group:emerging-exploit.rules") || strings.Contains(d, "scan") {
		t.Fatal(d)
	}
	if !strings.Contains(ModifyConfFor(p), `2019401 "^drop" "alert"`) {
		t.Fatal(ModifyConfFor(p))
	}
	w := p
	w.Inline = false
	if strings.Contains(DropConfFor(w), "group:") || strings.Contains(ModifyConfFor(w), "drop") || LocalRulesFor(w) != "" {
		t.Fatal("outside inline mode every rule stays alert")
	}
	if !strings.Contains(LocalRulesFor(p), "sid:9900001") {
		t.Fatal("test rule")
	}
}

func TestInlineChecks(t *testing.T) {
	p := inlinePlan()
	p.NeverCheck = []string{"not an address"}
	if p.Check() == nil {
		t.Fatal("bad address refused")
	}
	p = inlinePlan()
	p.InlineIfaces = nil
	if p.Check() == nil {
		t.Fatal("needs a network to check")
	}
	p = inlinePlan()
	p.Enabled = false
	if p.Check() == nil {
		t.Fatal("needs watching on")
	}
}

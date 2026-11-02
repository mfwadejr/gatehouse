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

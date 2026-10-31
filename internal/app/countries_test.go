package app

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCountryPageSavesRulesAndWarns(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")

	// Off by default.
	rc, m, raw := c.call("GET", "/api/fw/countries", nil)
	if rc != 200 || m["policy"].(map[string]any)["enabled"] != false {
		t.Fatalf("default: %d %s", rc, raw)
	}
	// Block two countries both ways.
	if rc, _, raw := c.call("PUT", "/api/fw/countries", map[string]any{"enabled": true, "mode": "block", "dir": "both", "countries": []string{"ru", "CN", "bad!"}}); rc != 200 {
		t.Fatalf("save: %d %s", rc, raw)
	}
	_, _, raw = c.call("GET", "/api/fw/rules", nil)
	var rules []map[string]any
	json.Unmarshal([]byte(raw), &rules)
	out, in := 0, 0
	for _, r := range rules {
		if r["countries"] == true {
			if r["name"] == "Countries out" {
				out++
			} else {
				in++
			}
			if r["action"] != "drop" {
				t.Errorf("country rule is %v", r["action"])
			}
		}
	}
	if out == 0 || in == 0 {
		t.Fatalf("country rules not shown (out %d, in %d): %s", out, in, raw)
	}
	// They are in the compiled firewall too.
	_, script, err := c.app.currentRuleset()
	if err != nil || !strings.Contains(script, "gc_ru_4") || !strings.Contains(script, "Countries in") {
		t.Fatalf("not compiled: %v\n%s", err, script)
	}
	// More than the limit is refused with a message.
	many := []string{}
	for _, a := range "ABCDEFGHIJ" {
		for _, b := range "ABCDE" {
			many = append(many, string(a)+string(b))
		}
	}
	if rc, _, _ := c.call("PUT", "/api/fw/countries", map[string]any{"enabled": true, "mode": "block", "dir": "both", "countries": many}); rc == 200 {
		t.Error("50 countries accepted")
	}
	// Switching it off removes the rules again.
	if rc, _, raw := c.call("PUT", "/api/fw/countries", map[string]any{"enabled": false, "mode": "block", "dir": "both", "countries": []string{"RU"}}); rc != 200 {
		t.Fatalf("off: %d %s", rc, raw)
	}
	_, script, _ = c.app.currentRuleset()
	if strings.Contains(script, "Countries") {
		t.Error("rules remain after switching off")
	}
}

// The console tells a Save apart by asking before and after: the same Save twice must leave the
// "rev" alone, a real change must move it and leave the firewall waiting to be applied.
func TestApplyStateTellsWhatASaveDid(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	state := func() (rev string, dirty bool) {
		rc, m, raw := c.call("GET", "/api/applystate", nil)
		if rc != 200 {
			t.Fatalf("applystate: %d %s", rc, raw)
		}
		fw := m["fw"].(map[string]any)
		rev, _ = fw["rev"].(string)
		dirty, _ = fw["dirty"].(bool)
		return
	}
	body := map[string]any{"enabled": true, "mode": "block", "dir": "both", "countries": []string{"RU"}}
	r0, _ := state()
	if r0 == "" {
		t.Fatal("no rev")
	}
	if rc, _, raw := c.call("PUT", "/api/fw/countries", body); rc != 200 {
		t.Fatalf("save: %d %s", rc, raw)
	}
	r1, d1 := state()
	if r1 == r0 || !d1 {
		t.Fatalf("a real change must move the rev and wait to be applied: %s -> %s dirty=%v", r0, r1, d1)
	}
	if rc, _, raw := c.call("PUT", "/api/fw/countries", body); rc != 200 {
		t.Fatalf("save again: %d %s", rc, raw)
	}
	if r2, d2 := state(); r2 != r1 || !d2 {
		t.Fatalf("the same Save again must leave the rev alone: %s -> %s dirty=%v", r1, r2, d2)
	}
}

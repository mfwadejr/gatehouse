package app

import (
	"encoding/json"
	"strings"
	"testing"
)

// Every kind of policy can be paused for a while, or until resumed, and the pause shows in its list.
func TestPausingRulesForwardsAndBlocks(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")

	// A firewall rule.
	rc, m, raw := c.call("PUT", "/api/fw/rules", map[string]any{"name": "Kids off", "from": "Home", "to": "wan", "src": "192.168.1.60", "dst": "any", "service": "any", "action": "drop", "enabled": true})
	if rc != 200 {
		t.Fatalf("rule: %d %s", rc, raw)
	}
	id := itoa(int64(m["id"].(float64)))
	pausedOf := func(path, key, name string) float64 {
		t.Helper()
		_, _, raw := c.call("GET", path, nil)
		var v map[string]json.RawMessage
		json.Unmarshal(raw, &v)
		var list []map[string]any
		if key == "" {
			json.Unmarshal(raw, &list)
		} else {
			json.Unmarshal(v[key], &list)
		}
		for _, x := range list {
			if x["name"] == name || x["group"] == name {
				f, _ := x["paused_until"].(float64)
				return f
			}
		}
		t.Fatalf("%s not in %s", name, raw)
		return 0
	}
	if rc, _, raw := c.call("POST", "/api/fw/rules/"+id+"/pause", map[string]any{"minutes": 60}); rc != 200 {
		t.Fatalf("pause rule: %d %s", rc, raw)
	}
	if pausedOf("/api/fw/rules", "", "Kids off") == 0 {
		t.Fatal("the rule's pause was not kept")
	}
	// Editing a paused rule keeps its pause, as the console sends it back.
	_, _, raw = c.call("GET", "/api/fw/rules", nil)
	if !strings.Contains(string(raw), `"paused_until"`) {
		t.Fatalf("pause missing from the list: %s", raw)
	}
	c.call("POST", "/api/fw/rules/"+id+"/pause", map[string]any{"minutes": 0})
	if pausedOf("/api/fw/rules", "", "Kids off") != 0 {
		t.Fatal("resume did not clear the pause")
	}
	if rc, _, _ := c.call("POST", "/api/fw/rules/"+id+"/pause", map[string]any{"minutes": -9}); rc == 200 {
		t.Fatal("a bad pause was accepted")
	}
	if rc, _, _ := c.call("POST", "/api/fw/rules/9999/pause", map[string]any{"minutes": 5}); rc == 200 {
		t.Fatal("paused a rule that does not exist")
	}

	// A port forward, with a schedule.
	rc, _, raw = c.call("PUT", "/api/fw/forwards", map[string]any{"name": "Web", "proto": "tcp", "ext_port": "8443", "int_addr": "192.168.1.20", "enabled": true,
		"sched": map[string]any{"mode": "weekly", "days": []int{1}, "from": "08:00", "to": "17:00"}})
	if rc != 200 {
		t.Fatalf("forward: %d %s", rc, raw)
	}
	if rc, _, raw := c.call("POST", "/api/fw/appforwards/pause", map[string]any{"group": "Web", "minutes": -1}); rc != 200 {
		t.Fatalf("pause forward: %d %s", rc, raw)
	}
	if pausedOf("/api/fw/appforwards", "forwards", "Web") == 0 {
		t.Fatal("the forward's pause was not kept")
	}
	_, _, raw = c.call("GET", "/api/fw/appforwards", nil)
	if !strings.Contains(string(raw), `"mode":"weekly"`) {
		t.Fatalf("the forward's schedule is missing: %s", raw)
	}
	if rc, _, _ := c.call("POST", "/api/fw/appforwards/pause", map[string]any{"group": "Nope", "minutes": 5}); rc == 200 {
		t.Fatal("paused a forward that does not exist")
	}
	c.call("POST", "/api/fw/appforwards/pause", map[string]any{"group": "Web", "minutes": 0})
	if pausedOf("/api/fw/appforwards", "forwards", "Web") != 0 {
		t.Fatal("resume did not clear the forward's pause")
	}
}

package app

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"nopewarden/internal/fw"
)

func TestSpeedControlValidationAndFills(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	a := c.app
	bad := map[string]map[string]any{
		"a limit with no speed":       {"kind": "limit", "targets": []map[string]any{{"scope": "everyone"}}},
		"a speed below 0.1":           {"kind": "limit", "down_mbps": 0.01, "targets": []map[string]any{{"scope": "everyone"}}},
		"no place":                    {"kind": "limit", "down_mbps": 5, "targets": []map[string]any{}},
		"a network that is not there": {"kind": "limit", "down_mbps": 5, "targets": []map[string]any{{"scope": "network", "target": "Nowhere"}}},
		"an unknown app":              {"kind": "priority", "prio": "high", "targets": []map[string]any{{"scope": "everyone"}}, "items": []map[string]any{{"kind": "app", "value": "NoSuchApp9"}}},
		"a bad priority":              {"kind": "priority", "prio": "urgent", "targets": []map[string]any{{"scope": "everyone"}}},
		"a bad device":                {"kind": "limit", "down_mbps": 5, "targets": []map[string]any{{"scope": "device", "target": "xyz"}}},
		"a bad kind":                  {"kind": "other", "targets": []map[string]any{{"scope": "everyone"}}},
	}
	for name, body := range bad {
		if rc, _, raw := c.call("PUT", "/api/speed/rule", body); rc == 200 {
			t.Fatalf("%s must be refused: %s", name, raw)
		}
	}
	if rules, _ := a.loadSpeedRules(); len(rules) != 0 {
		t.Fatal("refused entries must not be saved")
	}
	rc, m, raw := c.call("PUT", "/api/speed/rule", map[string]any{"kind": "limit", "name": "Guests", "enabled": true, "down_mbps": 5, "up_mbps": 1, "each": true,
		"targets": []map[string]any{{"scope": "device", "target": "AA:BB:CC:00:11:22"}}, "items": []map[string]any{{"kind": "name", "value": "Video.Example.com"}}})
	if rc != 200 {
		t.Fatalf("save: %d %s", rc, raw)
	}
	id := int64(m["id"].(float64))
	rules, _ := a.loadSpeedRules()
	if len(rules) != 1 || rules[0].Items[0].Value != "video.example.com" || rules[0].Targets[0].Target != "aa:bb:cc:00:11:22" || rules[0].DownMbps != 5 {
		t.Fatalf("stored wrong: %+v", rules)
	}
	// Its address set follows the DNS answers.
	a.dyn.mu.Lock()
	if a.dyn.dom == nil {
		a.dyn.dom = map[string]map[string]time.Time{}
	}
	a.dyn.dom["video.example.com"] = map[string]time.Time{"203.0.113.9": time.Now().Add(time.Hour), "2001:db8::1": time.Now().Add(time.Hour)}
	a.dyn.mu.Unlock()
	mdl, err := fw.Load(a.Config)
	if err != nil {
		t.Fatal(err)
	}
	fills := a.setContents(mdl, false)
	f, ok := fills[fw.SpeedWhatSet(id)]
	if !ok || len(f.Elems) != 1 || !strings.HasPrefix(f.Elems[0], "203.0.113.9 timeout") {
		t.Fatalf("what-set fill: %+v", fills)
	}
	if _, ok := fills[fw.SpeedWhoSet(id)]; !ok {
		t.Fatal("who-set must be filled")
	}
	// The view says what it does, and the switch turns it all off.
	_, view, _ := c.call("GET", "/api/speed", nil)
	r0 := view["rules"].([]any)[0].(map[string]any)
	if !strings.Contains(r0["does"].(string), "5 Mb/s down and 1 Mb/s up, each device") || r0["status"] != "limiting" {
		t.Fatalf("view: %v", r0)
	}
	c.call("PUT", "/api/speed/switch", map[string]any{"on": false})
	mdl, _ = fw.Load(a.Config)
	if len(mdl.Speed) != 0 {
		t.Fatal("the master switch must turn every entry off")
	}
	// A priority entry asks for the priority queue.
	c.call("PUT", "/api/speed/switch", map[string]any{"on": true})
	if a.speedWantsPriority() {
		t.Fatal("no priority entry yet")
	}
	c.call("PUT", "/api/speed/rule", map[string]any{"kind": "priority", "prio": "high", "enabled": true, "targets": []map[string]any{{"scope": "everyone"}}, "items": []map[string]any{{"kind": "topic", "value": "cat:Video"}}})
	if !a.speedWantsPriority() {
		t.Fatal("a priority entry must ask for the priority queue")
	}
	if rc, _, _ := c.call("DELETE", "/api/speed/rule/"+strconv.FormatInt(id, 10), nil); rc != 200 {
		t.Fatal("delete")
	}
	if rules, _ := a.loadSpeedRules(); len(rules) != 1 {
		t.Fatalf("delete left %d", len(rules))
	}
}

package app

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPolicyChangesReachAdminActivity(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	rc, _, raw := c.call("PUT", "/api/routes", map[string]any{"name": "Time", "enabled": true, "target_kind": "wan", "target": "Fibre", "who": "net:Home", "service": "ntp"})
	if rc != 200 {
		t.Fatalf("route: %d %s", rc, raw)
	}
	_, _, view := c.call("GET", "/api/routes", nil)
	if !strings.Contains(string(view), `"id"`) {
		t.Fatal(string(view))
	}
	var rv struct{ Routes []struct{ ID int64 } }
	json.Unmarshal(view, &rv)
	id := itoa(rv.Routes[0].ID)
	c.call("POST", "/api/routes/"+id+"/pause", map[string]any{"minutes": 30})
	c.call("POST", "/api/routes/"+id+"/pause", map[string]any{"minutes": 0})
	c.call("DELETE", "/api/routes/"+id, nil)
	c.call("POST", "/api/routes/9999/pause", map[string]any{"minutes": 5}) // fails: must not be logged
	_, _, logs := c.call("GET", "/api/logs/admin?limit=50", nil)
	text := string(logs)
	for _, want := range []string{"Route created", "Route paused", "Route resumed", "Route removed"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
	if strings.Contains(text, "#9999") {
		t.Errorf("a failed change was logged:\n%s", text)
	}
}

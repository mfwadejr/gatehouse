package app

import (
	"strings"
	"testing"
)

func TestPatternRulesThroughTheConsoleInterface(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	if rc, _, _ := c.call("POST", "/api/dns/rules", map[string]any{"domain": ".*", "action": "block", "kind": "re"}); rc == 200 {
		t.Fatal("a match-everything pattern was accepted")
	}
	_, res, _ := c.call("POST", "/api/dns/rules/try", map[string]any{"kind": "glob", "pattern": "ads*.example.net", "name": "ads1.example.net"})
	if res["error"] != nil || res["name_matches"] != true {
		t.Fatalf("try: %v", res)
	}
	_, res, _ = c.call("POST", "/api/dns/rules/try", map[string]any{"kind": "glob", "pattern": "ads*.example.net", "name": "www.example.net"})
	if res["name_matches"] == true {
		t.Fatalf("a name that does not match was reported as matching: %v", res)
	}
	_, res, _ = c.call("POST", "/api/dns/rules/try", map[string]any{"kind": "re", "pattern": "(", "name": "a.com"})
	if res["error"] == nil {
		t.Fatalf("a broken pattern had no explanation: %v", res)
	}
	if rc, _, raw := c.call("POST", "/api/dns/rules", map[string]any{"domain": "ads*.example.net", "action": "block", "kind": "glob", "note": "ads"}); rc != 200 {
		t.Fatalf("save: %d %s", rc, raw)
	}
	if rc, _, _ := c.call("POST", "/api/dns/rules", map[string]any{"domain": "plain.example.net", "action": "allow"}); rc != 200 {
		t.Fatal("plain rule failed")
	}
	rc, _, raw := c.call("GET", "/api/dns/rules", nil)
	if rc != 200 || !strings.Contains(string(raw), `"kind":"glob"`) || !strings.Contains(string(raw), `"text":"ads*.example.net"`) || !strings.Contains(string(raw), `"kind":"name"`) {
		t.Fatalf("list: %d %s", rc, raw)
	}
}

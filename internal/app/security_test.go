package app

import "testing"

func TestSecurityCheckVerdictAndFixes(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	rc, m, raw := c.call("GET", "/api/security/check", nil)
	if rc != 200 {
		t.Fatalf("%d %s", rc, raw)
	}
	items, _ := m["items"].([]any)
	if len(items) < 6 || m["summary"] == "" {
		t.Fatalf("%s", raw)
	}
	for _, it := range items {
		x := it.(map[string]any)
		if x["level"] != "ok" && x["fix"] == nil {
			t.Errorf("%v has no fix button", x["id"])
		}
	}
}

package app

import (
	"testing"

	"nopewarden/internal/fw"
)

// The counts endpoint takes a fresh look at the counters and returns each rule's total, each
// service's own count and the built-in console rules, and nothing that belongs to device meters.
func TestRuleCountsEndpoint(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	f := c.app.FW.(*fakeFW)
	f.mu.Lock()
	f.counters = map[string]fw.Counter{
		"rule:Home:wan:Web":              {Packets: 5, Bytes: 500},
		"rule:Home:wan:Web#svc:tcp/80":   {Packets: 3, Bytes: 300},
		"protected: management console":  {Packets: 9, Bytes: 900},
		fw.AcctPrefix + "up:192.168.1.5": {Packets: 99},
	}
	f.mu.Unlock()
	rc, m, raw := c.call("GET", "/api/fw/rules/counts", nil)
	if rc != 200 {
		t.Fatalf("%d %s", rc, raw)
	}
	hits, _ := m["hits"].(map[string]any)
	for _, k := range []string{"rule:Home:wan:Web", "rule:Home:wan:Web#svc:tcp/80", "protected: management console"} {
		h, ok := hits[k].(map[string]any)
		if !ok || h["hits"].(float64) == 0 {
			t.Fatalf("no count for %q in %v", k, hits)
		}
	}
	if len(hits) != 3 {
		t.Fatalf("device meters leaked into the rule counts: %v", hits)
	}
}

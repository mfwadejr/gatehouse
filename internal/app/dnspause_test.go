package app

import "testing"

func TestPausingBlockingActsAtOnceAndIsReported(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	c.stage = true // from the console: a pause must still act at once, not wait for Review
	paused := func() bool {
		_, m, _ := c.call("GET", "/api/dns/summary", nil)
		p, _ := m["pause"].(map[string]any)
		b, _ := p["paused"].(bool)
		return b
	}
	if paused() {
		t.Fatal("blocking should start switched on")
	}
	if rc, _, raw := c.call("POST", "/api/dns/pause", map[string]any{"minutes": 5}); rc != 200 {
		t.Fatalf("pause: %d %s", rc, raw)
	}
	if !paused() {
		t.Fatal("the pause did not take effect at once")
	}
	if rc, _, _ := c.call("POST", "/api/dns/pause", map[string]any{"minutes": 100000}); rc == 200 {
		t.Fatal("a silly length was accepted")
	}
	if rc, _, raw := c.call("POST", "/api/dns/resume", nil); rc != 200 || paused() {
		t.Fatalf("resume: %d %s", rc, raw)
	}
	if rc, _, _ := c.call("POST", "/api/dns/pause", map[string]any{"minutes": 0}); rc != 200 || !paused() {
		t.Fatal("pause until resumed did not hold")
	}
}

func TestBlockedAnswerTimeAndPrivateRelaySettingsAreSaved(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	s := dnsSettings(5000)
	s["private_relay"] = true
	s["block_ttl"] = 15
	if rc, _, raw := c.call("PUT", "/api/dns/settings", s); rc != 200 {
		t.Fatalf("%d %s", rc, raw)
	}
	_, m, _ := c.call("GET", "/api/dns/settings", nil)
	if m["private_relay"] != true || m["block_ttl"] != float64(15) {
		t.Fatalf("not saved: %v", m)
	}
	s["block_ttl"] = 99999
	if rc, _, _ := c.call("PUT", "/api/dns/settings", s); rc == 200 {
		t.Fatal("an out of range time was accepted")
	}
}

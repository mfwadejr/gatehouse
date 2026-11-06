package app

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

func dnsSettings(cache int) map[string]any {
	return map[string]any{"block_mode": "null", "local_domain": "lan", "rate_limit": 0, "rate_burst": 0,
		"cache_size": cache, "canary": false, "log_retention_days": 7, "anonymize": false}
}

func TestDNSChangesWaitForReviewAndCanBeDiscarded(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	state := func() bool {
		_, m, _ := c.call("GET", "/api/applystate", nil)
		d, _ := m["dns"].(map[string]any)
		b, _ := d["dirty"].(bool)
		return b
	}
	// Without the console's header a change is live at once, as before.
	if rc, _, raw := c.call("PUT", "/api/dns/settings", dnsSettings(5000)); rc != 200 || state() {
		t.Fatalf("plain save: %d %s dirty=%v", rc, raw, state())
	}
	// From the console it waits.
	c.stage = true
	if rc, _, raw := c.call("PUT", "/api/dns/settings", dnsSettings(6000)); rc != 200 {
		t.Fatalf("%d %s", rc, raw)
	}
	if !state() {
		t.Fatal("a DNS change from the console should wait")
	}
	_, m, _ := c.call("GET", "/api/staged", nil)
	if m["dns"] != true || lastStagedID(m, "Changed DNS settings.") == 0 {
		t.Fatalf("review list: %v", m)
	}
	// Apply makes it live and empties the list.
	if rc, _, raw := c.call("POST", "/api/dns/apply", nil); rc != 200 {
		t.Fatalf("%d %s", rc, raw)
	}
	if state() {
		t.Fatal("applied, so nothing is waiting")
	}
	_, m, _ = c.call("GET", "/api/staged", nil)
	if lastStagedID(m, "Changed DNS settings.") != 0 {
		t.Fatal("a DNS change that is live is no longer in the waiting list")
	}
	// A second change, then discard puts the old value back and stops waiting.
	if rc, _, raw := c.call("PUT", "/api/dns/settings", dnsSettings(7000)); rc != 200 {
		t.Fatalf("%d %s", rc, raw)
	}
	if !state() {
		t.Fatal("waiting again")
	}
	_, m, _ = c.call("GET", "/api/staged", nil)
	id := lastStagedID(m, "Changed DNS settings.")
	if rc, _, raw := c.call("POST", "/api/staged/discard", map[string]any{"from": id}); rc != 200 {
		t.Fatalf("discard: %d %s", rc, raw)
	}
	if state() {
		t.Fatal("discarded, so nothing waits")
	}
	_, m, _ = c.call("GET", "/api/dns/settings", nil)
	if got, _ := m["cache_size"].(float64); got != 6000 {
		t.Fatalf("discard should bring back 6000, got %v (%v)", got, m)
	}
}

func lastStagedID(m map[string]any, text string) int64 {
	var id int64
	items, _ := m["items"].([]any)
	for _, it := range items {
		x, _ := it.(map[string]any)
		if x["text"] == text {
			id = int64(x["id"].(float64))
		}
	}
	return id
}

func TestVPNSpeedEndpointAnswers(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	rc, m, raw := c.call("GET", "/api/vpn/speed", nil)
	if rc != 200 {
		t.Fatalf("%d %s", rc, raw)
	}
	if _, ok := m["vpns"].(map[string]any); !ok {
		t.Fatalf("expected a vpns object: %s", raw)
	}
}

func TestForgettingADeviceWaitsForReviewAndCanBeDiscarded(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	rc, _, raw := c.call("POST", "/api/devices", map[string]any{"mac": "aa:bb:cc:00:11:22", "name": "Old TV"})
	if rc != 200 {
		t.Fatalf("create: %d %s", rc, raw)
	}
	var made struct{ ID int64 }
	json.Unmarshal(raw, &made)
	c.stage = true
	rc, _, raw = c.call("DELETE", "/api/devices/"+strconv.FormatInt(made.ID, 10), nil)
	c.stage = false
	if rc != 200 {
		t.Fatalf("staged forget: %d %s", rc, raw)
	}
	_, _, raw = c.call("GET", "/api/devices", nil)
	if !strings.Contains(string(raw), "Old TV") || !strings.Contains(string(raw), `"forgetting":true`) {
		t.Fatalf("the device must still be listed, marked as being forgotten: %s", raw)
	}
	_, _, raw = c.call("GET", "/api/staged", nil)
	if !strings.Contains(string(raw), "Forgot the device Old TV.") {
		t.Fatalf("Review must say what is waiting: %s", raw)
	}
	if rc, _, raw = c.call("POST", "/api/dns/apply", map[string]any{}); rc != 200 {
		t.Fatalf("apply: %d %s", rc, raw)
	}
	_, _, raw = c.call("GET", "/api/devices", nil)
	if strings.Contains(string(raw), "Old TV") {
		t.Fatalf("applying must forget the device: %s", raw)
	}
}

func TestDiscardingAForgetKeepsTheDevice(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	_, _, raw := c.call("POST", "/api/devices", map[string]any{"mac": "aa:bb:cc:00:11:33", "name": "Keep me"})
	var made struct{ ID int64 }
	json.Unmarshal(raw, &made)
	c.stage = true
	c.call("PUT", "/api/dns/settings", dnsSettings(4000)) // a waiting change after the device exists
	c.call("DELETE", "/api/devices/"+strconv.FormatInt(made.ID, 10), nil)
	c.stage = false
	_, m, _ := c.call("GET", "/api/staged", nil)
	id := lastStagedID(m, "Forgot the device Keep me.")
	if id == 0 {
		t.Fatalf("the forget must be listed: %v", m)
	}
	if rc, _, raw := c.call("POST", "/api/staged/discard", map[string]any{"from": id}); rc != 200 {
		t.Fatalf("discard: %d %s", rc, raw)
	}
	_, _, raw = c.call("GET", "/api/devices", nil)
	c.call("POST", "/api/dns/apply", map[string]any{})
	_, _, raw = c.call("GET", "/api/devices", nil)
	if !strings.Contains(string(raw), "Keep me") || strings.Contains(string(raw), `"forgetting":true`) {
		t.Fatalf("a discarded forget must leave the device alone: %s", raw)
	}
}

func TestBlockingChangesWaitForReviewToo(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	a := c.app
	if a == nil {
		t.Skip("this box does not expose the app")
	}
	inForce := func() int { return len(a.cachedScopeRules()) }
	before := inForce()
	c.stage = true
	rc, _, raw := c.call("PUT", "/api/blocks", map[string]any{"kind": "name", "value": "games.example.com", "scope": "everyone", "enabled": true})
	c.stage = false
	if rc != 200 {
		t.Fatalf("save block: %d %s", rc, raw)
	}
	if inForce() != before {
		t.Fatalf("a waiting block must not be in force yet (%d rules, had %d)", inForce(), before)
	}
	_, _, raw = c.call("GET", "/api/staged", nil)
	if !strings.Contains(string(raw), "block") {
		t.Fatalf("Review must list the block: %s", raw)
	}
	if rc, _, raw = c.call("POST", "/api/dns/apply", map[string]any{}); rc != 200 {
		t.Fatalf("apply: %d %s", rc, raw)
	}
	if inForce() != before+1 {
		t.Fatalf("applying must put the block in force (%d rules, had %d)", inForce(), before)
	}
}

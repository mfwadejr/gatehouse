package app

import (
	"encoding/json"
	"testing"
)

// Every read-only page of the console answers on a fresh box with well-formed JSON, so a broken
// query or a missing table shows up here by name and not on someone's screen.
func TestReadPagesAnswerOnAFreshBox(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	for _, p := range []string{
		"/api/apps",
		"/api/auth/sessions",
		"/api/blocks",
		"/api/blocks/versions",
		"/api/dhcp/alerts",
		"/api/dhcp/leases",
		"/api/dhcp/reservations",
		"/api/dhcp/utilisation",
		"/api/diag/connections",
		"/api/diag/health",
		"/api/dns/lists",
		"/api/dns/summary",
		"/api/fw/geo",
		"/api/fw/pickers",
		"/api/fw/presets",
		"/api/internet/nat",
		"/api/logs/admin",
		"/api/logs/countries",
		"/api/logs/storage",
		"/api/logs/summary",
		"/api/logs/usage",
		"/api/net/status",
		"/api/net/throughput",
		"/api/net/zone-speed",
		"/api/overview/busiest",
		"/api/overview/devices",
		"/api/overview/happened",
		"/api/overview/history",
		"/api/overview/home",
		"/api/overview/map",
		"/api/overview/security",
		"/api/setup/wizard",
		"/api/system/disk",
		"/api/system/health",
		"/api/traffic/apps",
		"/api/traffic/domains",
		"/api/traffic/usage",
		"/api/vpn/choices",
	} {
		rc, _, raw := c.call("GET", p, nil)
		if rc != 200 {
			t.Errorf("GET %s: %d %s", p, rc, raw)
			continue
		}
		if !json.Valid(raw) {
			t.Errorf("GET %s: not JSON: %.80s", p, raw)
		}
	}
}

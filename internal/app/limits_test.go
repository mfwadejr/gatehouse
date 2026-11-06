package app

import (
	"fmt"
	"strings"
	"testing"

	"nopewarden/internal/fw"
)

// Each table that has a limit is filled to it and one past it: the last allowed row saves, the next
// is refused in plain words, and the rows already there are untouched.

func TestRoutesTableFillsToItsLimit(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	for i := 0; i < fw.MaxRoutes; i++ {
		rc, _, raw := c.call("PUT", "/api/routes", map[string]any{"name": fmt.Sprintf("R%02d", i), "enabled": true, "target_kind": "wan", "target": "Fibre", "who": "net:Home", "service": fmt.Sprintf("tcp/%d", 2000+i)})
		if rc != 200 {
			t.Fatalf("route %d: %d %s", i, rc, raw)
		}
	}
	rc, _, raw := c.call("PUT", "/api/routes", map[string]any{"name": "One too many", "enabled": true, "target_kind": "wan", "target": "Fibre", "who": "net:Home", "service": "tcp/3000"})
	if rc == 200 || !strings.Contains(string(raw), fmt.Sprintf("up to %d routes", fw.MaxRoutes)) {
		t.Fatalf("past the limit: %d %s", rc, raw)
	}
	_, _, view := c.call("GET", "/api/routes", nil)
	if n := strings.Count(string(view), `"name":"R`); n != fw.MaxRoutes {
		t.Fatalf("%d routes after refusal, want %d", n, fw.MaxRoutes)
	}
}

func TestConnectionGroupsFillToTheirLimit(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	members := []map[string]any{{"wan_id": wanIDByName(t, c, "Fibre"), "weight": 1}}
	for i := 0; i < fw.MaxGroups; i++ {
		rc, _, raw := c.call("PUT", "/api/net/groups", map[string]any{"name": fmt.Sprintf("G%d", i), "mode": "failover", "members": members})
		if rc != 200 {
			t.Fatalf("group %d: %d %s", i, rc, raw)
		}
	}
	rc, _, raw := c.call("PUT", "/api/net/groups", map[string]any{"name": "Extra", "mode": "failover", "members": members})
	if rc == 200 || !strings.Contains(string(raw), fmt.Sprintf("up to %d groups", fw.MaxGroups)) {
		t.Fatalf("past the limit: %d %s", rc, raw)
	}
}

func TestCountriesFillToTheirLimit(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	var ccs []string
	for i := 0; i < fw.MaxCountries+1; i++ {
		ccs = append(ccs, fmt.Sprintf("%c%c", 'A'+i/26, 'A'+i%26))
	}
	if rc, _, raw := c.call("PUT", "/api/fw/countries", map[string]any{"enabled": true, "mode": "block", "dir": "both", "countries": ccs[:fw.MaxCountries], "confirm": true}); rc != 200 {
		t.Fatalf("at the limit: %d %s", rc, raw)
	}
	rc, _, raw := c.call("PUT", "/api/fw/countries", map[string]any{"enabled": true, "mode": "block", "dir": "both", "countries": ccs, "confirm": true})
	if rc == 200 || !strings.Contains(string(raw), "Choose up to 40") {
		t.Fatalf("past the limit: %d %s", rc, raw)
	}
}

// Each website needs a set for each address family, so the limit of 96 is reached at about 48 websites.
func TestFirewallSetsFillToTheirLimit(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	var doms []string
	for i := 0; i < fw.MaxDynSets/2+1; i++ {
		doms = append(doms, fmt.Sprintf("dom:site%d.example.com", i))
	}
	rc, _, raw := c.call("PUT", "/api/fw/rules", map[string]any{"name": "Lots", "from": "Home", "to": "Internet", "dst": strings.Join(doms[:fw.MaxDynSets/2-1], ","), "action": "drop", "enabled": true})
	if rc != 200 {
		t.Fatalf("at the limit: %d %s", rc, raw)
	}
	rc, _, raw = c.call("PUT", "/api/fw/rules", map[string]any{"name": "Even more", "from": "Home", "to": "Internet", "dst": strings.Join(doms, ","), "action": "drop", "enabled": true})
	if rc == 200 || !strings.Contains(string(raw), "limit is") {
		t.Fatalf("past the limit: %d %s", rc, raw)
	}
}

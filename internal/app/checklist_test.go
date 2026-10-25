package app

import "testing"

func TestChecklistFollowsWhatTheMachineHas(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	rc, m, raw := c.call("GET", "/api/setup/checklist", nil)
	if rc != 200 {
		t.Fatalf("%d %s", rc, raw)
	}
	byID := map[string]bool{}
	for _, it := range m["items"].([]any) {
		x := it.(map[string]any)
		byID[x["id"].(string)] = x["done"].(bool)
	}
	// siteBox sets up a LAN zone and an internet link but no DHCP scope or two-step sign-in.
	if !byID["internet"] {
		t.Fatalf("internet should be done: %v", byID)
	}
	if byID["twostep"] {
		t.Fatalf("two-step is not on: %v", byID)
	}
	if rc, _, _ := c.call("PUT", "/api/setup/checklist", map[string]any{"hidden": true}); rc != 200 {
		t.Fatal("hide failed")
	}
	_, m, _ = c.call("GET", "/api/setup/checklist", nil)
	if m["hidden"] != true {
		t.Fatalf("not hidden: %v", m)
	}
	// Adding a network gives it a DHCP scope: the network item is then done.
	if rc, _, raw := c.call("POST", "/api/networks/add", map[string]any{"name": "Cameras", "port": "eth2", "internet": true, "isolate": false, "dhcp": "server"}); rc != 200 {
		t.Fatalf("add: %d %s", rc, raw)
	}
	_, m, _ = c.call("GET", "/api/setup/checklist", nil)
	for _, it := range m["items"].([]any) {
		if x := it.(map[string]any); x["id"] == "network" && x["done"] != true {
			t.Fatalf("network should be done: %v", x)
		}
	}
}

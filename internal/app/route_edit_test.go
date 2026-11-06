package app

import (
	"encoding/json"
	"testing"
)

// Editing a route in the console sends the route back, and saving must work however much of the
// list's own wording comes with it.
func TestAnEditedRouteCanBeSavedBack(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	if rc, _, raw := c.call("PUT", "/api/routes", map[string]any{"name": "Sites", "enabled": true, "target_kind": "wan", "target": "Fibre", "who": "net:Home", "where": "dom:xhamster.com"}); rc != 200 {
		t.Fatalf("create: %d %s", rc, raw)
	}
	_, _, raw := c.call("GET", "/api/routes", nil)
	var v struct{ Routes []map[string]any }
	json.Unmarshal(raw, &v)
	if len(v.Routes) != 1 {
		t.Fatalf("routes: %s", raw)
	}
	r := v.Routes[0]
	r["where"] = "dom:xhamster.com,dom:example.org"
	rc, _, out := c.call("PUT", "/api/routes", r) // the route exactly as the list showed it
	if rc != 200 {
		t.Fatalf("a route sent back whole must save: %d %s", rc, out)
	}
	_, _, raw = c.call("GET", "/api/routes", nil)
	var after struct{ Routes []struct{ Where string } }
	json.Unmarshal(raw, &after)
	if len(after.Routes) != 1 || after.Routes[0].Where != "dom:xhamster.com,dom:example.org" {
		t.Fatalf("the edit was not kept, or a second route appeared: %s", raw)
	}
	// A field that is not part of a route is still refused.
	if rc, _, _ := c.call("PUT", "/api/routes", map[string]any{"name": "x", "bogus": 1}); rc != 400 {
		t.Fatalf("an unknown field must still be refused, got %d", rc)
	}
}

package app

import (
	"encoding/json"
	"strings"
	"testing"

	"gatehouse/internal/fw"
	"gatehouse/internal/netcfg"
)

func TestRoutesSendOnlyChosenDestinationsThroughATunnel(t *testing.T) {
	c, fn, ff := siteBox(t, "192.168.1.1/24")
	if fw.RouteMarkBase != netcfg.MarkBase {
		t.Fatal("the firewall and the network settings disagree about the first route mark")
	}
	rc, m, raw := c.call("POST", "/api/vpn/wireguard/client", map[string]any{"label": "Mullvad", "conf": wgProvider})
	if rc != 200 {
		t.Fatalf("wg client: %d %s", rc, raw)
	}
	dev, _ := m["name"].(string)
	if dev == "" {
		dev = "wg0"
	}
	// A tunnel can be added with nobody using it, and a route can be saved with nobody yet.
	rc, rv, raw := c.call("PUT", "/api/routes", map[string]any{"name": "Streaming", "enabled": true, "target_kind": "tunnel", "target": dev, "kill": true,
		"uses": []map[string]string{}, "dests": []map[string]string{{"kind": "domain", "value": "https://www.example.com/watch"}}})
	if rc != 200 {
		t.Fatalf("route: %d %s", rc, raw)
	}
	var list []map[string]any
	b, _ := json.Marshal(rv["routes"])
	json.Unmarshal(b, &list)
	if len(list) != 1 || list[0]["state"] != "nobody" {
		t.Fatalf("a route with nobody yet should say so: %s", b)
	}
	if strings.Contains(ff.script, "route:Streaming") {
		t.Error("a route nobody uses was put in the firewall")
	}
	// Choose who uses it.
	rc, rv, raw = c.call("PUT", "/api/routes", map[string]any{"id": list[0]["id"], "name": "Streaming", "enabled": true, "target_kind": "tunnel", "target": dev, "kill": true,
		"uses":  []map[string]string{{"kind": "network", "value": "Home"}},
		"dests": []map[string]string{{"kind": "domain", "value": "https://www.example.com/watch"}, {"kind": "ip", "value": "203.0.113.0/24"}, {"kind": "region", "value": "nl"}}})
	if rc != 200 {
		t.Fatalf("route 2: %d %s", rc, raw)
	}
	for _, want := range []string{"chain routemark", "ip saddr 192.168.1.0/24", "ip daddr 203.0.113.0/24", "ip daddr @dm_", "ip daddr @gc_nl_4", "meta mark set 0x1000", "killswitch:Streaming"} {
		if !strings.Contains(ff.script, want) {
			t.Errorf("firewall lacks %q\n%s", want, ff.script)
		}
	}
	if strings.Contains(ff.script, "www.example.com") && !strings.Contains(ff.script, "set dm_") {
		t.Error("the website did not become a set")
	}
	var cl *netcfg.WG
	for i := range fn.plan.Config.WireGuard {
		if fn.plan.Config.WireGuard[i].Mode == "client" {
			cl = &fn.plan.Config.WireGuard[i]
		}
	}
	if cl == nil || len(cl.Marks) != 1 || cl.Marks[0] != 0x1000 || cl.Table != 300 {
		t.Fatalf("the tunnel does not carry the mark: %+v", cl)
	}
	// A bad destination is refused in plain words.
	if rc, _, raw := c.call("PUT", "/api/routes", map[string]any{"name": "Bad", "enabled": true, "target_kind": "tunnel", "target": dev,
		"dests": []map[string]string{{"kind": "domain", "value": "not a site"}}}); rc != 400 || !strings.Contains(string(raw), "website name") {
		t.Errorf("bad website: %d %s", rc, raw)
	}
	// A route to a VPN that does not exist is refused.
	if rc, _, _ := c.call("PUT", "/api/routes", map[string]any{"name": "Ghost", "enabled": true, "target_kind": "tunnel", "target": "wg9"}); rc != 400 {
		t.Errorf("route to a missing VPN: %d", rc)
	}
	// Removing it takes the mark away again.
	id := int64(list[0]["id"].(float64))
	if rc, _, raw := c.call("DELETE", "/api/routes/"+itoa(id), nil); rc != 200 {
		t.Fatalf("delete: %d %s", rc, raw)
	}
	if strings.Contains(ff.script, "route:Streaming") {
		t.Error("the route is still in the firewall")
	}
	for i := range fn.plan.Config.WireGuard {
		if len(fn.plan.Config.WireGuard[i].Marks) > 0 {
			t.Error("a mark remains in the network settings")
		}
	}
}

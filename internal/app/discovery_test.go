package app

import (
	"strings"
	"testing"

	"gatehouse/internal/fw"
)

func TestDiscoveryPairsReachTheFirewall(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	if rc, _, raw := c.call("PUT", "/api/fw/zones", map[string]any{"name": "Smart-home", "kind": "isolated", "ifaces": []string{"eth2"}, "internet": true, "gateway": true}); rc != 200 {
		t.Fatalf("zone: %d %s", rc, raw)
	}
	// Pairing a network with itself, or with one that does not exist, is refused.
	if rc, _, _ := c.call("PUT", "/api/fw/discovery", map[string]any{"pairs": []map[string]string{{"a": "Home", "b": "Home"}}}); rc == 200 {
		t.Fatal("self pair accepted")
	}
	if rc, _, _ := c.call("PUT", "/api/fw/discovery", map[string]any{"pairs": []map[string]string{{"a": "Home", "b": "Nowhere"}}}); rc == 200 {
		t.Fatal("unknown network accepted")
	}
	if rc, _, _ := c.call("PUT", "/api/fw/discovery", map[string]any{"pairs": []map[string]string{{"a": "Home", "b": "Smart-home"}}, "kinds": []string{"nonsense"}}); rc == 200 {
		t.Fatal("unknown kind accepted")
	}
	rc, _, raw := c.call("PUT", "/api/fw/discovery", map[string]any{"pairs": []map[string]string{{"a": "home", "b": "Smart-home"}, {"a": "Smart-home", "b": "Home"}}, "kinds": []string{"printers"}})
	if rc != 200 {
		t.Fatalf("%d %s", rc, raw)
	}
	_, m, _ := c.call("GET", "/api/fw/discovery", nil)
	if len(m["pairs"].([]any)) != 1 {
		t.Fatalf("duplicate pair kept: %v", m["pairs"])
	}
	model, err := fw.Load(c.app.Config)
	if err != nil {
		t.Fatal(err)
	}
	if len(model.MDNS) != 2 {
		t.Fatalf("interfaces: %v", model.MDNS)
	}
	script, err := fw.Compile(model)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(script, "udp dport 5353 accept") {
		t.Fatalf("no discovery rule:\n%s", script)
	}
	// Taking the pairs away takes the rule away.
	c.call("PUT", "/api/fw/discovery", map[string]any{"pairs": []map[string]string{}})
	model, _ = fw.Load(c.app.Config)
	script, _ = fw.Compile(model)
	if strings.Contains(script, "udp dport 5353") {
		t.Fatal("the rule stayed")
	}
}

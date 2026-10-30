package app

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestUPnPCardNeedsChoicesAndRemembersThem(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	rc, m, raw := c.call("GET", "/api/upnp/card", nil)
	if rc != 200 || m["on"] != false {
		t.Fatalf("%d %s", rc, raw)
	}
	nets := m["networks"].([]any)
	if len(nets) == 0 {
		t.Fatalf("no networks: %s", raw)
	}
	nid := nets[0].(map[string]any)["id"].(float64)
	conns0 := m["connections"].([]any)
	if len(conns0) == 0 {
		t.Fatalf("no connection: %s", raw)
	}
	wan := conns0[0].(map[string]any)["name"].(string)

	// Switching on with no choice is refused, and the reason says what is missing.
	rc, _, raw = c.call("PUT", "/api/upnp/card", map[string]any{"on": true, "mode": "network"})
	if rc == 200 || !strings.Contains(string(raw), "Tick at least one network") || !strings.Contains(string(raw), "internet connection") {
		t.Fatalf("must refuse: %d %s", rc, raw)
	}
	rc, _, raw = c.call("PUT", "/api/upnp/card", map[string]any{"on": true, "mode": "devices", "connections": []string{wan}})
	if rc == 200 || !strings.Contains(string(raw), "device") {
		t.Fatalf("devices mode needs a device: %d %s", rc, raw)
	}

	// A complete choice turns it on.
	rc, m, raw = c.call("PUT", "/api/upnp/card", map[string]any{"on": true, "mode": "network", "networks": []float64{nid}, "connections": []string{wan}})
	if rc != 200 || m["on"] != true || !strings.Contains(m["line"].(string), wan) {
		t.Fatalf("%d %s", rc, raw)
	}
	// Turning off keeps the picks, so turning back on needs nothing more.
	rc, m, _ = c.call("PUT", "/api/upnp/card", map[string]any{"on": false, "mode": "network", "networks": []float64{nid}, "connections": []string{wan}})
	if rc != 200 || m["on"] != false {
		t.Fatal("off failed")
	}
	_, m, raw = c.call("GET", "/api/upnp/card", nil)
	var ticked int
	for _, n := range m["networks"].([]any) {
		if n.(map[string]any)["ticked"] == true {
			ticked++
		}
	}
	if ticked != 1 || m["connections"].([]any)[0].(map[string]any)["ticked"] != true {
		t.Fatalf("picks lost: %s", raw)
	}
	// Specific devices: the list is stored and restricts.
	rc, m, raw = c.call("PUT", "/api/upnp/card", map[string]any{"on": true, "mode": "devices", "connections": []string{wan}, "devices": []map[string]string{{"mac": "AA:BB:CC:DD:EE:01", "note": "PS5"}}})
	if rc != 200 || m["on"] != true {
		t.Fatalf("%d %s", rc, raw)
	}
	var st struct{ Status struct{ Restrict bool } }
	b, _ := json.Marshal(m)
	json.Unmarshal(b, &st)
	if !st.Status.Restrict {
		t.Fatal("device mode must restrict")
	}
}

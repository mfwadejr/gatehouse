package app

import "testing"

func TestForcedDNSScopeRoutes(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	rc, m, raw := c.call("GET", "/api/fw/dnsforce", nil)
	if rc != 200 {
		t.Fatalf("dnsforce: %d %s", rc, raw)
	}
	nets, _ := m["networks"].([]any)
	if len(nets) == 0 || m["master"] != true {
		t.Fatalf("starting state: %s", raw)
	}
	first := nets[0].(map[string]any)
	id := first["id"]
	if rc, _, raw := c.call("PUT", "/api/fw/dnsforce/network", map[string]any{"id": id, "mode": "force"}); rc != 200 {
		t.Fatalf("force: %d %s", rc, raw)
	}
	if _, m, _ = c.call("GET", "/api/fw/dnsforce", nil); m["networks"].([]any)[0].(map[string]any)["mode"] != "force" {
		t.Fatal("network not forced after choosing force")
	}
	if rc, _, raw := c.call("PUT", "/api/fw/dnsforce/network", map[string]any{"id": id, "mode": "server", "target": "9.9.9.9"}); rc != 200 {
		t.Fatalf("server: %d %s", rc, raw)
	}
	if rc, _, _ := c.call("PUT", "/api/fw/dnsforce/network", map[string]any{"id": id, "mode": "server", "target": "127.0.0.1"}); rc == 200 {
		t.Fatal("loopback server accepted")
	}
	if rc, _, _ := c.call("PUT", "/api/fw/dnsforce/network", map[string]any{"id": id, "mode": "server"}); rc == 200 {
		t.Fatal("server mode without an address accepted")
	}
	_, m, _ = c.call("GET", "/api/fw/dnsforce", nil)
	got := m["networks"].([]any)[0].(map[string]any)
	if got["mode"] != "server" || got["target"] != "9.9.9.9" {
		t.Fatalf("network after save: %v", got)
	}
	if rc, _, raw := c.call("PUT", "/api/fw/dnsforce/network", map[string]any{"id": id, "mode": "off"}); rc != 200 {
		t.Fatalf("off: %d %s", rc, raw)
	}
	_, m, _ = c.call("GET", "/api/fw/dnsforce", nil)
	if got := m["networks"].([]any)[0].(map[string]any); got["mode"] != "off" || got["target"] != "" {
		t.Fatalf("network left alone: %v", got)
	}

	rc, em, raw := c.call("PUT", "/api/fw/dnsforce/exception", map[string]any{"who": "dev:AA:BB:CC:00:00:09", "mode": "off"})
	if rc != 200 {
		t.Fatalf("exception: %d %s", rc, raw)
	}
	if rc, _, _ := c.call("PUT", "/api/fw/dnsforce/exception", map[string]any{"who": "nonsense", "mode": "off"}); rc == 200 {
		t.Fatal("bad exception accepted")
	}
	if rc, _, _ := c.call("PUT", "/api/fw/dnsforce/exception", map[string]any{"who": "dev:aa:bb:cc:00:00:09", "mode": "server", "target": "not-an-ip"}); rc == 200 {
		t.Fatal("bad exception server accepted")
	}
	// The same device again changes it, not duplicates it.
	c.call("PUT", "/api/fw/dnsforce/exception", map[string]any{"who": "dev:aa:bb:cc:00:00:09", "mode": "gatehouse"})
	_, m, _ = c.call("GET", "/api/fw/dnsforce", nil)
	ex := m["exceptions"].([]any)
	if len(ex) != 1 || ex[0].(map[string]any)["mode"] != "gatehouse" || ex[0].(map[string]any)["who"] != "dev:aa:bb:cc:00:00:09" {
		t.Fatalf("exceptions: %v", ex)
	}
	if rc, _, raw := c.call("DELETE", "/api/fw/dnsforce/exception/"+istr(int(em["id"].(float64))), nil); rc != 200 {
		t.Fatalf("delete: %d %s", rc, raw)
	}
	_, m, _ = c.call("GET", "/api/fw/dnsforce", nil)
	if len(m["exceptions"].([]any)) != 0 {
		t.Fatal("exception not removed")
	}
}

func TestGroupChangeReachesTheVPN(t *testing.T) {
	c, fn, _ := siteBox(t, "192.168.1.1/24")
	a := c.app
	if rc, _, raw := c.call("POST", "/api/devices", map[string]any{"mac": "aa:bb:cc:dd:ee:21", "name": "Phone"}); rc != 200 {
		t.Fatalf("device: %d %s", rc, raw)
	}
	rc, m, raw := c.call("PUT", "/api/fw/groups", map[string]any{"name": "Away", "members": []string{}})
	if rc != 200 {
		t.Fatalf("group: %d %s", rc, raw)
	}
	before := fn.vpnCalls
	// A group no VPN uses does not touch the VPN.
	c.call("PUT", "/api/fw/groups", map[string]any{"id": int64(m["id"].(float64)), "name": "Away", "members": []string{"aa:bb:cc:dd:ee:21"}})
	if fn.vpnCalls != before {
		t.Fatalf("an unused group re-applied the VPN (%d calls)", fn.vpnCalls-before)
	}
	// A group a tunnel sends through the VPN does.
	a.Config.Exec(`INSERT INTO vpn_routes(tunnel,kind,value,label) VALUES('wg9','group','Away','')`)
	before = fn.vpnCalls
	rc, _, raw = c.call("PUT", "/api/fw/groups", map[string]any{"id": int64(m["id"].(float64)), "name": "Away", "members": []string{}})
	if rc != 200 {
		t.Fatalf("group again: %d %s", rc, raw)
	}
	if fn.vpnCalls <= before {
		t.Fatalf("the VPN was not applied again after its group changed: %s", raw)
	}
}

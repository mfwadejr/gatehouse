package app

import (
	"encoding/json"
	"strings"
	"testing"

	"gatehouse/internal/vpn"
)

func TestOtherVendorSite(t *testing.T) {
	c, fn, _ := siteBox(t, "192.168.1.1/24")
	_, theirPub, _ := vpn.NewKeyPair()
	body := map[string]any{"label": "Office UniFi", "public_key": theirPub, "remote_lans": []string{"192.168.50.0/24"}, "host": "home.example.com", "endpoint": "203.0.113.9:51820"}
	rc, m, raw := c.call("POST", "/api/vpn/wireguard/site/other", body)
	if rc != 200 {
		t.Fatalf("%d %s", rc, raw)
	}
	for _, k := range []string{"our_public_key", "our_endpoint", "their_address", "psk"} {
		if m[k] == nil || m[k] == "" {
			t.Fatalf("missing %s: %s", k, raw)
		}
	}
	if !strings.Contains(string(raw), "192.168.1.0/24") {
		t.Fatalf("our network not offered to them: %s", raw)
	}
	w := fn.plan.Config.WireGuard
	if len(w) != 1 || len(w[0].Peers) != 1 || w[0].Peers[0].Endpoint != "203.0.113.9:51820" || !strings.Contains(strings.Join(w[0].Peers[0].AllowedIPs, ","), "192.168.50.0/24") {
		t.Fatalf("plan: %+v", w)
	}
	// the same key again, a clashing network, junk
	if rc, _, _ := c.call("POST", "/api/vpn/wireguard/site/other", body); rc == 200 {
		t.Fatal("same key twice")
	}
	_, k2, _ := vpn.NewKeyPair()
	if rc, _, raw := c.call("POST", "/api/vpn/wireguard/site/other", map[string]any{"public_key": k2, "remote_lans": []string{"192.168.1.0/24"}}); rc == 200 || !strings.Contains(string(raw), "overlaps") {
		t.Fatalf("overlap accepted: %d %s", rc, raw)
	}
	if rc, _, _ := c.call("POST", "/api/vpn/wireguard/site/other", map[string]any{"public_key": "nope", "remote_lans": []string{"10.9.0.0/24"}}); rc == 200 {
		t.Fatal("bad key accepted")
	}
}

func TestServerFirstDeviceMTUAndPause(t *testing.T) {
	c, fn, _ := siteBox(t, "192.168.1.1/24")
	rc, m, raw := c.call("POST", "/api/vpn/wireguard/server", map[string]any{"endpoint": "h.example.com", "route": "all", "device": "Marvins phone"})
	if rc != 200 || m["device"] == nil || m["device_error"] != nil {
		t.Fatalf("%d %s", rc, raw)
	}
	id := int64(m["id"].(float64))
	// without an address the server is still made and the reason is returned
	c2, _, _ := siteBox(t, "192.168.1.1/24")
	_, m2, _ := c2.call("POST", "/api/vpn/wireguard/server", map[string]any{"route": "all", "device": "Laptop"})
	if m2["id"] == nil || m2["device_error"] == nil {
		t.Fatalf("expected device_error: %v", m2)
	}
	// MTU
	if rc, _, _ := c.call("PUT", "/api/vpn/wireguard/"+itoa(id), map[string]any{"mtu": 1100}); rc == 200 {
		t.Fatal("mtu too small accepted")
	}
	if rc, _, raw := c.call("PUT", "/api/vpn/wireguard/"+itoa(id), map[string]any{"mtu": 1380}); rc != 200 {
		t.Fatalf("%d %s", rc, raw)
	}
	if got := fn.plan.Config.WireGuard[0].MTU; got != 1380 {
		t.Fatalf("mtu in plan: %d", got)
	}
	// pause the device
	_, _, vraw := c.call("GET", "/api/vpn?fresh=1", nil)
	var v struct{ Vpns []VPNView }
	json.Unmarshal(vraw, &v)
	var pid int64
	for _, x := range v.Vpns {
		if x.ID == id {
			pid = x.Peers[0].ID
			if x.MTU != 1380 {
				t.Fatalf("view mtu %d", x.MTU)
			}
		}
	}
	if rc, _, raw := c.call("PUT", "/api/vpn/wireguard/"+itoa(id)+"/peers/"+itoa(pid), map[string]any{"enabled": false}); rc != 200 {
		t.Fatalf("%d %s", rc, raw)
	}
	if fn.plan.Config.WireGuard[0].Peers[0].Enabled {
		t.Fatal("paused device still enabled in the plan")
	}
	_, _, vraw = c.call("GET", "/api/vpn?fresh=1", nil)
	if !strings.Contains(string(vraw), `"paused":true`) {
		t.Fatalf("view: %s", vraw)
	}
	if rc, _, _ := c.call("PUT", "/api/vpn/wireguard/"+itoa(id)+"/peers/"+itoa(pid), map[string]any{"enabled": true}); rc != 200 {
		t.Fatal("resume failed")
	}
	// switch the whole VPN off
	if rc, _, _ := c.call("PUT", "/api/vpn/wireguard/"+itoa(id), map[string]any{"enabled": false}); rc != 200 {
		t.Fatal("off failed")
	}
	if fn.plan.Config.WireGuard[0].Enabled {
		t.Fatal("vpn still enabled in the plan")
	}
}

func TestVPNPortClashWarning(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	_, m, _ := c.call("POST", "/api/vpn/wireguard/server", map[string]any{"endpoint": "h.example.com", "route": "all", "port": 51999})
	_ = m
	if rc, _, raw := c.call("PUT", "/api/fw/forwards", map[string]any{"name": "Clash", "proto": "udp", "ext_port": "51990-52000", "int_addr": "192.168.1.20", "enabled": true}); rc != 200 {
		t.Fatalf("forward: %d %s", rc, raw)
	}
	_, _, raw := c.call("GET", "/api/vpn?fresh=1", nil)
	if !strings.Contains(string(raw), "also uses port 51999") {
		t.Fatalf("no clash warning: %s", raw[:min(len(raw), 900)])
	}
}

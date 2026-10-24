package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
	"time"
)

// siteBox starts a router with one LAN and returns a client logged in to it.
func siteBox(t *testing.T, lan string) (*client, *fakeNet, *fakeFW) {
	t.Helper()
	web, dnsAddr := freeAddr(t), freeAddr(t)
	fn, ff := &fakeNet{}, &fakeFW{}
	a, err := New(Config{DataDir: t.TempDir(), WebAddr: web, DNSAddrs: []string{dnsAddr}, Net: fn, FW: ff})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	a.Auth.CreateUser("admin", "correct horse battery", "admin")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-done })
	jar, _ := cookiejar.New(nil)
	c := &client{t: t, base: "http://" + web, hc: &http.Client{Jar: jar}}
	for i := 0; i < 50; i++ {
		if resp, err := c.hc.Get(c.base + "/api/health"); err == nil {
			resp.Body.Close()
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	_, m, _ := c.call("POST", "/api/auth/login", map[string]string{"username": "admin", "password": "correct horse battery"})
	c.csrf = m["csrf"].(string)
	rc, m, raw := c.call("PUT", "/api/fw/zones", map[string]any{"name": "Home", "kind": "trusted", "ifaces": []string{"eth1"}, "management": true, "internet": true, "gateway": true, "intra_allow": true})
	if rc != 200 {
		t.Fatalf("zone: %d %s", rc, raw)
	}
	_, _, ifraw := c.call("GET", "/api/net/interfaces", nil)
	var ifs []map[string]any
	json.Unmarshal(ifraw, &ifs)
	if rc, _, raw := c.call("PUT", "/api/net/interfaces", map[string]any{"id": ifs[0]["id"], "name": "eth1", "kind": "phys", "ipv4": lan, "zone_id": int64(m["id"].(float64)), "enabled": true}); rc != 200 {
		t.Fatalf("lan: %d %s", rc, raw)
	}
	if rc, _, raw := c.call("PUT", "/api/net/wans", map[string]any{"name": "Fibre", "iface": "eth0", "type": "dhcp", "enabled": true}); rc != 200 {
		t.Fatalf("wan: %d %s", rc, raw)
	}
	return c, fn, ff
}

func TestSiteToSite(t *testing.T) {
	ca, fnA, _ := siteBox(t, "192.168.1.1/24")
	cb, fnB, _ := siteBox(t, "192.168.50.1/24")

	// the other site's network may not overlap this one
	if rc, _, raw := ca.call("POST", "/api/vpn/wireguard/site", map[string]any{"label": "Cabin", "remote_lan": "192.168.1.0/24", "endpoint": "home.example.com"}); rc == 200 || !strings.Contains(string(raw), "overlaps") {
		t.Fatalf("overlap accepted: %d %s", rc, raw)
	}
	if rc, _, _ := ca.call("POST", "/api/vpn/wireguard/site", map[string]any{"label": "Cabin", "remote_lan": "nonsense", "endpoint": "home.example.com"}); rc == 200 {
		t.Fatal("bad network accepted")
	}

	rc, m, raw := ca.call("POST", "/api/vpn/wireguard/site", map[string]any{"label": "Cabin", "remote_lan": "192.168.50.0/24", "endpoint": "home.example.com"})
	if rc != 200 {
		t.Fatalf("site: %d %s", rc, raw)
	}
	code := m["code"].(string)
	if !strings.HasPrefix(code, sitePrefix) {
		t.Fatalf("code: %s", code)
	}
	wgA := fnA.plan.Config.WireGuard
	if len(wgA) != 1 || len(wgA[0].Peers) != 1 {
		t.Fatalf("plan A: %+v", wgA)
	}
	got := strings.Join(wgA[0].Peers[0].AllowedIPs, ",")
	if !strings.Contains(got, "192.168.50.0/24") || !strings.Contains(got, "/32") {
		t.Fatalf("A routes the other site: %s", got)
	}

	// pasted with line breaks and spaces, as people do
	broken := strings.Join([]string{code[:30], code[30:]}, "\n  ")
	rc, m, raw = cb.call("POST", "/api/vpn/wireguard/site/join", map[string]any{"code": broken})
	if rc != 200 {
		t.Fatalf("join: %d %s", rc, raw)
	}
	if w, _ := m["warning"].(string); w != "" {
		t.Fatalf("unexpected warning: %s", w)
	}
	wgB := fnB.plan.Config.WireGuard
	if len(wgB) != 1 || len(wgB[0].Peers) != 1 {
		t.Fatalf("plan B: %+v", wgB)
	}
	p := wgB[0].Peers[0]
	if p.Endpoint != "home.example.com:"+itoa(int64(wgA[0].Port)) || !strings.Contains(strings.Join(p.AllowedIPs, ","), "192.168.1.0/24") {
		t.Fatalf("B reaches A: %+v", p)
	}
	if wgB[0].Peers[0].Keepalive != 25 {
		t.Fatal("B must keep the link alive so it works behind a router")
	}

	// the same code again, and a code for a clashing network
	if rc, _, raw := cb.call("POST", "/api/vpn/wireguard/site/join", map[string]any{"code": code}); rc == 200 {
		t.Fatalf("code used twice: %s", raw)
	}
	cc, _, _ := siteBox(t, "192.168.1.1/24")
	if rc, _, raw := cc.call("POST", "/api/vpn/wireguard/site/join", map[string]any{"code": code}); rc == 200 || !strings.Contains(string(raw), "overlaps") {
		t.Fatalf("clashing site accepted: %d %s", rc, raw)
	}
	if rc, _, _ := cc.call("POST", "/api/vpn/wireguard/site/join", map[string]any{"code": "hello"}); rc == 200 {
		t.Fatal("junk accepted")
	}

	// a site link has no devices to add
	id := int64(m["id"].(float64))
	if rc, _, _ := cb.call("POST", "/api/vpn/wireguard/"+itoa(id)+"/peers", map[string]string{"name": "phone"}); rc == 200 {
		t.Fatal("devices were added to a site link")
	}
	_, _, vraw := ca.call("GET", "/api/vpn?fresh=1", nil)
	if !strings.Contains(string(vraw), `"site":true`) || !strings.Contains(string(vraw), "192.168.50.0/24") {
		t.Fatalf("view: %s", vraw)
	}
}

func TestVPNRouteChoices(t *testing.T) {
	c, fn, _ := siteBox(t, "192.168.1.1/24")
	f := false
	for _, tc := range []struct {
		in       map[string]any
		ok       bool
		routeAll bool
		access   string
	}{
		{map[string]any{"endpoint": "h.example.com", "route": "all"}, true, true, "full"},
		{map[string]any{"endpoint": "h.example.com", "route": "home"}, true, false, "full"},
		{map[string]any{"endpoint": "h.example.com", "route": "all", "reach": f}, true, true, "internet"},
		{map[string]any{"endpoint": "h.example.com", "route": "home", "reach": f}, false, false, ""},
		{map[string]any{"endpoint": "h.example.com", "route": "bogus"}, false, false, ""},
		{map[string]any{"endpoint": "h.example.com", "access": "internet"}, true, true, "internet"},
	} {
		rc, m, raw := c.call("POST", "/api/vpn/wireguard/server", tc.in)
		if (rc == 200) != tc.ok {
			t.Fatalf("%v: %d %s", tc.in, rc, raw)
		}
		if !tc.ok {
			continue
		}
		id := int64(m["id"].(float64))
		_, _, vraw := c.call("GET", "/api/vpn?fresh=1", nil)
		var v struct{ Vpns []VPNView }
		json.Unmarshal(vraw, &v)
		found := false
		for _, x := range v.Vpns {
			if x.ID == id && x.Kind == "wireguard" {
				found = true
				if x.RouteAll != tc.routeAll || x.Access != tc.access {
					t.Fatalf("%v: got route_all=%v access=%s", tc.in, x.RouteAll, x.Access)
				}
			}
		}
		if !found {
			t.Fatalf("%v: not listed", tc.in)
		}
	}
	_ = fn
}

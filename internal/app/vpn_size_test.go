package app

import (
	"encoding/json"
	"strings"
	"testing"

	"gatehouse/internal/netd"
)

func TestProfileTunMTU(t *testing.T) {
	cases := map[string]int{
		"client\ntun-mtu 1400\n": 1400, "  TUN-MTU 1380 \n": 1380, "client\n": 0, "tun-mtu 100\n": 0, "tun-mtu 99999\n": 0,
		"# tun-mtu 1400\n": 0,
	}
	for in, want := range cases {
		if got := profileTunMTU(in); got != want {
			t.Errorf("%q: got %d want %d", in, got, want)
		}
	}
}

func TestTunnelSizeIsWatchedForEveryOutboundTunnel(t *testing.T) {
	c, fn, _ := siteBox(t, "192.168.1.1/24")
	// A WireGuard provider tunnel with no check address of its own, and an OpenVPN one whose
	// profile sets a size.
	if rc, _, raw := c.call("POST", "/api/vpn/wireguard/client", map[string]any{"label": "Mullvad", "conf": wgProvider}); rc != 200 {
		t.Fatalf("wg client: %d %s", rc, raw)
	}
	prof := strings.Replace(ovpnProfile, "nobind\n", "nobind\ntun-mtu 1400\n", 1)
	if rc, _, raw := c.call("POST", "/api/vpn/openvpn/client", map[string]any{"label": "Provider", "profile": prof}); rc != 200 {
		t.Fatalf("ovpn client: %d %s", rc, raw)
	}
	got := map[string]netd.SizeSpec{}
	for _, s := range fn.vpn.Sizes {
		got[s.Name] = s
	}
	if len(got) != 2 {
		t.Fatalf("both outbound tunnels must be watched: %+v", fn.vpn.Sizes)
	}
	var ov netd.SizeSpec
	for n, s := range got {
		if strings.HasPrefix(n, "tun") {
			ov = s
		}
	}
	if ov.Start != 1400 || !ov.FromProfile {
		t.Fatalf("the profile's own size is the starting point: %+v", ov)
	}
	for _, ck := range fn.vpn.Checks {
		if ck.Exit {
			t.Fatal("the exit address is learned by the size watcher now, not by the old check")
		}
	}
}

func TestTestListsCanBeChangedAndRestored(t *testing.T) {
	c, fn, _ := siteBox(t, "192.168.1.1/24")
	rc, _, raw := c.call("GET", "/api/vpn/test-lists", nil)
	var l struct {
		Ping, Exit []string
		Custom     bool
		Max        int
	}
	json.Unmarshal(raw, &l)
	if rc != 200 || l.Custom || len(l.Ping) != 5 || l.Ping[0] != "1.1.1.1" || len(l.Exit) != 4 || l.Exit[0] != "dns:google" || l.Max != 8 {
		t.Fatalf("defaults: %d %s", rc, raw)
	}
	bad := []map[string]any{
		{"ping": []string{"example.com"}, "exit": []string{"dns:google"}},
		{"ping": []string{"1.1.1.1"}, "exit": []string{"web:http://plain.example"}},
		{"ping": []string{"1.1.1.1"}, "exit": []string{"file:///etc/passwd"}},
		{"ping": []string{}, "exit": []string{"dns:google"}},
		{"ping": []string{"1.1.1.1", "1.0.0.1", "8.8.8.8", "8.8.4.4", "9.9.9.9", "4.2.2.1", "4.2.2.2", "208.67.222.222", "208.67.220.220"}, "exit": []string{"dns:google"}},
		{"ping": []string{"2606:4700:4700::1111"}, "exit": []string{"dns:google"}},
	}
	for _, b := range bad {
		if rc, _, _ := c.call("PUT", "/api/vpn/test-lists", b); rc == 200 {
			t.Fatalf("should refuse %v", b)
		}
	}
	rc, _, raw = c.call("PUT", "/api/vpn/test-lists", map[string]any{"ping": []string{" 9.9.9.9 ", "1.1.1.1", "9.9.9.9"}, "exit": []string{"dns:cloudflare", "web:https://checkip.amazonaws.com"}})
	if rc != 200 {
		t.Fatalf("save: %d %s", rc, raw)
	}
	if got := strings.Join(fn.vpn.PingAddrs, ","); got != "9.9.9.9,1.1.1.1" || len(fn.vpn.ExitSources) != 2 {
		t.Fatalf("the helper must get the new lists, in order: %v %v", fn.vpn.PingAddrs, fn.vpn.ExitSources)
	}
	_, _, raw = c.call("GET", "/api/vpn/test-lists", nil)
	json.Unmarshal(raw, &l)
	if !l.Custom || l.Ping[0] != "9.9.9.9" {
		t.Fatalf("saved lists: %s", raw)
	}
	if rc, _, raw := c.call("PUT", "/api/vpn/test-lists", map[string]any{"restore": true}); rc != 200 {
		t.Fatalf("restore: %d %s", rc, raw)
	}
	_, _, raw = c.call("GET", "/api/vpn/test-lists", nil)
	json.Unmarshal(raw, &l)
	if l.Custom || l.Ping[0] != "1.1.1.1" || len(fn.vpn.PingAddrs) != 0 {
		t.Fatalf("restore: %s helper=%v", raw, fn.vpn.PingAddrs)
	}
}

func TestPacketSizeShowsOnTheTileAndCanBeTestedByHand(t *testing.T) {
	c, fn, _ := siteBox(t, "192.168.1.1/24")
	rc, m, raw := c.call("POST", "/api/vpn/wireguard/client", map[string]any{"label": "Mullvad", "conf": wgProvider})
	if rc != 200 {
		t.Fatalf("wg client: %d %s", rc, raw)
	}
	id := itoa(int64(m["id"].(float64)))
	fn.mu.Lock()
	fn.status = netd.VPNStatus{Tunnels: map[string]netd.TunnelInfo{"wg0": {MTU: 1360, Source: "measured", At: 100, Passing: true, ExitIP: "195.177.93.114"}}}
	fn.mu.Unlock()
	c.app.refreshVPN()
	_, _, raw = c.call("GET", "/api/vpn", nil)
	if !strings.Contains(string(raw), `"packet_size":1360`) || !strings.Contains(string(raw), `"packet_source":"measured"`) || !strings.Contains(string(raw), "195.177.93.114") {
		t.Fatalf("the tile needs the size, where it came from and the exit address: %s", raw)
	}
	rc, _, raw = c.call("POST", "/api/vpn/wireguard/"+id+"/packet/test", map[string]any{})
	if rc != 200 || !strings.Contains(string(raw), `"after":1360`) {
		t.Fatalf("manual test: %d %s", rc, raw)
	}
	// A test is not a change: it must not appear on the Review page.
	_, _, st := c.call("GET", "/api/staged", nil)
	if strings.Contains(string(st), "VPN") && strings.Contains(string(st), "Changed") {
		t.Fatalf("a test must not be journalled as a change: %s", st)
	}
	// The VPN server has no tunnel peer to measure against.
	rc, m, _ = c.call("POST", "/api/vpn/wireguard/server", map[string]any{"label": "Remote", "endpoint": "home.example.com"})
	if rc == 200 {
		if rc, _, _ := c.call("POST", "/api/vpn/wireguard/"+itoa(int64(m["id"].(float64)))+"/packet/test", map[string]any{}); rc == 200 {
			t.Fatal("the server must be refused")
		}
	}
}

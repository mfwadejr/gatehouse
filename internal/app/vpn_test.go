package app

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/cookiejar"
	"strconv"
	"strings"
	"testing"
	"time"

	"gatehouse/internal/netcfg"
	"gatehouse/internal/netd"
)

const ovpnProfile = `client
dev tun
proto udp
remote vpn.example.net 1194
nobind
<ca>
-----BEGIN CERTIFICATE-----
MIIB
-----END CERTIFICATE-----
</ca>
`

const wgProvider = `[Interface]
PrivateKey = yAnz5TF+lXXJte14tji3zlMNq+hd2rYUIgJBgB3fBmk=
Address = 10.2.0.2/32
DNS = 10.2.0.1

[Peer]
PublicKey = HIgo9xNzJMWLKASShiTqIybxZ0U3wGLiUeJ1PKf8ykw=
AllowedIPs = 0.0.0.0/0
Endpoint = 198.51.100.7:51820
`

func TestVPNFlow(t *testing.T) {
	web, dnsAddr := freeAddr(t), freeAddr(t)
	fn, ff := &fakeNet{}, &fakeFW{}
	a, err := New(Config{DataDir: t.TempDir(), WebAddr: web, DNSAddrs: []string{dnsAddr}, Net: fn, FW: ff})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.Auth.CreateUser("admin", "correct horse battery", "admin")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()
	defer func() { cancel(); <-done }()
	jar, _ := cookiejar.New(nil)
	c := &client{t: t, base: "http://" + web, hc: &http.Client{Jar: jar}}
	for i := 0; i < 300; i++ {
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
	if len(ifs) != 1 {
		t.Fatalf("interfaces: %s", ifraw)
	}
	if rc, _, raw := c.call("PUT", "/api/net/interfaces", map[string]any{"id": ifs[0]["id"], "name": "eth1", "kind": "phys", "ipv4": "192.168.1.1/24", "zone_id": int64(m["id"].(float64)), "enabled": true}); rc != 200 {
		t.Fatalf("lan: %d %s", rc, raw)
	}

	if rc, _, raw := c.call("PUT", "/api/net/wans", map[string]any{"name": "Fibre", "iface": "eth0", "type": "dhcp", "enabled": true}); rc != 200 {
		t.Fatalf("wan: %d %s", rc, raw)
	}

	// Remote access server.
	rc, m, raw = c.call("POST", "/api/vpn/wireguard/server", map[string]any{"label": "Remote access", "endpoint": "home.example.com"})
	if rc != 200 {
		t.Fatalf("wg server: %d %s", rc, raw)
	}
	srvID := int64(m["id"].(float64))
	if len(fn.plan.Config.WireGuard) != 1 || fn.plan.Config.WireGuard[0].Name != "wg0" || fn.plan.Config.WireGuard[0].Port != 51820 {
		t.Fatalf("plan: %+v", fn.plan.Config.WireGuard)
	}
	if !strings.Contains(ff.script, "51820") {
		t.Fatal("the VPN port was not opened in the firewall")
	}
	rc, m, raw = c.call("POST", "/api/vpn/wireguard/"+itoa(srvID)+"/peers", map[string]string{"name": "Marvin phone"})
	if rc != 200 {
		t.Fatalf("peer: %d %s", rc, raw)
	}
	cfg := m["config"].(string)
	if !strings.Contains(cfg, "Endpoint = home.example.com:51820") || !strings.Contains(cfg, "10.8.0.2/32") || !strings.HasPrefix(m["qr"].(string), "data:image/png") {
		t.Fatalf("client config: %s", cfg)
	}
	if bytes.Contains(raw, []byte(`"private_key"`)) {
		t.Fatal("unexpected key field")
	}
	if len(fn.plan.Config.WireGuard[0].Peers) != 1 {
		t.Fatal("peer not applied")
	}
	// a second device with the same name is refused
	if rc, _, _ := c.call("POST", "/api/vpn/wireguard/"+itoa(srvID)+"/peers", map[string]string{"name": "marvin phone"}); rc != 400 {
		t.Fatalf("duplicate name: %d", rc)
	}

	// A provider tunnel and who uses it.
	rc, m, raw = c.call("POST", "/api/vpn/wireguard/client", map[string]any{"label": "Mullvad", "conf": wgProvider})
	if rc != 200 {
		t.Fatalf("wg client: %d %s", rc, raw)
	}
	cliID := int64(m["id"].(float64))
	if rc, _, raw := c.call("PUT", "/api/vpn/wireguard/"+itoa(cliID)+"/uses", map[string]any{"uses": []map[string]string{
		{"kind": "network", "value": "Home"}, {"kind": "cidr", "value": "192.168.5.20"}}}); rc != 200 {
		t.Fatalf("uses: %d %s", rc, raw)
	}
	var cl *netcfg.WG
	for i := range fn.plan.Config.WireGuard {
		if fn.plan.Config.WireGuard[i].Mode == "client" {
			cl = &fn.plan.Config.WireGuard[i]
		}
	}
	if cl == nil || cl.Table != 300 || len(cl.Sources) != 2 || cl.Sources[0] != "192.168.1.0/24" {
		t.Fatalf("client plan: %+v", cl)
	}
	if !strings.Contains(ff.script, "wg1") {
		t.Fatal("the firewall does not know the tunnel")
	}
	// Overlap: the same device cannot go through two VPNs.
	rc, m, raw = c.call("POST", "/api/vpn/openvpn/client", map[string]any{"label": "Other", "profile": ovpnProfile})
	if rc != 200 {
		t.Fatalf("ovpn client: %d %s", rc, raw)
	}
	ovID := int64(m["id"].(float64))
	if rc, _, raw := c.call("PUT", "/api/vpn/openvpn/"+itoa(ovID)+"/uses", map[string]any{"uses": []map[string]string{{"kind": "cidr", "value": "192.168.1.77"}}}); rc != 400 || !strings.Contains(string(raw), "only one") {
		t.Fatalf("overlap: %d %s", rc, raw)
	}
	if len(fn.vpn.OpenVPN) != 1 || fn.vpn.OpenVPN[0].Role != "client" || !strings.Contains(fn.vpn.OpenVPN[0].Conf, "route-nopull") {
		t.Fatalf("ovpn plan: %+v", fn.vpn)
	}
	if len(fn.vpn.Checks) != 2 {
		t.Fatalf("checks: %+v", fn.vpn.Checks)
	}
	// Hostile profile is refused.
	if rc, _, _ := c.call("POST", "/api/vpn/openvpn/client", map[string]any{"label": "Bad", "profile": ovpnProfile + "up /bin/sh\n"}); rc != 400 {
		t.Fatalf("hostile profile: %d", rc)
	}

	// OpenVPN server, a device, then revoke it.
	rc, m, raw = c.call("POST", "/api/vpn/openvpn/server", map[string]any{"label": "Laptops", "endpoint": "home.example.com"})
	if rc != 200 {
		t.Fatalf("ovpn server: %d %s", rc, raw)
	}
	sID := int64(m["id"].(float64))
	rc, m, raw = c.call("POST", "/api/vpn/openvpn/"+itoa(sID)+"/peers", map[string]string{"name": "Work laptop"})
	if rc != 200 || !strings.Contains(m["config"].(string), "<tls-crypt>") || !strings.Contains(m["config"].(string), "remote home.example.com 1194") {
		t.Fatalf("ovpn device: %d %s", rc, raw)
	}
	calls := fn.vpnCalls
	_, m, _ = c.call("GET", "/api/vpn", nil)
	peers := m["vpns"].([]any)
	var pid int64
	for _, v := range peers {
		vm := v.(map[string]any)
		if vm["kind"] == "openvpn" && vm["role"] == "server" {
			pid = int64(vm["peers"].([]any)[0].(map[string]any)["id"].(float64))
		}
	}
	if rc, _, raw := c.call("DELETE", "/api/vpn/openvpn/"+itoa(sID)+"/peers/"+itoa(pid), nil); rc != 200 {
		t.Fatalf("revoke: %d %s", rc, raw)
	}
	if fn.vpnCalls <= calls {
		t.Fatal("the revocation list was not sent to the helper")
	}

	// Health.
	now := time.Now().Unix()
	var pubKey string
	for _, p := range fn.plan.Config.WireGuard {
		if p.Mode == "client" {
			pubKey = p.Peers[0].PublicKey
		}
	}
	fn.mu.Lock()
	fn.status = netd.VPNStatus{Now: now,
		WG:     map[string]netd.WGStatus{"wg0": {}, "wg1": {Peers: []netd.WGPeerStatus{{PublicKey: pubKey, Handshake: now - 20, Rx: 100, Tx: 200}}}},
		Links:  map[string]netd.LinkInfo{"tun0": {Up: true, Addr: "10.96.0.5"}},
		OVPN:   map[string]netd.OVPNStatus{"tun0": {Active: true}, "tun1": {Active: true}},
		Checks: map[string]netd.CheckResult{"wg1": {OK: true, RTTMs: 14, At: now, ExitIP: "203.0.113.9"}, "tun0": {OK: false, At: now}}}
	fn.mu.Unlock()
	_, m, _ = c.call("GET", "/api/vpn?fresh=1", nil)
	got := map[string]map[string]any{}
	for _, v := range m["vpns"].([]any) {
		vm := v.(map[string]any)
		got[vm["dev"].(string)] = vm
	}
	if got["wg1"]["state"] != "up" || got["wg1"]["exit_ip"] != "203.0.113.9" || got["wg1"]["address"] != "10.2.0.2/32" {
		t.Fatalf("wg1: %+v", got["wg1"])
	}
	if got["tun0"]["state"] != "degraded" {
		t.Fatalf("tun0 should be degraded: %+v", got["tun0"])
	}
	if got["wg0"]["state"] != "up" {
		t.Fatalf("wg0: %+v", got["wg0"])
	}
	// An alert is written when the state changes.
	fn.mu.Lock()
	fn.status.WG["wg1"] = netd.WGStatus{Peers: []netd.WGPeerStatus{{PublicKey: pubKey, Handshake: now - 600}}}
	fn.mu.Unlock()
	c.call("GET", "/api/vpn?fresh=1", nil)
	var n int
	a.Logs.QueryRow(`SELECT count(*) FROM events WHERE area='vpn' AND title LIKE '%Mullvad%down%'`).Scan(&n)
	if n != 1 {
		t.Fatalf("expected one alert, got %d", n)
	}

	// Deleting removes everything.
	if rc, _, raw := c.call("DELETE", "/api/vpn/wireguard/"+itoa(cliID), nil); rc != 200 {
		t.Fatalf("delete: %d %s", rc, raw)
	}
	for _, w := range fn.plan.Config.WireGuard {
		if w.Mode == "client" {
			t.Fatal("deleted tunnel is still in the plan")
		}
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func TestOpenVPNDeviceCanBePaused(t *testing.T) {
	c, fn, _ := siteBox(t, "192.168.1.1/24")
	rc, m, raw := c.call("POST", "/api/vpn/openvpn/server", map[string]any{"label": "Laptops", "endpoint": "home.example.com"})
	if rc != 200 {
		t.Fatalf("server: %d %s", rc, raw)
	}
	sID := int64(m["id"].(float64))
	if rc, _, raw := c.call("POST", "/api/vpn/openvpn/"+itoa(sID)+"/peers", map[string]string{"name": "Work laptop"}); rc != 200 {
		t.Fatalf("device: %d %s", rc, raw)
	}
	held := func() int {
		fn.mu.Lock()
		defer fn.mu.Unlock()
		for _, o := range fn.vpn.OpenVPN {
			if crl := o.Files["crl.pem"]; crl != "" {
				blk, _ := pem.Decode([]byte(crl))
				if blk == nil {
					t.Fatal("no CRL sent to the helper")
				}
				l, err := x509.ParseRevocationList(blk.Bytes)
				if err != nil {
					t.Fatal(err)
				}
				return len(l.RevokedCertificateEntries)
			}
		}
		t.Fatal("no OpenVPN server in the plan")
		return -1
	}
	peer := func() map[string]any {
		_, m, _ := c.call("GET", "/api/vpn", nil)
		for _, v := range m["vpns"].([]any) {
			vm := v.(map[string]any)
			if vm["kind"] == "openvpn" && vm["role"] == "server" {
				return vm["peers"].([]any)[0].(map[string]any)
			}
		}
		t.Fatal("no OpenVPN server listed")
		return nil
	}
	if n := held(); n != 0 {
		t.Fatalf("a fresh server holds %d devices out", n)
	}
	pid := int64(peer()["id"].(float64))
	if rc, _, raw := c.call("PUT", "/api/vpn/openvpn/"+itoa(sID)+"/peers/"+itoa(pid), map[string]any{"enabled": false}); rc != 200 {
		t.Fatalf("pause: %d %s", rc, raw)
	}
	if n := held(); n != 1 {
		t.Fatalf("the paused device is held out %d times, want once", n)
	}
	if p := peer(); p["paused"] != true {
		t.Fatalf("the device does not show as paused: %v", p)
	}
	if rc, _, raw := c.call("PUT", "/api/vpn/openvpn/"+itoa(sID)+"/peers/"+itoa(pid), map[string]any{"enabled": true}); rc != 200 {
		t.Fatalf("allow again: %d %s", rc, raw)
	}
	if n := held(); n != 0 {
		t.Fatalf("an allowed device is still held out (%d)", n)
	}
	if p := peer(); p["paused"] == true {
		t.Fatal("the device still shows as paused")
	}
}

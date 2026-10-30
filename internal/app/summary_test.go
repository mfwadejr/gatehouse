package app

import (
	"strings"
	"testing"
)

func TestSummaryHasNoSecretsAndEscapes(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	a := c.app
	if a == nil {
		t.Skip("no app")
	}
	c.call("PUT", "/api/wificards", map[string]any{"ssid": "Guest", "psk": "welcome-home-1"})
	a.Config.Exec(`INSERT INTO devices(mac,name,last_ip) VALUES('aa:bb:cc:dd:ee:07','<script>x</script>','192.168.1.50')`)
	a.Config.Exec(`INSERT INTO wifi_radios(iface,mode,ssid,psk) VALUES('wlan9','ap','Home','sealed-secret-value')`)
	rc, _, raw := c.call("GET", "/api/summary/print", nil)
	body := string(raw)
	if rc != 200 || !strings.Contains(body, "My network") || !strings.Contains(body, "192.168.1.1") {
		t.Fatalf("%d %s", rc, body)
	}
	for _, bad := range []string{"welcome-home-1", "sealed-secret-value", "<script>", "private_key"} {
		if strings.Contains(body, bad) {
			t.Fatalf("summary contains %q", bad)
		}
	}
	if !strings.Contains(body, "Home") || !strings.Contains(body, "&lt;script&gt;") {
		t.Fatalf("missing wifi name or escaped device: %s", body)
	}
}

func TestTopologyPicture(t *testing.T) {
	svg := topologySVG(topology{
		Internet: []string{"Fibre (address from provider)"},
		Nets: []topoNet{
			{Name: "Home", Addr: "192.168.1.1/24", Devices: []string{"Laptop", "<img src=x onerror=alert(1)>"}},
			{Name: "Cameras", Addr: "192.168.20.1/24", VLAN: "20", Devices: []string{"Front door"}, More: 7},
			{Name: "Guests"}, {Name: "Lab"},
		},
	})
	for _, want := range []string{"<svg", "Internet", "Gatehouse", "Home", "Cameras", "VLAN 20", "and 7 more", "Front door", "Guests", "Lab"} {
		if !strings.Contains(svg, want) {
			t.Fatalf("picture lacks %q", want)
		}
	}
	if strings.Contains(svg, "<img") || strings.Contains(svg, "<script") {
		t.Fatalf("text must be escaped: %s", svg)
	}
	// An empty network still draws.
	if s := topologySVG(topology{}); !strings.Contains(s, "No connection set up") {
		t.Fatalf("empty: %s", s)
	}
	// The printed page carries it, with no passwords.
	c, _, _ := siteBox(t, "192.168.1.1/24")
	_, _, raw := c.call("GET", "/api/summary/print", nil)
	if !strings.Contains(string(raw), "<svg") || !strings.Contains(string(raw), "Map") {
		t.Fatalf("page has no map: %s", raw)
	}
}

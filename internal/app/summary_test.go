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

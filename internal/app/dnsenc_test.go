package app

import "testing"

func TestEncryptedDNSSwitchAndUndo(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	a := c.app
	if a == nil {
		t.Skip("no app")
	}
	a.seedUpstreams()
	if m := a.encMode(); m != "automatic" {
		t.Fatalf("default mode %s", m)
	}
	if rc, _, _ := c.call("PUT", "/api/dns/encrypted", map[string]string{"mode": "bogus"}); rc == 200 {
		t.Fatal("bogus accepted")
	}
	if rc, _, raw := c.call("PUT", "/api/dns/encrypted", map[string]string{"mode": "provider:quad9"}); rc != 200 {
		t.Fatalf("%d %s", rc, raw)
	}
	if a.encMode() != "provider:quad9" || len(a.currentUps()) != 2 {
		t.Fatal(a.encMode())
	}
	c.call("PUT", "/api/dns/encrypted", map[string]string{"mode": "off"})
	if a.encMode() != "off" {
		t.Fatal(a.encMode())
	}
	if rc, _, _ := c.call("POST", "/api/dns/encrypted/undo", map[string]string{}); rc != 200 || a.encMode() != "provider:quad9" {
		t.Fatalf("undo: %s", a.encMode())
	}
	a.Config.Exec(`UPDATE dns_upstreams SET address='203.0.113.9:53', proto='udp' WHERE position=1`)
	if a.encMode() != "custom" {
		t.Fatal(a.encMode())
	}
}

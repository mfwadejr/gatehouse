package app

import (
	"strings"
	"testing"

	"nopewarden/internal/ha"
)

func modePorts(t *testing.T) {
	old := portsLister
	portsLister = func() []map[string]any {
		return []map[string]any{{"name": "eth0", "link": true}, {"name": "eth1", "link": true},
			{"name": "eth2", "link": false}}
	}
	t.Cleanup(func() { portsLister = old })
}

func TestModeCardReadsTheCurrentSetupInWords(t *testing.T) {
	modePorts(t)
	c, _, _ := siteBox(t, "192.168.1.1/24")
	c.call("PUT", "/api/net/ports/label", map[string]string{"name": "eth0", "label": "T-Mobile gateway"})
	_, m, _ := c.call("GET", "/api/mode", nil)
	if m["mode"] != "router" || m["paired"] != false {
		t.Fatalf("mode: %v", m)
	}
	h, _ := m["headline"].(string)
	if !strings.Contains(h, "T-Mobile gateway (eth0)") || !strings.Contains(h, "Fibre") || !strings.Contains(h, "eth1") {
		t.Fatalf("headline: %q", h)
	}
	// Bridge mode reads the same way.
	c.call("PUT", "/api/bridge", bridgeBodyPorts("eth0", "eth1"))
	if rc, _, raw := c.call("POST", "/api/bridge/apply", map[string]string{"code": ""}); rc != 200 {
		t.Fatalf("apply: %d %s", rc, raw)
	}
	if rc, _, raw := c.call("POST", "/api/bridge/confirm", map[string]any{}); rc != 200 {
		t.Fatalf("confirm: %d %s", rc, raw)
	}
	_, m, _ = c.call("GET", "/api/mode", nil)
	h, _ = m["headline"].(string)
	if m["mode"] != "bridge" || !strings.HasPrefix(h, "Bridge.") || !strings.Contains(h, "T-Mobile gateway (eth0) goes to your other router") {
		t.Fatalf("bridge headline: %v %q", m["mode"], h)
	}
}

func bridgeBodyPorts(out string, in ...string) map[string]any {
	b := bridgeBody()
	b["outside"], b["inside"] = out, in
	return b
}

func TestInternetPortChangeIsGuarded(t *testing.T) {
	modePorts(t)
	c, _, _ := siteBox(t, "192.168.1.1/24")
	a := c.app
	put := func(p string) (int, string) {
		rc, _, raw := c.call("PUT", "/api/mode/internet-port", map[string]string{"iface": p})
		return rc, string(raw)
	}
	if rc, raw := put("eth1"); rc == 200 || !strings.Contains(raw, "carries one of your networks") {
		t.Fatalf("a port with a network must be refused: %d %s", rc, raw)
	}
	if rc, raw := put("nope9"); rc == 200 || !strings.Contains(raw, "not a port") {
		t.Fatalf("unknown port: %d %s", rc, raw)
	}
	if rc, raw := put("eth0"); rc != 200 || !strings.Contains(raw, `"changed":false`) {
		t.Fatalf("same port is a no-op: %d %s", rc, raw)
	}
	if rc, raw := put("eth2"); rc != 200 || !strings.Contains(raw, `"changed":true`) {
		t.Fatalf("move: %d %s", rc, raw)
	}
	ws, _ := a.listWans(a.Config)
	if len(ws) != 1 || ws[0].Iface != "eth2" || ws[0].Name != "Fibre" {
		t.Fatalf("the connection must keep its name and settings: %+v", ws)
	}
}

func TestModeChangesAreRefusedWhilePaired(t *testing.T) {
	modePorts(t)
	c, _, _ := siteBox(t, "192.168.1.1/24")
	a := c.app
	a.haSave(&haLocal{Role: ha.Primary})
	_, m, _ := c.call("GET", "/api/mode", nil)
	if m["paired"] != true {
		t.Fatalf("paired: %v", m)
	}
	if rc, _, raw := c.call("PUT", "/api/mode/internet-port", map[string]string{"iface": "eth2"}); rc == 200 || !strings.Contains(string(raw), "high-availability pair") {
		t.Fatalf("port change while paired: %d %s", rc, raw)
	}
	c.call("PUT", "/api/bridge", bridgeBodyPorts("eth0", "eth1"))
	if rc, _, raw := c.call("POST", "/api/bridge/apply", map[string]string{"code": ""}); rc == 200 || !strings.Contains(string(raw), "high-availability pair") {
		t.Fatalf("bridge while paired: %d %s", rc, raw)
	}
	if a.bridgeLive() != nil {
		t.Fatal("nothing may change while paired")
	}
}

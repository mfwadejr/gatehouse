package app

import (
	"strings"
	"testing"
)

func bridgeBody() map[string]any {
	return map[string]any{"enabled": true, "outside": "tp1", "inside": []string{"tp2"}, "oob": "", "oob_addr": "",
		"mode": "static", "addr": "192.168.1.2/24", "gateway": "192.168.1.1", "dns": "", "subnets": []string{}, "allow": []any{}, "open": false}
}

func fakePorts(t *testing.T) {
	old := portsLister
	portsLister = func() []map[string]any {
		return []map[string]any{{"name": "tp1", "wifi": false}, {"name": "tp2", "wifi": false}, {"name": "tpw", "wifi": true}}
	}
	t.Cleanup(func() { portsLister = old })
}

func TestBridgeModeApplyConfirmAndLeave(t *testing.T) {
	fakePorts(t)
	c, fn, ff := siteBox(t, "192.168.1.1/24")
	a := c.app

	// Bad setups are refused in words and nothing is saved.
	bad := bridgeBody()
	bad["inside"] = []string{"tpw"}
	if rc, _, raw := c.call("PUT", "/api/bridge", bad); rc == 200 || !strings.Contains(string(raw), "wired") {
		t.Fatalf("a Wi-Fi card must be refused: %d %s", rc, raw)
	}
	if rc, _, raw := c.call("PUT", "/api/bridge", bridgeBody()); rc != 200 {
		t.Fatalf("save: %d %s", rc, raw)
	}
	// Saving stages only: the live networks and firewall are untouched.
	if a.bridgeLive() != nil {
		t.Fatal("a draft must not become live before it is confirmed")
	}
	if p, err := a.buildPlan(a.Config); err != nil || len(p.Config.WANs) != 1 {
		t.Fatalf("saved networks must stay in use until confirmed: %v", err)
	}
	if _, m, _ := c.call("GET", "/api/bridge", nil); m["dirty"] != true || m["live"] != false {
		t.Fatalf("status: %v", m)
	}

	rc, _, raw := c.call("POST", "/api/bridge/apply", map[string]string{"code": ""})
	if rc != 200 {
		t.Fatalf("apply: %d %s", rc, raw)
	}
	fn.mu.Lock()
	plan := fn.plan
	fn.mu.Unlock()
	if len(plan.Config.WANs) != 0 || len(plan.Config.Interfaces) != 1 || plan.Config.Interfaces[0].Name != "br-gate" || len(plan.Config.Interfaces[0].Members) != 2 {
		t.Fatalf("network plan: %+v", plan.Config)
	}
	ff.mu.Lock()
	script := ff.script
	ff.mu.Unlock()
	if !strings.Contains(script, "table bridge gatehouse_br") || strings.Contains(script, "masquerade") {
		t.Fatalf("firewall script:\n%s", script)
	}
	// Not confirmed yet: still not live.
	if a.bridgeLive() != nil {
		t.Fatal("live before confirmation")
	}
	if rc, _, raw := c.call("POST", "/api/bridge/confirm", map[string]any{}); rc != 200 {
		t.Fatalf("confirm: %d %s", rc, raw)
	}
	if l := a.bridgeLive(); l == nil || !l.Enabled {
		t.Fatal("not live after confirmation")
	}
	// The normal builders now give the bridge, so every later apply stays in bridge mode.
	if p, _ := a.buildPlan(a.Config); len(p.Config.WANs) != 0 || p.Config.Interfaces[0].Name != "br-gate" {
		t.Fatal("later network applies must stay in bridge mode")
	}
	if _, s, err := a.currentRuleset(); err != nil || !strings.Contains(s, "gatehouse_br") {
		t.Fatalf("later firewall applies must stay in bridge mode: %v", err)
	}
	if st := a.bridgeStatus(); st["dirty"] != false {
		t.Fatalf("nothing should be waiting: %v", st)
	}

	// Leaving bridge mode brings the saved networks straight back.
	off := bridgeBody()
	off["enabled"] = false
	if rc, _, raw := c.call("PUT", "/api/bridge", off); rc != 200 {
		t.Fatalf("save off: %d %s", rc, raw)
	}
	if rc, _, raw := c.call("POST", "/api/bridge/apply", map[string]string{"code": ""}); rc != 200 {
		t.Fatalf("apply off: %d %s", rc, raw)
	}
	fn.mu.Lock()
	plan = fn.plan
	fn.mu.Unlock()
	if len(plan.Config.WANs) != 1 || plan.Config.WANs[0].Name != "Fibre" {
		t.Fatalf("the saved internet connection must come back: %+v", plan.Config)
	}
	ff.mu.Lock()
	script = ff.script
	ff.mu.Unlock()
	if strings.Contains(script, "gatehouse_br") {
		t.Fatal("the routing ruleset must not contain the bridge filter")
	}
	if rc, _, raw := c.call("POST", "/api/bridge/confirm", map[string]any{}); rc != 200 {
		t.Fatalf("confirm off: %d %s", rc, raw)
	}
	if l := a.bridgeLive(); l == nil || l.Enabled {
		t.Fatal("still live after switching off")
	}
}

func TestBridgeApplyNothingToDo(t *testing.T) {
	fakePorts(t)
	c, _, _ := siteBox(t, "192.168.1.1/24")
	if rc, _, _ := c.call("POST", "/api/bridge/apply", map[string]string{"code": ""}); rc == 200 {
		t.Fatal("applying nothing must be refused")
	}
}

func TestBridgeCancelPutsEverythingBack(t *testing.T) {
	fakePorts(t)
	c, _, _ := siteBox(t, "192.168.1.1/24")
	c.call("PUT", "/api/bridge", bridgeBody())
	if rc, _, raw := c.call("POST", "/api/bridge/apply", map[string]string{"code": ""}); rc != 200 {
		t.Fatalf("%d %s", rc, raw)
	}
	if rc, _, raw := c.call("POST", "/api/bridge/cancel", map[string]any{}); rc != 200 {
		t.Fatalf("cancel: %d %s", rc, raw)
	}
	if c.app.bridgeLive() != nil {
		t.Fatal("a cancelled change must not become live")
	}
	if _, m, _ := c.call("GET", "/api/bridge", nil); m["dirty"] != true {
		t.Fatal("the staged setup should still be there to try again")
	}
}

package app

import (
	"errors"
	"testing"
)

func TestSpeedLimitsNeedPlanSpeedsAndRevertOnFailure(t *testing.T) {
	c, fn, _ := siteBox(t, "192.168.1.1/24")
	a := c.app
	if a == nil {
		t.Skip("no app")
	}
	a.Config.Exec(`DELETE FROM wan_links`)
	if _, err := a.Config.Exec(`INSERT INTO wan_links(name,iface,type,enabled,priority) VALUES('Fibre','eth0','dhcp',1,1)`); err != nil {
		t.Fatal(err)
	}
	// No plan speeds yet: refused with a plain reason.
	rc, _, raw := c.call("PUT", "/api/sqm", map[string]any{"links": map[string]bool{"Fibre": true}})
	if rc == 200 {
		t.Fatalf("accepted without plan speeds: %s", raw)
	}
	if rc, _, _ := c.call("PUT", "/api/sqm", map[string]any{"links": map[string]bool{"Nowhere": true}}); rc == 200 {
		t.Fatal("accepted an unknown connection")
	}
	c.call("PUT", "/api/speedtest", map[string]any{"schedule": false, "hour": 3, "links": map[string]any{"Fibre": map[string]any{"down_mbps": 500, "up_mbps": 50, "metered": false}}})
	if rc, _, raw := c.call("PUT", "/api/sqm", map[string]any{"links": map[string]bool{"Fibre": true}}); rc != 200 {
		t.Fatalf("%d %s", rc, raw)
	}
	fn.mu.Lock()
	p := fn.sqmPlans[len(fn.sqmPlans)-1]
	fn.mu.Unlock()
	if len(p.Links) != 1 || p.Links[0].Iface != "eth0" || p.Links[0].DownKbit != 475000 || p.Links[0].UpKbit != 47500 {
		t.Fatalf("plan: %+v", p)
	}
	_, m, _ := c.call("GET", "/api/sqm", nil)
	if m["queue"] != "cake" {
		t.Fatalf("%v", m)
	}
	// A failure leaves the old setting in place and says why.
	fn.mu.Lock()
	fn.sqmErr = errors.New("this system's kernel cannot shape incoming traffic")
	fn.mu.Unlock()
	if rc, _, _ := c.call("PUT", "/api/sqm", map[string]any{"links": map[string]bool{"Fibre": false}}); rc == 200 {
		t.Fatal("a failed change was reported as done")
	}
	fn.mu.Lock()
	fn.sqmErr = nil
	fn.mu.Unlock()
	if !a.sqmConfig().Links["Fibre"] {
		t.Fatal("the setting was not put back after the failure")
	}
	// Turning it off sends an empty plan that still names the port, so shaping is removed.
	if rc, _, raw := c.call("PUT", "/api/sqm", map[string]any{"links": map[string]bool{"Fibre": false}}); rc != 200 {
		t.Fatalf("%d %s", rc, raw)
	}
	fn.mu.Lock()
	p = fn.sqmPlans[len(fn.sqmPlans)-1]
	fn.mu.Unlock()
	if len(p.Links) != 0 || len(p.Known) == 0 {
		t.Fatalf("off plan: %+v", p)
	}
}

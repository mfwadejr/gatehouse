package app

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestRoutesCanBeReordered(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	for _, n := range []string{"First", "Second"} {
		if rc, _, raw := c.call("PUT", "/api/routes", map[string]any{"name": n, "enabled": true, "target_kind": "wan", "target": "Fibre", "who": "net:Home", "service": "ntp"}); rc != 200 {
			t.Fatalf("%s: %d %s", n, rc, raw)
		}
	}
	names := func() []string {
		_, _, raw := c.call("GET", "/api/routes", nil)
		var v struct {
			Routes []struct {
				ID   int64
				Name string
			}
		}
		json.Unmarshal(raw, &v)
		var out []string
		for _, r := range v.Routes {
			out = append(out, r.Name)
		}
		return out
	}
	_, _, raw := c.call("GET", "/api/routes", nil)
	var v struct{ Routes []struct{ ID int64 } }
	json.Unmarshal(raw, &v)
	if got := strings.Join(names(), ","); got != "First,Second" {
		t.Fatalf("start: %s", got)
	}
	if rc, _, raw := c.call("POST", "/api/routes/order", map[string]any{"order": []int64{v.Routes[1].ID, v.Routes[0].ID}}); rc != 200 {
		t.Fatalf("order: %d %s", rc, raw)
	}
	if got := strings.Join(names(), ","); got != "Second,First" {
		t.Fatalf("after: %s", got)
	}
}

func TestFirewallRulesCanBeReordered(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	for _, n := range []string{"Alpha", "Beta"} {
		if rc, _, raw := c.call("PUT", "/api/fw/rules", map[string]any{"name": n, "from": "Home", "to": "Internet", "dst": "203.0.113.9", "action": "drop", "enabled": true}); rc != 200 {
			t.Fatalf("%s: %d %s", n, rc, raw)
		}
	}
	var rules []struct {
		ID   int64
		Name string
	}
	_, _, raw := c.call("GET", "/api/fw/rules", nil)
	json.Unmarshal(raw, &rules)
	var ids []int64
	idOf := map[string]int64{}
	for _, r := range rules {
		idOf[r.Name] = r.ID
	}
	ids = []int64{idOf["Beta"], idOf["Alpha"]}
	if rc, _, raw := c.call("POST", "/api/fw/rules/order", map[string]any{"order": ids}); rc != 200 {
		t.Fatalf("order: %d %s", rc, raw)
	}
	_, _, raw = c.call("GET", "/api/fw/rules", nil)
	rules = nil
	json.Unmarshal(raw, &rules)
	pos := map[string]int{}
	for i, r := range rules {
		pos[r.Name] = i
	}
	if pos["Beta"] > pos["Alpha"] {
		t.Fatalf("order not kept: %s", raw)
	}
}

func TestKeepAddressButtonOnDevices(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	a := c.app
	mac := "aa:bb:cc:dd:ee:07"
	a.Config.Exec(`INSERT INTO dhcp_scopes(id,name,iface,subnet,range_start,range_end,router,mode) VALUES(1,'lan','eth1','192.168.1.0/24','192.168.1.100','192.168.1.200','192.168.1.1','serve')`)
	a.Config.Exec(`INSERT INTO dhcp_leases(scope_id,ip,mac,start,expires,state) VALUES(1,'192.168.1.150',?,?,?, 'active')`, mac, time.Now().Unix(), time.Now().Add(time.Hour).Unix())
	a.DHCP.Reload()
	c.call("POST", "/api/devices", map[string]any{"mac": mac, "name": "Printer"})
	id := func() string {
		_, _, raw := c.call("GET", "/api/devices", nil)
		var v struct {
			Devices []struct {
				ID       int64
				Reserved bool
			}
		}
		json.Unmarshal(raw, &v)
		if len(v.Devices) != 1 {
			t.Fatalf("devices: %s", raw)
		}
		return itoa(v.Devices[0].ID)
	}()
	reserved := func() bool {
		_, _, raw := c.call("GET", "/api/devices", nil)
		var v struct{ Devices []struct{ Reserved bool } }
		json.Unmarshal(raw, &v)
		return v.Devices[0].Reserved
	}
	if reserved() {
		t.Fatal("reserved before asking")
	}
	// The row button sends the address the device has now.
	if rc, _, raw := c.call("PUT", "/api/devices/"+id+"/reservation", map[string]any{"keep": true, "ip": "192.168.1.150"}); rc != 200 {
		t.Fatalf("keep: %d %s", rc, raw)
	}
	if !reserved() {
		t.Fatal("the address was not kept")
	}
	if rc, _, raw := c.call("PUT", "/api/devices/"+id+"/reservation", map[string]any{"keep": false}); rc != 200 {
		t.Fatalf("release: %d %s", rc, raw)
	}
	if reserved() {
		t.Fatal("the address was not released")
	}
}

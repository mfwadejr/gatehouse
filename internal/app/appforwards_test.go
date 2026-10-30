package app

import (
	"strings"
	"testing"
)

func TestAppForwardFollowsDeviceAndLimits(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	a := c.app
	if _, err := a.Config.Exec(`INSERT INTO dhcp_scopes(name,iface,subnet) VALUES('Home','eth1','192.168.1.0/24')`); err != nil {
		t.Fatal(err)
	}
	a.Config.Exec(`INSERT INTO dhcp_leases(scope_id,ip,mac,hostname,start,expires) VALUES(1,'192.168.1.50','aa:bb:cc:dd:ee:01','kitchen-pc',0,9999999999)`)

	// A device that has not been seen yet is refused with a plain reason.
	rc, _, raw := c.call("PUT", "/api/fw/appforwards", map[string]any{"app": "Plex", "mac": "aa:bb:cc:dd:ee:99", "enabled": true})
	if rc == 200 || !strings.Contains(string(raw), "not seen that device") {
		t.Fatalf("%d %s", rc, raw)
	}
	// An app Gatehouse does not know is refused.
	if rc, _, raw = c.call("PUT", "/api/fw/appforwards", map[string]any{"app": "Nope", "mac": "aa:bb:cc:dd:ee:01", "enabled": true}); rc == 200 || !strings.Contains(string(raw), "does not know an app") {
		t.Fatalf("%d %s", rc, raw)
	}
	// Plex on the kitchen PC, through Fibre only, from two countries.
	rc, _, raw = c.call("PUT", "/api/fw/appforwards", map[string]any{"app": "Plex", "mac": "aa:bb:cc:dd:ee:01", "connections": []string{"Fibre"}, "countries": []string{"nl", "de"}, "src": []string{"203.0.113.0/24"}, "enabled": true})
	if rc != 200 {
		t.Fatalf("%d %s", rc, raw)
	}
	_, m, raw := c.call("GET", "/api/fw/appforwards", nil)
	fs := m["forwards"].([]any)
	if len(fs) != 1 {
		t.Fatalf("%s", raw)
	}
	f := fs[0].(map[string]any)
	if !strings.Contains(f["text"].(string), "Plex on kitchen-pc is open on port 32400 through Fibre") || f["ip"] != "192.168.1.50" {
		t.Fatalf("%v", f)
	}
	if got := f["countries"].([]any); len(got) != 2 || got[0] != "NL" {
		t.Fatalf("countries: %v", got)
	}
	// The same name again is refused.
	if rc, _, _ = c.call("PUT", "/api/fw/appforwards", map[string]any{"app": "Plex", "mac": "aa:bb:cc:dd:ee:01", "enabled": true}); rc == 200 {
		t.Fatal("duplicate name accepted")
	}
	// The device's address changes: the forward follows it.
	a.Config.Exec(`UPDATE dhcp_leases SET ip='192.168.1.77' WHERE mac='aa:bb:cc:dd:ee:01'`)
	a.syncForwardDevices()
	_, m, _ = c.call("GET", "/api/fw/appforwards", nil)
	if m["forwards"].([]any)[0].(map[string]any)["ip"] != "192.168.1.77" {
		t.Fatalf("did not follow: %v", m["forwards"])
	}
	// A two-port app makes two forwards that are shown as one.
	if rc, _, raw = c.call("PUT", "/api/fw/appforwards", map[string]any{"app": "Web server", "name": "My site", "mac": "aa:bb:cc:dd:ee:01", "enabled": true}); rc != 200 {
		t.Fatalf("%d %s", rc, raw)
	}
	_, m, _ = c.call("GET", "/api/fw/appforwards", nil)
	if len(m["forwards"].([]any)) != 2 {
		t.Fatalf("%v", m["forwards"])
	}
	_, lm, _ := c.call("GET", "/api/fw/forwards", nil)
	_ = lm
	// Turn off, then delete the group.
	if rc, _, _ = c.call("POST", "/api/fw/appforwards/enable", map[string]any{"group": "My site", "enabled": false}); rc != 200 {
		t.Fatal("disable")
	}
	if rc, _, _ = c.call("DELETE", "/api/fw/appforwards/My%20site", nil); rc != 200 {
		t.Fatal("delete")
	}
	_, m, _ = c.call("GET", "/api/fw/appforwards", nil)
	if len(m["forwards"].([]any)) != 1 {
		t.Fatalf("%v", m["forwards"])
	}
}

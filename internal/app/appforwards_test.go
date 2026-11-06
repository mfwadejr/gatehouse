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
	// An app NopeWarden does not know is refused.
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

// Three Plex servers need three different outside ports. Nothing picks a port for the person: a
// clash is refused as a question, a chosen port is used as typed, and a rename of the saved app
// carries through to the open ports.
func TestCustomOutsidePortsAndRenameCarryThrough(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	a := c.app
	a.Config.Exec(`INSERT INTO dhcp_scopes(name,iface,subnet) VALUES('Home','eth1','192.168.1.0/24')`)
	for i, ip := range []string{"192.168.1.50", "192.168.1.51", "192.168.1.52"} {
		a.Config.Exec(`INSERT INTO dhcp_leases(scope_id,ip,mac,hostname,start,expires) VALUES(1,?,?,?,0,9999999999)`, ip, "aa:bb:cc:dd:ee:0"+string(rune('1'+i)), "server"+string(rune('1'+i)))
	}
	put := func(body map[string]any) (int, map[string]any) {
		rc, m, _ := c.call("PUT", "/api/fw/appforwards", body)
		return rc, m
	}
	// The first Plex takes the standard port.
	if rc, m := put(map[string]any{"app": "Plex", "name": "Plex 1", "mac": "aa:bb:cc:dd:ee:01", "enabled": true}); rc != 200 {
		t.Fatalf("first: %d %v", rc, m)
	}
	// The second one clashes. It is refused as a question that names who has the port; nothing is saved.
	rc, m := put(map[string]any{"app": "Plex", "name": "Plex 2", "mac": "aa:bb:cc:dd:ee:02", "enabled": true})
	cf, _ := m["conflict"].(map[string]any)
	if rc != 409 || cf == nil || cf["port"] != "32400" || !strings.Contains(cf["used_by"].(string), "Plex on server1") {
		t.Fatalf("clash: %d %v", rc, m)
	}
	_, list, _ := c.call("GET", "/api/fw/appforwards", nil)
	if len(list["forwards"].([]any)) != 1 {
		t.Fatalf("a refused port must save nothing: %v", list["forwards"])
	}
	// The person chooses non-sequential outside ports; the device keeps its own port 32400.
	if rc, m = put(map[string]any{"app": "Plex", "name": "Plex 2", "mac": "aa:bb:cc:dd:ee:02", "enabled": true, "outside": map[string]string{"tcp/32400": "41234"}}); rc != 200 {
		t.Fatalf("second: %d %v", rc, m)
	}
	if rc, m = put(map[string]any{"app": "Plex", "name": "Plex 3", "mac": "aa:bb:cc:dd:ee:03", "enabled": true, "outside": map[string]string{"tcp/32400": "50111"}}); rc != 200 {
		t.Fatalf("third: %d %v", rc, m)
	}
	var ext, in string
	a.Config.QueryRow(`SELECT ext_port,int_port FROM nat_forwards WHERE grp='Plex 2'`).Scan(&ext, &in)
	if ext != "41234" || in != "32400" {
		t.Fatalf("ports stored as %s -> %s", ext, in)
	}
	_, list, _ = c.call("GET", "/api/fw/appforwards", nil)
	fs := list["forwards"].([]any)
	if len(fs) != 3 || !strings.Contains(fs[1].(map[string]any)["text"].(string), "41234 (to port 32400 on the device)") {
		t.Fatalf("%v", fs)
	}
	// Editing a group does not clash with itself.
	if rc, m = put(map[string]any{"group": "Plex 2", "app": "Plex", "name": "Plex 2", "mac": "aa:bb:cc:dd:ee:02", "enabled": true, "outside": map[string]string{"tcp/32400": "41234"}}); rc != 200 {
		t.Fatalf("edit: %d %v", rc, m)
	}
	// A bad number and a port range that clashes are refused with a reason. The console, DNS and
	// DHCP numbers are allowed: those only answer on your own networks, not from the internet.
	for outside, want := range map[string]string{"0": "not a valid port", "70000": "not a valid port", "32400": "already used"} {
		rc, m := put(map[string]any{"app": "Plex", "name": "Plex 4", "mac": "aa:bb:cc:dd:ee:03", "enabled": true, "outside": map[string]string{"tcp/32400": outside}})
		if rc == 200 || !strings.Contains(m["error"].(string), want) {
			t.Fatalf("outside %s: %d %v", outside, rc, m)
		}
	}
	// The live check names what uses a port, and only offers a free one when asked.
	_, chk, _ := c.call("GET", "/api/fw/portcheck?proto=tcp&port=32400&suggest=1", nil)
	if !strings.Contains(chk["used_by"].(string), "Plex") || chk["suggest"] != "32401" {
		t.Fatalf("portcheck: %v", chk)
	}
	_, chk, _ = c.call("GET", "/api/fw/portcheck?proto=tcp&port=45000", nil)
	if chk["used_by"] != "" || chk["suggest"] != nil {
		t.Fatalf("a free port: %v", chk)
	}

	// Renaming a saved app carries through to the open ports made from it, and Services shows them.
	if rc, _, raw := c.call("PUT", "/api/fw/portlists", map[string]any{"name": "My game", "ports": []string{"udp/27000-27010"}}); rc != 200 {
		t.Fatalf("%d %s", rc, raw)
	}
	if rc, m = put(map[string]any{"app": "My game", "mac": "aa:bb:cc:dd:ee:01", "enabled": true}); rc != 200 {
		t.Fatalf("game: %d %v", rc, m)
	}
	_, svc, _ := c.call("GET", "/api/fw/services", nil)
	cust := svc["custom"].([]any)
	if len(cust) != 1 || !strings.Contains(strings.Join(toStrings(cust[0].(map[string]any)["used_by"]), ","), "Open port: My game") {
		t.Fatalf("used by: %v", cust)
	}
	if len(svc["open_ports"].([]any)) != 4 {
		t.Fatalf("services should list every open port: %v", svc["open_ports"])
	}
	id := cust[0].(map[string]any)["id"]
	if rc, _, raw := c.call("PUT", "/api/fw/portlists", map[string]any{"id": id, "name": "Arena", "ports": []string{"udp/27000-27010"}}); rc != 200 {
		t.Fatalf("rename: %d %s", rc, raw)
	}
	var g, app string
	a.Config.QueryRow(`SELECT grp,app FROM nat_forwards WHERE proto='udp'`).Scan(&g, &app)
	if g != "Arena" || app != "Arena" {
		t.Fatalf("rename did not carry through: group %q app %q", g, app)
	}
	// A saved app that still has open ports cannot be deleted.
	if rc, _, raw := c.call("DELETE", "/api/fw/portlists/"+itoa(int64(id.(float64))), nil); rc == 200 || !strings.Contains(string(raw), "open port Arena") {
		t.Fatalf("delete: %d %s", rc, raw)
	}
}

func toStrings(v any) []string {
	var out []string
	for _, x := range v.([]any) {
		out = append(out, x.(string))
	}
	return out
}

func TestForwardingAPortTheFirewallUsesWarnsButWorks(t *testing.T) {
	a := newScopeApp(t)
	a.Config.SetSetting("console.port", "8443")
	for _, c := range []struct {
		proto string
		lo    int
		hi    int
		want  string
	}{
		{"tcp", 8443, 8443, "console"},
		{"tcp", 22, 22, "remote login"},
		{"udp", 53, 53, "DNS"},
		{"tcp", 53, 53, "DNS"},
		{"udp", 67, 68, "DHCP"},
	} {
		n := a.routerPortNotes(c.proto, c.lo, c.hi, "the NAS")
		if len(n) == 0 || !strings.Contains(strings.Join(n, " "), c.want) || !strings.Contains(strings.Join(n, " "), "NAS") && c.want != "DHCP" {
			t.Errorf("%s %d-%d: %v", c.proto, c.lo, c.hi, n)
		}
	}
	for _, c := range []struct {
		proto string
		p     int
	}{{"tcp", 32400}, {"udp", 22}, {"tcp", 443}, {"tcp", 80}} {
		if n := a.routerPortNotes(c.proto, c.p, c.p, "x"); len(n) != 0 {
			t.Errorf("%s %d should have no note: %v", c.proto, c.p, n)
		}
	}
}

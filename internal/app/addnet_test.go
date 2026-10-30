package app

import (
	"encoding/json"
	"net/netip"
	"strconv"
	"strings"
	"testing"
)

func TestSuggestPrefixSkipsWhatIsUsed(t *testing.T) {
	used := []netip.Prefix{netip.MustParsePrefix("192.168.10.0/24"), netip.MustParsePrefix("192.168.20.0/23")}
	p, ok := suggestPrefix(used)
	if !ok || p.String() != "192.168.30.0/24" {
		t.Fatalf("got %v", p)
	}
	if rt := routerOf(p); rt.String() != "192.168.30.1/24" {
		t.Fatal(rt)
	}
	sm := summarise(netip.MustParsePrefix("192.168.30.1/24"))
	if sm.Usable != 254 || sm.From == "" || !strings.Contains(sm.Text, "254 usable") {
		t.Fatalf("%+v", sm)
	}
}

func TestAddNetworkMakesAllThreeParts(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")

	rc, m, raw := c.call("GET", "/api/networks/suggest", nil)
	if rc != 200 || m["address"] == nil || m["vlan"] == nil {
		t.Fatalf("suggest: %d %s", rc, raw)
	}
	addr := m["address"].(string)
	if strings.HasPrefix(addr, "192.168.1.") {
		t.Fatalf("suggested a range that is in use: %s", addr)
	}

	// Defaults: server DHCP, isolated, internet on, one-day leases.
	rc, _, raw = c.call("POST", "/api/networks/add", map[string]any{"name": "Cameras", "port": "eth2", "internet": true, "isolate": true})
	if rc != 200 {
		t.Fatalf("add: %d %s", rc, raw)
	}
	_, _, raw = c.call("GET", "/api/dhcp/scopes", nil)
	var scopes []map[string]any
	json.Unmarshal(raw, &scopes)
	var cam map[string]any
	for _, s := range scopes {
		if s["name"] == "Cameras" {
			cam = s
		}
	}
	if cam == nil || cam["mode"] != "serve" || cam["iface"] != "eth2" || cam["lease_seconds"].(float64) != 86400 || cam["range_start"] == "" {
		t.Fatalf("scope: %v", cam)
	}
	_, _, raw = c.call("GET", "/api/net/interfaces", nil)
	var ifs []map[string]any
	json.Unmarshal(raw, &ifs)
	found := false
	for _, i := range ifs {
		if i["name"] == "eth2" {
			found = true
			if i["ipv4"] == "" || i["pd"] == true {
				t.Fatalf("interface: %v", i)
			}
		}
	}
	if !found {
		t.Fatalf("no interface: %s", raw)
	}
	_, _, raw = c.call("GET", "/api/fw/rules", nil)
	if strings.Contains(string(raw), "Cameras") && strings.Contains(string(raw), "Matrix") {
		t.Fatalf("an isolated network got open rules: %s", raw)
	}

	// The port is taken now; a VLAN on it is fine; the same name is not.
	if rc, _, _ := c.call("POST", "/api/networks/add", map[string]any{"name": "Again", "port": "eth2"}); rc == 200 {
		t.Fatal("a port with a network was reused")
	}
	if rc, _, raw := c.call("POST", "/api/networks/add", map[string]any{"name": "Guests", "port": "eth2", "vlan": 30, "internet": true, "isolate": false,
		"dhcp": "relay", "relay": "192.168.1.5", "lease": "1h", "ipv6": "auto"}); rc != 200 {
		t.Fatalf("vlan: %d %s", rc, raw)
	}
	if rc, _, _ := c.call("POST", "/api/networks/add", map[string]any{"name": "Guests", "port": "eth2", "vlan": 31}); rc == 200 {
		t.Fatal("duplicate network name accepted")
	}
	// Not isolated: open both ways with the other local networks.
	_, _, raw = c.call("GET", "/api/fw/rules", nil)
	var rules []map[string]any
	json.Unmarshal(raw, &rules)
	open := 0
	for _, r := range rules {
		if r["matrix"] == true && r["action"] == "accept" {
			open++
		}
	}
	if open < 2 {
		t.Fatalf("expected the matrix to open Guests both ways: %s", raw)
	}
	// An address that overlaps something in use is refused, with the reason.
	rc, _, raw = c.call("POST", "/api/networks/add", map[string]any{"name": "Clash", "port": "eth2", "vlan": 40, "address": "192.168.1.1/24"})
	if rc == 200 || !strings.Contains(string(raw), "overlaps") {
		t.Fatalf("overlap not refused: %d %s", rc, raw)
	}
	// Bad choices are refused, and nothing half-made is left behind.
	before := len(scopes)
	for _, bad := range []map[string]any{
		{"name": "", "port": "eth2", "vlan": 50},
		{"name": "X", "port": "eth2", "vlan": 5000},
		{"name": "X", "port": "eth2", "vlan": 50, "dhcp": "relay"},
		{"name": "X", "port": "eth2", "vlan": 50, "lease": "forever"},
	} {
		if rc, _, _ := c.call("POST", "/api/networks/add", bad); rc == 200 {
			t.Errorf("accepted %v", bad)
		}
	}
	_, _, raw = c.call("GET", "/api/dhcp/scopes", nil)
	json.Unmarshal(raw, &scopes)
	if len(scopes) != before+1 { // Guests was added after the first count
		t.Fatalf("scopes %d, before %d", len(scopes), before)
	}

	// The live summary.
	rc, m, _ = c.call("GET", "/api/networks/check?address=192.168.77.1/24", nil)
	if rc != 200 || m["ok"] != true {
		t.Fatalf("check: %v", m)
	}
	_, m, _ = c.call("GET", "/api/networks/check?address=192.168.1.9/24", nil)
	if m["ok"] != false {
		t.Fatalf("overlap not reported: %v", m)
	}
}

func TestFirstNetworkOnAMachineWithNoManagementNetworkCanManage(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	// Take management away from every network, as on a machine with no network of its own yet.
	if _, err := c.app.Config.Exec(`UPDATE zones SET management=0`); err != nil {
		t.Fatal(err)
	}
	var zs []map[string]any
	rc, _, raw := c.call("POST", "/api/networks/add", map[string]any{"name": "Trusted", "port": "eth2", "internet": true, "isolate": false})
	if rc != 200 {
		t.Fatalf("the first network was refused: %d %s", rc, raw)
	}
	_, _, raw = c.call("GET", "/api/fw/zones", nil)
	json.Unmarshal(raw, &zs)
	for _, z := range zs {
		if z["name"] == "Trusted" && z["management"] == true && z["kind"] == "trusted" {
			return
		}
	}
	t.Fatalf("the new network should manage the device: %s", raw)
}

func TestMoveNetworkFollowsEverywhere(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	rc, _, raw := c.call("POST", "/api/networks/add", map[string]any{"name": "Cams", "port": "eth1", "vlan": 20, "address": "192.168.50.1/24", "dhcp": "server"})
	if rc != 200 {
		t.Fatalf("add: %d %s", rc, raw)
	}
	_, _, lraw := c.call("GET", "/api/net/interfaces", nil)
	var list []map[string]any
	json.Unmarshal(lraw, &list)
	var id float64
	for _, r := range list {
		if r["name"] == "eth1.20" {
			id = r["id"].(float64)
		}
	}
	if id == 0 {
		t.Fatalf("network not found: %s", lraw)
	}
	if rc, _, raw := c.call("POST", "/api/networks/move", map[string]any{"id": id, "port": "eth1", "vlan": 30}); rc != 200 {
		t.Fatalf("move: %d %s", rc, raw)
	}
	_, _, sc := c.call("GET", "/api/dhcp/scopes", nil)
	if !strings.Contains(string(sc), "eth1.30") {
		t.Fatalf("scope did not follow: %s", sc)
	}
	if rc, _, _ := c.call("POST", "/api/networks/move", map[string]any{"id": id, "port": "eth1", "vlan": 0}); rc != 400 {
		t.Fatalf("moving onto a port that has a network must be refused, got %d", rc)
	}
	if rc, _, raw := c.call("POST", "/api/networks/move", map[string]any{"id": id, "port": "eth2", "vlan": 0}); rc != 200 {
		t.Fatalf("to another whole port: %d %s", rc, raw)
	}
}

func idOf(t *testing.T, raw []byte, key, val string) float64 {
	t.Helper()
	var list []map[string]any
	json.Unmarshal(raw, &list)
	for _, r := range list {
		if r[key] == val {
			return r["id"].(float64)
		}
	}
	t.Fatalf("no row with %s=%q in %s", key, val, raw)
	return 0
}

// Removing a network takes its DHCP range and (when it was the last port) its firewall network
// with it, and tells the person what goes before they confirm.
func TestRemovingANetworkTakesItsLeftoversWithIt(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	if rc, _, raw := c.call("POST", "/api/networks/add", map[string]any{"name": "Cameras", "port": "eth2", "internet": true, "isolate": true}); rc != 200 {
		t.Fatalf("add: %d %s", rc, raw)
	}
	_, _, raw := c.call("GET", "/api/net/interfaces", nil)
	id := idOf(t, raw, "name", "eth2")
	rc, m, raw := c.call("GET", "/api/net/interfaces/"+fmtID(id)+"/removal", nil)
	if rc != 200 || m["remove_zone"] != true || m["zone"] != "Cameras" || len(m["scopes"].([]any)) != 1 {
		t.Fatalf("impact: %d %s", rc, raw)
	}
	if rc, _, raw := c.call("DELETE", "/api/net/interfaces/"+fmtID(id), nil); rc != 200 {
		t.Fatalf("remove: %d %s", rc, raw)
	}
	for path, what := range map[string]string{"/api/dhcp/scopes": "Cameras", "/api/fw/zones": "Cameras", "/api/net/interfaces": "eth2"} {
		_, _, raw := c.call("GET", path, nil)
		if strings.Contains(string(raw), `"`+what+`"`) {
			t.Errorf("%s still lists %s after removing the network: %s", path, what, raw)
		}
	}
}

// A network that can manage Gatehouse keeps its firewall network when its port goes.
func TestRemovingTheManagementPortKeepsItsFirewallNetwork(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	_, _, raw := c.call("GET", "/api/fw/zones", nil)
	var zs []map[string]any
	json.Unmarshal(raw, &zs)
	var mgmt string
	for _, z := range zs {
		if z["management"] == true {
			mgmt = z["name"].(string)
		}
	}
	if mgmt == "" {
		t.Skip("the test box has no network that can manage it")
	}
	_, _, raw = c.call("GET", "/api/net/interfaces", nil)
	var ifs []map[string]any
	json.Unmarshal(raw, &ifs)
	for _, i := range ifs {
		_, _, zraw := c.call("GET", "/api/fw/zones", nil)
		if i["zone_id"] == nil || !strings.Contains(string(zraw), mgmt) {
			continue
		}
		rc, m, raw := c.call("GET", "/api/net/interfaces/"+fmtID(i["id"].(float64))+"/removal", nil)
		if rc != 200 {
			t.Fatalf("impact: %d %s", rc, raw)
		}
		if m["management"] == true && m["remove_zone"] != false {
			t.Fatalf("a management network would lose its firewall network: %s", raw)
		}
	}
}

// Removing a firewall network also removes a leftover DHCP range of the same name whose port is gone.
func TestRemovingAFirewallNetworkRemovesALeftoverRange(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	if rc, _, raw := c.call("PUT", "/api/fw/zones", map[string]any{"name": "Wi-Fi", "kind": "trusted", "ifaces": []string{}, "internet": true, "gateway": true, "intra_allow": true}); rc != 200 {
		t.Fatalf("zone: %d %s", rc, raw)
	}
	if _, err := c.app.Config.Exec(`INSERT INTO dhcp_scopes(name,iface,subnet,range_start,range_end,router) VALUES('Wi-Fi','wlp3s0','192.168.23.0/24','192.168.23.100','192.168.23.200','192.168.23.1')`); err != nil {
		t.Fatal(err)
	}
	_, _, raw := c.call("GET", "/api/fw/zones", nil)
	id := idOf(t, raw, "name", "Wi-Fi")
	rc, m, raw := c.call("GET", "/api/fw/zones/"+fmtID(id)+"/removal", nil)
	if rc != 200 || len(m["scopes"].([]any)) != 1 {
		t.Fatalf("impact: %d %s", rc, raw)
	}
	if rc, _, raw := c.call("DELETE", "/api/fw/zones/"+fmtID(id), nil); rc != 200 {
		t.Fatalf("remove: %d %s", rc, raw)
	}
	_, _, raw = c.call("GET", "/api/dhcp/scopes", nil)
	if strings.Contains(string(raw), "Wi-Fi") {
		t.Errorf("the Wi-Fi address range is still there: %s", raw)
	}
}

func fmtID(f float64) string { return strconv.FormatInt(int64(f), 10) }

// A port can sit in a bridge only if it belongs nowhere else, and the reason is said in words.
func TestBridgeRefusesPortsInUseElsewhere(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	if rc, _, raw := c.call("POST", "/api/networks/add", map[string]any{"name": "Cameras", "port": "eth2", "internet": true, "isolate": true}); rc != 200 {
		t.Fatalf("add: %d %s", rc, raw)
	}
	br := map[string]any{"name": "br-test", "kind": "bridge", "ipv4": "10.77.0.1/24", "enabled": true, "members": []string{"eth2"}}
	rc, _, raw := c.call("PUT", "/api/net/interfaces", br)
	if rc == 200 || !strings.Contains(string(raw), "already has a network of its own") {
		t.Fatalf("a port with its own network must be refused: %d %s", rc, raw)
	}
	br["members"] = []string{"eth3", "eth3"}
	if rc, _, raw = c.call("PUT", "/api/net/interfaces", br); rc == 200 || !strings.Contains(string(raw), "listed twice") {
		t.Fatalf("duplicate: %d %s", rc, raw)
	}
	br["members"] = []string{"eth3"}
	if rc, _, raw = c.call("PUT", "/api/net/interfaces", br); rc != 200 {
		t.Fatalf("a free port must join: %d %s", rc, raw)
	}
	// A second bridge cannot take the same port.
	br2 := map[string]any{"name": "br-two", "kind": "bridge", "ipv4": "10.78.0.1/24", "enabled": true, "members": []string{"eth3"}}
	if rc, _, raw = c.call("PUT", "/api/net/interfaces", br2); rc == 200 || !strings.Contains(string(raw), "already belongs to another network") {
		t.Fatalf("port in two bridges: %d %s", rc, raw)
	}
	// The suggest list shows Wi-Fi cards only on request.
	if _, m, _ := c.call("GET", "/api/networks/suggest?all=1", nil); m["ports"] == nil {
		t.Fatal("ports missing")
	}
}

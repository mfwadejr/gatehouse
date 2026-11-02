package app

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func listLine(dir string) []byte {
	return []byte(fmt.Sprintf(`{"event_type":"alert","src_ip":"203.0.113.9","dest_ip":"192.168.1.150","dest_port":443,"proto":"TCP","direction":%q,"alert":{"signature_id":2400001,"signature":"ET DROP Spamhaus DROP Listed Traffic Inbound group 9","category":"Misc Attack","severity":2}}`, dir))
}

func TestThreatAlertsAreGroupedNamedByAliasAndWorded(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	a := c.app
	a.Config.SetSetting(threatModeSetting, "watch")
	mac := "aa:bb:cc:dd:ee:01"
	a.Config.Exec(`INSERT INTO dhcp_scopes(id,name,iface,subnet,range_start,range_end,router) VALUES(1,'lan','eth1','192.168.1.0/24','192.168.1.100','192.168.1.200','192.168.1.1')`)
	a.Config.Exec(`INSERT INTO dhcp_leases(scope_id,ip,mac,start,expires,state,hostname) VALUES(1,'192.168.1.150',?,?,?, 'active','dtbp0741s')`, mac, time.Now().Unix(), time.Now().Add(time.Hour).Unix())
	a.DHCP.Reload()
	now := time.Now()
	for i := 0; i < 9; i++ {
		a.threatIngest(listLine("to_client"), now.Add(time.Duration(i)*time.Second))
	}
	groups := a.threatGroups(60)
	if len(groups) != 1 || groups[0].Count != 9 {
		t.Fatalf("repeats must fold into one row with a count: %+v", groups)
	}
	g := groups[0]
	// no name given yet: the name the device reported
	if g.Device != "dtbp0741s" {
		t.Fatalf("device: %q", g.Device)
	}
	// started by the device (to_client means the destination started it)
	if g.Started != "device" || !strings.Contains(g.Title, "contacted an address on a known-bad list (Spamhaus)") || strings.Contains(g.Title, "break into") {
		t.Fatalf("wording: %+v", g)
	}
	// the person names the device afterwards: old alerts show the new name
	if rc, _, raw := c.call("POST", "/api/devices", map[string]any{"mac": mac, "name": "Marvin's MacBook"}); rc != 200 {
		t.Fatalf("%d %s", rc, raw)
	}
	g = a.threatGroups(60)[0]
	if g.Device != "Marvin's MacBook" || !strings.HasPrefix(g.Title, "Marvin's MacBook contacted") {
		t.Fatalf("alias not used: %+v", g)
	}
	// an unknown address falls back to the address
	if got := a.deviceLabeler()("", "192.168.1.77"); got != "192.168.1.77" {
		t.Fatalf("fallback: %q", got)
	}
	// who started it is not said when the engine does not say
	if a.threatTitleFor("") == "" {
		t.Fatal("no title")
	}
}

func (a *App) threatTitleFor(dir string) string {
	return threatTitle(threatKindList, "ET DROP Spamhaus DROP Listed Traffic", "the MacBook", dir)
}

func TestEngineNoiseIsQuietAndNotCalledAThreat(t *testing.T) {
	if threatKind("Generic Protocol Command Decode", "SURICATA TLS invalid record type") != threatKindOdd {
		t.Fatal("engine warnings must be the quiet group")
	}
	title := threatTitle(threatKindOdd, "SURICATA STREAM Packet with invalid timestamp", "vSeeBox", "")
	if strings.Contains(strings.ToLower(title), "suspicious") || strings.Contains(title, "SURICATA") {
		t.Fatalf("title: %q", title)
	}
	if got := threatTitle(threatKindList, "ET DROP Spamhaus x", "Mac", "in"); !strings.HasPrefix(got, "An address on a known-bad list (Spamhaus) contacted Mac") {
		t.Fatalf("in: %q", got)
	}
	if got := threatTitle(threatKindList, "ET DROP Spamhaus x", "Mac", ""); !strings.Contains(got, "exchanged traffic") {
		t.Fatalf("unknown direction: %q", got)
	}
}

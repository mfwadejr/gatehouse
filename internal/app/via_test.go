package app

import (
	"strings"
	"testing"
	"time"

	"nopewarden/internal/netd"
)

func viaTestApp(t *testing.T, state string) *App {
	a, _ := newSpeedApp(t)
	a.vpn.snap = vpnSnapshot{Views: []VPNView{{Dev: "ex-nl", Label: "ExpressVPN NL", Role: "client", State: state,
		Uses: []UseView{{Kind: "device", Value: "aa:bb:cc:dd:ee:01", Label: "Marvin's Mac", IPs: []string{"192.168.50.20/32"}}}}}}
	return a
}

func logConn(t *testing.T, a *App, from, to, out string) {
	t.Helper()
	a.storeFW([]netd.FWEvent{{TS: time.Now().Unix(), Verdict: "allow", Tag: "internet:LAN", In: "lan0", Out: out, Src: from, Dst: to, Proto: "tcp", DPort: 443}})
}

func TestLogRecordsWhichConnectionItLeftThrough(t *testing.T) {
	a := viaTestApp(t, "up")
	logConn(t, a, "192.168.50.20", "93.184.216.34", "ex-nl")
	logConn(t, a, "192.168.50.30", "93.184.216.34", "eth0")
	logConn(t, a, "192.168.50.30", "192.168.50.1", "")
	if l, k := a.viaOf("ex-nl"); l != "ExpressVPN NL" || k != "tunnel" {
		t.Fatalf("tunnel: %q %q", l, k)
	}
	if l, k := a.viaOf("eth0"); l != "Fibre" || k != "wan" {
		t.Fatalf("wan: %q %q", l, k)
	}
	if l, _ := a.viaOf("lan0"); l != "" {
		t.Fatalf("a local port is not a connection: %q", l)
	}
	items := a.searchFWf(fwFilter{Limit: 10})
	got := map[string]string{}
	for _, it := range items {
		got[it.Src] = it.Via
	}
	if got["192.168.50.20"] != "ExpressVPN NL" {
		t.Fatalf("log rows: %+v", got)
	}
}

func TestViaCheckSaysWhetherTheDeviceUsesItsVPN(t *testing.T) {
	a := viaTestApp(t, "up")
	if c := a.viaCheck("192.168.50.20", "Mac"); c.Verdict != "idle" || c.Tunnel != "ExpressVPN NL" {
		t.Fatalf("nothing logged yet: %+v", c)
	}
	logConn(t, a, "192.168.50.20", "1.1.1.1", "ex-nl")
	logConn(t, a, "192.168.50.20", "8.8.8.8", "ex-nl")
	c := a.viaCheck("192.168.50.20", "Mac")
	if c.Verdict != "ok" || c.Through != 2 || c.Direct != 0 || !strings.Contains(c.Sentence, "went through ExpressVPN NL") {
		t.Fatalf("%+v", c)
	}
	// One connection goes out the normal way: that is a leak, and the sentence says so.
	logConn(t, a, "192.168.50.20", "9.9.9.9", "eth0")
	c = a.viaCheck("192.168.50.20", "Mac")
	if c.Verdict != "leak" || c.Direct != 1 || len(c.Others) != 1 || c.Others[0].Label != "Fibre" || len(c.Leaked) != 1 {
		t.Fatalf("%+v", c)
	}
	// A device that is not sent through a VPN has nothing to check.
	if c := a.viaCheck("192.168.50.99", "TV"); c.Verdict != "none" {
		t.Fatalf("%+v", c)
	}
}

func TestDevicesShowTheVPNTheyAreSentThrough(t *testing.T) {
	a := viaTestApp(t, "down")
	l, dev, st := a.tunnelOfIP("192.168.50.20")
	if l != "ExpressVPN NL" || dev != "ex-nl" || st != "down" {
		t.Fatalf("%q %q %q", l, dev, st)
	}
	if l, _, _ := a.tunnelOfIP("192.168.50.21"); l != "" {
		t.Fatalf("other address: %q", l)
	}
}

func TestDNSPathFollowsTheVPNAndItsKillSwitch(t *testing.T) {
	a := viaTestApp(t, "up")
	set := func(state string, kill bool) {
		a.vpn.snap = vpnSnapshot{Views: []VPNView{{Dev: "ex-nl", Label: "ExpressVPN NL", Role: "client", State: state, Enabled: true, KillSwitch: kill,
			Uses: []UseView{{Kind: "device", Value: "aa:bb:cc:dd:ee:01", IPs: []string{"192.168.50.20/32"}}}}}}
	}
	set("up", true)
	if p := a.dnsPath("192.168.50.20"); p.Dev != "ex-nl" || !p.Strict || p.Hold {
		t.Fatalf("up, kill switch on: %+v", p)
	}
	set("up", false)
	if p := a.dnsPath("192.168.50.20"); p.Dev != "ex-nl" || p.Strict || p.Hold {
		t.Fatalf("up, kill switch off: %+v", p)
	}
	set("down", true)
	if p := a.dnsPath("192.168.50.20"); !p.Hold {
		t.Fatalf("down with kill switch must hold the lookup: %+v", p)
	}
	set("down", false)
	if p := a.dnsPath("192.168.50.20"); p.Dev != "" || p.Hold {
		t.Fatalf("down without kill switch must use the normal connection: %+v", p)
	}
	set("up", true)
	if p := a.dnsPath("192.168.50.99"); p.Dev != "" || p.Hold {
		t.Fatalf("a device that does not use the VPN must be untouched: %+v", p)
	}
}

func TestCheckCountsNameLookupsAndNoticesAChangedAddress(t *testing.T) {
	a := viaTestApp(t, "up")
	if _, err := a.Config.Exec(`INSERT INTO devices(mac,name,last_ip) VALUES('aa:bb:cc:dd:ee:01','macbookpro','192.168.50.106')`); err != nil {
		t.Fatal(err)
	}
	// the routing was set up for .20 but the Mac is at .106 now
	c := a.viaCheckMAC("aa:bb:cc:dd:ee:01")
	if c.Verdict != "address" || !strings.Contains(c.Sentence, "192.168.50.106") || !strings.Contains(c.Sentence, "192.168.50.20") {
		t.Fatalf("a changed address was not noticed: %+v", c)
	}
	// now the routing follows the Mac: connections and lookups through the tunnel are counted
	a.vpn.snap = vpnSnapshot{Views: []VPNView{{Dev: "ex-nl", Label: "ExpressVPN NL", Role: "client", State: "up",
		Uses: []UseView{{Kind: "device", Value: "aa:bb:cc:dd:ee:01", IPs: []string{"192.168.50.106/32"}}}}}}
	logConn(t, a, "192.168.50.106", "1.1.1.1", "ex-nl")
	now := time.Now().Unix()
	for _, row := range []struct{ out string }{{"ex-nl"}, {"ex-nl"}, {""}} {
		if _, err := a.Logs.Exec(`INSERT INTO dns_queries(ts,client,client_name,qname,qtype,result,out_if) VALUES(?,?,?,?,?,?,?)`, now, "192.168.50.106", "macbookpro", "example.org", "A", "forwarded", row.out); err != nil {
			t.Fatal(err)
		}
	}
	c = a.viaCheckMAC("aa:bb:cc:dd:ee:01")
	if c.Verdict != "leak" || c.DNSThrough != 2 || c.DNSDirect != 1 || c.Through != 1 || c.CheckedAt == 0 || c.Window != 900 {
		t.Fatalf("%+v", c)
	}
	// older lookups are not counted
	a.Logs.Exec(`UPDATE dns_queries SET ts=ts-3600`)
	c = a.viaCheckMAC("aa:bb:cc:dd:ee:01")
	if c.DNSDirect != 0 || c.DNSThrough != 0 || c.Verdict != "ok" {
		t.Fatalf("old rows counted: %+v", c)
	}
}

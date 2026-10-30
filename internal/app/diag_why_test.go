package app

import (
	"net/netip"
	"strings"
	"testing"

	"gatehouse/internal/fw"
)

func TestRuleAddrAndServiceMatch(t *testing.T) {
	ip := netip.MustParseAddr("192.168.1.50")
	g := map[string]fw.DevGroup{"Kids": {MACs: []string{"aa:bb:cc:dd:ee:01"}}}
	cases := []struct {
		f           string
		mac         string
		match, sure bool
	}{
		{"any", "", true, true},
		{"192.168.1.0/24", "", true, true},
		{"192.168.2.0/24", "", false, true},
		{"192.168.1.40-192.168.1.60", "", true, true},
		{"grp:Kids", "aa:bb:cc:dd:ee:01", true, true},
		{"grp:Kids", "aa:bb:cc:dd:ee:99", false, true},
		{"geo:DE", "", false, false},
	}
	for _, c := range cases {
		m, s := ruleAddrMatch(c.f, ip, c.mac, g)
		if m != c.match || s != c.sure {
			t.Errorf("%q mac=%q got %v/%v want %v/%v", c.f, c.mac, m, s, c.match, c.sure)
		}
	}
	if m, s := ruleServiceMatch("https", "tcp", 443, nil); !m || !s {
		t.Error("https 443")
	}
	if m, s := ruleServiceMatch("https", "tcp", 80, nil); m || !s {
		t.Error("https 80")
	}
	if m, s := ruleServiceMatch("https", "tcp", 0, nil); !m || s {
		t.Error("no port is unsure")
	}
}

func TestWhyFindsPauseAndCustomBlock(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	a := c.app
	if a == nil {
		t.Skip("box has no app")
	}
	a.Config.Exec(`INSERT INTO devices(mac,name,last_ip) VALUES('aa:bb:cc:dd:ee:07','laptop','192.168.1.50')`)
	var id int64
	a.Config.QueryRow(`SELECT id FROM devices WHERE mac='aa:bb:cc:dd:ee:07'`).Scan(&id)

	if err := a.Lists.AddCustom("blocked-example.com", "block", ""); err != nil {
		t.Fatal(err)
	}
	r, err := a.whyCannotReach(id, "blocked-example.com", 443, "tcp")
	if err != nil {
		t.Fatal(err)
	}
	if r.Verdict != "blocked" || r.Findings[1].Fix == nil || r.Findings[1].Fix.Kind != "allow-domain" {
		t.Fatalf("%+v", r)
	}

	rc, _, raw := c.call("POST", "/api/devices/"+itoa(id)+"/pause", map[string]any{"minutes": 30})
	if rc != 200 {
		t.Fatalf("%d %s", rc, raw)
	}
	r, _ = a.whyCannotReach(id, "93.184.216.34", 443, "tcp")
	if r.Verdict != "blocked" || r.Findings[0].Stage != "pause" || r.Findings[0].Fix.Kind != "resume-device" || !strings.Contains(r.Summary, "paused") {
		t.Fatalf("%+v", r)
	}
	rc, _, raw = c.call("POST", "/api/diag/why", map[string]any{"device": id, "target": "https://93.184.216.34/"})
	if rc != 200 {
		t.Fatalf("%d %s", rc, raw)
	}
	if rc, _, _ = c.call("POST", "/api/diag/why", map[string]any{"device": 9999, "target": "x.com"}); rc == 200 {
		t.Fatal("unknown device")
	}
}

package app

import (
	"strings"
	"testing"
	"time"
)

func TestUsagePeriodFollowsBillingDay(t *testing.T) {
	loc := time.UTC
	f, to := usagePeriod(time.Date(2026, 10, 20, 12, 0, 0, 0, loc), 15)
	if f != time.Date(2026, 10, 15, 0, 0, 0, 0, loc) || to != time.Date(2026, 11, 15, 0, 0, 0, 0, loc) {
		t.Fatal(f, to)
	}
	f, to = usagePeriod(time.Date(2026, 10, 3, 12, 0, 0, 0, loc), 15)
	if f != time.Date(2026, 9, 15, 0, 0, 0, 0, loc) || to != time.Date(2026, 10, 15, 0, 0, 0, 0, loc) {
		t.Fatal(f, to)
	}
}

func TestISPReportOutagesAndSpeed(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	a := c.app
	if a == nil {
		t.Skip("no app")
	}
	from, _, label, _ := a.ispMonth("")
	base := from.Unix() + 3600
	a.Logs.Exec(`INSERT INTO outages(start,end) VALUES(?,?)`, base, base+1800)      // 30 minutes
	a.Logs.Exec(`INSERT INTO outages(start,end) VALUES(?,?)`, base+7200, base+7230) // a blip, ignored
	a.Logs.Exec(`INSERT INTO speedtests(ts,link,down_bps,up_bps,delay_ms,loaded_ms) VALUES(?,?,?,?,?,?)`, from.Unix()+60, "Fibre", 400e6, 40e6, 10, 30)
	a.Logs.Exec(`INSERT INTO speedtests(ts,link,down_bps,up_bps,delay_ms,loaded_ms) VALUES(?,?,?,?,?,?)`, from.Unix()+120, "Fibre", 200e6, 40e6, 12, 90)
	r, err := a.ispReport(label)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Outages) != 1 || r.DownSecs != 1800 || len(r.Links) != 1 || r.Links[0].Tests != 2 || r.Links[0].AvgDownMbps != 300 {
		t.Fatalf("%+v", r)
	}
	if rc, _, _ := c.call("GET", "/api/isp/report?month=nonsense", nil); rc == 200 {
		t.Fatal("bad month accepted")
	}
	rc, _, raw := c.call("GET", "/api/isp/report/print?month="+label, nil)
	if rc != 200 || !strings.Contains(string(raw), "Internet report") || !strings.Contains(string(raw), "30 min") {
		t.Fatalf("%d %s", rc, raw)
	}
}

func TestUsageCapConfigAndTotals(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	a := c.app
	if a == nil {
		t.Skip("no app")
	}
	if rc, _, _ := c.call("PUT", "/api/usage", map[string]any{"on": true, "cap_gb": 0, "start_day": 1}); rc == 200 {
		t.Fatal("cap of zero accepted")
	}
	if rc, _, _ := c.call("PUT", "/api/usage", map[string]any{"on": false, "cap_gb": 0, "start_day": 31}); rc == 200 {
		t.Fatal("day 31 accepted")
	}
	rc, m, raw := c.call("PUT", "/api/usage", map[string]any{"on": true, "cap_gb": 100, "start_day": 1, "levels": []int{90, 50}})
	if rc != 200 {
		t.Fatalf("%d %s", rc, raw)
	}
	v := a.usageNow()
	a.Logs.Exec(`INSERT INTO traffic(ts,mac,up,down) VALUES(?,'',?,?)`, v.From+3600, int64(10e9), int64(40e9))
	a.Logs.Exec(`INSERT INTO traffic(ts,mac,up,down) VALUES(?,'aa:bb:cc:dd:ee:01',?,?)`, v.From+3600, int64(1e9), int64(5e9))
	v = a.usageNow()
	if int(v.Pct) != 50 || len(v.Top) != 1 || v.Levels[0] != 50 || m["cap_gb"].(float64) != 100 {
		t.Fatalf("%+v", v)
	}
}

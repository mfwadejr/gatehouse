package app

import (
	"testing"
	"time"
)

func TestThreatOverviewCountsAlertsAndFeed(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	a := c.app
	now := time.Now()
	add := func(age time.Duration, sig, cat, src, dst string) {
		a.Logs.Exec(`INSERT INTO threat_alerts(ts,sid,signature,plain,category,severity,src,dst,dport,proto,mac,device,dir) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			now.Add(-age).Unix(), 1000+int64(age/time.Minute), sig, "", cat, 2, src, dst, 443, "tcp", "", "", "")
	}
	add(10*time.Minute, "ET CINS Active Threat Intelligence Poor Reputation IP 1", "Misc Attack", "192.168.1.20", "203.0.113.9")
	add(5*time.Hour, "ET SCAN Nmap", "Attempted Information Leak scan", "198.51.100.7", "192.168.1.20")
	add(3*24*time.Hour, "SURICATA STREAM reassembly", "Generic Protocol Command Decode", "192.168.1.30", "203.0.113.1")
	add(10*24*time.Hour, "ET DROP Dshield", "Misc Attack", "192.168.1.20", "203.0.113.5") // older than a week: not counted
	hour, day, week, kinds, devs := a.threatCounts(now)
	if hour != 1 || day != 2 || week != 3 {
		t.Fatalf("hour/day/week = %d/%d/%d", hour, day, week)
	}
	if len(kinds) != 3 || kinds[len(kinds)-1].Kind != threatKindOdd || !kinds[len(kinds)-1].Quiet {
		t.Fatalf("harmless kinds should come last: %+v", kinds)
	}
	if len(devs) != 1 || devs[0].Count != 2 {
		t.Fatalf("odd traffic must not make a device look bad, and one device had two real alerts: %+v", devs)
	}
	a.Logs.Exec(`INSERT INTO events(ts,area,severity,actor,title,detail) VALUES(?,?,?,?,?,?)`, now.Unix(), "threat", "info", "system", "Threat rules updated", "41892 rules are loaded.")
	a.Logs.Exec(`INSERT INTO events(ts,area,severity,actor,title,detail) VALUES(?,?,?,?,?,?)`, now.Unix(), "system", "warning", "system", "Threat watching was switched off", "It used too much memory.")
	a.Logs.Exec(`INSERT INTO events(ts,area,severity,actor,title,detail) VALUES(?,?,?,?,?,?)`, now.Unix(), "system", "info", "admin", "Renamed a device", "x")
	feed := a.threatFeed(12)
	if len(feed) != 2 {
		t.Fatalf("feed should hold the two threat lines only: %+v", feed)
	}
	ov := a.threatOverview(now)
	if ov["installed"] != false || ov["alerts"].(map[string]int)["week"] != 3 {
		t.Fatalf("overview: %+v", ov)
	}
}

func TestThreatHistoryIsOnlySavedWhileWatching(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	a := c.app
	if pts := a.threatHistPoints(); len(pts) != 0 {
		t.Fatalf("saved points while off: %+v", pts)
	}
	a.Config.SetSetting(threatModeSetting, "watch")
	if pts := a.threatHistPoints(); len(pts) != 0 {
		t.Fatalf("saved points while not running: %+v", pts)
	}
	a.threatW.mu.Lock()
	a.threatW.runningSince, a.threatW.cpuPct, a.threatW.memMB = time.Now(), 12.5, 300
	a.threatW.mu.Unlock()
	if pts := a.threatHistPoints(); len(pts) != 3 || pts[1].Key != "threat.cpu" || pts[1].Val != 12.5 || pts[2].Val != 300 {
		t.Fatalf("points: %+v", pts)
	}
}

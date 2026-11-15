package app

import (
	"testing"
	"time"
)

func TestThreatEdgeNumbers(t *testing.T) {
	a, _ := newSpeedApp(t)
	now := time.Now()
	ins := func(ts int64, action, from, fz, tz, proto string, port int) {
		a.Logs.Exec(`INSERT INTO fw_events(ts,action,from_zone,from_addr,to_zone,to_addr,proto,dport) VALUES(?,?,?,?,?,?,?,?)`, ts, action, fz, from, tz, "192.0.2.2", proto, port)
	}
	// One address tries many doors, another one door, an inside device is ignored, an allowed one is ignored.
	for p := 1; p <= 20; p++ {
		ins(now.Unix()-60, "drop", "198.51.100.7", "Internet", "Home", "tcp", 1000+p)
	}
	for i := 0; i < 5; i++ {
		ins(now.Unix()-30, "drop", "203.0.113.5", "Internet", "Home", "tcp", 22)
	}
	ins(now.Unix()-30, "drop", "192.168.1.9", "Home", "Internet", "tcp", 25)
	ins(now.Unix()-30, "allow", "203.0.113.6", "Internet", "Home", "tcp", 443)
	// An older bad day.
	ins(now.AddDate(0, 0, -3).Unix(), "drop", "203.0.113.5", "Internet", "Home", "tcp", 22)

	e := a.threatEdge(now, 24)
	if e["total"].(int) != 25 || e["sources"].(int) != 2 {
		t.Fatalf("total/sources = %v/%v", e["total"], e["sources"])
	}
	pr := e["probers"].([]edgeProber)
	if len(pr) != 2 || pr[0].Addr != "198.51.100.7" || pr[0].Ports != 20 || pr[0].Reason != "Trying many doors to see which are open" {
		t.Fatalf("probers = %+v", pr)
	}
	if pr[1].Reason != "Knocking on SSH (tcp/22)" {
		t.Fatalf("reason = %q", pr[1].Reason)
	}
	ports := e["ports"].([]edgePort)
	if ports[0].Label != "SSH (tcp/22)" || ports[0].Sources != 1 {
		t.Fatalf("ports = %+v", ports[0])
	}
	days := e["days"].([]edgeDay)
	if len(days) != 30 || days[29].Total != 25 || days[29].Worst < 25 {
		t.Fatalf("days = %+v", days[27:])
	}
	if days[26].Total != 1 {
		t.Fatalf("three days ago = %+v", days[26])
	}
}

func TestDailyHistoryRoll(t *testing.T) {
	a, _ := newSpeedApp(t)
	now := time.Now()
	end := dayStart(now)
	start := dayStart(time.Unix(end-3600, 0))
	for i, v := range []float64{10, 20, 60} {
		a.Logs.Exec(`INSERT INTO metrics(ts,key,value) VALUES(?,?,?)`, start+int64(i)*300+10, "b|dns.qps", v)
	}
	a.rollDaily(now)
	var avg, lo, hi float64
	a.Logs.QueryRow(`SELECT value FROM metrics WHERE key='d|dns.qps' AND ts=?`, start).Scan(&avg)
	a.Logs.QueryRow(`SELECT value FROM metrics WHERE key='n|dns.qps' AND ts=?`, start).Scan(&lo)
	a.Logs.QueryRow(`SELECT value FROM metrics WHERE key='x|dns.qps' AND ts=?`, start).Scan(&hi)
	if avg != 30 || lo != 10 || hi != 60 {
		t.Fatalf("avg %v low %v high %v", avg, lo, hi)
	}
	h := a.history("1y")
	if h["low"] == nil || len(h["series"].(map[string][][2]float64)["dns.qps"]) != 1 {
		t.Fatalf("history = %+v", h)
	}
	// A shorter keep removes older days; Everything keeps them.
	a.Logs.Exec(`INSERT INTO metrics(ts,key,value) VALUES(?,?,?)`, now.Unix()-400*86400, "d|dns.qps", 5)
	a.recordHistory(now)
	var n int
	a.Logs.QueryRow(`SELECT COUNT(*) FROM metrics WHERE key='d|dns.qps' AND ts<?`, now.Unix()-366*86400).Scan(&n)
	if n != 0 {
		t.Fatalf("old day kept with 1 year: %d", n)
	}
	a.Config.SetSetting("logs.history_days", "0")
	a.Logs.Exec(`INSERT INTO metrics(ts,key,value) VALUES(?,?,?)`, now.Unix()-900*86400, "d|dns.qps", 5)
	a.recordHistory(now.Add(time.Minute))
	a.Logs.QueryRow(`SELECT COUNT(*) FROM metrics WHERE key='d|dns.qps' AND ts<?`, now.Unix()-366*86400).Scan(&n)
	if n != 1 {
		t.Fatalf("Everything should keep it: %d", n)
	}
}

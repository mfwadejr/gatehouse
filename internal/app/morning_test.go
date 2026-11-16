package app

import (
	"testing"
	"time"
)

func TestYesterdayNumbers(t *testing.T) {
	a, _ := newSpeedApp(t)
	now := time.Now()
	to := dayStart(now)
	from := dayStart(time.Unix(to-3600, 0))
	mid := from + 12*3600
	for i := 0; i < 10; i++ {
		res := "forwarded"
		if i < 3 {
			res = "blocked"
		}
		a.Logs.Exec(`INSERT INTO dns_queries(ts,client,qname,qtype,result) VALUES(?,?,?,?,?)`, mid+int64(i), "192.168.1.5", "x.example", "A", res)
	}
	a.Logs.Exec(`INSERT INTO dns_queries(ts,client,qname,qtype,result) VALUES(?,?,?,?,?)`, to+10, "192.168.1.5", "today.example", "A", "forwarded")
	for i := 0; i < 4; i++ {
		a.Logs.Exec(`INSERT INTO fw_events(ts,action,from_zone,from_addr,to_zone,to_addr,proto,dport) VALUES(?,?,?,?,?,?,?,?)`, mid+int64(i), "drop", "Internet", "198.51.100.7", "Home", "192.0.2.2", "tcp", 22)
	}
	// One outage that began the evening before and ended an hour into yesterday, one that is still open now.
	a.Logs.Exec(`INSERT INTO outages(start,end) VALUES(?,?)`, from-600, from+3600)
	a.Logs.Exec(`INSERT INTO outages(start,end) VALUES(?,0)`, to-1800)
	a.Logs.Exec(`INSERT INTO metrics(ts,key,value) VALUES(?,?,?)`, from, "x|devices.online", 14)
	y := a.yesterday(now)
	if y.DNSTotal != 10 || y.DNSBlocked != 3 || y.BlockedIn != 4 || y.ProbeAddrs != 1 {
		t.Fatalf("counts = %+v", y)
	}
	if y.Outages != 2 || y.DownSeconds != 3600+1800 {
		t.Fatalf("outages = %d for %d s", y.Outages, y.DownSeconds)
	}
	if y.DevicesPeak != 14 {
		t.Fatalf("peak = %d", y.DevicesPeak)
	}
}

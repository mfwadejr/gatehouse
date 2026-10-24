package app

import (
	"strings"
	"testing"
	"time"

	"gatehouse/internal/netd"
)

func TestBaseDomain(t *testing.T) {
	for in, want := range map[string]string{"www.cdn.example.com.": "example.com", "a.b.bbc.co.uk": "bbc.co.uk", "localhost": "localhost", "x.y": "x.y"} {
		if got := baseDomain(in); got != want {
			t.Errorf("%s: %s want %s", in, got, want)
		}
	}
}

func TestLogFilters(t *testing.T) {
	web, dnsAddr := freeAddr(t), freeAddr(t)
	a, err := New(Config{DataDir: t.TempDir(), WebAddr: web, DNSAddrs: []string{dnsAddr}, Net: &fakeNet{}, FW: &fakeFW{}})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	now := time.Now().Unix()
	a.storeFW([]netd.FWEvent{
		{TS: now - 9, Verdict: "drop", Tag: "default:fwd", In: "br1", Out: "br0", Src: "192.168.20.5", Dst: "192.168.1.10", Proto: "tcp", DPort: 445},
		{TS: now - 8, Verdict: "allow", Tag: "internet:Home", In: "br0", Out: "eth0", Src: "192.168.1.20", Dst: "142.250.80.46", Proto: "tcp", DPort: 443},
		{TS: now - 7, Verdict: "allow", Tag: "internet:Home", In: "br0", Out: "eth0", Src: "192.168.1.21", Dst: "1.1.1.1", Proto: "udp", DPort: 53},
	})
	a.Logs.Exec(`UPDATE fw_events SET from_zone='Guests',to_zone='Home' WHERE dport=445`)
	a.Logs.Exec(`UPDATE fw_events SET from_zone='Home',to_zone='Internet' WHERE dport<>445`)
	n := func(q string) []LogItem {
		t.Helper()
		var out struct{ Items []LogItem }
		rr := newRec()
		a.logSearch(rr, newReq("/api/logs?"+q), nil)
		decode(t, rr, &out)
		return out.Items
	}
	if got := n("port=443"); len(got) != 1 || got[0].Dst != "142.250.80.46" || got[0].Service != "Web (HTTPS)" {
		t.Fatalf("port: %+v", got)
	}
	if got := n("service=DNS"); len(got) != 1 || got[0].Dst != "1.1.1.1" {
		t.Fatalf("service: %+v", got)
	}
	if got := n("verdict=blocked&kind=firewall"); len(got) != 1 || got[0].Port != 445 {
		t.Fatalf("blocked: %+v", got)
	}
	if got := n("device=192.168.1.20"); len(got) != 1 {
		t.Fatalf("device: %+v", got)
	}
	if got := n("direction=outbound"); len(got) != 2 {
		t.Fatalf("direction: %+v", got)
	}
	if got := n("direction=between"); len(got) != 1 || got[0].Port != 445 {
		t.Fatalf("between: %+v", got)
	}
	s := a.logSummary(fwFilter{Since: now - 100})
	if s == nil || len(s) == 0 {
		t.Fatal("empty summary")
	}
	// retention
	if u := a.logUsage(); u == nil {
		t.Fatal("no usage")
	}
	a.enforceLogCap()
	if got := n("port=443"); len(got) != 1 {
		t.Fatal("cap removed fresh rows")
	}
	if strings.HasPrefix(csvSafe("=cmd()"), "=") {
		t.Fatal("csv formula not neutralised")
	}
	if d := a.topDomains("1d"); d["approximate"] != true {
		t.Fatal("domains not marked approximate")
	}
}

func TestAppNames(t *testing.T) {
	web, dnsAddr := freeAddr(t), freeAddr(t)
	a, err := New(Config{DataDir: t.TempDir(), WebAddr: web, DNSAddrs: []string{dnsAddr}, Net: &fakeNet{}, FW: &fakeFW{}})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	now := time.Now().Unix()
	a.Logs.Exec(`INSERT INTO fw_events(ts,action,from_zone,from_addr,to_zone,to_addr,proto,dport,rule,sport,src_name,dst_name,reason) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		now-5, "allow", "Home", "192.168.1.20", "Internet", "142.250.1.1", "tcp", 443, "internet:Home", 5000, "TV", "r1---sn-x.googlevideo.com", "ok")
	a.Logs.Exec(`INSERT INTO fw_events(ts,action,from_zone,from_addr,to_zone,to_addr,proto,dport,rule,sport,src_name,dst_name,reason) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		now-4, "allow", "Home", "192.168.1.20", "Internet", "9.9.9.9", "tcp", 443, "internet:Home", 5001, "TV", "example.org", "ok")
	get := func(q string) []LogItem {
		var out struct{ Items []LogItem }
		rr := newRec()
		a.logSearch(rr, newReq("/api/logs?"+q), nil)
		decode(t, rr, &out)
		return out.Items
	}
	all := get("kind=firewall")
	if len(all) != 2 || all[1].App != "YouTube" || all[0].App != "" {
		t.Fatalf("app labels: %+v", all)
	}
	if got := get("app=YouTube"); len(got) != 1 || got[0].Dst != "142.250.1.1" {
		t.Fatalf("app filter: %+v", got)
	}
	if s := a.logSummary(fwFilter{Since: now - 100}); s["apps"] == nil {
		t.Fatal("no apps in the summary")
	}
	a.Config.SetSetting("apps.enabled", "0")
	if got := get("kind=firewall"); got[1].App != "" {
		t.Fatal("app names shown while switched off")
	}
	if r := a.topApps("1d"); r["enabled"] != false {
		t.Fatal("top apps while switched off")
	}
}

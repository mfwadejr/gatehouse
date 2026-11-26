package app

import (
	"net"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/miekg/dns"

	"nopewarden/internal/api"
	"nopewarden/internal/auth"
	"nopewarden/internal/netd"
)

func adminCtx() *api.Ctx { return &api.Ctx{User: &auth.User{Username: "admin", Role: "admin"}} }

func findRes(t *testing.T, a *App, q string) (items []LogItem, sentence string) {
	t.Helper()
	var out struct {
		Items    []LogItem
		Sentence string
	}
	rr := newRec()
	a.findTraffic(rr, newReq("/api/diag/find?"+q), adminCtx())
	decode(t, rr, &out)
	return out.Items, out.Sentence
}

func TestFindTraffic(t *testing.T) {
	a, _ := newSpeedApp(t)
	now := time.Now().Unix()
	a.storeFW([]netd.FWEvent{
		{TS: now - 30, Verdict: "allow", Tag: "rule:Home:Internet:Web", In: "br0", Out: "eth0", Src: "192.168.1.20", Dst: "142.250.80.46", Proto: "tcp", SPort: 51000, DPort: 443},
		{TS: now - 20, Verdict: "allow", Tag: "rule:Home:Internet:Web", In: "br0", Out: "eth0", Src: "192.168.1.20", Dst: "203.0.113.50", Proto: "tcp", SPort: 51002, DPort: 443},
		{TS: now - 10, Verdict: "drop", Tag: "default:fwd", In: "br0", Out: "eth0", Src: "192.168.1.20", Dst: "203.0.113.9", Proto: "tcp", SPort: 51001, DPort: 25},
		{TS: now - 5, Verdict: "allow", Tag: "rule:Home:Internet:Web", In: "br0", Out: "eth0", Src: "192.168.1.77", Dst: "142.250.80.46", Proto: "tcp", SPort: 51005, DPort: 443},
	})
	a.Logs.Exec(`UPDATE fw_events SET outcome=?,bytes_out=2600,bytes_in=120000,dur=20 WHERE to_addr='142.250.80.46' AND from_addr='192.168.1.20'`, outCompleted)
	a.Logs.Exec(`UPDATE fw_events SET outcome=?,bytes_out=60,bytes_in=0,dur=0 WHERE to_addr='203.0.113.50'`, outTimedOut)

	if got, s := findRes(t, a, "src=192.168.1.20"); len(got) != 3 || !strings.Contains(s, "Found 3 connections from 192.168.1.20") || !strings.Contains(s, "1 got no reply") || !strings.Contains(s, "1 completed") {
		t.Fatalf("by source: %d %q", len(got), s)
	}
	if got, _ := findRes(t, a, "src=192.168.1.20&result=noreply"); len(got) != 1 || got[0].Dst != "203.0.113.50" {
		t.Fatalf("no reply: %+v", got)
	}
	if got, _ := findRes(t, a, "result=blocked"); len(got) != 1 || got[0].Port != 25 {
		t.Fatalf("blocked: %+v", got)
	}
	if got, _ := findRes(t, a, "dst=203.0.113.0/24"); len(got) != 2 {
		t.Fatalf("a range: %+v", got)
	}
	if got, _ := findRes(t, a, "dst=142.250.80.46:443"); len(got) != 2 {
		t.Fatalf("host and port: %+v", got)
	}
	if got, _ := findRes(t, a, "src=192.168.1.2&dst="); len(got) != 0 {
		t.Fatalf("an address is exact, not a part: %+v", got)
	}
	if got, s := findRes(t, a, "src=10.9.9.9"); len(got) != 0 || !strings.Contains(s, "No connections from 10.9.9.9") {
		t.Fatalf("nothing: %q", s)
	}
	// Live: only what is newer than the mark.
	if got, _ := findRes(t, a, "since="+fmtI(now-12)); len(got) != 2 {
		t.Fatalf("since the mark: %+v", got)
	}
	// A change tried after a connection started marks it as open before the change.
	a.Logs.Exec(`UPDATE fw_events SET outcome=? WHERE from_addr='192.168.1.77'`, outOpen)
	a.fwLoaded.Store(now)
	got, _ := findRes(t, a, "src=192.168.1.77")
	if len(got) != 1 || !got[0].BeforeChange {
		t.Fatalf("open before the change: %+v", got)
	}
}

func fmtI(n int64) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func TestEndConnection(t *testing.T) {
	a, fn := newSpeedApp(t)
	rr := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/diag/open/end", strings.NewReader(`{"proto":"tcp","src":"192.168.1.20","sport":51000,"dst":"142.250.80.46","dport":443}`))
	a.endConnection(rr, r, adminCtx())
	if rr.Code != 200 || len(fn.ended) != 1 || fn.ended[0] != "tcp 192.168.1.20:51000>142.250.80.46:443" {
		t.Fatalf("%d %v %s", rr.Code, fn.ended, rr.Body.String())
	}
}

func TestDNSCheckVerdicts(t *testing.T) {
	a, _ := newSpeedApp(t)
	now := time.Now().Unix()
	ip := "192.168.1.20"
	run := func() map[string]any {
		var out map[string]any
		rr := newRec()
		a.dnsCheck(rr, newReq("/api/diag/dnscheck?ip="+ip), adminCtx())
		decode(t, rr, &out)
		return out
	}
	a.Logs.Exec(`INSERT INTO dns_queries(ts,client,client_name,qname,qtype,result) VALUES(?,?,?,?,?,?)`, now-10, ip, "Laptop", "example.com", "A", "forwarded")
	if v := run(); v["verdict"] != "Uses NopeWarden's DNS" {
		t.Fatalf("clean: %v", v)
	}
	// Connections that were stopped.
	a.storeFW([]netd.FWEvent{
		{TS: now - 300, Verdict: "drop", Tag: "rule:dns:dot", In: "br0", Out: "eth0", Src: ip, Dst: "1.1.1.1", Proto: "tcp", SPort: 40000, DPort: 853},
	})
	if v := run(); !strings.HasPrefix(v["verdict"].(string), "Tried to skip") {
		t.Fatalf("stopped: %v", v)
	}
	// Connections that got through to Google: named from the known list, with a rhythm.
	for i := 0; i < 5; i++ {
		a.storeFW([]netd.FWEvent{{TS: now - int64(3600-i*600), Verdict: "allow", Tag: "internet:Home", In: "br0", Out: "eth0", Src: ip, Dst: "8.8.8.8", Proto: "tcp", SPort: 41000 + i, DPort: 443}})
	}
	v := run()
	if !strings.HasPrefix(v["verdict"].(string), "Also talks") || !strings.Contains(v["detail"].(string), "Google Public DNS") || !strings.Contains(v["detail"].(string), "about every") {
		t.Fatalf("skipping: %v", v)
	}
	// An unknown server on 443 is named by the device's own lookup, and called "probably", never "confirmed".
	rec := func(name, addr string) {
		a.names.rememberFrom(ip, name, []dns.RR{&dns.A{Hdr: dns.RR_Header{Name: name + ".", Rrtype: dns.TypeA, Class: dns.ClassINET}, A: net.ParseIP(addr).To4()}})
	}
	rec("dns.nextdns.io", "203.0.113.77")
	a.storeFW([]netd.FWEvent{{TS: now - 20, Verdict: "allow", Tag: "internet:Home", In: "br0", Out: "eth0", Src: ip, Dst: "203.0.113.77", Proto: "tcp", SPort: 42000, DPort: 443}})
	a.storeFW([]netd.FWEvent{{TS: now - 19, Verdict: "allow", Tag: "internet:Home", In: "br0", Out: "eth0", Src: ip, Dst: "203.0.113.88", Proto: "tcp", SPort: 42001, DPort: 443}}) // an ordinary site
	v = run()
	var found, ordinary bool
	for _, s := range v["servers"].([]any) {
		m := s.(map[string]any)
		if m["addr"] == "203.0.113.77" && m["level"] == "probably" && m["name"] == "dns.nextdns.io" {
			found = true
		}
		if m["addr"] == "203.0.113.88" {
			ordinary = true
		}
	}
	if !found || ordinary {
		t.Fatalf("probably: found=%v ordinary=%v %v", found, ordinary, v["servers"])
	}
	// A device is told apart: another device did not look that name up.
	if a.names.getFor("192.168.1.99", "203.0.113.77") != "" || a.names.getFor(ip, "203.0.113.77") != "dns.nextdns.io" {
		t.Fatal("names are kept per device")
	}
}

func TestDNSNameWords(t *testing.T) {
	for n, want := range map[string]bool{"dns.google": true, "cloudflare-dns.com": true, "doh.opendns.com": true, "mozilla.cloudflare-dns.com": true, "dns.nextdns.io": true, "example.com": false, "netflix.com": false, "dnsimple.com": false} {
		if got := looksLikeDNSName(n); got != want {
			t.Errorf("%s: %v", n, got)
		}
	}
	if everyWords(2, 0, 100) != "" || everyWords(5, 0, 2400) != "about every 10 minutes" || everyWords(10, 0, 540) != "about every minute" {
		t.Fatal("rhythm words")
	}
}

func TestRecentLookups(t *testing.T) {
	a, _ := newSpeedApp(t)
	now := time.Now().Unix()
	for i := 0; i < 60; i++ {
		a.Logs.Exec(`INSERT INTO dns_queries(ts,client,client_name,qname,qtype,result,rule) VALUES(?,?,?,?,?,?,?)`, now-int64(i), "192.168.1.20", "Laptop", "site"+fmtI(int64(i))+".example", "A", "forwarded", "")
	}
	a.Logs.Exec(`INSERT INTO dns_queries(ts,client,client_name,qname,qtype,result,rule) VALUES(?,?,?,?,?,?,?)`, now, "192.168.1.20", "Laptop", "ads.example", "A", "blocked", "StevenBlack")
	var out struct {
		Items []struct {
			QName, Why, Result string
		}
		LastMinute int `json:"last_minute"`
	}
	rr := newRec()
	a.dnsRecent(rr, newReq("/api/dns/recent"), adminCtx())
	decode(t, rr, &out)
	if len(out.Items) != 50 || out.LastMinute != 61 {
		t.Fatalf("%d items, %d in the minute", len(out.Items), out.LastMinute)
	}
	var sawBlocked bool
	for _, it := range out.Items {
		if it.QName == "ads.example" && strings.Contains(it.Why, "StevenBlack") {
			sawBlocked = true
		}
		if it.Why == "" {
			t.Fatal("every row says why")
		}
	}
	if !sawBlocked {
		t.Fatal("the blocked lookup says which list blocked it")
	}
}

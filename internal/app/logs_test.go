package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gatehouse/internal/netd"
)

func TestFWReason(t *testing.T) {
	cases := []struct{ verdict, tag, from, to, want string }{
		{"allow", "internet:Home", "Home", "Internet", `"Home" network is allowed to use the internet`},
		{"drop", "rule:Guests:Home:No guests", "Guests", "Home", `Blocked by your rule "No guests" (Guests to Home)`},
		{"allow", "rule:Home:Internet:Games", "Home", "Internet", `Allowed by your rule "Games"`},
		{"drop", "default:fwd", "Guests", "Home", `no rule allows "Guests" to reach "Home"`},
		{"drop", "default:in", "Internet", "Router", `connect to the router itself`},
		{"drop", "killswitch:Mullvad", "Kids", "Internet", `kill switch for VPN "Mullvad"`},
	}
	for _, c := range cases {
		if got := fwReason(c.verdict, c.tag, c.from, c.to); !strings.Contains(got, c.want) {
			t.Errorf("%s: %q does not contain %q", c.tag, got, c.want)
		}
	}
}

func TestNetdParsesKernelLog(t *testing.T) {
	rec := "4,1234,56789,-;gh|drop|rule:Guests:Home:No guests IN=br1 OUT=br0 MAC=aa SRC=192.168.20.5 DST=192.168.1.10 LEN=60 TOS=0x00 PROTO=TCP SPT=51000 DPT=445 WINDOW=64240"
	ev, ok := netd.ParseKmsg(rec, time.Unix(1000, 0))
	if !ok || ev.Verdict != "drop" || ev.Tag != "rule:Guests:Home:No guests" || ev.Src != "192.168.20.5" || ev.DPort != 445 || ev.Proto != "tcp" || ev.In != "br1" {
		t.Fatalf("parsed: %+v %v", ev, ok)
	}
	if _, ok := netd.ParseKmsg("6,1,2,-;usb 1-1: new device", time.Now()); ok {
		t.Fatal("unrelated kernel line accepted")
	}
}

func TestUnifiedLog(t *testing.T) {
	web, dnsAddr := freeAddr(t), freeAddr(t)
	fn := &fakeNet{}
	a, err := New(Config{DataDir: t.TempDir(), WebAddr: web, DNSAddrs: []string{dnsAddr}, Net: fn, FW: &fakeFW{}})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	now := time.Now().Unix()
	a.storeFW([]netd.FWEvent{
		{TS: now - 5, Verdict: "drop", Tag: "default:fwd", In: "br1", Out: "br0", Src: "192.168.20.5", Dst: "192.168.1.10", Proto: "tcp", DPort: 445},
		{TS: now - 3, Verdict: "allow", Tag: "internet:Home", In: "br0", Out: "eth0", Src: "192.168.1.20", Dst: "142.250.80.46", Proto: "tcp", DPort: 443},
	})
	a.Logs.Exec(`INSERT INTO events(ts,area,severity,actor,title,detail) VALUES(?,?,?,?,?,?)`, now-1, "vpn", "warning", "system", "VPN Mullvad is down", "No answer")
	res := func(query string) []LogItem {
		t.Helper()
		var out struct{ Items []LogItem }
		rr := newRec()
		a.logSearch(rr, newReq(query), nil)
		decode(t, rr, &out)
		return out.Items
	}
	if got := res("/api/logs?q=445"); len(got) != 1 || got[0].Verdict != "blocked" || !strings.Contains(got[0].Why, "no rule allows") {
		t.Fatalf("port search: %+v", got)
	}
	if got := res("/api/logs?verdict=allowed&kind=firewall"); len(got) != 1 || got[0].What != "TCP to 142.250.80.46:443" {
		t.Fatalf("allowed: %+v", got)
	}
	if got := res("/api/logs?q=mullvad"); len(got) != 1 || got[0].Kind != "event" {
		t.Fatalf("event search: %+v", got)
	}
	if got := res("/api/logs"); len(got) != 3 || got[0].Kind != "event" {
		t.Fatalf("merged order: %+v", got)
	}
}

func newRec() *httptest.ResponseRecorder { return httptest.NewRecorder() }
func newReq(u string) *http.Request      { return httptest.NewRequest("GET", u, nil) }
func decode(t *testing.T, r *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.Unmarshal(r.Body.Bytes(), v); err != nil {
		t.Fatalf("%v: %s", err, r.Body.String())
	}
}

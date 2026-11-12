package app

import (
	"fmt"
	"net/netip"
	"testing"
	"time"

	"nopewarden/internal/flowexp"
	"nopewarden/internal/netd"
)

const ctLine = "ipv4     2 tcp      6 %s %s src=192.168.1.20 dst=142.250.80.46 sport=51000 dport=443 packets=%d bytes=%d %ssrc=142.250.80.46 dst=203.0.113.5 sport=443 dport=51000 packets=%d bytes=%d mark=0 use=1\n"

func ctText(timeout, state string, op, ob int, unrep string, rp, rb int) string {
	return fmt.Sprintf(ctLine, timeout, state, op, ob, unrep, rp, rb)
}

// The parser keeps the kernel's TCP state and the "nothing came back" flag.
func TestConntrackStateAndUnreplied(t *testing.T) {
	got := flowexp.ParseConntrack(ctText("431000", "ESTABLISHED", 10, 2000, "", 8, 90000) + ctText("100", "SYN_SENT", 1, 60, "[UNREPLIED] ", 0, 0))
	if len(got) != 2 {
		t.Fatalf("parsed %d", len(got))
	}
	if got[0].State != "ESTABLISHED" || got[0].Unreplied || got[0].OBytes != 2000 || got[0].RBytes != 90000 {
		t.Fatalf("%+v", got[0])
	}
	if got[1].State != "SYN_SENT" || !got[1].Unreplied {
		t.Fatalf("%+v", got[1])
	}
}

func newFollowed(ts int64) (*connTrack, connUpdate) {
	var ct connTrack
	ct.add(netd.FWEvent{TS: ts, Verdict: "allow", Src: "192.168.1.20", Dst: "142.250.80.46", Proto: "tcp", SPort: 51000, DPort: 443}, 7)
	return &ct, connUpdate{}
}

func conns(t *testing.T, text string) []flowexp.Conn {
	t.Helper()
	c := flowexp.ParseConntrack(text)
	if len(c) == 0 {
		t.Fatal("nothing parsed")
	}
	return c
}

// A connection that is open, then closes normally: still open with running totals, then completed.
func TestConnectionCompletes(t *testing.T) {
	now := time.Unix(1000, 0)
	ct, _ := newFollowed(now.Unix() - 5)
	u := ct.step(now, conns(t, ctText("400000", "ESTABLISHED", 10, 2000, "", 8, 90000)))
	if len(u) != 1 || u[0].Outcome != outOpen || u[0].BytesOut != 2000 || u[0].BytesIn != 90000 || u[0].ID != 7 {
		t.Fatalf("open: %+v", u)
	}
	later := now.Add(20 * time.Second)
	u = ct.step(later, conns(t, ctText("100", "TIME_WAIT", 14, 2600, "", 12, 120000)))
	if len(u) != 1 || u[0].Outcome != outCompleted || u[0].BytesOut != 2600 || u[0].BytesIn != 120000 || u[0].Dur != 25 {
		t.Fatalf("done: %+v", u)
	}
	if ct.open() != 0 {
		t.Fatal("a finished connection is no longer followed")
	}
}

// A connection that was reset, and one nobody answered.
func TestConnectionResetAndTimedOut(t *testing.T) {
	now := time.Unix(1000, 0)
	ct, _ := newFollowed(now.Unix() - 2)
	u := ct.step(now, conns(t, ctText("10", "CLOSE", 5, 400, "", 3, 200)))
	if len(u) != 1 || u[0].Outcome != outReset {
		t.Fatalf("reset: %+v", u)
	}
	ct, _ = newFollowed(now.Unix() - 2)
	ct.step(now, conns(t, ctText("100", "SYN_SENT", 1, 60, "[UNREPLIED] ", 0, 0)))
	u = ct.step(now.Add(130*time.Second), []flowexp.Conn{{Proto: 17, Src: netip.MustParseAddr("10.9.9.9"), Dst: netip.MustParseAddr("10.9.9.8")}})
	if len(u) != 1 || u[0].Outcome != outTimedOut {
		t.Fatalf("timed out: %+v", u)
	}
}

// A logged connection that never shows up in the list is dropped without a guess.
func TestConnectionNeverSeenIsDropped(t *testing.T) {
	now := time.Unix(1000, 0)
	ct, _ := newFollowed(now.Unix() - 100)
	if u := ct.step(now, nil); len(u) != 0 || ct.open() != 0 {
		t.Fatalf("%+v open=%d", u, ct.open())
	}
}

func TestRuleLabel(t *testing.T) {
	for in, want := range map[string]string{
		"rule:Home:Internet:No games": "No games", "internet:Home": "Internet access for Home", "default:in": "Default: nothing allows this to the router",
		"protected: management console": "management console", "forward": "Port forward"} {
		if got := ruleLabel(in); got != want {
			t.Errorf("%s: %q want %q", in, got, want)
		}
	}
}

// What is stored comes back on the log row and in the spreadsheet, one column for each detail.
func TestLogDetailAndExport(t *testing.T) {
	a, _ := newSpeedApp(t)
	now := time.Now().Unix()
	a.storeFW([]netd.FWEvent{
		{TS: now - 5, Verdict: "allow", Tag: "rule:Home:Internet:Web", In: "br0", Out: "eth0", Src: "192.168.1.20", Dst: "142.250.80.46", Proto: "tcp", SPort: 51000, DPort: 443},
		{TS: now - 4, Verdict: "drop", Tag: "default:fwd", In: "br0", Out: "eth0", Src: "192.168.1.20", Dst: "203.0.113.9", Proto: "tcp", SPort: 51001, DPort: 25},
	})
	a.Logs.Exec(`UPDATE fw_events SET outcome=?,bytes_out=2600,bytes_in=120000,dur=20 WHERE dport=443`, outCompleted)
	var out struct{ Items []LogItem }
	rr := newRec()
	a.logSearch(rr, newReq("/api/logs?kind=firewall"), nil)
	decode(t, rr, &out)
	var ok, blocked *LogItem
	for i := range out.Items {
		if out.Items[i].Port == 443 {
			ok = &out.Items[i]
		} else {
			blocked = &out.Items[i]
		}
	}
	if ok == nil || blocked == nil {
		t.Fatalf("%+v", out.Items)
	}
	if ok.Outcome != outCompleted || ok.BytesOut == nil || *ok.BytesOut != 2600 || *ok.BytesIn != 120000 || ok.DurS == nil || *ok.DurS != 20 || ok.RuleName != "Web" {
		t.Fatalf("allowed row: %+v", ok)
	}
	if blocked.Outcome != outBlocked || blocked.BytesOut != nil || blocked.DurS != nil {
		t.Fatalf("blocked row: %+v", blocked)
	}
	// CSV: same number of fields in every row as in the header, blanks where it does not apply.
	row := logCSVRow(*ok)
	if len(row) != len(logCSVHeader) {
		t.Fatalf("%d fields for %d columns", len(row), len(logCSVHeader))
	}
	dns := logCSVRow(LogItem{Kind: "dns", TS: now, Verdict: "allowed", Who: "tv", Addr: "192.168.1.30", What: "example.com", Detail: "AAAA lookup", RCode: "NXDOMAIN", AnswerUS: 12500, Server: "1.1.1.1"})
	if len(dns) != len(logCSVHeader) || dns[14] != "example.com" || dns[15] != "AAAA" || dns[16] != "NXDOMAIN" || dns[17] != "12.5" || dns[18] != "1.1.1.1" || dns[19] != "" {
		t.Fatalf("dns row: %q", dns)
	}
}

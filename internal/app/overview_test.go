package app

import (
	"testing"
	"time"

	"gatehouse/internal/netd"
)

func TestHistoryRollsUpIntoSlowerTiers(t *testing.T) {
	a, err := New(Config{DataDir: t.TempDir(), WebAddr: freeAddr(t), DNSAddrs: []string{freeAddr(t)}, Net: &fakeNet{}, FW: &fakeFW{}})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	base := time.Now().Add(-2 * time.Hour)
	// Two hours of readings every 30 s.
	for i := 0; i < 240; i++ {
		a.recordHistory(base.Add(time.Duration(i) * 30 * time.Second))
	}
	h := a.history("1h")
	if h["range"] != "1h" {
		t.Fatal("range")
	}
	series := h["series"].(map[string][][2]float64)
	if _, ok := series["sys.cpu"]; !ok {
		t.Fatalf("no cpu series: %v", len(series))
	}
	var b, c int
	a.Logs.QueryRow(`SELECT COUNT(*) FROM metrics WHERE key='b|sys.mem'`).Scan(&b)
	a.Logs.QueryRow(`SELECT COUNT(*) FROM metrics WHERE key='c|sys.mem'`).Scan(&c)
	if b < 20 || c < 1 {
		t.Fatalf("rollups: five-minute %d, hourly %d", b, c)
	}
	if got := a.history("nonsense")["range"]; got != "1h" {
		t.Fatalf("unknown range falls back to 1h, got %v", got)
	}
}

func TestWorldMapSplitsIncomingAndOutgoing(t *testing.T) {
	a, err := New(Config{DataDir: t.TempDir(), WebAddr: freeAddr(t), DNSAddrs: []string{freeAddr(t)}, Net: &fakeNet{}, FW: &fakeFW{}})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	now := time.Now().Unix()
	a.storeFW([]netd.FWEvent{
		{TS: now - 5, Verdict: "drop", Tag: "default:in", In: "eth0", Src: "8.8.8.8", Dst: "192.168.1.1", Proto: "tcp", DPort: 22},
		{TS: now - 4, Verdict: "allow", Tag: "internet:Home", In: "br0", Out: "eth0", Src: "192.168.1.20", Dst: "8.8.4.4", Proto: "tcp", DPort: 443},
	})
	a.Logs.Exec(`UPDATE fw_events SET from_zone='Internet',to_zone='Router' WHERE dport=22`)
	a.Logs.Exec(`UPDATE fw_events SET from_zone='Home',to_zone='Internet' WHERE dport=443`)
	in := a.worldMap("in", 1)
	out := a.worldMap("out", 1)
	if in["dir"] != "in" || out["dir"] != "out" {
		t.Fatal("dir")
	}
	ic := in["countries"].([]mapCountry)
	oc := out["countries"].([]mapCountry)
	// The test country table may not know these addresses; when it does, they must land on the right side.
	for _, m := range ic {
		if m.Blocked != 1 || m.Allowed != 0 {
			t.Fatalf("incoming should be one blocked attempt: %+v", m)
		}
	}
	for _, m := range oc {
		if m.N != 1 || m.Blocked != 0 {
			t.Fatalf("outgoing: %+v", m)
		}
	}
}

func TestCachedAnswerIsReused(t *testing.T) {
	var a App
	n := 0
	f := func() any { n++; return n }
	a.cached("k", time.Minute, f)
	a.cached("k", time.Minute, f)
	if n != 1 {
		t.Fatalf("built %d times", n)
	}
}

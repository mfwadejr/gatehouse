package app

import (
	"math"
	"strings"
	"testing"
	"time"
)

func TestPercentileAndDailyStep(t *testing.T) {
	v := []float64{10, 20, 30, 40, 50}
	if p := percentile(v, 0.25); p != 20 {
		t.Fatalf("25th: %v", p)
	}
	if p := percentile([]float64{10, 20}, 0.25); math.Abs(p-12.5) > 1e-9 {
		t.Fatalf("interpolated: %v", p)
	}
	if percentile(nil, 0.25) != 0 || percentile([]float64{7}, 0.25) != 7 {
		t.Fatal("edge cases")
	}
	// Up at most 10 % a day, down at most 20 %, nothing for a change under 2 %, no move past the target.
	if got := stepToward(100, 200, 24*time.Hour); math.Abs(got-110) > 1e-9 {
		t.Fatalf("up a day: %v", got)
	}
	if got := stepToward(100, 50, 24*time.Hour); math.Abs(got-80) > 1e-9 {
		t.Fatalf("down a day: %v", got)
	}
	if got := stepToward(100, 101, 24*time.Hour); got != 100 {
		t.Fatalf("deadband: %v", got)
	}
	if got := stepToward(100, 105, 24*time.Hour); got != 105 {
		t.Fatalf("must not overshoot: %v", got)
	}
	if got := stepToward(100, 200, time.Hour); got > 100.5 {
		t.Fatalf("an hour is a small step: %v", got)
	}
}

func addTest(t *testing.T, a *App, link string, ago time.Duration, down, up float64, busy, shaped, quick int, errText string) {
	t.Helper()
	if _, err := a.Logs.Exec(`INSERT INTO speedtests(ts,link,source,quick,down_bps,up_bps,error,busy,shaped) VALUES(?,?,?,?,?,?,?,?,?)`,
		time.Now().Add(-ago).Unix(), link, "auto", quick, down*1e6, up*1e6, errText, busy, shaped); err != nil {
		t.Fatal(err)
	}
}

func TestMeasuredSpeedUsesOnlyGoodTests(t *testing.T) {
	a, _ := newSpeedApp(t)
	// Ten good tests over three days: downloads 300 to 390.
	for i := 0; i < 10; i++ {
		addTest(t, a, "Fibre", time.Duration(i)*7*time.Hour, float64(300+10*i), 30, 0, 0, 0, "")
	}
	// Tests that must not count.
	addTest(t, a, "Fibre", time.Hour, 20, 2, 1, 0, 0, "")       // line was busy
	addTest(t, a, "Fibre", 2*time.Hour, 20, 2, 0, 1, 0, "")     // went through the queue
	addTest(t, a, "Fibre", 3*time.Hour, 20, 2, 0, 0, 1, "")     // short test
	addTest(t, a, "Fibre", 4*time.Hour, 0, 0, 0, 0, 0, "boom")  // failed
	addTest(t, a, "Fibre", 10*24*time.Hour, 20, 2, 0, 0, 0, "") // too old
	a.Logs.Exec(`INSERT INTO outages(start,end) VALUES(?,?)`, time.Now().Add(-5*time.Hour).Unix(), time.Now().Add(-4*time.Hour-30*time.Minute).Unix())
	addTest(t, a, "Fibre", 4*time.Hour+45*time.Minute, 20, 2, 0, 0, 0, "") // during an outage
	m := a.sqmMeasure("Fibre", time.Now())
	if m.Tests != 10 {
		t.Fatalf("only the 10 good tests count, got %d", m.Tests)
	}
	want := percentile([]float64{300, 310, 320, 330, 340, 350, 360, 370, 380, 390}, 0.25) * sqmTrim
	if math.Abs(m.Down-want) > 0.01 {
		t.Fatalf("typical %v want %v", m.Down, want)
	}
	if m.Learning {
		t.Fatalf("10 tests over 2 days or more is enough: %+v", m)
	}
}

func TestFollowingMeasurementsLearningFixedAndPromised(t *testing.T) {
	c, fn, _ := siteBox(t, "192.168.1.1/24")
	a := c.app
	if a == nil {
		t.Skip("no app")
	}
	a.Config.Exec(`DELETE FROM wan_links`)
	a.Config.Exec(`INSERT INTO wan_links(name,iface,type,enabled,priority) VALUES('Home','eth0','dhcp',1,1)`)
	c.call("PUT", "/api/speedtest", map[string]any{"schedule": false, "hour": 3, "links": map[string]any{"Home": map[string]any{"down_mbps": 500, "up_mbps": 50, "metered": false}}})
	last := func() (int, int) {
		fn.mu.Lock()
		defer fn.mu.Unlock()
		p := fn.sqmPlans[len(fn.sqmPlans)-1]
		return p.Links[0].DownKbit, p.Links[0].UpKbit
	}
	// Nothing measured yet: the promised speed less 5 %, as before.
	if rc, _, raw := c.call("PUT", "/api/sqm", map[string]any{"links": map[string]bool{"Home": true}}); rc != 200 {
		t.Fatalf("%d %s", rc, raw)
	}
	if d, u := last(); d != 475000 || u != 47500 {
		t.Fatalf("promised: %d %d", d, u)
	}
	// Two good tests: still learning, so 85 % of the best.
	addTest(t, a, "Home", 5*time.Hour, 360, 40, 0, 0, 0, "")
	addTest(t, a, "Home", 2*time.Hour, 369, 41, 0, 0, 0, "")
	c.call("PUT", "/api/sqm", map[string]any{"mode": map[string]string{"Home": "follow"}, "links": map[string]bool{"Home": true}, "fixed": map[string]any{}})
	a.sqmRetune(time.Now(), true)
	d, _, why := a.sqmWanted("Home", time.Now())
	if why != "learning" || math.Abs(d-369*0.85) > 0.01 {
		t.Fatalf("learning: %v %q", d, why)
	}
	// Enough tests over enough days: the typical speed, below the promise.
	for i := 0; i < 10; i++ {
		addTest(t, a, "Home", time.Duration(i+6)*9*time.Hour, 369, 41, 0, 0, 0, "")
	}
	d, u, why := a.sqmWanted("Home", time.Now())
	if why != "measured" || d >= 475 || d < 340 || u > 47.5 {
		t.Fatalf("measured: %v %v %q", d, u, why)
	}
	// The queue moves toward it a little at a time, never past the daily limit.
	sh := a.sqmShapes()
	s := sh["Home"]
	s.Checked = time.Now().Add(-24 * time.Hour).Unix()
	sh["Home"] = s
	a.saveSqmShapes(sh)
	before := a.sqmShapes()["Home"].DownMbps
	a.sqmRetune(time.Now(), true)
	after := a.sqmShapes()["Home"].DownMbps
	if after < before*0.79 || after > before+0.001 {
		t.Fatalf("one day moves at most 20 %% down: %v to %v", before, after)
	}
	// Fixed numbers are used exactly.
	if rc, _, raw := c.call("PUT", "/api/sqm", map[string]any{"mode": map[string]string{"Home": "fixed"}, "fixed": map[string]any{"Home": map[string]any{"down_mbps": 300, "up_mbps": 30}}}); rc != 200 {
		t.Fatalf("%d %s", rc, raw)
	}
	if dk, uk := last(); dk != 300000 || uk != 30000 {
		t.Fatalf("fixed: %d %d", dk, uk)
	}
}

func TestAutomaticTestsWaitForAQuietLine(t *testing.T) {
	a, fn := newSpeedApp(t)
	a.setSpeedConfigForTest(speedConfig{Hour: 3, Links: map[string]speedLink{"Fibre": {DownMbps: 500, UpMbps: 50, Metered: new(bool)}}})
	a.Config.SetSetting("sqm.config", `{"links":{"Fibre":true},"auto":true}`)
	count := func() int { fn.mu.Lock(); defer fn.mu.Unlock(); return len(fn.speedCalls) }
	// A busy line: nothing runs.
	busy := make([]float64, 60)
	for i := range busy {
		busy[i] = 400e6
	}
	a.traffic.series = map[string]*trafficSeries{"Fibre": {Dev: "eth0", Rx: busy, Tx: make([]float64, 60)}}
	a.sqmAutoTests(t.Context(), time.Now())
	if count() != 0 {
		t.Fatal("a test started on a busy line")
	}
	// Quiet: it runs once, and is recorded as automatic and not busy.
	a.traffic.series["Fibre"].Rx = make([]float64, 60)
	a.sqmAutoTests(t.Context(), time.Now())
	if count() != 1 {
		t.Fatalf("calls: %d", count())
	}
	var src string
	var busyFlag int
	a.Logs.QueryRow(`SELECT source,busy FROM speedtests ORDER BY id DESC LIMIT 1`).Scan(&src, &busyFlag)
	if src != "auto" || busyFlag != 0 {
		t.Fatalf("row: %q %d", src, busyFlag)
	}
	// Not again for four hours.
	a.sqmAutoTests(t.Context(), time.Now().Add(3*time.Hour))
	if count() != 1 {
		t.Fatal("ran again too soon")
	}
	a.sqmAutoTests(t.Context(), time.Now().Add(4*time.Hour+time.Minute))
	if count() != 2 {
		t.Fatalf("should run after four hours, calls %d", count())
	}
	// Metered lines are left alone.
	yes := true
	a.setSpeedConfigForTest(speedConfig{Hour: 3, Links: map[string]speedLink{"Fibre": {Metered: &yes}}})
	a.sqmAutoTests(t.Context(), time.Now().Add(9*time.Hour))
	if count() != 2 {
		t.Fatal("a metered line must not be tested automatically")
	}
}

func TestBeforeAndAfterWords(t *testing.T) {
	better := checkWords(sqmCheckSide{LoadedMS: 180, DownMbps: 400, UpMbps: 40}, sqmCheckSide{LoadedMS: 30, DownMbps: 370, UpMbps: 38})
	same := checkWords(sqmCheckSide{LoadedMS: 40, DownMbps: 400, UpMbps: 40}, sqmCheckSide{LoadedMS: 38, DownMbps: 370, UpMbps: 38})
	worse := checkWords(sqmCheckSide{LoadedMS: 40, DownMbps: 400, UpMbps: 40}, sqmCheckSide{LoadedMS: 120, DownMbps: 370, UpMbps: 38})
	none := checkWords(sqmCheckSide{}, sqmCheckSide{LoadedMS: 30})
	for k, w := range map[string]string{"Better": better, "About the same": same, "Worse": worse, "could not be measured": none} {
		if !strings.Contains(w, k) {
			t.Fatalf("%q missing from %q", k, w)
		}
	}
}

func TestBeforeAndAfterCheckLiftsAndRestoresTheQueue(t *testing.T) {
	c, fn, _ := siteBox(t, "192.168.1.1/24")
	a := c.app
	if a == nil {
		t.Skip("no app")
	}
	a.Config.Exec(`DELETE FROM wan_links`)
	a.Config.Exec(`INSERT INTO wan_links(name,iface,type,enabled,priority) VALUES('Home','eth0','dhcp',1,1)`)
	c.call("PUT", "/api/speedtest", map[string]any{"schedule": false, "hour": 3, "links": map[string]any{"Home": map[string]any{"down_mbps": 500, "up_mbps": 50, "metered": false}}})
	if rc, _, _ := c.call("POST", "/api/sqm/check", map[string]any{"link": "Home"}); rc == 200 {
		t.Fatal("a check with Keep delay low off must be refused")
	}
	c.call("PUT", "/api/sqm", map[string]any{"links": map[string]bool{"Home": true}})
	if rc, _, raw := c.call("POST", "/api/sqm/check", map[string]any{"link": "Home"}); rc != 200 {
		t.Fatalf("%d %s", rc, raw)
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		_, m, _ := c.call("GET", "/api/sqm", nil)
		if ck, _ := m["check"].(map[string]any); ck != nil && ck["running"] == false {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	var res sqmCheckResult
	if raw := a.setting("sqm.check.Home", ""); raw == "" {
		t.Fatal("no result was kept")
	}
	_ = res
	fn.mu.Lock()
	plans := fn.sqmPlans
	fn.mu.Unlock()
	if len(plans[len(plans)-1].Links) != 1 {
		t.Fatalf("the queue must be back after the check: %+v", plans[len(plans)-1])
	}
	lifted := false
	for _, p := range plans {
		if len(p.Links) == 0 && len(p.Known) > 0 {
			lifted = true
		}
	}
	if !lifted {
		t.Fatal("the queue was never lifted for the first measurement")
	}
	var shaped, n int
	a.Logs.QueryRow(`SELECT COALESCE(SUM(shaped),0),COUNT(*) FROM speedtests WHERE source LIKE 'check-%'`).Scan(&shaped, &n)
	if n != 2 || shaped != 1 {
		t.Fatalf("one unshaped and one shaped test expected, got %d tests, %d shaped", n, shaped)
	}
}

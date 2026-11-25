package app

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"nopewarden/internal/netd"
)

func upsOK() netd.UPSStatus {
	return netd.UPSStatus{Tool: true, Answering: true, Charge: 100, Runtime: 40 * 60, Load: 30, Raw: "OL"}
}

func powerApp(t *testing.T) (*App, *fakeNet) {
	t.Helper()
	a := newScopeApp(t)
	cfg := a.powerConfig()
	cfg.On = true
	cfg.Delay = 1
	a.savePowerConfig(cfg)
	old := powerSecond
	powerSecond = 20 * time.Millisecond
	t.Cleanup(func() { powerSecond = old })
	return a, a.Net.(*fakeNet)
}

func evCount(a *App, like string) int {
	var n int
	a.Logs.QueryRow(`SELECT COUNT(*) FROM events WHERE title LIKE ?`, like).Scan(&n)
	return n
}

func TestParseUPSReading(t *testing.T) {
	r := netd.ParseUPS("battery.charge: 87\nbattery.runtime: 1320\nups.load: 31\ninput.voltage: 229.0\nups.status: OB DISCHRG RB\ndevice.mfr: American Power Conversion\ndevice.model: Back-UPS ES 700G\n")
	if !r.OnBattery || !r.Replace || r.Charge != 87 || r.Runtime != 1320 || r.Load != 31 || r.Model != "Back-UPS ES 700G" || !r.Answering {
		t.Fatalf("%+v", r)
	}
}

func TestPowerCutLowBatteryShutdownAndBack(t *testing.T) {
	a, fn := powerApp(t)
	now := time.Now()
	a.powerStep(upsOK(), now)
	if evCount(a, "Power cut") != 0 {
		t.Fatal("mains power is not an event")
	}
	cut := upsOK()
	cut.OnBattery, cut.Raw, cut.Charge, cut.Runtime = true, "OB DISCHRG", 80, 20*60
	a.powerStep(cut, now.Add(time.Minute))
	if evCount(a, "Power cut") != 1 {
		t.Fatal("power cut not logged")
	}
	if l := a.powerViewNow().Lines; len(l) != 1 || l[0].ID != "cut" {
		t.Fatalf("overview lines %v", l)
	}
	a.powerStep(cut, now.Add(time.Minute+15*time.Second))
	if evCount(a, "Power cut") != 1 {
		t.Fatal("a cut is told once")
	}
	// Battery low (5 minutes left) starts the countdown, and the machine is shut down with the UPS told to cut output.
	low := cut
	low.Charge, low.Runtime = 30, 4*60
	a.powerStep(low, now.Add(10*time.Minute))
	if evCount(a, "UPS battery low") != 1 {
		t.Fatal("low battery not logged")
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		fn.mu.Lock()
		n := len(fn.shut)
		fn.mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(fn.shut) != 1 || !fn.shut[0] {
		t.Fatalf("shutdown calls %v", fn.shut)
	}
}

func TestPowerBackBeforeTheShutdownCancelsIt(t *testing.T) {
	a, fn := powerApp(t)
	cfg := a.powerConfig()
	cfg.Delay = 60
	a.savePowerConfig(cfg)
	now := time.Now()
	cut := upsOK()
	cut.OnBattery, cut.Raw, cut.LowBattery, cut.Charge, cut.Runtime = true, "OB LB", true, 10, 100
	a.powerStep(upsOK(), now)
	a.powerStep(cut, now.Add(time.Minute))
	if a.powerViewNow().ShutAt == 0 {
		t.Fatal("a shutdown should be waiting")
	}
	a.powerStep(upsOK(), now.Add(12*time.Minute))
	if a.powerViewNow().ShutAt != 0 {
		t.Fatal("power back should cancel it")
	}
	var detail string
	a.Logs.QueryRow(`SELECT detail FROM events WHERE title='Power is back'`).Scan(&detail)
	if !strings.Contains(detail, "Power was out for 11 minutes") {
		t.Fatalf("detail %q", detail)
	}
	if len(a.powerViewNow().Lines) != 0 {
		t.Fatal("the Overview lines should clear when the power is back")
	}
	time.Sleep(50 * time.Millisecond)
	if len(fn.shut) != 0 {
		t.Fatal("it must not shut down")
	}
}

func TestEachEventHonoursItsOwnChoices(t *testing.T) {
	a, _ := powerApp(t)
	cfg := a.powerConfig()
	cfg.Events["cut"] = powerAct{Log: false, Alert: false, Overview: false}
	cfg.Events["low"] = powerAct{Log: true}
	a.savePowerConfig(cfg)
	now := time.Now()
	cut := upsOK()
	cut.OnBattery, cut.Raw = true, "OB"
	a.powerStep(upsOK(), now)
	a.powerStep(cut, now.Add(time.Minute))
	if evCount(a, "Power cut") != 0 || len(a.powerViewNow().Lines) != 0 {
		t.Fatal("everything was switched off for a power cut")
	}
	cut.LowBattery = true
	a.powerStep(cut, now.Add(2*time.Minute))
	if evCount(a, "UPS battery low") != 1 || a.powerViewNow().ShutAt != 0 {
		t.Fatal("low: log only, no shutdown")
	}
}

func TestBatteryNeedsReplacingIsRepeatedDailyAndNotRespondingIsTold(t *testing.T) {
	a, _ := powerApp(t)
	now := time.Now()
	rb := upsOK()
	rb.Replace, rb.Raw = true, "OL RB"
	a.powerStep(rb, now)
	a.powerStep(rb, now.Add(time.Hour))
	if evCount(a, "UPS battery needs replacing") != 1 {
		t.Fatal("told once the first day")
	}
	if l := a.powerViewNow().Lines; len(l) != 1 || l[0].ID != "replace" {
		t.Fatalf("lines %v", l)
	}
	a.powerStep(rb, now.Add(25*time.Hour))
	if evCount(a, "UPS battery needs replacing") != 2 {
		t.Fatal("a reminder after a day")
	}
	// Not answering for four readings in a row.
	dead := netd.UPSStatus{Tool: true, Charge: -1, Runtime: -1, Load: -1}
	for i := 0; i < 3; i++ {
		a.powerStep(dead, now.Add(time.Duration(i)*15*time.Second))
	}
	if evCount(a, "UPS not responding") != 0 {
		t.Fatal("three misses is not enough")
	}
	a.powerStep(dead, now.Add(time.Minute))
	a.powerStep(dead, now.Add(75*time.Second))
	if evCount(a, "UPS not responding") != 1 {
		t.Fatal("not responding should be told once")
	}
}

func TestRestartDuringACutReportsPowerBack(t *testing.T) {
	a, _ := powerApp(t)
	a.Config.SetSetting("power.outage", fmtInt(time.Now().Add(-30*time.Minute).Unix()))
	a.powerStep(upsOK(), time.Now())
	var detail string
	a.Logs.QueryRow(`SELECT detail FROM events WHERE title='Power is back'`).Scan(&detail)
	if !strings.Contains(detail, "30 minutes") {
		t.Fatalf("detail %q", detail)
	}
}

func fmtInt(n int64) string { return strconv.FormatInt(n, 10) }

func TestTestPowerCutIsMarkedAndNeverShutsDown(t *testing.T) {
	a, fn := powerApp(t)
	old := powerTestPause
	powerTestPause = 10 * time.Millisecond
	defer func() { powerTestPause = old }()
	if err := a.powerTest(false); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && evCount(a, "Test: Power is back") == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if evCount(a, "Test: Power cut") != 1 || evCount(a, "Test: UPS battery low") != 1 || evCount(a, "Test: Power is back") != 1 {
		t.Fatal("the test should log all three steps, marked as a test")
	}
	time.Sleep(100 * time.Millisecond)
	if len(fn.shut) != 0 || a.powerViewNow().ShutAt != 0 {
		t.Fatal("a test must not shut the machine down")
	}
	var d string
	a.Logs.QueryRow(`SELECT detail FROM events WHERE title='Test: UPS battery low'`).Scan(&d)
	if !strings.Contains(d, "would have shut down in") {
		t.Fatalf("detail %q", d)
	}
	var n int
	a.Logs.QueryRow(`SELECT COUNT(*) FROM power_outages`).Scan(&n)
	if n != 0 {
		t.Fatal("a test is not a real outage")
	}
}

func TestPowerLineInTheDailySummary(t *testing.T) {
	a, _ := powerApp(t)
	to := time.Now().Unix()
	from := to - 86400
	if got := a.powerSummaryLines(from, to); len(got) != 1 || !strings.Contains(got[0], "stayed on") {
		t.Fatalf("%v", got)
	}
	a.Logs.Exec(`INSERT INTO power_outages(start,end,test) VALUES(?,?,0)`, from+3600, from+3600+720)
	if got := a.powerSummaryLines(from, to); !strings.Contains(got[0], "failed 1 time yesterday, 12 minutes") {
		t.Fatalf("%v", got)
	}
}

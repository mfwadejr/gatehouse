package app

import (
	"strings"
	"testing"
	"time"

	"nopewarden/internal/netd"
)

func TestTrendLinesAreInPlainWords(t *testing.T) {
	if got := trendLine("realloc", 3, 7*24*time.Hour); got != "3 more sectors moved to spare than a week ago" {
		t.Fatal(got)
	}
	if got := trendLine("crc", 1, 3*24*time.Hour); got != "1 more cable or connection error than 3 days ago" {
		t.Fatal(got)
	}
}

func TestSelfTestsRunWhenDueAndOnlyOnce(t *testing.T) {
	c := defaultDiskTests()
	sun := time.Date(2026, 11, 1, 3, 5, 0, 0, time.Local) // the first Sunday of the month
	if sun.Weekday() != time.Sunday {
		t.Fatal("test date is not a Sunday")
	}
	ran := map[string]string{}
	if got := testDue(c, sun.Add(-10*time.Minute), ran); got != "" {
		t.Fatalf("before the time: %q", got)
	}
	if got := testDue(c, sun, ran); got != "short" {
		t.Fatalf("03:05 should start the short test, got %q", got)
	}
	ran["short"] = sun.Format("2006-01-02")
	if got := testDue(c, sun.Add(10*time.Minute), ran); got != "" {
		t.Fatalf("short already ran today: %q", got)
	}
	if got := testDue(c, sun.Add(30*time.Minute), ran); got != "long" {
		t.Fatalf("03:35 should start the long test, got %q", got)
	}
	// The second Sunday has no long test, only the short one.
	second := sun.AddDate(0, 0, 7).Add(30 * time.Minute)
	if got := testDue(c, second, map[string]string{}); got != "short" {
		t.Fatalf("second Sunday: %q", got)
	}
	c.Short.On = false
	if got := testDue(c, second, map[string]string{}); got != "" {
		t.Fatalf("switched off: %q", got)
	}
}

func TestDriveWatchTellsWhenACounterRisesAndWhenADriveGoes(t *testing.T) {
	a := newScopeApp(t)
	fn := a.Net.(*fakeNet)
	drive := func(realloc int64) netd.Drive {
		return netd.Drive{Name: "sda", Model: "Samsung PM841", Kind: "SATA SSD", Word: "Healthy", Wear: 5, Counters: map[string]int64{"realloc": realloc, "wear": 5}}
	}
	set := func(ds ...netd.Drive) {
		fn.mu.Lock()
		fn.disk = netd.DiskReport{Tool: true, Drives: ds}
		fn.mu.Unlock()
	}
	count := func(title string) int {
		var n int
		a.Logs.QueryRow(`SELECT COUNT(*) FROM events WHERE title LIKE ?`, title).Scan(&n)
		return n
	}
	now := time.Now()
	set(drive(0))
	a.diskWatch(now)
	if count("A drive's error count%") != 0 {
		t.Fatal("first reading must not raise anything")
	}
	set(drive(3))
	a.diskWatch(now.Add(15 * time.Minute))
	if count("A drive's error count went up%") != 1 {
		t.Fatal("a rise should be written down")
	}
	// Seen again at the same value: not told twice.
	a.diskWatch(now.Add(30 * time.Minute))
	if count("A drive's error count went up%") != 1 {
		t.Fatal("told twice")
	}
	// The card shows the rise as a plain line and turns amber.
	a.dropCached("disk-health")
	rep := a.diskReport(0)
	d := rep.Drives[0]
	if len(d.Trend) != 1 || !strings.HasPrefix(d.Trend[0], "3 more sectors moved to spare than") || d.Level != "warn" {
		t.Fatalf("trend %v level %q", d.Trend, d.Level)
	}
	// The drive vanishes: two readings in a row without it.
	set()
	fn.mu.Lock()
	fn.disk = netd.DiskReport{Tool: true, Drives: []netd.Drive{{Name: "sdb", Model: "Other", Kind: "SATA SSD", Word: "Healthy", Wear: -1}}}
	fn.mu.Unlock()
	a.diskWatch(now.Add(45 * time.Minute))
	if count("A drive has gone%") != 0 {
		t.Fatal("one missed reading is not enough")
	}
	a.diskWatch(now.Add(60 * time.Minute))
	if count("A drive has gone%") != 1 {
		t.Fatal("two missed readings should raise it")
	}
	a.dropCached("disk-health")
	var gone bool
	for _, d := range a.diskReport(0).Drives {
		if d.Gone && d.Name == "sda" && d.Level == "bad" {
			gone = true
		}
	}
	if !gone {
		t.Fatal("the missing drive should be listed as gone")
	}
}

func TestDriveWatchStartsDueSelfTests(t *testing.T) {
	a := newScopeApp(t)
	fn := a.Net.(*fakeNet)
	fn.disk = netd.DiskReport{Tool: true, Drives: []netd.Drive{{Name: "sda", Model: "X", Kind: "SATA SSD", Word: "Healthy", Wear: -1}}}
	sun := time.Date(2026, 11, 1, 3, 5, 0, 0, time.Local)
	a.diskWatch(sun)
	a.diskWatch(sun.Add(15 * time.Minute)) // the same morning: no second start
	if len(fn.tests) != 1 || fn.tests[0] != "sda:short" {
		t.Fatalf("tests started: %v", fn.tests)
	}
}

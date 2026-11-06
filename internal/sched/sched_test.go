package sched

import (
	"testing"
	"time"
)

func TestParseAndString(t *testing.T) {
	for _, in := range []string{"off", "every:6", "daily:03:30", "weekly:1:04:05"} {
		s, err := Parse(in)
		if err != nil || s.String() != in {
			t.Fatalf("%s -> %v %v", in, s, err)
		}
	}
	for _, bad := range []string{"", "every:0", "every:x", "every:721", "daily:25:00", "daily:3", "weekly:7:01:00", "hourly"} {
		if _, err := Parse(bad); err == nil {
			t.Fatalf("%q should be refused", bad)
		}
	}
}

func TestNext(t *testing.T) {
	now := time.Date(2026, 11, 4, 10, 0, 0, 0, time.UTC)
	e, _ := Parse("every:6")
	if !e.Due(time.Time{}, now) {
		t.Fatal("never run is due")
	}
	if e.Due(now.Add(-5*time.Hour), now) || !e.Due(now.Add(-6*time.Hour), now) {
		t.Fatal("every 6 hours")
	}
	d, _ := Parse("daily:03:30")
	last := time.Date(2026, 11, 4, 3, 31, 0, 0, time.UTC)
	if d.Due(last, now) {
		t.Fatal("already ran today")
	}
	if got := d.Next(last, now); !got.Equal(time.Date(2026, 11, 5, 3, 30, 0, 0, time.UTC)) {
		t.Fatal(got)
	}
	if !d.Due(time.Date(2026, 11, 3, 3, 31, 0, 0, time.UTC), now) {
		t.Fatal("missed slot catches up")
	}
	w, _ := Parse("weekly:1:03:00") // Monday; 2026-11-04 is a Wednesday
	if got := w.Next(time.Date(2026, 11, 4, 3, 5, 0, 0, time.UTC), now); !got.Equal(time.Date(2026, 11, 9, 3, 0, 0, 0, time.UTC)) {
		t.Fatal(got)
	}
	o, _ := Parse("off")
	if o.Due(time.Time{}, now) || !o.Next(time.Time{}, now).IsZero() {
		t.Fatal("off never due")
	}
}

func TestRetry(t *testing.T) {
	if RetryDelay(0) != 5*time.Minute || RetryDelay(99) != time.Hour {
		t.Fatal("retry")
	}
}

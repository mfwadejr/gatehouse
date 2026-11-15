package dnssrv

import (
	"testing"
	"time"
)

// The Live chart on the DNS page is split by how lookups were answered, so blocked and cached
// answers must be counted per second as well as in total.
func TestStatsSplitsEachSecondByOutcome(t *testing.T) {
	s := NewStats()
	for i := 0; i < 3; i++ {
		s.Record(OutBlocked, time.Millisecond, false)
	}
	for i := 0; i < 2; i++ {
		s.Record(OutCached, time.Millisecond, false)
	}
	s.Record(OutForwarded, 20*time.Millisecond, false)
	s.Tick()
	snap := s.Snapshot()
	if got := snap.History[59]; got != 6 {
		t.Fatalf("total in the last second: got %v, want 6", got)
	}
	if got := snap.HistoryBlocked[59]; got != 3 {
		t.Fatalf("blocked in the last second: got %v, want 3", got)
	}
	if got := snap.HistoryCached[59]; got != 2 {
		t.Fatalf("cached in the last second: got %v, want 2", got)
	}
	if snap.QPSBlocked <= 0 || snap.QPSCached <= 0 {
		t.Fatalf("the smoothed rates should be positive: blocked %v cached %v", snap.QPSBlocked, snap.QPSCached)
	}
	if snap.RecentMS <= 0 {
		t.Fatalf("a forwarded answer was recorded, so the recent answer time should be positive")
	}
	// The next second starts empty.
	s.Tick()
	if got := s.Snapshot().HistoryBlocked[59]; got != 0 {
		t.Fatalf("an empty second should read 0, got %v", got)
	}
}

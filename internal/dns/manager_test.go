package dnssrv

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func listByName(t *testing.T, m *Manager, name string) ListInfo {
	t.Helper()
	ls, err := m.Lists()
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range ls {
		if l.Name == name {
			return l
		}
	}
	t.Fatalf("no list %q", name)
	return ListInfo{}
}

// A list is counted as blocked, allowed and not used, and names that two lists share count once.
func TestBlocklistCounts(t *testing.T) {
	var calls atomic.Int32
	_, m := newTestServer(t, &calls)
	a := "||ads.example.com^\n||track.example.com^\n@@||ok.example.com^\n/ads[0-9]+\\.example\\.com/\n"
	if _, err := m.AddLocal("one", a, "abp"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.AddLocal("two", "ads.example.com\nother.example.net\n", "domains"); err != nil {
		t.Fatal(err)
	}
	one := listByName(t, m, "one")
	if one.Entries != 2 || one.Allowed != 1 || one.Skipped != 1 {
		t.Fatalf("list one: blocked %d allowed %d skipped %d, want 2/1/1", one.Entries, one.Allowed, one.Skipped)
	}
	two := listByName(t, m, "two")
	if two.Entries != 2 || two.Allowed != 0 {
		t.Fatalf("list two: %+v", two)
	}
	if n := m.UniqueBlocked(); n != 3 { // ads, track, other
		t.Fatalf("unique blocked = %d, want 3", n)
	}
	// An older row that only knew one number is corrected from the file on the next reload.
	m.db.Exec(`UPDATE blocklists SET entries=99, allowed=0, skipped=0 WHERE name='one'`)
	if err := m.Reload(); err != nil {
		t.Fatal(err)
	}
	if one = listByName(t, m, "one"); one.Entries != 2 || one.Allowed != 1 {
		t.Fatalf("not corrected on reload: %+v", one)
	}
}

// A failed download is tried again soon, with a longer wait each time, and a good one ends it.
func TestBlocklistRetriesSoon(t *testing.T) {
	var calls atomic.Int32
	_, m := newTestServer(t, &calls)
	m.AllowPrivate = true
	var hits atomic.Int32
	failing := atomic.Bool{}
	failing.Store(true)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if failing.Load() {
			http.Error(w, "nope", http.StatusInternalServerError)
			return
		}
		w.Write([]byte("ads.example.com\ntrack.example.com\n"))
	}))
	defer srv.Close()
	now := time.Unix(1_700_000_000, 0)
	m.now = func() time.Time { return now }
	id, err := m.Add("web", srv.URL, "domains", 24)
	if err != nil {
		t.Fatal(err)
	}
	if got := m.due(); len(got) != 1 {
		t.Fatalf("a new list is due at once, got %v", got)
	}
	ctx := context.Background()
	if m.Refresh(ctx, id) == nil {
		t.Fatal("expected a failure")
	}
	l := listByName(t, m, "web")
	if l.Status != "error" || l.RetryAt != now.Add(time.Minute).Unix() {
		t.Fatalf("after first failure: %+v", l)
	}
	if !strings.Contains(l.Plain, "website answered") {
		t.Errorf("plain error = %q", l.Plain)
	}
	if got := m.due(); len(got) != 0 {
		t.Fatalf("not due before the retry time: %v", got)
	}
	now = now.Add(61 * time.Second)
	if got := m.due(); len(got) != 1 {
		t.Fatalf("due after a minute: %v", got)
	}
	m.Refresh(ctx, id)
	if l = listByName(t, m, "web"); l.RetryAt != now.Add(2*time.Minute).Unix() {
		t.Fatalf("second wait should be 2 minutes: %+v", l)
	}
	// The internet comes back: tried at once, not after the wait.
	m.Nudge()
	if got := m.due(); len(got) != 1 {
		t.Fatalf("a nudge should make it due: %v", got)
	}
	failing.Store(false)
	if err := m.Refresh(ctx, id); err != nil {
		t.Fatal(err)
	}
	l = listByName(t, m, "web")
	if l.Status != "ok" || l.Entries != 2 || l.RetryAt != 0 || l.Error != "" {
		t.Fatalf("after success: %+v", l)
	}
	if got := m.due(); len(got) != 0 {
		t.Fatalf("a fresh list is not due: %v", got)
	}
	now = now.Add(25 * time.Hour)
	if got := m.due(); len(got) != 1 {
		t.Fatalf("due again after the interval: %v", got)
	}
}

func TestRetryDelaySteps(t *testing.T) {
	want := []time.Duration{time.Minute, 2 * time.Minute, 5 * time.Minute, 15 * time.Minute, 30 * time.Minute, time.Hour, time.Hour}
	for i, w := range want {
		if got := retryDelay(i); got != w {
			t.Errorf("retryDelay(%d) = %v, want %v", i, got, w)
		}
	}
}

func TestPlainListError(t *testing.T) {
	for in, want := range map[string]string{
		`Get "https://x": dial tcp: lookup x on [::1]:53: server misbehaving`: "Couldn't reach the list's website.",
		"server answered 404 Not Found":                                       "The list's website answered with an error.",
		"no usable entries found (3 lines skipped)":                           "The file did not look like a blocklist.",
		"something odd": "Couldn't download this list.",
	} {
		if got := PlainListError(in); got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
}

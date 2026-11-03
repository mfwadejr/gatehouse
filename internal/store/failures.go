package store

import (
	"database/sql"
	"sync"
	"time"
)

// Failures records writes that did not go through. Much of the program writes best-effort (a log
// line, a counter) and does not stop for a failed one; this keeps every such failure from being
// silent, so the health check can say "the disk would not take writes" instead of nothing.
type Failures struct {
	mu   sync.Mutex
	n    int64
	last string
	at   time.Time
	ring []time.Time // times of recent failures, newest last, at most 100
}

func (f *Failures) note(err error) {
	f.mu.Lock()
	f.n++
	f.last, f.at = err.Error(), time.Now()
	f.ring = append(f.ring, f.at)
	if len(f.ring) > 100 {
		f.ring = f.ring[len(f.ring)-100:]
	}
	f.mu.Unlock()
}

// Recent says how many writes failed within the window, and the newest failure's text and time.
func (d *DB) WriteFailures(window time.Duration) (n int, last string, at time.Time) {
	d.fails.mu.Lock()
	defer d.fails.mu.Unlock()
	cut := time.Now().Add(-window)
	for _, t := range d.fails.ring {
		if t.After(cut) {
			n++
		}
	}
	return n, d.fails.last, d.fails.at
}

// Exec runs a write and remembers it when it fails, then returns the error as usual.
func (d *DB) Exec(q string, args ...any) (sql.Result, error) {
	r, err := d.DB.Exec(q, args...)
	if err != nil {
		d.fails.note(err)
	}
	return r, err
}

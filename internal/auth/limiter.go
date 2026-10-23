package auth

import (
	"sync"
	"time"
)

// Limiter blocks a key for a while after too many failures in a window.
type Limiter struct {
	mu      sync.Mutex
	max     int
	window  time.Duration
	block   time.Duration
	now     func() time.Time
	entries map[string]*limEntry
	lastGC  time.Time
}

type limEntry struct {
	fails        []time.Time
	blockedUntil time.Time
}

func NewLimiter(max int, window, block time.Duration) *Limiter {
	return &Limiter{max: max, window: window, block: block, now: time.Now, entries: map[string]*limEntry{}}
}

// Blocked reports whether key is currently blocked and until when.
func (l *Limiter) Blocked(key string) (time.Time, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	e := l.entries[key]
	if e == nil {
		return time.Time{}, false
	}
	if l.now().Before(e.blockedUntil) {
		return e.blockedUntil, true
	}
	return time.Time{}, false
}

// Fail records a failure and returns true if the key is now blocked.
func (l *Limiter) Fail(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.gc(now)
	e := l.entries[key]
	if e == nil {
		e = &limEntry{}
		l.entries[key] = e
	}
	cut := now.Add(-l.window)
	kept := e.fails[:0]
	for _, t := range e.fails {
		if t.After(cut) {
			kept = append(kept, t)
		}
	}
	e.fails = append(kept, now)
	if len(e.fails) >= l.max {
		e.blockedUntil = now.Add(l.block)
		e.fails = nil
		return true
	}
	return false
}

// Reset clears a key after a successful login.
func (l *Limiter) Reset(key string) {
	l.mu.Lock()
	delete(l.entries, key)
	l.mu.Unlock()
}

func (l *Limiter) gc(now time.Time) {
	if now.Sub(l.lastGC) < time.Minute {
		return
	}
	l.lastGC = now
	for k, e := range l.entries {
		if now.After(e.blockedUntil) && (len(e.fails) == 0 || now.Sub(e.fails[len(e.fails)-1]) > l.window) {
			delete(l.entries, k)
		}
	}
}

package app

import (
	"sync/atomic"
	"time"

	"nopewarden/internal/fw"
)

// loadTracker notes when a firewall change was last tried, so Find traffic can say which connections
// were already open when the rules changed.
type loadTracker struct {
	fw.Controller
	at *atomic.Int64
}

func (l *loadTracker) Apply(script string, window time.Duration, author string) (string, time.Time, error) {
	tok, dl, err := l.Controller.Apply(script, window, author)
	if err == nil {
		l.at.Store(time.Now().Unix())
	}
	return tok, dl, err
}

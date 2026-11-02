package api

import (
	"log"
	"strings"
	"sync"
	"time"
)

// A record of the console's live connections: when each opened and closed, why it closed, and
// from which address. When a browser reports dropped streams, this shows whether the box or the
// network between them ended them.

type StreamEntry struct {
	At      int64  `json:"at"`
	Event   string `json:"event"` // "opened" or "closed"
	Addr    string `json:"addr"`
	Topics  string `json:"topics"`
	Why     string `json:"why,omitempty"`
	Seconds int64  `json:"seconds,omitempty"`
}

type streamLog struct {
	mu       sync.Mutex
	open     int
	nOpened  int64
	nClosed  int64
	byReason map[string]int64
	recent   []StreamEntry
}

const streamLogKeep = 200

func (l *streamLog) add(e StreamEntry) {
	l.recent = append(l.recent, e)
	if len(l.recent) > streamLogKeep {
		l.recent = l.recent[len(l.recent)-streamLogKeep:]
	}
}

func (l *streamLog) opened(addr string, topics []string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.open++
	l.nOpened++
	l.add(StreamEntry{At: time.Now().Unix(), Event: "opened", Addr: addr, Topics: strings.Join(topics, ",")})
}

func (l *streamLog) closed(addr string, topics []string, since time.Time, why string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.open--
	l.nClosed++
	if l.byReason == nil {
		l.byReason = map[string]int64{}
	}
	l.byReason[why]++
	secs := int64(time.Since(since).Seconds())
	l.add(StreamEntry{At: time.Now().Unix(), Event: "closed", Addr: addr, Topics: strings.Join(topics, ","), Why: why, Seconds: secs})
	// Only the unusual endings go to the system log, so a repeating pattern shows there too.
	if why != "the browser went away" {
		log.Printf("console live connection from %s closed after %ds: %s", addr, secs, why)
	}
}

// StreamStats is what the Diagnostics page shows.
func (s *Server) StreamStats() map[string]any {
	l := &s.streams
	l.mu.Lock()
	defer l.mu.Unlock()
	by := map[string]int64{}
	for k, v := range l.byReason {
		by[k] = v
	}
	recent := make([]StreamEntry, len(l.recent))
	copy(recent, l.recent)
	for i, j := 0, len(recent)-1; i < j; i, j = i+1, j-1 {
		recent[i], recent[j] = recent[j], recent[i]
	}
	return map[string]any{"open": l.open, "opened": l.nOpened, "closed": l.nClosed, "by_reason": by, "recent": recent}
}

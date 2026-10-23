package netd

import (
	"context"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// FWEvent is one firewall decision about a new connection, as the kernel logged it.
type FWEvent struct {
	Seq     uint64 `json:"seq"`
	TS      int64  `json:"ts"`
	Verdict string `json:"verdict"` // allow or drop
	Tag     string `json:"tag"`     // why: the comment of the rule that decided
	In      string `json:"in"`
	Out     string `json:"out"`
	Src     string `json:"src"`
	Dst     string `json:"dst"`
	Proto   string `json:"proto"`
	SPort   int    `json:"sport"`
	DPort   int    `json:"dport"`
}

// ParseKmsg reads one /dev/kmsg record written by a Gatehouse log rule. Anything else,
// including other programs' kernel messages, is ignored.
func ParseKmsg(rec string, now time.Time) (FWEvent, bool) {
	var ev FWEvent
	i := strings.Index(rec, ";gh|")
	if i < 0 {
		return ev, false
	}
	msg := strings.TrimSpace(rec[i+4:])
	verdict, rest, ok := strings.Cut(msg, "|")
	if !ok || (verdict != "allow" && verdict != "drop") {
		return ev, false
	}
	cut := strings.Index(rest, " IN=")
	if cut < 0 {
		return ev, false
	}
	ev.Verdict, ev.Tag = verdict, strings.TrimSpace(rest[:cut])
	ev.TS = now.Unix()
	for _, f := range strings.Fields(rest[cut:]) {
		k, v, ok := strings.Cut(f, "=")
		if !ok {
			continue
		}
		switch k {
		case "IN":
			ev.In = v
		case "OUT":
			ev.Out = v
		case "SRC":
			if ev.Src == "" {
				ev.Src = v
			}
		case "DST":
			if ev.Dst == "" {
				ev.Dst = v
			}
		case "PROTO":
			ev.Proto = strings.ToLower(v)
		case "SPT":
			ev.SPort, _ = strconv.Atoi(v)
		case "DPT":
			ev.DPort, _ = strconv.Atoi(v)
		}
	}
	if ev.Src == "" || ev.Dst == "" {
		return ev, false
	}
	return ev, true
}

// FWLog keeps the most recent firewall events in memory until the daemon collects them.
type FWLog struct {
	Path string // /dev/kmsg

	mu   sync.Mutex
	ring []FWEvent
	seq  uint64
}

const fwRing = 4000

func NewFWLog() *FWLog { return &FWLog{Path: "/dev/kmsg"} }

func (l *FWLog) add(ev FWEvent) {
	l.mu.Lock()
	l.seq++
	ev.Seq = l.seq
	l.ring = append(l.ring, ev)
	if len(l.ring) > fwRing {
		l.ring = l.ring[len(l.ring)-fwRing:]
	}
	l.mu.Unlock()
}

// Since returns the events after seq, oldest first, and the newest sequence number.
func (l *FWLog) Since(seq uint64, max int) ([]FWEvent, uint64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []FWEvent
	for _, e := range l.ring {
		if e.Seq > seq {
			out = append(out, e)
		}
	}
	if max > 0 && len(out) > max {
		out = out[:max]
	}
	return out, l.seq
}

// Run follows the kernel log until ctx ends. It starts at the end, so old messages from
// before this start are not replayed.
func (l *FWLog) Run(ctx context.Context) {
	f, err := os.Open(l.Path)
	if err != nil {
		return
	}
	defer f.Close()
	f.Seek(0, 2)
	go func() { <-ctx.Done(); f.Close() }()
	buf := make([]byte, 8192)
	for {
		n, err := f.Read(buf)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			// A record that was overwritten before it was read reports EPIPE; carry on.
			time.Sleep(50 * time.Millisecond)
			continue
		}
		if ev, ok := ParseKmsg(string(buf[:n]), time.Now()); ok {
			l.add(ev)
		}
	}
}

package app

import (
	"context"
	"net/netip"
	"strings"
	"sync"
	"time"

	"nopewarden/internal/flowexp"
	"nopewarden/internal/netd"
)

// What became of the connections in the log. The firewall log records a connection when it starts.
// This looks at the kernel's list of live connections every few seconds and, for each connection that
// was logged as allowed, keeps how it ended, the bytes each way and how long it lasted, then writes
// that back to its log row. Only IPv4 connections are in the list it reads, so others stay "not known".

// Words for how a connection ended, kept fixed so the log and the CSV agree.
const (
	outBlocked   = "Blocked"
	outCompleted = "Completed"
	outReset     = "Reset"
	outTimedOut  = "Timed out"
	outOpen      = "Still open"
)

type ctKey struct {
	proto        string
	src, dst     netip.Addr
	sport, dport int
}

type watched struct {
	id         int64
	ts         int64
	seen       bool
	ob, rb     int64 // -1 when the kernel is not counting
	lastActive int64
	state      string
	unreplied  bool
	udp        bool
}

type connTrack struct {
	mu    sync.Mutex
	watch map[ctKey]*watched
	acct  time.Time
}

type connUpdate struct {
	ID       int64
	Outcome  string
	BytesOut int64
	BytesIn  int64
	Dur      int64
}

const connWatchMax = 50000

// add starts following a connection that was just logged as allowed.
func (ct *connTrack) add(e netd.FWEvent, id int64) {
	proto := strings.ToLower(e.Proto)
	if (proto != "tcp" && proto != "udp") || e.SPort <= 0 || e.DPort <= 0 {
		return
	}
	src, err1 := netip.ParseAddr(e.Src)
	dst, err2 := netip.ParseAddr(e.Dst)
	if err1 != nil || err2 != nil || !src.Is4() || !dst.Is4() {
		return
	}
	ct.mu.Lock()
	defer ct.mu.Unlock()
	if ct.watch == nil {
		ct.watch = map[ctKey]*watched{}
	}
	if len(ct.watch) >= connWatchMax {
		return // under a flood of connections, keep what is already followed
	}
	ct.watch[ctKey{proto, src, dst, e.SPort, e.DPort}] = &watched{id: id, ts: e.TS, ob: -1, rb: -1, lastActive: e.TS, udp: proto == "udp"}
}

func (ct *connTrack) open() int {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	return len(ct.watch)
}

func outcomeOf(w *watched, present bool) string {
	switch {
	case w.unreplied:
		return outTimedOut
	case w.udp:
		if present {
			return outOpen
		}
		return outCompleted
	}
	switch w.state {
	case "CLOSE":
		return outReset
	case "TIME_WAIT", "CLOSE_WAIT", "LAST_ACK", "FIN_WAIT":
		return outCompleted
	case "ESTABLISHED", "SYN_RECV":
		if present {
			return outOpen
		}
		return outTimedOut // it went quiet and the kernel forgot it
	}
	if present {
		return outOpen
	}
	return outCompleted
}

// step takes one look at the kernel's list and says which log rows to bring up to date.
func (ct *connTrack) step(now time.Time, conns []flowexp.Conn) []connUpdate {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	if len(ct.watch) == 0 {
		return nil
	}
	cur := make(map[ctKey]flowexp.Conn, len(conns))
	for _, c := range conns {
		p := "tcp"
		switch c.Proto {
		case 17:
			p = "udp"
		case 6:
		default:
			continue
		}
		cur[ctKey{p, c.Src, c.Dst, int(c.SPort), int(c.DPort)}] = c
	}
	var out []connUpdate
	for k, w := range ct.watch {
		c, ok := cur[k]
		if !ok {
			if !w.seen {
				if now.Unix()-w.ts > 40 {
					delete(ct.watch, k) // never found in the list: nothing to say about it
				}
				continue
			}
			out = append(out, connUpdate{w.id, outcomeOf(w, false), w.ob, w.rb, maxI(w.lastActive-w.ts, 0)})
			delete(ct.watch, k)
			continue
		}
		w.seen = true
		ob, rb := int64(-1), int64(-1)
		if c.OPackets > 0 || c.RPackets > 0 || c.OBytes > 0 || c.RBytes > 0 {
			ob, rb = int64(c.OBytes), int64(c.RBytes)
		}
		changed := ob != w.ob || rb != w.rb || c.State != w.state || c.Unreplied != w.unreplied
		if ob != w.ob || rb != w.rb {
			w.lastActive = now.Unix()
		}
		w.ob, w.rb, w.state, w.unreplied = ob, rb, c.State, c.Unreplied
		final := !w.udp && (c.State == "TIME_WAIT" || c.State == "CLOSE")
		if changed || final {
			out = append(out, connUpdate{w.id, outcomeOf(w, !final), w.ob, w.rb, maxI(w.lastActive-w.ts, 0)})
		}
		if final {
			delete(ct.watch, k)
		}
	}
	return out
}

func maxI(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// connStatsLoop reads the connection list while there are logged connections to follow.
func (a *App) connStatsLoop(ctx context.Context) {
	if a.Net == nil {
		return
	}
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			a.connStatsStep(now)
		}
	}
}

func (a *App) connStatsStep(now time.Time) {
	ct := &a.conns
	if ct.open() == 0 {
		return
	}
	if time.Since(ct.acct) > time.Minute { // byte counts need the kernel's accounting on
		a.Net.FlowAcct(true)
		ct.acct = time.Now()
	}
	text, err := a.Net.ConntrackDump()
	if err != nil {
		return
	}
	ups := ct.step(now, flowexp.ParseConntrack(text))
	if len(ups) == 0 {
		return
	}
	tx, err := a.Logs.Begin()
	if err != nil {
		return
	}
	for _, u := range ups {
		tx.Exec(`UPDATE fw_events SET outcome=?,bytes_out=?,bytes_in=?,dur=? WHERE id=?`, u.Outcome, u.BytesOut, u.BytesIn, u.Dur, u.ID)
	}
	if err := tx.Commit(); err != nil {
		tx.Rollback()
	}
}

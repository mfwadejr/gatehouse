// Package wanmon watches the internet connections and decides which one carries traffic.
//
// With one connection there is nothing to decide and systemd-networkd owns the routes.
// With two or more, each connection keeps its own routing table and the monitor owns the
// main default route: failover uses the best working connection, load balancing spreads
// flows across all working ones. A firewall mark per connection makes replies leave through
// the connection a request came in on.
package wanmon

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sort"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// Link is one internet connection under watch.
type Link struct {
	Name     string
	Dev      string
	Priority int
	Weight   int
	Table    int    // routing table holding this link's own default route
	Mark     uint32 // firewall mark identifying traffic that arrived on this link
	Kind     string // icmp or tcp
	Target   string // address to probe; tcp targets are host:port
	Interval time.Duration
	Failures int // consecutive failures before the link is considered down
	Recover  int // consecutive successes before it is considered up again
}

// Mode selects how working links share traffic.
type Mode string

const (
	Failover Mode = "failover"
	Balance  Mode = "balance"
)

// State is what the monitor knows about a link.
type State struct {
	Up      bool          `json:"up"`
	Gateway string        `json:"gateway"`
	RTT     time.Duration `json:"rtt"`
	Error   string        `json:"error,omitempty"`
	Since   time.Time     `json:"since"`
	Active  bool          `json:"active"` // carrying traffic now
	// Via names the connections a group is using right now (groups only; their key starts with "group:").
	Via   []string `json:"via,omitempty"`
	fails int
	succ  int
}

// Nexthop is one entry of the default route.
type Nexthop struct {
	Dev     string
	Gateway string
	Weight  int
}

// Checker probes a link.
type Checker interface {
	Check(ctx context.Context, l Link) (time.Duration, error)
}

// Router reads and writes the kernel's routing state.
type Router interface {
	Gateway(l Link) (string, error)     // the link's own default gateway
	SetDefault(nh []Nexthop) error      // replace the main table default route
	EnsureLink(l Link, gw string) error // per-link table and fwmark rule
	RemoveLink(l Link) error
}

// Plan chooses the nexthops for the main default route.
// A link with no known gateway cannot carry traffic. If nothing is up the best candidate is kept
// so the route is already in place when the link comes back.
func Plan(links []Link, st map[string]*State, mode Mode) []Nexthop {
	usable := func(l Link) bool { s := st[l.Name]; return s != nil && s.Gateway != "" }
	var up, any []Link
	for _, l := range links {
		if !usable(l) {
			continue
		}
		any = append(any, l)
		if st[l.Name].Up {
			up = append(up, l)
		}
	}
	by := func(ls []Link) {
		sort.SliceStable(ls, func(i, j int) bool { return ls[i].Priority < ls[j].Priority })
	}
	by(up)
	by(any)
	pick := up
	if len(pick) == 0 {
		if len(any) == 0 {
			return nil
		}
		return []Nexthop{{Dev: any[0].Dev, Gateway: st[any[0].Name].Gateway, Weight: 1}}
	}
	if mode == Failover {
		l := pick[0]
		return []Nexthop{{Dev: l.Dev, Gateway: st[l.Name].Gateway, Weight: 1}}
	}
	var out []Nexthop
	for _, l := range pick {
		w := l.Weight
		if w < 1 {
			w = 1
		}
		out = append(out, Nexthop{Dev: l.Dev, Gateway: st[l.Name].Gateway, Weight: w})
	}
	return out
}

// Monitor runs the checks and keeps the routes in step.
type Monitor struct {
	Links   []Link
	Mode    Mode
	Groups  []Group
	Checker Checker
	Router  Router
	// OnChange is called when a link goes up or down or the active set changes.
	OnChange func(map[string]State)

	mu     sync.Mutex
	states map[string]*State
	active []Nexthop
	gact   map[string][]Nexthop
	now    func() time.Time
}

func New(links []Link, mode Mode, c Checker, r Router) *Monitor {
	m := &Monitor{Links: links, Mode: mode, Checker: c, Router: r, states: map[string]*State{}, now: time.Now}
	for _, l := range links {
		m.states[l.Name] = &State{Up: true, Since: m.now()}
	}
	return m
}

// Snapshot returns a copy of every link's state.
func (m *Monitor) Snapshot() map[string]State {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.snapshotLocked()
}

func (m *Monitor) snapshotLocked() map[string]State {
	out := map[string]State{}
	on := map[string]bool{}
	for _, nh := range m.active {
		on[nh.Dev] = true
	}
	for _, l := range m.Links {
		s := *m.states[l.Name]
		s.Active = on[l.Dev] && s.Up
		out[l.Name] = s
	}
	devName := map[string]string{}
	for _, l := range m.Links {
		devName[l.Dev] = l.Name
	}
	for _, g := range m.Groups {
		plan, seen := m.gact[g.Name]
		st := State{Up: len(plan) > 0, Since: m.now(), Active: len(plan) > 0}
		for _, nh := range plan {
			st.Via = append(st.Via, devName[nh.Dev])
		}
		if seen && len(plan) == 0 {
			if g.Block {
				st.Error = "No connection in this group works. Its devices are cut off until one comes back."
			} else {
				st.Error = "No connection in this group works. Its devices are using the default connections."
			}
		}
		out["group:"+g.Name] = st
	}
	return out
}

// Observe records one probe result for a link and re-plans. It is exported so tests and the
// run loop share one code path.
func (m *Monitor) Observe(l Link, rtt time.Duration, err error) {
	m.mu.Lock()
	s := m.states[l.Name]
	changed := false
	fail, rec := l.Failures, l.Recover
	if fail < 1 {
		fail = 3
	}
	if rec < 1 {
		rec = 3
	}
	if err != nil {
		s.fails++
		s.succ = 0
		s.Error = err.Error()
		if s.Up && s.fails >= fail {
			s.Up, s.Since, changed = false, m.now(), true
		}
	} else {
		s.succ++
		s.fails = 0
		s.Error = ""
		s.RTT = rtt
		if !s.Up && s.succ >= rec {
			s.Up, s.Since, changed = true, m.now(), true
		}
	}
	m.mu.Unlock()
	m.replan(changed)
}

func (m *Monitor) replan(force bool) {
	m.mu.Lock()
	plan := Plan(m.Links, m.states, m.Mode)
	mainChanged := fmt.Sprint(plan) != fmt.Sprint(m.active)
	if mainChanged {
		m.active = plan
	}
	if m.gact == nil {
		m.gact = map[string][]Nexthop{}
	}
	type gchange struct {
		g  Group
		nh []Nexthop
	}
	var gch []gchange
	for _, g := range m.Groups {
		gp := GroupPlan(g, m.Links, m.states)
		old, seen := m.gact[g.Name]
		if !seen || fmt.Sprint(gp) != fmt.Sprint(old) {
			m.gact[g.Name] = gp
			gch = append(gch, gchange{g, gp})
		}
	}
	snap := m.snapshotLocked()
	m.mu.Unlock()
	if mainChanged && len(plan) > 0 {
		if err := m.Router.SetDefault(plan); err != nil {
			m.mu.Lock()
			m.active = nil // try again on the next observation
			m.mu.Unlock()
		}
	}
	if gr, ok := m.Router.(GroupRouter); ok {
		for _, c := range gch {
			if err := gr.SetTable(c.g.Table, c.nh, c.g.Block); err != nil {
				m.mu.Lock()
				delete(m.gact, c.g.Name) // try again on the next observation
				m.mu.Unlock()
			}
		}
	}
	if (force || mainChanged || len(gch) > 0) && m.OnChange != nil {
		m.OnChange(snap)
	}
}

// Refresh re-reads gateways and installs per-link tables and rules. Call it at start and
// whenever a link's address configuration changes.
func (m *Monitor) Refresh() error {
	var errs []error
	for _, l := range m.Links {
		gw, err := m.Router.Gateway(l)
		m.mu.Lock()
		m.states[l.Name].Gateway = gw
		m.mu.Unlock()
		if err != nil || gw == "" {
			continue
		}
		if err := m.Router.EnsureLink(l, gw); err != nil {
			errs = append(errs, err)
		}
	}
	if gr, ok := m.Router.(GroupRouter); ok {
		if err := gr.Prune(m.Groups); err != nil {
			errs = append(errs, err)
		}
		for _, g := range m.Groups {
			if err := gr.SyncRules(g); err != nil {
				errs = append(errs, err)
			}
		}
	}
	m.replan(true)
	return errors.Join(errs...)
}

// Run probes every link on its interval until ctx ends.
func (m *Monitor) Run(ctx context.Context) {
	if err := m.Refresh(); err != nil {
		_ = err
	}
	var wg sync.WaitGroup
	for _, l := range m.Links {
		l := l
		if l.Interval <= 0 {
			l.Interval = 5 * time.Second
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			t := time.NewTicker(l.Interval)
			defer t.Stop()
			n := 0
			for {
				cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
				rtt, err := m.Checker.Check(cctx, l)
				cancel()
				if ctx.Err() != nil {
					return
				}
				m.Observe(l, rtt, err)
				n++
				if n%12 == 0 { // pick up gateway changes from DHCP renewals
					m.Refresh()
				}
				select {
				case <-ctx.Done():
					return
				case <-t.C:
				}
			}
		}()
	}
	wg.Wait()
}

// NetChecker probes through the link's own interface, so a healthy backup cannot hide a
// dead primary.
type NetChecker struct{}

func bindTo(dev string) func(network, address string, c syscall.RawConn) error {
	return func(network, address string, c syscall.RawConn) error {
		var serr error
		if err := c.Control(func(fd uintptr) { serr = unix.SetsockoptString(int(fd), unix.SOL_SOCKET, unix.SO_BINDTODEVICE, dev) }); err != nil {
			return err
		}
		return serr
	}
}

func (NetChecker) Check(ctx context.Context, l Link) (time.Duration, error) {
	start := time.Now()
	switch l.Kind {
	case "tcp":
		d := net.Dialer{Control: bindTo(l.Dev), Timeout: 3 * time.Second}
		c, err := d.DialContext(ctx, "tcp", l.Target)
		if err != nil {
			return 0, err
		}
		c.Close()
		return time.Since(start), nil
	default:
		return icmpPing(ctx, l)
	}
}

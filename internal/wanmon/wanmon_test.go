package wanmon

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

type fakeRouter struct {
	gws     map[string]string
	defs    [][]Nexthop
	ensured []string
}

func (f *fakeRouter) Gateway(l Link) (string, error) { return f.gws[l.Dev], nil }
func (f *fakeRouter) SetDefault(nh []Nexthop) error {
	f.defs = append(f.defs, append([]Nexthop(nil), nh...))
	return nil
}
func (f *fakeRouter) EnsureLink(l Link, gw string) error {
	f.ensured = append(f.ensured, fmt.Sprintf("%s>%s", l.Dev, gw))
	return nil
}
func (f *fakeRouter) RemoveLink(Link) error { return nil }

func links() []Link {
	return []Link{
		{Name: "Fibre", Dev: "eth0", Priority: 1, Weight: 3, Table: 100, Mark: 0x100, Failures: 3, Recover: 2},
		{Name: "LTE", Dev: "wwan0", Priority: 2, Weight: 1, Table: 101, Mark: 0x101, Failures: 3, Recover: 2},
	}
}

func newMon(mode Mode) (*Monitor, *fakeRouter) {
	r := &fakeRouter{gws: map[string]string{"eth0": "203.0.113.1", "wwan0": "198.51.100.1"}}
	m := New(links(), mode, nil, r)
	m.Refresh()
	return m, r
}

func lastDefault(r *fakeRouter) string {
	if len(r.defs) == 0 {
		return "none"
	}
	s := ""
	for _, n := range r.defs[len(r.defs)-1] {
		s += fmt.Sprintf("%s/%d ", n.Dev, n.Weight)
	}
	return s
}

func TestFailoverAndRecovery(t *testing.T) {
	m, r := newMon(Failover)
	if got := lastDefault(r); got != "eth0/1 " {
		t.Fatalf("initial: %q", got)
	}
	fibre := m.Links[0]
	boom := errors.New("timeout")
	// Two failures are tolerated; the third switches.
	m.Observe(fibre, 0, boom)
	m.Observe(fibre, 0, boom)
	if got := lastDefault(r); got != "eth0/1 " {
		t.Fatalf("switched too early: %q", got)
	}
	m.Observe(fibre, 0, boom)
	if got := lastDefault(r); got != "wwan0/1 " {
		t.Fatalf("no failover: %q", got)
	}
	if s := m.Snapshot(); s["Fibre"].Up || !s["LTE"].Active || s["Fibre"].Error != "timeout" {
		t.Fatalf("snapshot: %+v", s)
	}
	// One good probe is not enough to come back; two are.
	m.Observe(fibre, time.Millisecond, nil)
	if got := lastDefault(r); got != "wwan0/1 " {
		t.Fatalf("returned too early: %q", got)
	}
	m.Observe(fibre, time.Millisecond, nil)
	if got := lastDefault(r); got != "eth0/1 " {
		t.Fatalf("did not return: %q", got)
	}
	// A flapping link does not reset the success streak counter incorrectly: failure then success restarts it.
	m.Observe(fibre, 0, boom)
	m.Observe(fibre, time.Millisecond, nil)
	if !m.Snapshot()["Fibre"].Up {
		t.Fatal("a single failure took a healthy link down")
	}
}

func TestBalanceUsesWeights(t *testing.T) {
	m, r := newMon(Balance)
	if got := lastDefault(r); got != "eth0/3 wwan0/1 " {
		t.Fatalf("balance: %q", got)
	}
	boom := errors.New("x")
	for i := 0; i < 3; i++ {
		m.Observe(m.Links[1], 0, boom)
	}
	if got := lastDefault(r); got != "eth0/3 " {
		t.Fatalf("down link still in the set: %q", got)
	}
}

func TestAllDownKeepsBestRoute(t *testing.T) {
	m, r := newMon(Failover)
	boom := errors.New("x")
	for _, l := range m.Links {
		for i := 0; i < 3; i++ {
			m.Observe(l, 0, boom)
		}
	}
	if got := lastDefault(r); got != "eth0/1 " {
		t.Fatalf("with everything down the primary should stay routed: %q", got)
	}
}

func TestNoGatewayNoRoute(t *testing.T) {
	r := &fakeRouter{gws: map[string]string{"eth0": "", "wwan0": "198.51.100.1"}}
	m := New(links(), Failover, nil, r)
	m.Refresh()
	if got := lastDefault(r); got != "wwan0/1 " {
		t.Fatalf("a link without a gateway must not be chosen: %q", got)
	}
}

func TestOnChangeFires(t *testing.T) {
	m, _ := newMon(Failover)
	n := 0
	m.OnChange = func(map[string]State) { n++ }
	boom := errors.New("x")
	for i := 0; i < 3; i++ {
		m.Observe(m.Links[0], 0, boom)
	}
	if n != 1 {
		t.Fatalf("OnChange calls = %d", n)
	}
}

// gwChecker answers for the internet probe and the next hop separately.
type gwChecker struct{ gw bool }

func (gwChecker) Check(ctx context.Context, l Link) (time.Duration, error) {
	return 0, errors.New("no answer")
}
func (g gwChecker) CheckGateway(ctx context.Context, l Link, gw string) (time.Duration, error) {
	if g.gw {
		return 2 * time.Millisecond, nil
	}
	return 0, errors.New("no answer")
}

func oneLink() []Link {
	return []Link{{Name: "Fibre", Dev: "eth0", Priority: 1, Weight: 1, Table: 101, Mark: 1, Kind: "icmp", Target: "1.1.1.1", Failures: 3, Recover: 2}}
}

func TestSingleLinkIsNotHealthyUntilChecked(t *testing.T) {
	r := &fakeRouter{gws: map[string]string{"eth0": "192.0.2.1"}}
	m := New(oneLink(), Failover, nil, r)
	m.Watch = true
	m.Refresh()
	st := m.Snapshot()["Fibre"]
	if st.Checked || st.Health != "unknown" {
		t.Fatalf("a link nobody has probed must be unknown: %+v", st)
	}
	m.Observe(m.Links[0], 9*time.Millisecond, nil)
	st = m.Snapshot()["Fibre"]
	if !st.Checked || st.Health != "working" || !st.Active || st.RTT != 9*time.Millisecond {
		t.Fatalf("after one answer: %+v", st)
	}
}

func TestWatchModeNeverTouchesRoutes(t *testing.T) {
	r := &fakeRouter{gws: map[string]string{"eth0": "192.0.2.1"}}
	m := New(oneLink(), Failover, nil, r)
	m.Watch = true
	m.Refresh()
	boom := errors.New("x")
	for i := 0; i < 5; i++ {
		m.Observe(m.Links[0], 0, boom)
	}
	m.Observe(m.Links[0], time.Millisecond, nil)
	m.Observe(m.Links[0], time.Millisecond, nil)
	if len(r.defs) != 0 {
		t.Fatalf("watch-only changed the default route: %v", r.defs)
	}
	if len(r.ensured) != 0 {
		t.Fatalf("watch-only installed link state: %v", r.ensured)
	}
}

func TestHealthTellsNoInternetFromNoLink(t *testing.T) {
	for _, tc := range []struct {
		gw   bool
		want string
	}{{true, "no-internet"}, {false, "no-link"}} {
		r := &fakeRouter{gws: map[string]string{"eth0": "192.0.2.1"}}
		m := New(oneLink(), Failover, gwChecker{gw: tc.gw}, r)
		m.Watch = true
		m.Refresh()
		l := m.Links[0]
		for i := 0; i < 3; i++ {
			m.probeGateway(context.Background(), l)
			m.Observe(l, 0, errors.New("no answer"))
		}
		st := m.Snapshot()["Fibre"]
		if st.Up || st.Health != tc.want {
			t.Fatalf("gateway answers=%v: %+v", tc.gw, st)
		}
		if tc.gw && st.GwRTT == 0 {
			t.Fatal("the next hop's answer time should be recorded")
		}
	}
}

func TestLossIsTheShareOfRecentProbesUnanswered(t *testing.T) {
	r := &fakeRouter{gws: map[string]string{"eth0": "192.0.2.1"}}
	m := New(oneLink(), Failover, nil, r)
	m.Watch = true
	m.Refresh()
	l := m.Links[0]
	for i := 0; i < 8; i++ {
		m.Observe(l, time.Millisecond, nil)
	}
	m.Observe(l, 0, errors.New("x"))
	m.Observe(l, time.Millisecond, nil)
	if got := m.Snapshot()["Fibre"].Loss; got != 10 {
		t.Fatalf("1 of 10 lost should be 10 %%, got %d", got)
	}
}

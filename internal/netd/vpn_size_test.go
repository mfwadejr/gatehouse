package netd

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// fakeTunnel stands in for the network: packets up to limit get through, and some addresses never
// answer.
type fakeTunnel struct {
	mu     sync.Mutex
	limit  int
	silent map[string]bool
	mtu    int
	index  int
	up     bool
	sets   []int
}

func newFake(limit int) *fakeTunnel {
	return &fakeTunnel{limit: limit, silent: map[string]bool{}, mtu: 1500, index: 7, up: true}
}

func (f *fakeTunnel) attach(v *VPN) {
	v.PingSize = func(dev, target string, size int) bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		return f.up && !f.silent[target] && size <= f.limit
	}
	v.SetMTU = func(dev string, mtu int) error {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.sets = append(f.sets, mtu)
		f.mtu = mtu
		return nil
	}
	v.IfaceInfo = func(dev string) (int, int, bool, bool) {
		f.mu.Lock()
		defer f.mu.Unlock()
		return f.index, f.mtu, f.up, true
	}
}

func TestProbePicksLargestSizeTwoAddressesAgreeOn(t *testing.T) {
	v := NewVPN(t.TempDir())
	f := newFake(1368) // like the PIA tunnel on the owner's line
	f.silent["9.9.9.9"] = true
	f.attach(v)
	rep := v.probe("tun2", SizeSpec{Name: "tun2"}, 1500)
	if !rep.Passing || rep.After != 1360 || rep.Source != "measured" {
		t.Fatalf("want 1360 measured, got %+v", rep)
	}
	if len(rep.Alive) != 4 {
		t.Fatalf("the silent address should be left out: %v", rep.Alive)
	}
}

func TestProbeWithOneAddressAnsweringUsesIt(t *testing.T) {
	v := NewVPN(t.TempDir())
	f := newFake(1420)
	for _, a := range DefaultPingAddrs[1:] {
		f.silent[a] = true
	}
	f.attach(v)
	rep := v.probe("tun2", SizeSpec{Name: "tun2"}, 1500)
	if rep.After != 1420 || len(rep.Alive) != 1 {
		t.Fatalf("got %+v", rep)
	}
}

func TestProbeNothingAnswersMeansNoSize(t *testing.T) {
	v := NewVPN(t.TempDir())
	f := newFake(0)
	f.attach(v)
	st := &tunState{}
	rep := v.measure("tun2", SizeSpec{Name: "tun2"}, st, 1500)
	if rep.Passing || len(f.sets) != 0 {
		t.Fatalf("must not set a size when nothing answers: %+v sets=%v", rep, f.sets)
	}
	if v.info("tun2").Passing {
		t.Fatal("status should say not passing")
	}
	if st.nextProbe.IsZero() || time.Until(st.nextProbe) > 11*time.Second {
		t.Fatalf("first retry should be about 10 s away: %v", time.Until(st.nextProbe))
	}
}

func TestProbeSmallPassesButBigNeverFallsBackTo1280AndRetries(t *testing.T) {
	v := NewVPN(t.TempDir())
	f := newFake(100) // only the small test packet (84) gets through
	f.attach(v)
	st := &tunState{}
	rep := v.measure("tun2", SizeSpec{Name: "tun2"}, st, 1500)
	if rep.After != 1280 || rep.Source != "default" || st.confirmed {
		t.Fatalf("got %+v confirmed=%v", rep, st.confirmed)
	}
	if time.Until(st.nextProbe) > 11*time.Second {
		t.Fatal("an unconfirmed size is tested again within seconds")
	}
	// The path improves: the next measurement confirms a real size and moves up.
	f.limit = 1400
	rep = v.measure("tun2", SizeSpec{Name: "tun2"}, st, 1280)
	if rep.After != 1400 || !st.confirmed || f.mtu != 1400 {
		t.Fatalf("should move up to 1400: %+v mtu=%d", rep, f.mtu)
	}
	if time.Until(st.nextProbe) < 29*time.Minute {
		t.Fatal("a confirmed size is measured again after half an hour")
	}
}

func TestProbeStartsFromTheProfileSize(t *testing.T) {
	v := NewVPN(t.TempDir())
	f := newFake(1500)
	f.attach(v)
	rep := v.probe("tun2", SizeSpec{Name: "tun2", Start: 1400, FromProfile: true}, 1400)
	if rep.After != 1400 || rep.Source != "profile" {
		t.Fatalf("got %+v", rep)
	}
	for _, r := range rep.Rows {
		if r.Size > 1400 {
			t.Fatalf("the profile size is the ceiling, but %d was tried", r.Size)
		}
	}
	wg := v.probe("wg0", SizeSpec{Name: "wg0"}, 1420)
	if wg.Rows[0].Size != 1420 {
		t.Fatalf("WireGuard starts at 1420, got %d", wg.Rows[0].Size)
	}
}

func TestOwnCheckAddressIsTriedFirst(t *testing.T) {
	v := NewVPN(t.TempDir())
	got := v.pingTargets(SizeSpec{Name: "tun2", Check: "10.8.0.1"})
	if got[0] != "10.8.0.1" || got[1] != "1.1.1.1" || len(got) != 6 {
		t.Fatalf("got %v", got)
	}
}

func TestExitLookupGoesInOrderAndRetriesAtOnce(t *testing.T) {
	v := NewVPN(t.TempDir())
	var asked []string
	fail := true
	v.ExitLookup = func(dev, src string) (string, error) {
		asked = append(asked, src)
		if fail || src != "web:https://checkip.amazonaws.com" {
			return "", errors.New("timed out")
		}
		return "195.177.93.114", nil
	}
	st := &tunState{}
	v.lookExit("tun2", "tun2", st, false)
	if v.info("tun2").ExitIP != "" || v.info("tun2").ExitError == "" {
		t.Fatalf("a failure should say why: %+v", v.info("tun2"))
	}
	if len(asked) != 4 {
		t.Fatalf("every service is tried in turn: %v", asked)
	}
	fail = false
	asked = nil
	v.lookExit("tun2", "tun2", st, false) // the very next round, not ten minutes later
	if v.info("tun2").ExitIP != "195.177.93.114" || len(asked) != 2 || asked[0] != "dns:google" {
		t.Fatalf("got %+v asked=%v", v.info("tun2"), asked)
	}
	asked = nil
	v.lookExit("tun2", "tun2", st, false)
	if len(asked) != 0 {
		t.Fatal("a good answer is kept for ten minutes")
	}
	v.lookExit("tun2", "tun2", st, true) // a reconnect asks again at once
	if len(asked) == 0 {
		t.Fatal("a reconnect must ask again")
	}
}

func TestPublicIPOnly(t *testing.T) {
	for _, bad := range []string{"10.0.0.1", "192.168.1.5", "127.0.0.1", "100.64.1.1", "169.254.1.1", "hello", ""} {
		if _, ok := publicIP(bad); ok {
			t.Fatalf("%q must not count as an exit address", bad)
		}
	}
	if ip, ok := publicIP(" 195.177.93.114\n"); !ok || ip != "195.177.93.114" {
		t.Fatal("a public address must be accepted")
	}
}

func TestWatcherRemeasuresAfterReconnectAndWhenBigPacketsStop(t *testing.T) {
	v := NewVPN(t.TempDir())
	v.Interval = 20 * time.Millisecond
	f := newFake(1368)
	f.attach(v)
	v.ExitLookup = func(dev, src string) (string, error) { return "195.177.93.114", nil }
	ctx, cancel := context.WithCancel(context.Background())
	reqs := make(chan sizeReq)
	v.tunnels = map[string]TunnelInfo{}
	v.wg.Add(1)
	go v.watchTunnel(ctx, SizeSpec{Name: "tun2"}, reqs)
	defer func() { cancel(); v.wg.Wait() }()

	wait := func(cond func() bool, what string) {
		t.Helper()
		for i := 0; i < 300; i++ {
			if cond() {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("timed out waiting for %s", what)
	}
	wait(func() bool { return v.info("tun2").MTU == 1360 && v.info("tun2").ExitIP != "" }, "the first measurement")

	// The tunnel drops and comes back as a new device with the default size again.
	f.mu.Lock()
	f.index, f.mtu = 8, 1500
	f.mu.Unlock()
	wait(func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		return f.mtu == 1360 && len(f.sets) >= 2
	}, "the size to be set again after a reconnect")

	// The path gets worse without the tunnel restarting: a big packet stops getting through.
	f.mu.Lock()
	f.limit = 1300
	f.mu.Unlock()
	wait(func() bool { return v.info("tun2").MTU == 1300 }, "a new measurement after the path changed")
}

func TestSizeTestOnDemandReportsAndApplies(t *testing.T) {
	v := NewVPN(t.TempDir())
	v.Interval = time.Hour
	f := newFake(1368)
	f.attach(v)
	v.ExitLookup = func(dev, src string) (string, error) { return "195.177.93.114", nil }
	ctx, cancel := context.WithCancel(context.Background())
	reqs := make(chan sizeReq)
	v.reqs["tun2"] = reqs
	v.wg.Add(1)
	go v.watchTunnel(ctx, SizeSpec{Name: "tun2"}, reqs)
	defer func() { cancel(); v.wg.Wait() }()
	time.Sleep(100 * time.Millisecond)
	f.mu.Lock()
	f.limit, f.mtu = 1400, 1300
	f.mu.Unlock()
	rep, err := v.SizeTest("tun2")
	if err != nil || rep.After != 1400 || rep.Before != 1300 || len(rep.Rows) == 0 || rep.Rows[0].Size < rep.Rows[len(rep.Rows)-1].Size {
		t.Fatalf("got %+v err=%v", rep, err)
	}
	if _, err := v.SizeTest("tun9"); err == nil {
		t.Fatal("an unknown tunnel must be refused")
	}
}

func TestValidateSizeLists(t *testing.T) {
	v := NewVPN(t.TempDir())
	ok := VPNPlan{Sizes: []SizeSpec{{Name: "tun2"}, {Name: "wg0", Check: "1.1.1.1"}}, PingAddrs: []string{"1.1.1.1"}, ExitSources: DefaultExitSources}
	if err := v.validate(ok); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []VPNPlan{
		{Sizes: []SizeSpec{{Name: "eth0"}}},
		{PingAddrs: []string{"example.com"}},
		{ExitSources: []string{"web:http://insecure.example"}},
		{ExitSources: []string{"web:https://user@host.example"}},
		{ExitSources: []string{"file:///etc/passwd"}},
	} {
		if err := v.validate(bad); err == nil {
			t.Fatalf("should refuse %+v", bad)
		}
	}
}

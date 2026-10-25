package netd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gatehouse/internal/netcfg"
	"gatehouse/internal/speedtest"
	"gatehouse/internal/wanmon"
)

func newSvc(t *testing.T) *Service {
	d := t.TempDir()
	in := &netcfg.Installer{
		NetworkdDir: filepath.Join(d, "network"), PPPDir: filepath.Join(d, "ppp/peers"),
		PPPSecrets: filepath.Join(d, "ppp/chap-secrets"), SysctlFile: filepath.Join(d, "sysctl.conf"),
		StateDir: filepath.Join(d, "state"), Now: time.Now,
		Reload:   func() error { return nil },
		Services: func(a, b []string) error { return nil },
	}
	os.MkdirAll(in.NetworkdDir, 0o755)
	return New(in, filepath.Join(d, "netd"))
}

func plan() Plan {
	return Plan{
		Config: netcfg.Config{
			Interfaces: []netcfg.Interface{{Name: "lan", Kind: "phys", Parent: "eth1", IPv4: "192.168.1.1/24", Enabled: true}},
			WANs:       []netcfg.WAN{{Name: "isp", Iface: "eth0", Type: "dhcp", Enabled: true, Priority: 1}},
		},
		Mode:  "failover",
		Links: []LinkSpec{{Name: "isp", Dev: "eth0", Table: 100, Mark: 0x100, Kind: "icmp", Target: "1.1.1.1"}},
	}
}

func TestApplyConfirmPersists(t *testing.T) {
	s := newSvc(t)
	if err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	tok, _, err := s.Apply(plan(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.path("monitor.json")); err == nil {
		t.Fatal("monitor settings saved before confirmation")
	}
	if err := s.Confirm(tok); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.path("monitor.json")); err != nil {
		t.Fatal("monitor settings not saved")
	}
}

func TestRejectsBadPlans(t *testing.T) {
	s := newSvc(t)
	p := plan()
	p.Mode = "yolo"
	if _, _, err := s.Apply(p, time.Minute); err == nil {
		t.Fatal("bad mode accepted")
	}
	p = plan()
	p.Links[0].Table = 5
	if _, _, err := s.Apply(p, time.Minute); err == nil {
		t.Fatal("bad table accepted")
	}
	p = plan()
	p.Config.Interfaces[0].IPv4 = "nonsense"
	if _, _, err := s.Apply(p, time.Minute); err == nil {
		t.Fatal("bad address accepted")
	}
}

func TestUnconfirmedRollsBackOnBoot(t *testing.T) {
	s := newSvc(t)
	if _, _, err := s.Apply(plan(), time.Minute); err != nil {
		t.Fatal(err)
	}
	glob := filepath.Join(s.Inst.NetworkdDir, "gatehouse-*.network")
	if m, _ := filepath.Glob(glob); len(m) == 0 {
		t.Fatal("not installed")
	}
	s2 := New(s.Inst, s.StateDir)
	s2.Start(context.Background())
	defer s2.Stop()
	if m, _ := filepath.Glob(glob); len(m) != 0 {
		t.Fatal("unconfirmed change survived a restart")
	}
}

type okChecker struct{}

func (okChecker) Check(ctx context.Context, l wanmon.Link) (time.Duration, error) {
	return 7 * time.Millisecond, nil
}

// touchRouter fails the test if the monitor changes any route.
type touchRouter struct{ t *testing.T }

func (r touchRouter) Gateway(l wanmon.Link) (string, error) { return "192.0.2.1", nil }
func (r touchRouter) SetDefault(nh []wanmon.Nexthop) error {
	r.t.Error("a single connection's route must be left to systemd-networkd")
	return nil
}
func (r touchRouter) EnsureLink(l wanmon.Link, gw string) error {
	r.t.Error("a single connection must not get its own table")
	return nil
}
func (r touchRouter) RemoveLink(l wanmon.Link) error { return nil }

func TestSingleConnectionIsWatchedButNeverRouted(t *testing.T) {
	s := newSvc(t)
	s.NewMon = func(l []wanmon.Link, m wanmon.Mode) *wanmon.Monitor {
		return wanmon.New(l, m, okChecker{}, touchRouter{t})
	}
	if err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	tok, _, err := s.Apply(plan(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Confirm(tok); err != nil {
		t.Fatal(err)
	}
	var st wanmon.State
	for i := 0; i < 100; i++ {
		st = s.Status()["isp"]
		if st.Checked {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !st.Checked || st.Health != "working" || st.RTT != 7*time.Millisecond {
		t.Fatalf("a single connection should be measured: %+v", st)
	}
}

func TestSpeedTestRefusesBadOrMissingDevices(t *testing.T) {
	s := &Service{}
	for _, dev := range []string{"", "eth0;rm", "this-port-does-not-exist"} {
		if _, err := s.SpeedTest(context.Background(), speedtest.Params{Dev: dev, Server: "https://127.0.0.1:1"}); err == nil {
			t.Errorf("%q should be refused", dev)
		}
	}
	// One test at a time.
	speedMu.Lock()
	defer speedMu.Unlock()
	if _, err := s.SpeedTest(context.Background(), speedtest.Params{Dev: "lo", Server: "https://127.0.0.1:1"}); err == nil || !strings.Contains(err.Error(), "another speed test") {
		t.Fatalf("want busy, got %v", err)
	}
}

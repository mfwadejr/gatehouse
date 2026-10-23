package netd

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gatehouse/internal/netcfg"
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

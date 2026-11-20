package netd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeIP stands in for the ip and networkctl tools: it answers the questions the watcher asks and
// writes down every change it is told to make.
type fakeIP struct {
	addr    string // what `ip -o addr show` prints
	rules   string // what `ip rule show` prints
	routes  string // what `ip route show table 195 default` prints
	changes []string
}

func (f *fakeIP) run(_ time.Duration, name string, args ...string) (string, error) {
	cmd := name + " " + strings.Join(args, " ")
	switch {
	case strings.HasPrefix(cmd, "ip -4 -o addr show"):
		return f.addr, nil
	case cmd == "ip -4 rule show":
		return f.rules, nil
	case strings.HasPrefix(cmd, "ip -4 route show table 195"):
		return f.routes, nil
	}
	f.changes = append(f.changes, cmd)
	return "", nil
}

func newMgmtSvc(t *testing.T, plan MgmtPlan) *Service {
	t.Helper()
	body, err := MgmtNetwork(plan)
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{Root: t.TempDir()}
	p := s.rootPath(mgmtNetFile)
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, []byte(body), 0o644)
	os.MkdirAll(filepath.Join(s.Root, "sys/class/net/eth2"), 0o755)
	os.WriteFile(filepath.Join(s.Root, "sys/class/net/eth2/ifindex"), []byte("7\n"), 0o644)
	os.MkdirAll(filepath.Join(s.Root, "run/systemd/netif/leases"), 0o755)
	return s
}

func withFake(t *testing.T, f *fakeIP) {
	t.Helper()
	old := sysRun
	sysRun = f.run
	t.Cleanup(func() { sysRun = old })
}

func TestMgmtDHCPFileKeepsTheRouterInItsOwnTableOnly(t *testing.T) {
	b, err := MgmtNetwork(MgmtPlan{Dev: "eth2", Mode: "dhcp"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"DHCP=ipv4", "RouteTable=195", "UseGateway=yes", "UseRoutes=yes", "UseDNS=no"} {
		if !strings.Contains(b, want) {
			t.Errorf("the DHCP file is missing %q:\n%s", want, b)
		}
	}
	// The router must never reach the main table: every route from the lease goes to table 195.
	if strings.Contains(b, "UseGateway=no") {
		t.Error("the router from the lease is thrown away, so other subnets cannot be answered")
	}
}

func TestMgmtStaticFileStillHasItsOwnRule(t *testing.T) {
	b, err := MgmtNetwork(MgmtPlan{Dev: "eth2", Mode: "static", CIDR: "192.168.2.69/24", Gateway: "192.168.2.1"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b, "From=192.168.2.69/32") || !strings.Contains(b, "Gateway=192.168.2.1") {
		t.Fatalf("static file lost its rule or gateway:\n%s", b)
	}
}

func TestMgmtSyncAnswersOtherSubnetsFromTheDHCPAddress(t *testing.T) {
	s := newMgmtSvc(t, MgmtPlan{Dev: "eth2", Mode: "dhcp"})
	f := &fakeIP{addr: "3: eth2    inet 192.168.2.69/24 brd 192.168.2.255 scope global dynamic eth2\n"}
	withFake(t, f)
	os.WriteFile(filepath.Join(s.Root, "run/systemd/netif/leases/7"), []byte("ADDRESS=192.168.2.69\nROUTER=192.168.2.1\nSERVER_ADDRESS=192.168.2.1\n"), 0o644)
	s.mgmtSync()
	got := strings.Join(f.changes, "\n")
	for _, want := range []string{
		"ip -4 rule add pref 100 from 192.168.2.69/32 lookup 195",
		"ip -4 route replace 192.168.2.0/24 dev eth2 scope link table 195",
		"ip -4 route replace default via 192.168.2.1 dev eth2 table 195",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "table main") || strings.Contains(got, "default via 192.168.2.1 dev eth2\n") {
		t.Errorf("the router leaked into the main table:\n%s", got)
	}
}

func TestMgmtSyncFollowsALeaseThatChangesAndDoesNotRepeatItself(t *testing.T) {
	s := newMgmtSvc(t, MgmtPlan{Dev: "eth2", Mode: "dhcp"})
	f := &fakeIP{
		addr:   "3: eth2    inet 192.168.2.70/24 brd 192.168.2.255 scope global dynamic eth2\n",
		rules:  "100:\tfrom 192.168.2.69 lookup 195\n0:\tfrom all lookup local\n32766:\tfrom all lookup main\n",
		routes: "default via 192.168.2.1 dev eth2 proto dhcp\n",
	}
	withFake(t, f)
	s.mgmtSync()
	got := strings.Join(f.changes, "\n")
	if !strings.Contains(got, "ip -4 rule del pref 100 from 192.168.2.69 lookup 195") || !strings.Contains(got, "ip -4 rule add pref 100 from 192.168.2.70/32 lookup 195") {
		t.Fatalf("the rule did not follow the new address:\n%s", got)
	}
	// Now the rule is right: nothing is added again.
	f.rules = "100:\tfrom 192.168.2.70 lookup 195\n0:\tfrom all lookup local\n"
	f.changes = nil
	s.mgmtSync()
	for _, c := range f.changes {
		if strings.Contains(c, "rule add") || strings.Contains(c, "rule del") {
			t.Errorf("a correct rule was touched: %s", c)
		}
	}
}

func TestMgmtSyncWithoutAnAddressLeavesNoStaleRule(t *testing.T) {
	s := newMgmtSvc(t, MgmtPlan{Dev: "eth2", Mode: "dhcp"})
	f := &fakeIP{rules: "100:\tfrom 192.168.2.69 lookup 195\n"}
	withFake(t, f)
	s.mgmtSync()
	got := strings.Join(f.changes, "\n")
	if !strings.Contains(got, "rule del pref 100 from 192.168.2.69 lookup 195") || strings.Contains(got, "rule add") {
		t.Fatalf("a port with no address must not keep a rule for an old one:\n%s", got)
	}
}

func TestMgmtSyncLeavesAFixedAddressToTheNetworkFile(t *testing.T) {
	s := newMgmtSvc(t, MgmtPlan{Dev: "eth2", Mode: "static", CIDR: "192.168.2.69/24", Gateway: "192.168.2.1"})
	f := &fakeIP{addr: "3: eth2    inet 192.168.2.69/24 scope global eth2\n"}
	withFake(t, f)
	s.mgmtSync()
	if len(f.changes) != 0 {
		t.Fatalf("the watcher changed something for a fixed address: %v", f.changes)
	}
}

func TestMgmtParsers(t *testing.T) {
	if rs := mgmtRules("100:\tfrom 10.0.0.5 lookup 195\n200:\tfrom all lookup 195\n300:\tfrom 10.0.0.9 lookup 100\n"); len(rs) != 2 {
		t.Errorf("rules: %+v", rs)
	}
	if routerOf("ADDRESS=1.2.3.4\nROUTER=10.0.0.1 10.0.0.2\n") != "10.0.0.1" {
		t.Error("lease router")
	}
	if routerOf("default via 10.0.0.1 dev eth2 proto dhcp\n") != "10.0.0.1" {
		t.Error("route router")
	}
	if routerOf("nothing here") != "" {
		t.Error("no router expected")
	}
}

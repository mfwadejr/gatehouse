package wanmon

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func sh(t *testing.T, ns, cmd string) string {
	t.Helper()
	var c *exec.Cmd
	if ns == "" {
		c = exec.Command("sh", "-c", cmd)
	} else {
		c = exec.Command("ip", "netns", "exec", ns, "sh", "-c", cmd)
	}
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("%s: %s: %v\n%s", ns, cmd, err, out)
	}
	return strings.TrimSpace(string(out))
}

func needRoot(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("needs root")
	}
	if _, err := exec.LookPath("ip"); err != nil {
		t.Skip("needs ip")
	}
}

// inNS runs f on a thread that has joined the namespace.
func inNS(name string, f func()) error {
	var err error
	done := make(chan struct{})
	go func() {
		defer close(done)
		runtime.LockOSThread() // never unlocked: the thread is discarded with the goroutine
		fd, e := unix.Open("/var/run/netns/"+name, unix.O_RDONLY, 0)
		if e != nil {
			err = e
			return
		}
		defer unix.Close(fd)
		if e := unix.Setns(fd, unix.CLONE_NEWNET); e != nil {
			err = e
			return
		}
		f()
	}()
	<-done
	return err
}

type nsChecker struct{ ns string }

func (c nsChecker) Check(ctx context.Context, l Link) (rtt time.Duration, err error) {
	if e := inNS(c.ns, func() { rtt, err = NetChecker{}.Check(ctx, l) }); e != nil {
		return 0, e
	}
	return
}

func TestNetnsFailoverWithRealRoutesAndICMP(t *testing.T) {
	needRoot(t)
	nss := []string{"gwrouter", "gwisp1", "gwisp2", "gwlan"}
	clean := func() {
		for _, n := range nss {
			exec.Command("ip", "netns", "del", n).Run()
		}
	}
	clean()
	t.Cleanup(clean)
	for _, n := range nss {
		sh(t, "", "ip netns add "+n)
		sh(t, n, "ip link set lo up")
	}
	legs := []struct{ peer, rif, rip, pip string }{
		{"gwisp1", "eth0", "203.0.113.2/24", "203.0.113.1/24"},
		{"gwisp2", "eth1", "198.51.100.2/24", "198.51.100.1/24"},
		{"gwlan", "eth2", "192.168.1.1/24", "192.168.1.50/24"},
	}
	for i, g := range legs {
		a, b := fmt.Sprintf("gw-r%d", i), fmt.Sprintf("gw-p%d", i)
		sh(t, "", fmt.Sprintf("ip link add %s type veth peer name %s && ip link set %s netns gwrouter name %s && ip link set %s netns %s name eth0", a, b, a, g.rif, b, g.peer))
		sh(t, "gwrouter", fmt.Sprintf("ip addr add %s dev %s && ip link set %s up", g.rip, g.rif, g.rif))
		sh(t, g.peer, fmt.Sprintf("ip addr add %s dev eth0 && ip link set eth0 up", g.pip))
	}
	// Both providers can reach the probe target 192.0.2.7.
	for _, isp := range []string{"gwisp1", "gwisp2"} {
		sh(t, isp, "ip addr add 192.0.2.7/32 dev lo")
	}
	sh(t, "gwrouter", "echo 1 > /proc/sys/net/ipv4/ip_forward")
	// The router starts with the gateways only inside each link's own table, as networkd would configure.
	sh(t, "gwrouter", "ip route add default via 203.0.113.1 dev eth0 table 100 && ip route add default via 198.51.100.1 dev eth1 table 101")

	ls := []Link{
		{Name: "Fibre", Dev: "eth0", Priority: 1, Weight: 1, Table: 100, Mark: 0x100, Kind: "icmp", Target: "192.0.2.7", Interval: 200 * time.Millisecond, Failures: 3, Recover: 2},
		{Name: "LTE", Dev: "eth1", Priority: 2, Weight: 1, Table: 101, Mark: 0x101, Kind: "icmp", Target: "192.0.2.7", Interval: 200 * time.Millisecond, Failures: 3, Recover: 2},
	}
	router := &IPRouter{Netns: "gwrouter"}
	m := New(ls, Failover, nsChecker{"gwrouter"}, router)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go m.Run(ctx)

	routeVia := func() string {
		return sh(t, "gwrouter", "ip route get 8.8.8.8 | head -1")
	}
	waitFor := func(what, want string, d time.Duration) {
		t.Helper()
		end := time.Now().Add(d)
		var got string
		for time.Now().Before(end) {
			got = routeVia()
			if strings.Contains(got, want) {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatalf("%s: wanted %q in %q; states %+v", what, want, got, m.Snapshot())
	}
	waitFor("start on fibre", "dev eth0", 5*time.Second)
	if st := m.Snapshot()["Fibre"]; !st.Up || st.RTT == 0 {
		t.Fatalf("fibre should be up with a measured round trip: %+v", st)
	}
	// The fwmark rule exists, so marked replies can leave through their own link.
	if rules := sh(t, "gwrouter", "ip rule show"); !strings.Contains(rules, "fwmark 0x101 lookup 101") {
		t.Fatalf("missing policy rule:\n%s", rules)
	}

	// Fibre's upstream stops answering: the probe target disappears from that provider.
	sh(t, "gwisp1", "ip addr del 192.0.2.7/32 dev lo")
	waitFor("failover to lte", "dev eth1", 14*time.Second)
	if st := m.Snapshot(); st["Fibre"].Up || !st["LTE"].Active {
		t.Fatalf("state after failure: %+v", st)
	}
	// It comes back and traffic returns to the preferred link.
	sh(t, "gwisp1", "ip addr add 192.0.2.7/32 dev lo")
	waitFor("return to fibre", "dev eth0", 6*time.Second)
}

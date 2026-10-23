package fw

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// These tests build a small network out of Linux namespaces and send real packets through
// the generated ruleset. They need root, ip, nft and python3, and skip themselves otherwise.

type lab struct {
	t     *testing.T
	procs []*exec.Cmd
}

func need(t *testing.T) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("needs root")
	}
	for _, b := range []string{"ip", "nft", "python3", "unshare"} {
		if _, err := exec.LookPath(b); err != nil {
			t.Skipf("needs %s", b)
		}
	}
}

func (l *lab) sh(ns, cmd string) (string, error) {
	var c *exec.Cmd
	if ns == "" {
		c = exec.Command("sh", "-c", cmd)
	} else {
		c = exec.Command("ip", "netns", "exec", ns, "sh", "-c", cmd)
	}
	b, err := c.CombinedOutput()
	return strings.TrimSpace(string(b)), err
}

func (l *lab) must(ns, cmd string) {
	l.t.Helper()
	if out, err := l.sh(ns, cmd); err != nil {
		l.t.Fatalf("%s: %s: %v\n%s", ns, cmd, err, out)
	}
}

// listen starts a TCP listener that answers "hi" in a namespace.
func (l *lab) listen(ns, addr string, port int) {
	l.t.Helper()
	py := fmt.Sprintf(`import socket,threading
s=socket.socket(); s.setsockopt(socket.SOL_SOCKET,socket.SO_REUSEADDR,1); s.bind(("%s",%d)); s.listen(16)
while True:
    c,_=s.accept(); c.sendall(b"hi"); c.close()`, addr, port)
	cmd := exec.Command("ip", "netns", "exec", ns, "python3", "-c", py)
	if err := cmd.Start(); err != nil {
		l.t.Fatal(err)
	}
	l.procs = append(l.procs, cmd)
}

func (l *lab) udpEcho(ns, addr string, port int) {
	l.t.Helper()
	py := fmt.Sprintf(`import socket
s=socket.socket(socket.AF_INET,socket.SOCK_DGRAM); s.bind(("%s",%d))
while True:
    d,a=s.recvfrom(512); s.sendto(b"router",a)`, addr, port)
	cmd := exec.Command("ip", "netns", "exec", ns, "python3", "-c", py)
	if err := cmd.Start(); err != nil {
		l.t.Fatal(err)
	}
	l.procs = append(l.procs, cmd)
}

// reach reports whether a TCP connection from ns to addr:port succeeds.
func (l *lab) reach(ns, addr string, port int) bool {
	py := fmt.Sprintf(`import socket,sys
s=socket.socket(); s.settimeout(1.2)
try:
    s.connect(("%s",%d)); sys.exit(0 if s.recv(2)==b"hi" else 1)
except Exception:
    sys.exit(1)`, addr, port)
	return exec.Command("ip", "netns", "exec", ns, "python3", "-c", py).Run() == nil
}

func (l *lab) udpAsk(ns, addr string, port int) string {
	py := fmt.Sprintf(`import socket,sys
s=socket.socket(socket.AF_INET,socket.SOCK_DGRAM); s.settimeout(1.2)
try:
    s.sendto(b"q",("%s",%d)); print(s.recvfrom(64)[0].decode())
except Exception:
    print("none")`, addr, port)
	out, _ := exec.Command("ip", "netns", "exec", ns, "python3", "-c", py).Output()
	return strings.TrimSpace(string(out))
}

func (l *lab) apply(m *Model) {
	l.t.Helper()
	script, err := Compile(m)
	if err != nil {
		l.t.Fatal(err)
	}
	f := filepath.Join(l.t.TempDir(), "r.nft")
	os.WriteFile(f, []byte(script), 0o600)
	if out, err := l.sh("ghfw", "nft -f "+f); err != nil {
		l.t.Fatalf("apply: %v\n%s\n%s", err, out, script)
	}
}

func newLab(t *testing.T) *lab {
	need(t)
	l := &lab{t: t}
	nss := []string{"ghfw", "ghinet", "ghhome", "ghguest", "ghcam"}
	cleanup := func() {
		for _, p := range l.procs {
			p.Process.Kill()
			p.Wait()
		}
		for _, n := range nss {
			exec.Command("ip", "netns", "del", n).Run()
		}
	}
	cleanup()
	t.Cleanup(cleanup)
	for _, n := range nss {
		l.must("", "ip netns add "+n)
		l.must(n, "ip link set lo up")
	}
	// Router ports are eth0 (internet), eth1 (home), eth2 (guest), eth3 (cameras).
	type leg struct{ peerNS, routerIf, peerIP, routerIP, peerGW string }
	legs := []leg{
		{"ghinet", "eth0", "203.0.113.1/24", "203.0.113.2/24", ""},
		{"ghhome", "eth1", "192.168.1.50/24", "192.168.1.1/24", "192.168.1.1"},
		{"ghguest", "eth2", "192.168.2.50/24", "192.168.2.1/24", "192.168.2.1"},
		{"ghcam", "eth3", "192.168.3.50/24", "192.168.3.1/24", "192.168.3.1"},
	}
	for i, g := range legs {
		a, b := fmt.Sprintf("gh-r%d", i), fmt.Sprintf("gh-p%d", i)
		l.must("", fmt.Sprintf("ip link add %s type veth peer name %s", a, b))
		l.must("", fmt.Sprintf("ip link set %s netns ghfw name %s", a, g.routerIf))
		l.must("", fmt.Sprintf("ip link set %s netns %s name eth0", b, g.peerNS))
		l.must("ghfw", fmt.Sprintf("ip addr add %s dev %s && ip link set %s up", g.routerIP, g.routerIf, g.routerIf))
		l.must(g.peerNS, fmt.Sprintf("ip addr add %s dev eth0 && ip link set eth0 up", g.peerIP))
		if g.peerGW != "" {
			l.must(g.peerNS, "ip route add default via "+g.peerGW)
		}
	}
	// The "internet" host routes replies for the NATed source back through the router and
	// also knows how to answer if packets arrive for its own addresses.
	l.must("ghinet", "ip addr add 198.51.100.7/32 dev lo")
	l.must("ghfw", "echo 1 > /proc/sys/net/ipv4/ip_forward")
	l.must("ghfw", "ip route add 198.51.100.7/32 via 203.0.113.1")
	for _, n := range []string{"ghhome", "ghguest", "ghcam"} {
		l.must(n, "ip route add 198.51.100.7/32 via "+map[string]string{"ghhome": "192.168.1.1", "ghguest": "192.168.2.1", "ghcam": "192.168.3.1"}[n])
	}
	return l
}

func labModel() *Model {
	m := DefaultModel([]string{"eth0"}, []string{"eth1"})
	guest := Zone{Name: "Guests", Kind: KindGuest, Ifaces: []string{"eth2"}}
	ApplyPreset(&guest)
	cam := Zone{Name: "Cameras", Kind: KindIsolated, Ifaces: []string{"eth3"}}
	ApplyPreset(&cam)
	m.Zones = append(m.Zones, guest, cam)
	m.Rules = []Rule{
		{Name: "Guests print", From: "Guests", To: "Home", Dst: "192.168.1.50", Service: "tcp/631", Action: "accept", Enabled: true},
		{Name: "Cameras to NVR", From: "Cameras", To: "Home", Dst: "192.168.1.50", Service: "tcp/554", Action: "accept", Enabled: true},
	}
	m.Forwards = []Forward{{Name: "Web server", Proto: "tcp", ExtPort: "8443", IntAddr: "192.168.1.50", IntPort: "443", Enabled: true}}
	return m
}

func TestNetnsPolicy(t *testing.T) {
	l := newLab(t)
	// Services: the internet host, a home server with several ports, and the router's console.
	for _, p := range []int{80, 443} {
		l.listen("ghinet", "0.0.0.0", p)
	}
	for _, p := range []int{443, 554, 631, 8080} {
		l.listen("ghhome", "0.0.0.0", p)
	}
	l.listen("ghguest", "0.0.0.0", 9000)
	l.listen("ghfw", "0.0.0.0", 443)
	l.listen("ghfw", "0.0.0.0", 8080) // something else on the router that must stay closed
	l.udpEcho("ghfw", "0.0.0.0", 53)
	time.Sleep(700 * time.Millisecond)

	// Sanity: before any rules everything on the same L2 works, so later failures come from the ruleset.
	l.apply(labModel())

	type check struct {
		name string
		got  func() bool
		want bool
	}
	checks := []check{
		{"home to internet", func() bool { return l.reach("ghhome", "198.51.100.7", 80) }, true},
		{"guest to internet", func() bool { return l.reach("ghguest", "198.51.100.7", 80) }, true},
		{"camera to internet", func() bool { return l.reach("ghcam", "198.51.100.7", 80) }, false},
		{"home to guest network", func() bool { return l.reach("ghhome", "192.168.2.50", 9000) }, true},
		{"guest to home server", func() bool { return l.reach("ghguest", "192.168.1.50", 8080) }, false},
		{"guest to printer port (exception)", func() bool { return l.reach("ghguest", "192.168.1.50", 631) }, true},
		{"camera to nvr (exception)", func() bool { return l.reach("ghcam", "192.168.1.50", 554) }, true},
		{"camera to other home port", func() bool { return l.reach("ghcam", "192.168.1.50", 8080) }, false},
		{"home to router console", func() bool { return l.reach("ghhome", "192.168.1.1", 443) }, true},
		{"guest to router console", func() bool { return l.reach("ghguest", "192.168.2.1", 443) }, false},
		{"camera to router console", func() bool { return l.reach("ghcam", "192.168.3.1", 443) }, false},
		{"home to other router port", func() bool { return l.reach("ghhome", "192.168.1.1", 8080) }, false},
		{"internet to router console", func() bool { return l.reach("ghinet", "203.0.113.2", 443) }, false},
		{"internet to home server", func() bool { return l.reach("ghinet", "192.168.1.50", 8080) }, false},
		{"internet to forwarded port", func() bool { return l.reach("ghinet", "203.0.113.2", 8443) }, true},
		{"internet to unforwarded port", func() bool { return l.reach("ghinet", "203.0.113.2", 8444) }, false},
		{"guest dns is redirected to the router", func() bool { return l.udpAsk("ghguest", "8.8.8.8", 53) == "router" }, true},
	}
	for _, c := range checks {
		if got := c.got(); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}

	// A second apply replaces the first atomically and leaves other tables alone.
	l.must("ghfw", "nft add table ip bystander")
	l.apply(labModel())
	if out, _ := l.sh("ghfw", "nft list tables"); !strings.Contains(out, "bystander") || strings.Count(out, "gatehouse") != 1 {
		t.Errorf("unexpected tables after re-apply:\n%s", out)
	}
	if !l.reach("ghhome", "192.168.1.1", 443) {
		t.Error("console unreachable after re-apply")
	}

	// Changing a network's type takes effect: make Guests Trusted-like by allowing it to reach home.
	m := labModel()
	m.Rules = append(m.Rules, Rule{Name: "Open home", From: "Guests", To: "Home", Service: "any", Action: "accept", Enabled: true})
	l.apply(m)
	if !l.reach("ghguest", "192.168.1.50", 8080) {
		t.Error("new exception did not open access")
	}
	// ...and a drop exception placed first wins over the preset that allows the internet.
	m = labModel()
	m.Rules = append([]Rule{{Name: "Block web", From: "Home", To: "wan", Service: "http", Action: "drop", Enabled: true}}, m.Rules...)
	l.apply(m)
	if l.reach("ghhome", "198.51.100.7", 80) {
		t.Error("explicit drop did not override the preset")
	}
	if !l.reach("ghhome", "198.51.100.7", 443) {
		t.Error("drop of http also blocked https")
	}
}

// A model that would lock the administrator out is refused before anything changes.
func TestNetnsRefusesLockout(t *testing.T) {
	l := newLab(t)
	l.listen("ghfw", "0.0.0.0", 443)
	time.Sleep(500 * time.Millisecond)
	l.apply(labModel())
	if !l.reach("ghhome", "192.168.1.1", 443) {
		t.Fatal("console should be reachable")
	}
	bad := labModel()
	bad.Zones[0].Management = false
	if _, err := Compile(bad); err == nil {
		t.Fatal("lockout model compiled")
	}
	// The running ruleset is untouched.
	if !l.reach("ghhome", "192.168.1.1", 443) {
		t.Fatal("console lost after refused change")
	}
	// Even a ruleset that tries to drop everything in an explicit rule cannot cut the console.
	m := labModel()
	m.Rules = append([]Rule{{Name: "Drop all", From: "Home", To: "self", Service: "any", Action: "drop", Enabled: true}}, m.Rules...)
	l.apply(m)
	if !l.reach("ghhome", "192.168.1.1", 443) {
		t.Fatal("an editable rule was able to block the management console")
	}
}

// With two internet connections, a request that arrives on the second one must be answered
// through the second one even though the default route points at the first.
func TestNetnsRepliesLeaveByIngressLink(t *testing.T) {
	need(t)
	nss := []string{"mwfw", "mwisp1", "mwisp2", "mwhome"}
	clean := func() {
		for _, n := range nss {
			exec.Command("ip", "netns", "del", n).Run()
		}
	}
	clean()
	t.Cleanup(clean)
	l := &lab{t: t}
	for _, n := range nss {
		l.must("", "ip netns add "+n)
		l.must(n, "ip link set lo up")
	}
	legs := []struct{ peer, rif, rip, pip, gw string }{
		{"mwisp1", "eth0", "203.0.113.2/24", "203.0.113.1/24", ""},
		{"mwisp2", "eth4", "203.0.114.2/24", "203.0.114.1/24", ""},
		{"mwhome", "eth1", "192.168.1.1/24", "192.168.1.50/24", "192.168.1.1"},
	}
	for i, g := range legs {
		a, b := fmt.Sprintf("mw-r%d", i), fmt.Sprintf("mw-p%d", i)
		l.must("", fmt.Sprintf("ip link add %s type veth peer name %s && ip link set %s netns mwfw name %s && ip link set %s netns %s name eth0", a, b, a, g.rif, b, g.peer))
		l.must("mwfw", fmt.Sprintf("ip addr add %s dev %s && ip link set %s up", g.rip, g.rif, g.rif))
		l.must(g.peer, fmt.Sprintf("ip addr add %s dev eth0 && ip link set eth0 up", g.pip))
		if g.gw != "" {
			l.must(g.peer, "ip route add default via "+g.gw)
		}
	}
	l.must("mwfw", "echo 1 > /proc/sys/net/ipv4/ip_forward")
	// The default route points at the first connection.
	l.must("mwfw", "ip route add default via 203.0.113.1 dev eth0")
	// Per-link table and mark rule for the second connection (what the routing monitor installs).
	l.must("mwfw", "ip route add default via 203.0.114.1 dev eth4 table 101 && ip rule add fwmark 0x101 lookup 101 priority 1101")
	l.must("mwfw", "ip route add default via 203.0.113.1 dev eth0 table 100 && ip rule add fwmark 0x100 lookup 100 priority 1100")

	m := DefaultModel([]string{"eth0", "eth4"}, []string{"eth1"})
	m.Forwards = []Forward{
		{Name: "Web", Proto: "tcp", ExtPort: "8443", IntAddr: "192.168.1.50", IntPort: "443", Enabled: true},
		{Name: "Web2", Proto: "tcp", ExtPort: "8444", IntAddr: "192.168.1.50", IntPort: "443", Enabled: true},
	}
	script, err := Compile(m)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(script, "ct mark set 0x101") {
		t.Fatalf("second link not marked:\n%s", script)
	}
	f := filepath.Join(t.TempDir(), "r.nft")
	os.WriteFile(f, []byte(script), 0o600)
	l.must("mwfw", "nft -f "+f)

	cmd := exec.Command("ip", "netns", "exec", "mwhome", "python3", "-c", `import socket
s=socket.socket(); s.setsockopt(socket.SOL_SOCKET,socket.SO_REUSEADDR,1); s.bind(("0.0.0.0",443)); s.listen(8)
while True:
    c,_=s.accept(); c.sendall(b"hi"); c.close()`)
	cmd.Start()
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	time.Sleep(600 * time.Millisecond)

	// Remote clients are not on the link's own subnet, as on the real internet, so replies need
	// the mark to find their way back.
	l.must("mwisp1", "ip addr add 192.0.2.91/32 dev lo")
	l.must("mwisp2", "ip addr add 192.0.2.92/32 dev lo")
	from := func(ns, src, dst string, port int) bool {
		py := fmt.Sprintf(`import socket,sys
s=socket.socket(); s.settimeout(1.5); s.bind(("%s",0))
try:
    s.connect(("%s",%d)); sys.exit(0 if s.recv(2)==b"hi" else 1)
except Exception:
    sys.exit(1)`, src, dst, port)
		return exec.Command("ip", "netns", "exec", ns, "python3", "-c", py).Run() == nil
	}
	if !from("mwisp1", "192.0.2.91", "203.0.113.2", 8443) {
		t.Error("request on the first link failed")
	}
	if !from("mwisp2", "192.0.2.92", "203.0.114.2", 8443) {
		t.Error("request on the second link was not answered through the second link")
	}
	// Control: with the marking removed, a brand new connection on the second link fails,
	// which proves the check above measures the marks and not luck.
	l.must("mwfw", "nft flush chain inet gatehouse wanmark")
	if from("mwisp2", "192.0.2.92", "203.0.114.2", 8444) {
		t.Error("control: the second link answered without marks, so the test proves nothing")
	}
}

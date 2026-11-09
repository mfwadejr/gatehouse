package installer

import (
	"context"
	"encoding/binary"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"nopewarden/internal/dhcp"
)

// fakeOffer builds the reply a server would send to a discover.
func fakeOffer(req []byte, server, give string, bits int, withMask bool) []byte {
	b := make([]byte, 240)
	b[0], b[1], b[2] = 2, 1, 6
	copy(b[4:8], req[4:8])
	copy(b[16:20], netip.MustParseAddr(give).AsSlice())
	copy(b[28:], req[28:34])
	binary.BigEndian.PutUint32(b[236:], 0x63825363)
	b = append(b, 53, 1, dhcp.Offer)
	b = append(b, 54, 4)
	b = append(b, netip.MustParseAddr(server).AsSlice()...)
	if withMask {
		b = append(b, 1, 4)
		b = append(b, net.CIDRMask(bits, 32)...)
	}
	b = append(b, 3, 4)
	b = append(b, netip.MustParseAddr(server).AsSlice()...)
	return append(b, 255)
}

func TestOffersFromTheProbe(t *testing.T) {
	got := offersFrom([]dhcp.RogueFinding{
		{Server: net.ParseIP("192.168.1.1"), Offer: net.ParseIP("192.168.1.155"), Mask: net.IPv4(255, 255, 255, 0)},
		{Server: net.ParseIP("10.0.0.1"), Offer: net.ParseIP("10.0.0.50")}, // no mask: heard, but names no network
	})
	if len(got) != 2 || got[0].Server != netip.MustParseAddr("192.168.1.1") {
		t.Fatalf("%+v", got)
	}
	if got[0].Given != netip.MustParsePrefix("192.168.1.155/24") || got[0].Network() != netip.MustParsePrefix("192.168.1.0/24") {
		t.Fatalf("given %v network %v", got[0].Given, got[0].Network())
	}
	if got[1].Network().IsValid() {
		t.Fatalf("an offer with no mask should name no network: %v", got[1].Network())
	}
}

func TestSharesServer(t *testing.T) {
	a := ProbeResult{Offers: []Offer{{Server: netip.MustParseAddr("192.168.1.1")}}}
	b := ProbeResult{Offers: []Offer{{Server: netip.MustParseAddr("192.168.1.1")}, {Server: netip.MustParseAddr("192.168.1.2")}}}
	c := ProbeResult{Offers: []Offer{{Server: netip.MustParseAddr("10.0.0.1")}}}
	if s, ok := SharesServer(a, b); !ok || s.String() != "192.168.1.1" {
		t.Fatal("a shared server was missed")
	}
	if _, ok := SharesServer(a, c); ok {
		t.Fatal("two different servers are not one network")
	}
	if _, ok := SharesServer(a, ProbeResult{}); ok {
		t.Fatal("nothing heard shares nothing")
	}
}

// listenServer opens the DHCP server port on one device, as a test server would.
func listenServer(dev string) (*net.UDPConn, error) {
	lc := net.ListenConfig{Control: func(_, _ string, c syscall.RawConn) error {
		var serr error
		if err := c.Control(func(fd uintptr) {
			for _, o := range []int{unix.SO_REUSEADDR, unix.SO_REUSEPORT, unix.SO_BROADCAST} {
				if serr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, o, 1); serr != nil {
					return
				}
			}
			serr = unix.SetsockoptString(int(fd), unix.SOL_SOCKET, unix.SO_BINDTODEVICE, dev)
		}); err != nil {
			return err
		}
		return serr
	}}
	pc, err := lc.ListenPacket(context.Background(), "udp4", ":67")
	if err != nil {
		return nil, err
	}
	return pc.(*net.UDPConn), nil
}

// TestDHCPHelper is not a test: the real-socket test below runs it inside a network namespace as a DHCP server.
func TestDHCPHelper(t *testing.T) {
	if os.Getenv("NW_DHCP_HELPER") == "" {
		t.Skip("helper process")
	}
	dev, server, give := os.Getenv("NW_DEV"), os.Getenv("NW_SERVER"), os.Getenv("NW_GIVE")
	c, err := listenServer(dev)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	os.Stdout.WriteString("ready\n")
	buf := make([]byte, 1500)
	c.SetDeadline(time.Now().Add(20 * time.Second))
	for {
		n, _, err := c.ReadFromUDP(buf)
		if err != nil {
			return
		}
		if n >= 240 && buf[0] == 1 {
			c.WriteToUDP(fakeOffer(buf[:n], server, give, 24, true), &net.UDPAddr{IP: net.IPv4bcast, Port: 68})
		}
	}
}

func sh(t *testing.T, args ...string) {
	t.Helper()
	if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
		t.Fatalf("%s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// TestProbeAgainstARealServer sends real packets over a virtual cable to a server in its own network
// namespace. It needs root and the ip command, and is skipped without them.
func TestProbeAgainstARealServer(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("needs root")
	}
	if _, err := exec.LookPath("ip"); err != nil {
		t.Skip("needs the ip command")
	}
	if exec.Command("ip", "netns", "add", "nwprobe").Run() != nil {
		t.Skip("cannot make a network namespace here")
	}
	t.Cleanup(func() {
		exec.Command("ip", "netns", "del", "nwprobe").Run()
		exec.Command("ip", "link", "del", "nwp0").Run()
	})
	sh(t, "ip", "link", "add", "nwp0", "type", "veth", "peer", "name", "nwp1")
	sh(t, "ip", "link", "set", "nwp1", "netns", "nwprobe")
	sh(t, "ip", "netns", "exec", "nwprobe", "ip", "addr", "add", "192.168.77.1/24", "dev", "nwp1")
	sh(t, "ip", "netns", "exec", "nwprobe", "ip", "link", "set", "nwp1", "up")
	sh(t, "ip", "link", "set", "nwp0", "up") // no address on this end, as on a port the installer has not set up

	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	srv := exec.Command("ip", "netns", "exec", "nwprobe", exe, "-test.run=^TestDHCPHelper$", "-test.v")
	srv.Env = append(os.Environ(), "NW_DHCP_HELPER=1", "NW_DEV=nwp1", "NW_SERVER=192.168.77.1", "NW_GIVE=192.168.77.50")
	out, _ := srv.StdoutPipe()
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Process.Kill(); srv.Wait() })
	buf := make([]byte, 256)
	for got := ""; !strings.Contains(got, "ready"); {
		n, err := out.Read(buf)
		if err != nil {
			t.Fatalf("the test server did not start: %q", got)
		}
		got += string(buf[:n])
	}
	time.Sleep(300 * time.Millisecond)

	res := ProbeDHCP("nwp0", 3*time.Second)
	if res.Err != nil {
		t.Fatalf("probe failed: %v", res.Err)
	}
	if len(res.Offers) != 1 || res.Offers[0].Server != netip.MustParseAddr("192.168.77.1") {
		t.Fatalf("offers: %+v", res.Offers)
	}
	if got := res.Offers[0].Network(); got != netip.MustParsePrefix("192.168.77.0/24") {
		t.Fatalf("network %v", got)
	}

	// A port nobody answers on: no offers and no error.
	sh(t, "ip", "link", "add", "nwq0", "type", "veth", "peer", "name", "nwq1")
	t.Cleanup(func() { exec.Command("ip", "link", "del", "nwq0").Run() })
	sh(t, "ip", "link", "set", "nwq0", "up")
	sh(t, "ip", "link", "set", "nwq1", "up")
	quiet := ProbeDHCP("nwq0", 2*time.Second)
	if quiet.Err != nil || quiet.Heard() {
		t.Fatalf("quiet port: %+v", quiet)
	}

	// A port that does not exist cannot be listened on; that is "could not check", not a crash.
	if bad := ProbeDHCP("nwnone0", time.Second); bad.Err == nil {
		t.Fatal("a missing port should report that it could not be checked")
	}
}

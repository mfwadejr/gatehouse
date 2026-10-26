package dhcp

import (
	"net"
	"os"
	"os/exec"
	"runtime"
	"testing"

	"golang.org/x/sys/unix"
)

// A real check: two ends of a virtual cable in a private network namespace. The kernel answers ARP
// for an address on one end, which stands in for a device that never answers pings.
func TestARPInUseOnAVirtualCable(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("needs root")
	}
	if _, err := exec.LookPath("ip"); err != nil {
		t.Skip("ip not installed")
	}
	// The thread is never unlocked: when this test ends the thread is thrown away, so no other test
	// ever runs in the private namespace.
	runtime.LockOSThread()
	if err := unix.Unshare(unix.CLONE_NEWNET); err != nil {
		t.Skip("cannot make a network namespace here: ", err)
	}
	for _, c := range [][]string{
		{"link", "add", "ghv0", "type", "veth", "peer", "name", "ghv1"},
		{"addr", "add", "10.77.0.1/24", "dev", "ghv0"},
		{"addr", "add", "10.77.0.2/24", "dev", "ghv1"},
		{"link", "set", "ghv0", "up"},
		{"link", "set", "ghv1", "up"},
	} {
		if b, err := exec.Command("ip", c...).CombinedOutput(); err != nil {
			t.Skipf("ip %v: %v %s", c, err, b)
		}
	}
	if !ARPInUse("ghv0", net.ParseIP("10.77.0.2")) {
		t.Error("an address that answers ARP was not found")
	}
	if ARPInUse("ghv0", net.ParseIP("10.77.0.99")) {
		t.Error("a free address was reported in use")
	}
}

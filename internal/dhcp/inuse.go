package dhcp

import (
	"context"
	"net"
	"os"
	"syscall"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/sys/unix"
)

// PingInUse asks one question: does anything on this port already answer at this address? It sends
// one echo request out of that port and waits a moment. A device that does not answer pings is not
// found, so this catches most clashes but not all of them.
func PingInUse(iface string, ip net.IP) bool {
	dst := ip.To4()
	if dst == nil {
		return false
	}
	lc := net.ListenConfig{Control: func(network, address string, c syscall.RawConn) error {
		var serr error
		if err := c.Control(func(fd uintptr) { serr = unix.SetsockoptString(int(fd), unix.SOL_SOCKET, unix.SO_BINDTODEVICE, iface) }); err != nil {
			return err
		}
		return serr
	}}
	conn, err := lc.ListenPacket(context.Background(), "ip4:icmp", "0.0.0.0")
	if err != nil {
		return false // no permission or no such port: do not hold up the offer
	}
	defer conn.Close()
	id := os.Getpid() & 0xffff
	b, err := (&icmp.Message{Type: ipv4.ICMPTypeEcho, Body: &icmp.Echo{ID: id, Seq: 1, Data: []byte("gatehouse")}}).Marshal(nil)
	if err != nil {
		return false
	}
	if _, err := conn.WriteTo(b, &net.IPAddr{IP: dst}); err != nil {
		return false
	}
	conn.SetReadDeadline(time.Now().Add(350 * time.Millisecond))
	buf := make([]byte, 1500)
	for {
		n, peer, err := conn.ReadFrom(buf)
		if err != nil {
			return false
		}
		m, err := icmp.ParseMessage(1, buf[:n])
		if err != nil || m.Type != ipv4.ICMPTypeEchoReply {
			continue
		}
		if e, ok := m.Body.(*icmp.Echo); ok && e.ID == id && peer.String() == dst.String() {
			return true
		}
	}
}

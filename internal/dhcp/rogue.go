package dhcp

import (
	"context"
	"fmt"
	"net"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// RogueFinding is another DHCP server seen on a segment.
type RogueFinding struct {
	Server net.IP `json:"server"`
	Offer  net.IP `json:"offer"`
	Mask   net.IP `json:"mask,omitempty"` // the network size the server offered with it, when it said
	MAC    string `json:"mac"`
}

// Probe broadcasts a DISCOVER on iface and returns every OFFER heard within wait, whoever sent it.
// Offers whose server identifier is one of ours (ignore) are skipped. It never completes a lease:
// no REQUEST is sent, so no address is taken. It binds port 68 on the interface, sharing it with any
// client already there; if it cannot bind, the call fails with an error instead of guessing.
func Probe(ctx context.Context, iface string, ignore []net.IP, wait time.Duration) ([]RogueFinding, error) {
	ifi, err := net.InterfaceByName(iface)
	if err != nil {
		return nil, err
	}
	lc := net.ListenConfig{Control: func(network, address string, c syscall.RawConn) error {
		var serr error
		if err := c.Control(func(fd uintptr) {
			// Share the client port with a DHCP client that may be running on this machine (the installer's
			// own network setup, for one), so listening does not depend on it being idle.
			for _, opt := range []int{unix.SO_REUSEADDR, unix.SO_REUSEPORT} {
				if serr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, opt, 1); serr != nil {
					return
				}
			}
			if serr = unix.SetsockoptString(int(fd), unix.SOL_SOCKET, unix.SO_BINDTODEVICE, iface); serr != nil {
				return
			}
			serr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_BROADCAST, 1)
		}); err != nil {
			return err
		}
		return serr
	}}
	pc, err := lc.ListenPacket(ctx, "udp4", ":68")
	if err != nil {
		return nil, fmt.Errorf("dhcp probe on %s: %w", iface, err)
	}
	defer pc.Close()
	mac := net.HardwareAddr{0x02, 0x47, 0x48, 0x50, 0x52, 0x42} // locally administered, "GHPRB"
	p := &Packet{Op: 1, HType: 1, HLen: 6, XID: uint32(time.Now().UnixNano()), Flags: 0x8000, CHAddr: mac,
		CIAddr: net.IPv4zero.To4(), YIAddr: net.IPv4zero.To4(), SIAddr: net.IPv4zero.To4(), GIAddr: net.IPv4zero.To4()}
	p.Set(OptMsgType, []byte{Discover})
	p.Set(OptClientID, append([]byte{1}, mac...))
	p.Set(OptParamList, []byte{OptSubnetMask, OptRouter, OptDNS})
	_ = ifi
	discover := p.Marshal()
	send := func() error {
		_, err := pc.WriteTo(discover, &net.UDPAddr{IP: net.IPv4bcast, Port: 67})
		return err
	}
	if err := send(); err != nil {
		return nil, err
	}
	// Some servers answer slowly (a router checks that an address is free first) or miss the first
	// broadcast, so the same discover (same transaction) is sent again every couple of seconds.
	const resendEvery = 2 * time.Second
	nextSend := time.Now().Add(resendEvery)
	deadline := time.Now().Add(wait)
	var out []RogueFinding
	seen := map[string]bool{}
	buf := make([]byte, 2048)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			break
		}
		until := deadline
		if nextSend.Before(until) {
			until = nextSend
		}
		pc.SetReadDeadline(until)
		n, _, err := pc.ReadFrom(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() && time.Now().Before(deadline) {
				if time.Now().Before(nextSend) {
					continue
				}
				if send() == nil {
					nextSend = time.Now().Add(resendEvery)
					continue
				}
			}
			break
		}
		r, err := Parse(buf[:n])
		if err != nil || r.Type() != Offer || r.XID != p.XID {
			continue
		}
		sid := r.IPOption(OptServerID)
		if sid == nil || seen[sid.String()] {
			continue
		}
		ours := false
		for _, ip := range ignore {
			if ip.Equal(sid) {
				ours = true
			}
		}
		if ours {
			continue
		}
		seen[sid.String()] = true
		out = append(out, RogueFinding{Server: sid, Offer: r.YIAddr, Mask: r.IPOption(OptSubnetMask)})
	}
	return out, nil
}

package wanmon

import (
	"context"
	"errors"
	"net"
	"os"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

// icmpPing sends one echo request out of the link's interface and waits for the reply.
func icmpPing(ctx context.Context, l Link) (time.Duration, error) {
	return icmpPingTo(ctx, l.Dev, l.Target)
}

// icmpPingTo is icmpPing for any device and address.
func icmpPingTo(ctx context.Context, dev, target string) (time.Duration, error) {
	dst := net.ParseIP(target)
	if dst == nil || dst.To4() == nil {
		return 0, errors.New("the check address must be an IPv4 address")
	}
	lc := net.ListenConfig{Control: bindTo(dev)}
	pc, err := lc.ListenPacket(ctx, "ip4:icmp", "0.0.0.0")
	if err != nil {
		return 0, err
	}
	defer pc.Close()
	id := os.Getpid() & 0xffff
	msg := icmp.Message{Type: ipv4.ICMPTypeEcho, Code: 0, Body: &icmp.Echo{ID: id, Seq: 1, Data: []byte("gatehouse")}}
	b, err := msg.Marshal(nil)
	if err != nil {
		return 0, err
	}
	start := time.Now()
	if _, err := pc.WriteTo(b, &net.IPAddr{IP: dst}); err != nil {
		return 0, err
	}
	deadline := time.Now().Add(2 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	pc.SetReadDeadline(deadline)
	buf := make([]byte, 1500)
	for {
		n, peer, err := pc.ReadFrom(buf)
		if err != nil {
			return 0, err
		}
		m, err := icmp.ParseMessage(1, buf[:n])
		if err != nil || m.Type != ipv4.ICMPTypeEchoReply {
			continue
		}
		if e, ok := m.Body.(*icmp.Echo); ok && e.ID == id && peer.String() == dst.String() {
			return time.Since(start), nil
		}
	}
}

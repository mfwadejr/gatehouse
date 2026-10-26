package dhcp

import (
	"net"
	"testing"
)

func TestARPProbeFrame(t *testing.T) {
	mac := net.HardwareAddr{0x02, 0, 0, 0, 0, 0x01}
	f := arpProbeFrame(mac, net.ParseIP("192.168.1.50"))
	if len(f) != 42 {
		t.Fatalf("frame is %d bytes, want 42", len(f))
	}
	if f[12] != 0x08 || f[13] != 0x06 || f[21] != 1 {
		t.Fatal("not an ARP request")
	}
	if string(f[28:32]) != "\x00\x00\x00\x00" {
		t.Fatal("a probe must not carry a sender address")
	}
	if net.IP(f[38:42]).String() != "192.168.1.50" {
		t.Fatalf("target is %v", net.IP(f[38:42]))
	}
}

func TestARPReplyRecognised(t *testing.T) {
	ours := net.HardwareAddr{0x02, 0, 0, 0, 0, 0x01}
	other := net.HardwareAddr{0x02, 0, 0, 0, 0, 0x09}
	reply := func(from net.HardwareAddr, ip string, op byte) []byte {
		f := arpProbeFrame(from, net.ParseIP("192.168.1.2"))
		f[21] = op
		copy(f[22:28], from)
		copy(f[28:32], net.ParseIP(ip).To4())
		return f
	}
	ip := net.ParseIP("192.168.1.50")
	if !arpReplyFrom(reply(other, "192.168.1.50", 2), ip, ours) {
		t.Error("a reply from the address was missed")
	}
	if arpReplyFrom(reply(other, "192.168.1.51", 2), ip, ours) {
		t.Error("a reply about another address counted")
	}
	if arpReplyFrom(reply(other, "192.168.1.50", 1), ip, ours) {
		t.Error("a request counted as a reply")
	}
	if arpReplyFrom(reply(ours, "192.168.1.50", 2), ip, ours) {
		t.Error("our own packet counted")
	}
	if arpReplyFrom([]byte{1, 2, 3}, ip, ours) {
		t.Error("a short frame counted")
	}
}

func TestARPInUseDoesNotBreakWithoutAPort(t *testing.T) {
	if ARPInUse("no-such-port0", net.ParseIP("192.168.1.50")) {
		t.Error("a missing port reported an address in use")
	}
	if ARPInUse("lo", net.ParseIP("::1")) {
		t.Error("an IPv6 address reported in use")
	}
}

package dhcp

import (
	"bytes"
	"encoding/binary"
	"net"
	"time"

	"golang.org/x/sys/unix"
)

// ARPInUse asks the network directly: who has this address? Unlike a ping it also finds devices
// that never answer pings (many phones, printers and firewalled computers), because every device
// must answer ARP to be reachable at all. The question is an ARP probe (RFC 5227: the sender
// address is left empty), so nothing on the network learns a wrong address from it.
func ARPInUse(iface string, ip net.IP) bool {
	dst := ip.To4()
	if dst == nil {
		return false
	}
	ifc, err := net.InterfaceByName(iface)
	if err != nil || len(ifc.HardwareAddr) != 6 {
		return false // no such port or not an Ethernet port: do not hold up the offer
	}
	fd, err := unix.Socket(unix.AF_PACKET, unix.SOCK_RAW, int(htons(unix.ETH_P_ARP)))
	if err != nil {
		return false // no permission: do not hold up the offer
	}
	defer unix.Close(fd)
	if err := unix.Bind(fd, &unix.SockaddrLinklayer{Protocol: htons(unix.ETH_P_ARP), Ifindex: ifc.Index}); err != nil {
		return false
	}
	tv := unix.NsecToTimeval(int64(40 * time.Millisecond))
	unix.SetsockoptTimeval(fd, unix.SOL_SOCKET, unix.SO_RCVTIMEO, &tv)
	frame := arpProbeFrame(ifc.HardwareAddr, dst)
	to := &unix.SockaddrLinklayer{Ifindex: ifc.Index, Halen: 6, Addr: [8]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}}
	// A second try catches a device that was busy; the whole check stays under a third of a second.
	deadline := time.Now().Add(300 * time.Millisecond)
	buf := make([]byte, 1500)
	for try := 0; try < 2 && time.Now().Before(deadline); try++ {
		if err := unix.Sendto(fd, frame, 0, to); err != nil {
			return false
		}
		until := time.Now().Add(130 * time.Millisecond)
		for time.Now().Before(until) {
			n, _, err := unix.Recvfrom(fd, buf, 0)
			if err != nil {
				continue // timed out; the loop re-checks the clock
			}
			if arpReplyFrom(buf[:n], dst, ifc.HardwareAddr) {
				return true
			}
		}
	}
	return false
}

func htons(v uint16) uint16 { return v<<8 | v>>8 }

// arpProbeFrame builds an Ethernet broadcast carrying an ARP request for target, with an empty sender address.
func arpProbeFrame(mac net.HardwareAddr, target net.IP) []byte {
	b := make([]byte, 0, 42)
	b = append(b, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff)
	b = append(b, mac...)
	b = append(b, 0x08, 0x06)             // ARP
	b = append(b, 0x00, 0x01, 0x08, 0x00) // Ethernet, IPv4
	b = append(b, 6, 4, 0x00, 0x01)       // address sizes, request
	b = append(b, mac...)
	b = append(b, 0, 0, 0, 0)       // sender address left empty: this is a probe
	b = append(b, 0, 0, 0, 0, 0, 0) // target hardware address unknown
	b = append(b, target.To4()...)
	return b
}

// arpReplyFrom says whether frame is an ARP reply from the address, not sent by us.
func arpReplyFrom(frame []byte, ip net.IP, ours net.HardwareAddr) bool {
	if len(frame) < 42 || binary.BigEndian.Uint16(frame[12:14]) != 0x0806 {
		return false
	}
	if binary.BigEndian.Uint16(frame[20:22]) != 2 { // 2 is a reply
		return false
	}
	if bytes.Equal(frame[22:28], ours) {
		return false
	}
	return bytes.Equal(frame[28:32], ip.To4())
}

// Package dhcp implements a DHCPv4 server: packet codec, lease engine and transport.
package dhcp

import (
	"encoding/binary"
	"errors"
	"net"
)

// Message types (option 53).
const (
	Discover = 1
	Offer    = 2
	Request  = 3
	Decline  = 4
	Ack      = 5
	Nak      = 6
	Release  = 7
	Inform   = 8
)

// Option codes used by the server.
const (
	OptSubnetMask  = 1
	OptRouter      = 3
	OptDNS         = 6
	OptHostname    = 12
	OptDomain      = 15
	OptBroadcast   = 28
	OptNTP         = 42
	OptRequestedIP = 50
	OptLeaseTime   = 51
	OptMsgType     = 53
	OptServerID    = 54
	OptParamList   = 55
	OptMessage     = 56
	OptRenewal     = 58
	OptRebinding   = 59
	OptClientID    = 61
	OptEnd         = 255
)

var magic = [4]byte{99, 130, 83, 99}

const headerLen = 236

// Packet is a decoded DHCPv4 message.
type Packet struct {
	Op      byte // 1 request, 2 reply
	HType   byte
	HLen    byte
	Hops    byte
	XID     uint32
	Secs    uint16
	Flags   uint16
	CIAddr  net.IP
	YIAddr  net.IP
	SIAddr  net.IP
	GIAddr  net.IP
	CHAddr  net.HardwareAddr
	Options map[byte][]byte
	// OptOrder keeps the order options were written in, for stable output.
	optOrder []byte
}

var errShort = errors.New("dhcp: short packet")

// Parse decodes a packet. It is strict about length and cookie and tolerant of option layout.
func Parse(b []byte) (*Packet, error) {
	if len(b) < headerLen+4 {
		return nil, errShort
	}
	if [4]byte(b[236:240]) != magic {
		return nil, errors.New("dhcp: bad cookie")
	}
	p := &Packet{
		Op: b[0], HType: b[1], HLen: b[2], Hops: b[3],
		XID:     binary.BigEndian.Uint32(b[4:8]),
		Secs:    binary.BigEndian.Uint16(b[8:10]),
		Flags:   binary.BigEndian.Uint16(b[10:12]),
		CIAddr:  net.IP(append([]byte(nil), b[12:16]...)),
		YIAddr:  net.IP(append([]byte(nil), b[16:20]...)),
		SIAddr:  net.IP(append([]byte(nil), b[20:24]...)),
		GIAddr:  net.IP(append([]byte(nil), b[24:28]...)),
		Options: map[byte][]byte{},
	}
	hl := int(p.HLen)
	if hl > 16 {
		return nil, errors.New("dhcp: bad hardware length")
	}
	p.CHAddr = append(net.HardwareAddr(nil), b[28:28+hl]...)
	o := b[240:]
	for i := 0; i < len(o); {
		c := o[i]
		if c == 0 {
			i++
			continue
		}
		if c == OptEnd {
			break
		}
		if i+1 >= len(o) {
			return nil, errShort
		}
		l := int(o[i+1])
		if i+2+l > len(o) {
			return nil, errShort
		}
		if _, dup := p.Options[c]; !dup { // first occurrence wins
			p.Options[c] = append([]byte(nil), o[i+2:i+2+l]...)
		}
		i += 2 + l
	}
	return p, nil
}

// Type returns the DHCP message type, or 0.
func (p *Packet) Type() byte {
	if v := p.Options[OptMsgType]; len(v) == 1 {
		return v[0]
	}
	return 0
}

// Set stores an option.
func (p *Packet) Set(code byte, v []byte) {
	if p.Options == nil {
		p.Options = map[byte][]byte{}
	}
	if _, ok := p.Options[code]; !ok {
		p.optOrder = append(p.optOrder, code)
	}
	p.Options[code] = v
}

func (p *Packet) SetIPs(code byte, ips []net.IP) {
	var b []byte
	for _, ip := range ips {
		if v4 := ip.To4(); v4 != nil {
			b = append(b, v4...)
		}
	}
	if len(b) > 0 {
		p.Set(code, b)
	}
}

func (p *Packet) SetU32(code byte, v uint32) {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, v)
	p.Set(code, b)
}

// IPOption returns an option holding a single address.
func (p *Packet) IPOption(code byte) net.IP {
	if v := p.Options[code]; len(v) == 4 {
		return net.IP(v)
	}
	return nil
}

// Marshal encodes the packet, padded to the 300 byte BOOTP minimum.
func (p *Packet) Marshal() []byte {
	b := make([]byte, headerLen+4, 320)
	b[0], b[1], b[2], b[3] = p.Op, p.HType, p.HLen, p.Hops
	binary.BigEndian.PutUint32(b[4:], p.XID)
	binary.BigEndian.PutUint16(b[8:], p.Secs)
	binary.BigEndian.PutUint16(b[10:], p.Flags)
	copy(b[12:16], p.CIAddr.To4())
	copy(b[16:20], p.YIAddr.To4())
	copy(b[20:24], p.SIAddr.To4())
	copy(b[24:28], p.GIAddr.To4())
	copy(b[28:44], p.CHAddr)
	copy(b[236:240], magic[:])
	written := map[byte]bool{}
	put := func(c byte) {
		v, ok := p.Options[c]
		if !ok || written[c] || len(v) > 255 {
			return
		}
		written[c] = true
		b = append(b, c, byte(len(v)))
		b = append(b, v...)
	}
	put(OptMsgType) // always first
	for _, c := range p.optOrder {
		put(c)
	}
	b = append(b, OptEnd)
	for len(b) < 300 {
		b = append(b, 0)
	}
	return b
}

// Broadcast reports whether the client asked for a broadcast reply.
func (p *Packet) Broadcast() bool { return p.Flags&0x8000 != 0 }

func ip4(s string) net.IP { return net.ParseIP(s).To4() }

func ipToU32(ip net.IP) uint32 { return binary.BigEndian.Uint32(ip.To4()) }

func u32ToIP(n uint32) net.IP {
	b := make(net.IP, 4)
	binary.BigEndian.PutUint32(b, n)
	return b
}

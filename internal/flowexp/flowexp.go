// Package flowexp turns finished and ongoing connections into flow records and sends them to a
// collector over UDP, as IPFIX (RFC 7011) or NetFlow version 9 (RFC 3954). Each connection becomes
// two records, one per direction, as most collectors expect. Sending is best effort: a dead
// collector never slows the router.
package flowexp

import (
	"encoding/binary"
	"errors"
	"net/netip"
	"time"
)

// Record is one direction of one connection.
type Record struct {
	Start, End time.Time
	Src, Dst   netip.Addr // IPv4 only
	SrcPort    uint16
	DstPort    uint16
	Proto      uint8
	Octets     uint64
	Packets    uint64
}

// Format is the export format.
type Format string

const (
	IPFIX Format = "ipfix"
	V9    Format = "netflow9"
)

const (
	templateID = 256
	maxPacket  = 1400
	recSize    = 8 + 8 + 4 + 4 + 2 + 2 + 1 + 8 + 8 // IPFIX data record size
	recSizeV9  = 4 + 4 + 4 + 4 + 2 + 2 + 1 + 4 + 4 // NetFlow v9 data record size (32-bit counters)
)

// Encoder builds export packets. It is not safe for concurrent use.
type Encoder struct {
	Format   Format
	DomainID uint32
	Boot     time.Time // for NetFlow v9 "uptime" fields
	seq      uint32
	tmplAt   time.Time
}

func be16(b []byte, v uint16) { binary.BigEndian.PutUint16(b, v) }
func be32(b []byte, v uint32) { binary.BigEndian.PutUint32(b, v) }
func be64(b []byte, v uint64) { binary.BigEndian.PutUint64(b, v) }

// Encode splits records into packets of at most about 1400 bytes. The template is included in the
// first packet and again at least once a minute, because UDP can lose it.
func (e *Encoder) Encode(recs []Record, now time.Time) ([][]byte, error) {
	if e.Format != IPFIX && e.Format != V9 {
		return nil, errors.New("unknown export format")
	}
	var out [][]byte
	withTemplate := e.tmplAt.IsZero() || now.Sub(e.tmplAt) >= time.Minute
	for len(recs) > 0 || withTemplate {
		pkt, n := e.packet(recs, now, withTemplate)
		out = append(out, pkt)
		recs = recs[n:]
		withTemplate = false
		if n == 0 && len(recs) == 0 {
			break
		}
	}
	if len(out) > 0 {
		e.tmplAt = now
	}
	return out, nil
}

func (e *Encoder) packet(recs []Record, now time.Time, withTemplate bool) ([]byte, int) {
	if e.Format == IPFIX {
		return e.ipfix(recs, now, withTemplate)
	}
	return e.v9(recs, now, withTemplate)
}

var ipfixFields = [][2]uint16{ // element id, length
	{152, 8}, // flowStartMilliseconds
	{153, 8}, // flowEndMilliseconds
	{8, 4},   // sourceIPv4Address
	{12, 4},  // destinationIPv4Address
	{7, 2},   // sourceTransportPort
	{11, 2},  // destinationTransportPort
	{4, 1},   // protocolIdentifier
	{1, 8},   // octetDeltaCount
	{2, 8},   // packetDeltaCount
}

var v9Fields = [][2]uint16{
	{22, 4}, // FIRST_SWITCHED
	{21, 4}, // LAST_SWITCHED
	{8, 4},  // IPV4_SRC_ADDR
	{12, 4}, // IPV4_DST_ADDR
	{7, 2},  // L4_SRC_PORT
	{11, 2}, // L4_DST_PORT
	{4, 1},  // PROTOCOL
	{1, 4},  // IN_BYTES
	{2, 4},  // IN_PKTS
}

func (e *Encoder) ipfix(recs []Record, now time.Time, withTemplate bool) ([]byte, int) {
	b := make([]byte, 16, maxPacket)
	if withTemplate {
		tl := 4 + 4 + 4*len(ipfixFields)
		s := make([]byte, tl)
		be16(s[0:], 2) // template set
		be16(s[2:], uint16(tl))
		be16(s[4:], templateID)
		be16(s[6:], uint16(len(ipfixFields)))
		for i, f := range ipfixFields {
			be16(s[8+4*i:], f[0])
			be16(s[10+4*i:], f[1])
		}
		b = append(b, s...)
	}
	n := 0
	var data []byte
	for _, r := range recs {
		if len(b)+4+len(data)+recSize > maxPacket {
			break
		}
		rec := make([]byte, recSize)
		be64(rec[0:], uint64(r.Start.UnixMilli()))
		be64(rec[8:], uint64(r.End.UnixMilli()))
		s4, d4 := r.Src.As4(), r.Dst.As4()
		copy(rec[16:], s4[:])
		copy(rec[20:], d4[:])
		be16(rec[24:], r.SrcPort)
		be16(rec[26:], r.DstPort)
		rec[28] = r.Proto
		be64(rec[29:], r.Octets)
		be64(rec[37:], r.Packets)
		data = append(data, rec...)
		n++
	}
	if n > 0 {
		pad := (4 - (4+len(data))%4) % 4
		s := make([]byte, 4)
		be16(s[0:], templateID)
		be16(s[2:], uint16(4+len(data)+pad))
		b = append(b, s...)
		b = append(b, data...)
		b = append(b, make([]byte, pad)...)
	}
	be16(b[0:], 10)
	be16(b[2:], uint16(len(b)))
	be32(b[4:], uint32(now.Unix()))
	be32(b[8:], e.seq) // sequence counts data records exported before this packet
	be32(b[12:], e.DomainID)
	e.seq += uint32(n)
	return b, n
}

func (e *Encoder) v9(recs []Record, now time.Time, withTemplate bool) ([]byte, int) {
	b := make([]byte, 20, maxPacket)
	count := 0
	if withTemplate {
		tl := 4 + 4 + 4*len(v9Fields)
		s := make([]byte, tl)
		be16(s[0:], 0) // template flowset
		be16(s[2:], uint16(tl))
		be16(s[4:], templateID)
		be16(s[6:], uint16(len(v9Fields)))
		for i, f := range v9Fields {
			be16(s[8+4*i:], f[0])
			be16(s[10+4*i:], f[1])
		}
		b = append(b, s...)
		count++
	}
	up := func(t time.Time) uint32 {
		d := t.Sub(e.Boot).Milliseconds()
		if d < 0 {
			d = 0
		}
		return uint32(d)
	}
	sat := func(v uint64) uint32 {
		if v > 0xffffffff {
			return 0xffffffff
		}
		return uint32(v)
	}
	n := 0
	var data []byte
	for _, r := range recs {
		if len(b)+4+len(data)+recSizeV9 > maxPacket {
			break
		}
		rec := make([]byte, recSizeV9)
		be32(rec[0:], up(r.Start))
		be32(rec[4:], up(r.End))
		s4, d4 := r.Src.As4(), r.Dst.As4()
		copy(rec[8:], s4[:])
		copy(rec[12:], d4[:])
		be16(rec[16:], r.SrcPort)
		be16(rec[18:], r.DstPort)
		rec[20] = r.Proto
		be32(rec[21:], sat(r.Octets))
		be32(rec[25:], sat(r.Packets))
		data = append(data, rec...)
		n++
	}
	if n > 0 {
		pad := (4 - (4+len(data))%4) % 4
		s := make([]byte, 4)
		be16(s[0:], templateID)
		be16(s[2:], uint16(4+len(data)+pad))
		b = append(b, s...)
		b = append(b, data...)
		b = append(b, make([]byte, pad)...)
		count += n
	}
	be16(b[0:], 9)
	be16(b[2:], uint16(count))
	be32(b[4:], up(now))
	be32(b[8:], uint32(now.Unix()))
	be32(b[12:], e.seq) // v9 sequence counts export packets
	be32(b[16:], e.DomainID)
	e.seq++
	return b, n
}

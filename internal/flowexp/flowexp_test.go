package flowexp

import (
	"encoding/binary"
	"net/netip"
	"testing"
	"time"
)

func rec(i int) Record {
	t := time.Unix(1700000000+int64(i), 0)
	return Record{Start: t, End: t.Add(2 * time.Second), Src: netip.MustParseAddr("192.168.1.10"), Dst: netip.MustParseAddr("203.0.113.9"),
		SrcPort: uint16(40000 + i), DstPort: 443, Proto: 6, Octets: 1234 + uint64(i), Packets: 12}
}

func TestIPFIXPacketsAreWellFormed(t *testing.T) {
	e := &Encoder{Format: IPFIX, DomainID: 7}
	var recs []Record
	for i := 0; i < 50; i++ {
		recs = append(recs, rec(i))
	}
	now := time.Unix(1700001000, 0)
	pkts, err := e.Encode(recs, now)
	if err != nil || len(pkts) < 2 {
		t.Fatalf("want several packets, got %d (%v)", len(pkts), err)
	}
	total := 0
	for pi, p := range pkts {
		if len(p) > 1400 {
			t.Fatalf("packet too big: %d", len(p))
		}
		if binary.BigEndian.Uint16(p[0:]) != 10 || int(binary.BigEndian.Uint16(p[2:])) != len(p) {
			t.Fatalf("bad header: % x", p[:16])
		}
		if binary.BigEndian.Uint32(p[12:]) != 7 {
			t.Fatal("domain id")
		}
		off := 16
		sawTemplate := false
		for off < len(p) {
			id := binary.BigEndian.Uint16(p[off:])
			l := int(binary.BigEndian.Uint16(p[off+2:]))
			if l < 4 || off+l > len(p) || l%4 != 0 {
				t.Fatalf("bad set length %d at %d", l, off)
			}
			switch {
			case id == 2:
				sawTemplate = true
				if binary.BigEndian.Uint16(p[off+4:]) != templateID || int(binary.BigEndian.Uint16(p[off+6:])) != len(ipfixFields) {
					t.Fatal("template contents")
				}
			case id == templateID:
				body := l - 4
				n := body / recSize
				for k := 0; k < n; k++ {
					r := p[off+4+k*recSize:]
					if binary.BigEndian.Uint16(r[26:]) != 443 || r[28] != 6 || binary.BigEndian.Uint64(r[37:]) != 12 {
						t.Fatalf("record %d mangled", total)
					}
					total++
				}
			default:
				t.Fatalf("unexpected set %d", id)
			}
			off += l
		}
		if pi == 0 && !sawTemplate {
			t.Fatal("the first packet must carry the template")
		}
		if pi > 0 && sawTemplate {
			t.Fatal("the template is only repeated after a minute")
		}
	}
	if total != 50 {
		t.Fatalf("records sent: %d", total)
	}
	// Within a minute the template is not repeated; after a minute it is.
	p2, _ := e.Encode(recs[:1], now.Add(10*time.Second))
	if binary.BigEndian.Uint16(p2[0][16:]) == 2 {
		t.Fatal("template repeated too early")
	}
	p3, _ := e.Encode(recs[:1], now.Add(70*time.Second))
	if binary.BigEndian.Uint16(p3[0][16:]) != 2 {
		t.Fatal("template not repeated after a minute")
	}
	// The sequence number counts the records already sent.
	if got := binary.BigEndian.Uint32(p2[0][8:]); got != 50 {
		t.Fatalf("sequence %d, want 50", got)
	}
}

func TestNetFlow9(t *testing.T) {
	e := &Encoder{Format: V9, DomainID: 1, Boot: time.Unix(1699990000, 0)}
	pkts, err := e.Encode([]Record{rec(1), rec(2)}, time.Unix(1700001000, 0))
	if err != nil || len(pkts) != 1 {
		t.Fatalf("%d %v", len(pkts), err)
	}
	p := pkts[0]
	if binary.BigEndian.Uint16(p[0:]) != 9 || binary.BigEndian.Uint16(p[2:]) != 3 { // 1 template + 2 records
		t.Fatalf("header: % x", p[:20])
	}
	if binary.BigEndian.Uint16(p[20:]) != 0 { // template flowset id 0
		t.Fatal("template flowset")
	}
	if len(p)%4 != 0 {
		t.Fatal("flowsets must be padded to 4 bytes")
	}
}

func TestUnknownFormat(t *testing.T) {
	if _, err := (&Encoder{Format: "x"}).Encode(nil, time.Now()); err == nil {
		t.Fatal("must refuse")
	}
}

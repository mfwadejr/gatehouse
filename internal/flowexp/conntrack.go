package flowexp

import (
	"net/netip"
	"strconv"
	"strings"
	"time"
)

// Conn is one live connection from the kernel's list, with counts for both directions.
type Conn struct {
	Proto            uint8
	Src, Dst         netip.Addr // as the connection began
	SPort, DPort     uint16
	RSrc, RDst       netip.Addr // the reply direction (after any address translation)
	RSPort, RDPort   uint16
	OPackets, OBytes uint64
	RPackets, RBytes uint64
}

type key struct {
	proto        uint8
	src, dst     netip.Addr
	sport, dport uint16
}

func (c Conn) key() key { return key{c.Proto, c.Src, c.Dst, c.SPort, c.DPort} }

// ParseConntrack reads the text of /proc/net/nf_conntrack. Only IPv4 connections that are not on the
// loopback are kept. Lines it cannot read are skipped.
func ParseConntrack(text string) []Conn {
	var out []Conn
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) < 6 || f[0] != "ipv4" {
			continue
		}
		pn, err := strconv.Atoi(f[3])
		if err != nil || pn < 0 || pn > 255 {
			continue
		}
		c := Conn{Proto: uint8(pn)}
		var nsrc, ndst, nsp, ndp, npk, nby int
		for _, tok := range f[4:] {
			k, v, ok := strings.Cut(tok, "=")
			if !ok {
				continue
			}
			switch k {
			case "src":
				a, _ := netip.ParseAddr(v)
				if nsrc == 0 {
					c.Src = a
				} else if nsrc == 1 {
					c.RSrc = a
				}
				nsrc++
			case "dst":
				a, _ := netip.ParseAddr(v)
				if ndst == 0 {
					c.Dst = a
				} else if ndst == 1 {
					c.RDst = a
				}
				ndst++
			case "sport", "dport":
				n, _ := strconv.ParseUint(v, 10, 16)
				if k == "sport" {
					if nsp == 0 {
						c.SPort = uint16(n)
					} else if nsp == 1 {
						c.RSPort = uint16(n)
					}
					nsp++
				} else {
					if ndp == 0 {
						c.DPort = uint16(n)
					} else if ndp == 1 {
						c.RDPort = uint16(n)
					}
					ndp++
				}
			case "packets":
				n, _ := strconv.ParseUint(v, 10, 64)
				if npk == 0 {
					c.OPackets = n
				} else if npk == 1 {
					c.RPackets = n
				}
				npk++
			case "bytes":
				n, _ := strconv.ParseUint(v, 10, 64)
				if nby == 0 {
					c.OBytes = n
				} else if nby == 1 {
					c.RBytes = n
				}
				nby++
			}
		}
		if nsrc < 2 || ndst < 2 || !c.Src.Is4() || !c.Dst.Is4() || !c.RSrc.Is4() || !c.RDst.Is4() {
			continue
		}
		if c.Src.IsLoopback() || c.Dst.IsLoopback() {
			continue
		}
		out = append(out, c)
	}
	return out
}

type seen struct {
	conn       Conn
	start      time.Time
	exportedAt time.Time
	op, ob     uint64 // counts already exported
	rp, rb     uint64
}

// Tracker turns successive looks at the connection list into records. A connection is reported when
// it ends, and a long one is reported again every Active interval with what moved since.
type Tracker struct {
	Active time.Duration // how often to report a connection that stays open; 5 minutes if zero
	m      map[key]*seen
}

func sub(a, b uint64) uint64 {
	if a < b {
		return a // the counter restarted; report what is there
	}
	return a - b
}

func (s *seen) records(c Conn, end time.Time) []Record {
	var out []Record
	op, ob := sub(c.OPackets, s.op), sub(c.OBytes, s.ob)
	rp, rb := sub(c.RPackets, s.rp), sub(c.RBytes, s.rb)
	if op > 0 || ob > 0 {
		out = append(out, Record{Start: s.exportedAt, End: end, Src: c.Src, Dst: c.Dst, SrcPort: c.SPort, DstPort: c.DPort, Proto: c.Proto, Octets: ob, Packets: op})
	}
	if rp > 0 || rb > 0 {
		out = append(out, Record{Start: s.exportedAt, End: end, Src: c.RSrc, Dst: c.RDst, SrcPort: c.RSPort, DstPort: c.RDPort, Proto: c.Proto, Octets: rb, Packets: rp})
	}
	s.op, s.ob, s.rp, s.rb, s.exportedAt = c.OPackets, c.OBytes, c.RPackets, c.RBytes, end
	return out
}

// Poll takes the current list and returns the records now due.
func (t *Tracker) Poll(now time.Time, conns []Conn) []Record {
	if t.m == nil {
		t.m = map[key]*seen{}
	}
	active := t.Active
	if active <= 0 {
		active = 5 * time.Minute
	}
	var out []Record
	cur := make(map[key]bool, len(conns))
	for _, c := range conns {
		k := c.key()
		cur[k] = true
		s := t.m[k]
		if s == nil {
			t.m[k] = &seen{conn: c, start: now, exportedAt: now}
			continue
		}
		s.conn = c
		if now.Sub(s.exportedAt) >= active {
			out = append(out, s.records(c, now)...)
		}
	}
	for k, s := range t.m {
		if !cur[k] {
			out = append(out, s.records(s.conn, now)...)
			delete(t.m, k)
		}
	}
	return out
}

// Open is how many connections are being followed.
func (t *Tracker) Open() int { return len(t.m) }

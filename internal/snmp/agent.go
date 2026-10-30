package snmp

import (
	"crypto/subtle"
	"net"
	"sort"
)

// Agent answers read-only requests from a snapshot of variables.
type Agent struct {
	Community string
	// Vars returns the current variables. The agent sorts them.
	Vars func() []Var
}

const maxResponse = 1400

func sorted(vs []Var) []Var {
	out := append([]Var(nil), vs...)
	sort.Slice(out, func(i, j int) bool { return out[i].OID.Cmp(out[j].OID) < 0 })
	return out
}

// next is the first variable after o.
func next(vs []Var, o OID) (Var, bool) {
	i := sort.Search(len(vs), func(i int) bool { return vs[i].OID.Cmp(o) > 0 })
	if i < len(vs) {
		return vs[i], true
	}
	return Var{}, false
}

func get(vs []Var, o OID) Value {
	i := sort.Search(len(vs), func(i int) bool { return vs[i].OID.Cmp(o) >= 0 })
	if i < len(vs) && vs[i].OID.Cmp(o) == 0 {
		return vs[i].Val
	}
	return Value{Tag: tNoObject}
}

func varbind(o OID, v Value) []byte {
	return tlv(tSequence, append(tlv(tOID, encOID(o)), v.enc()...))
}

// Handle answers one datagram, or returns nil to stay silent.
func (a *Agent) Handle(req []byte) []byte {
	r := &reader{req}
	tag, msg, err := r.next()
	if err != nil || tag != tSequence {
		return nil
	}
	m := &reader{msg}
	t, vb, err := m.next()
	if err != nil || t != tInteger || decInt(vb) != 1 { // version 2c only
		return nil
	}
	t, comm, err := m.next()
	if err != nil || t != tOctets || a.Community == "" || subtle.ConstantTimeCompare(comm, []byte(a.Community)) != 1 {
		return nil
	}
	pt, pdu, err := m.next()
	if err != nil || (pt != pduGet && pt != pduNext && pt != pduBulk) {
		return nil
	}
	p := &reader{pdu}
	_, idb, err1 := p.next()
	_, ab, err2 := p.next()
	_, bb, err3 := p.next()
	vt, vl, err4 := p.next()
	if err1 != nil || err2 != nil || err3 != nil || err4 != nil || vt != tSequence {
		return nil
	}
	var asked []OID
	l := &reader{vl}
	for len(l.b) > 0 {
		st, one, err := l.next()
		if err != nil || st != tSequence {
			return nil
		}
		x := &reader{one}
		ot, ob, err := x.next()
		if err != nil || ot != tOID {
			return nil
		}
		o, err := decOID(ob)
		if err != nil {
			return nil
		}
		asked = append(asked, o)
		if len(asked) > 64 {
			return nil
		}
	}
	var vs []Var
	if a.Vars != nil {
		vs = sorted(a.Vars())
	}
	var out [][]byte
	size := 0
	add := func(o OID, v Value) bool {
		b := varbind(o, v)
		if size+len(b) > maxResponse-64 && len(out) > 0 {
			return false
		}
		out = append(out, b)
		size += len(b)
		return true
	}
	switch pt {
	case pduGet:
		for _, o := range asked {
			add(o, get(vs, o))
		}
	case pduNext:
		for _, o := range asked {
			if v, ok := next(vs, o); ok {
				add(v.OID, v.Val)
			} else {
				add(o, Value{Tag: tEndOfMib})
			}
		}
	case pduBulk:
		nonRep, maxRep := int(decInt(ab)), int(decInt(bb))
		if nonRep < 0 {
			nonRep = 0
		}
		if nonRep > len(asked) {
			nonRep = len(asked)
		}
		if maxRep < 0 {
			maxRep = 0
		}
		if maxRep > 50 {
			maxRep = 50
		}
		for _, o := range asked[:nonRep] {
			if v, ok := next(vs, o); ok {
				add(v.OID, v.Val)
			} else {
				add(o, Value{Tag: tEndOfMib})
			}
		}
		cur := append([]OID(nil), asked[nonRep:]...)
	rep:
		for i := 0; i < maxRep && len(cur) > 0; i++ {
			allEnd := true
			for j, o := range cur {
				v, ok := next(vs, o)
				if !ok {
					if !add(o, Value{Tag: tEndOfMib}) {
						break rep
					}
					continue
				}
				allEnd = false
				if !add(v.OID, v.Val) {
					break rep
				}
				cur[j] = v.OID
			}
			if allEnd {
				break
			}
		}
	}
	resp := tlv(pduResp, append(append(append(tlv(tInteger, idb), tlv(tInteger, encInt(0))...), tlv(tInteger, encInt(0))...), tlv(tSequence, concat(out))...))
	return tlv(tSequence, append(append(tlv(tInteger, encInt(1)), tlv(tOctets, comm)...), resp...))
}

func concat(parts [][]byte) []byte {
	var b []byte
	for _, p := range parts {
		b = append(b, p...)
	}
	return b
}

// Serve answers datagrams until the socket closes.
func (a *Agent) Serve(pc net.PacketConn) error {
	buf := make([]byte, 2048)
	for {
		n, from, err := pc.ReadFrom(buf)
		if err != nil {
			return err
		}
		if resp := a.Handle(buf[:n]); resp != nil {
			pc.WriteTo(resp, from)
		}
	}
}

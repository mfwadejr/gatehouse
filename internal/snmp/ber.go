// Package snmp is a small read-only SNMP version 2c agent: GET, GETNEXT and GETBULK. It never
// changes anything on the device. Requests with the wrong community or another version are ignored.
package snmp

import (
	"errors"
	"strconv"
	"strings"
)

// Types used on the wire.
const (
	tInteger   = 0x02
	tOctets    = 0x04
	tNull      = 0x05
	tOID       = 0x06
	tSequence  = 0x30
	tCounter32 = 0x41
	tGauge32   = 0x42
	tTicks     = 0x43
	tCounter64 = 0x46
	tNoObject  = 0x80
	tNoInst    = 0x81
	tEndOfMib  = 0x82
	pduGet     = 0xa0
	pduNext    = 0xa1
	pduResp    = 0xa2
	pduBulk    = 0xa5
)

// OID is an object identifier.
type OID []uint32

// ParseOID reads "1.3.6.1.2.1".
func ParseOID(s string) (OID, error) {
	s = strings.TrimPrefix(s, ".")
	var o OID
	for _, p := range strings.Split(s, ".") {
		n, err := strconv.ParseUint(p, 10, 32)
		if err != nil {
			return nil, errors.New("bad OID")
		}
		o = append(o, uint32(n))
	}
	if len(o) < 2 {
		return nil, errors.New("bad OID")
	}
	return o, nil
}

// MustOID is for fixed tables.
func MustOID(s string) OID {
	o, err := ParseOID(s)
	if err != nil {
		panic(err)
	}
	return o
}

func (o OID) String() string {
	var b strings.Builder
	for i, n := range o {
		if i > 0 {
			b.WriteByte('.')
		}
		b.WriteString(strconv.FormatUint(uint64(n), 10))
	}
	return b.String()
}

// Cmp orders OIDs the way SNMP walks them.
func (o OID) Cmp(p OID) int {
	for i := 0; i < len(o) && i < len(p); i++ {
		if o[i] != p[i] {
			if o[i] < p[i] {
				return -1
			}
			return 1
		}
	}
	switch {
	case len(o) < len(p):
		return -1
	case len(o) > len(p):
		return 1
	}
	return 0
}

// Value is one answer.
type Value struct {
	Tag   byte
	Int   int64
	U     uint64
	Bytes []byte
	OID   OID
}

func Int(n int64) Value        { return Value{Tag: tInteger, Int: n} }
func Str(s string) Value       { return Value{Tag: tOctets, Bytes: []byte(s)} }
func Raw(b []byte) Value       { return Value{Tag: tOctets, Bytes: b} }
func ObjectID(o OID) Value     { return Value{Tag: tOID, OID: o} }
func Counter32(n uint64) Value { return Value{Tag: tCounter32, U: n & 0xffffffff} }
func Gauge32(n uint64) Value {
	if n > 0xffffffff {
		n = 0xffffffff
	}
	return Value{Tag: tGauge32, U: n}
}
func Ticks(n uint64) Value     { return Value{Tag: tTicks, U: n & 0xffffffff} }
func Counter64(n uint64) Value { return Value{Tag: tCounter64, U: n} }

// Var is an OID with its value.
type Var struct {
	OID OID
	Val Value
}

func tlv(tag byte, body []byte) []byte {
	n := len(body)
	out := []byte{tag}
	switch {
	case n < 128:
		out = append(out, byte(n))
	case n < 256:
		out = append(out, 0x81, byte(n))
	default:
		out = append(out, 0x82, byte(n>>8), byte(n))
	}
	return append(out, body...)
}

func encInt(n int64) []byte {
	var b []byte
	for {
		b = append([]byte{byte(n)}, b...)
		n >>= 8
		if (n == 0 && b[0]&0x80 == 0) || (n == -1 && b[0]&0x80 != 0) {
			return b
		}
	}
}

func encUint(u uint64) []byte {
	b := []byte{byte(u)}
	for u >>= 8; u > 0; u >>= 8 {
		b = append([]byte{byte(u)}, b...)
	}
	if b[0]&0x80 != 0 {
		b = append([]byte{0}, b...)
	}
	return b
}

func encOID(o OID) []byte {
	if len(o) < 2 {
		return []byte{0}
	}
	out := []byte{byte(o[0]*40 + o[1])}
	for _, n := range o[2:] {
		var tmp []byte
		tmp = append(tmp, byte(n&0x7f))
		for n >>= 7; n > 0; n >>= 7 {
			tmp = append([]byte{byte(n&0x7f) | 0x80}, tmp...)
		}
		out = append(out, tmp...)
	}
	return out
}

func (v Value) enc() []byte {
	switch v.Tag {
	case tInteger:
		return tlv(tInteger, encInt(v.Int))
	case tOctets:
		return tlv(tOctets, v.Bytes)
	case tOID:
		return tlv(tOID, encOID(v.OID))
	case tCounter32, tGauge32, tTicks, tCounter64:
		return tlv(v.Tag, encUint(v.U))
	case tNoObject, tNoInst, tEndOfMib:
		return []byte{v.Tag, 0}
	}
	return []byte{tNull, 0}
}

// reader walks one level of tag-length-value items.
type reader struct{ b []byte }

var errShort = errors.New("short")

func (r *reader) next() (tag byte, body []byte, err error) {
	if len(r.b) < 2 {
		return 0, nil, errShort
	}
	tag = r.b[0]
	n := int(r.b[1])
	off := 2
	if n&0x80 != 0 {
		k := n & 0x7f
		if k == 0 || k > 2 || len(r.b) < 2+k {
			return 0, nil, errShort
		}
		n = 0
		for i := 0; i < k; i++ {
			n = n<<8 | int(r.b[2+i])
		}
		off = 2 + k
	}
	if len(r.b) < off+n {
		return 0, nil, errShort
	}
	body = r.b[off : off+n]
	r.b = r.b[off+n:]
	return tag, body, nil
}

func decInt(b []byte) int64 {
	if len(b) == 0 {
		return 0
	}
	var n int64
	if b[0]&0x80 != 0 {
		n = -1
	}
	for _, c := range b {
		n = n<<8 | int64(c)
	}
	return n
}

func decOID(b []byte) (OID, error) {
	if len(b) == 0 {
		return nil, errShort
	}
	o := OID{uint32(b[0] / 40), uint32(b[0] % 40)}
	var n uint32
	for _, c := range b[1:] {
		n = n<<7 | uint32(c&0x7f)
		if c&0x80 == 0 {
			o = append(o, n)
			n = 0
		}
	}
	return o, nil
}

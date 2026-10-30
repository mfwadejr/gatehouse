package snmp

import (
	"bytes"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// request builds a v2c request the way a manager would.
func request(version int64, comm string, pdu byte, id int64, a, b int64, oids ...string) []byte {
	var vbs []byte
	for _, s := range oids {
		vbs = append(vbs, tlv(tSequence, append(tlv(tOID, encOID(MustOID(s))), tNull, 0))...)
	}
	body := append(append(append(tlv(tInteger, encInt(id)), tlv(tInteger, encInt(a))...), tlv(tInteger, encInt(b))...), tlv(tSequence, vbs)...)
	return tlv(tSequence, append(append(tlv(tInteger, encInt(version)), tlv(tOctets, []byte(comm))...), tlv(pdu, body)...))
}

type answer struct {
	pdu  byte
	id   int64
	vars []Var
}

func parse(t *testing.T, b []byte) answer {
	t.Helper()
	r := &reader{b}
	_, msg, err := r.next()
	if err != nil {
		t.Fatal(err)
	}
	m := &reader{msg}
	m.next()
	m.next()
	pt, pdu, err := m.next()
	if err != nil {
		t.Fatal(err)
	}
	p := &reader{pdu}
	_, idb, _ := p.next()
	p.next()
	p.next()
	_, vl, _ := p.next()
	ans := answer{pdu: pt, id: decInt(idb)}
	l := &reader{vl}
	for len(l.b) > 0 {
		_, one, err := l.next()
		if err != nil {
			t.Fatal(err)
		}
		x := &reader{one}
		_, ob, _ := x.next()
		o, _ := decOID(ob)
		vt, vb, _ := x.next()
		v := Value{Tag: vt}
		switch vt {
		case tInteger:
			v.Int = decInt(vb)
		case tOctets:
			v.Bytes = vb
		case tCounter32, tGauge32, tTicks, tCounter64:
			for _, c := range vb {
				v.U = v.U<<8 | uint64(c)
			}
		}
		ans.vars = append(ans.vars, Var{o, v})
	}
	return ans
}

func agent() *Agent {
	return &Agent{Community: "s3cret-comm", Vars: func() []Var {
		return MIBVars(System{Descr: "Gatehouse test", Name: "gate", Uptime: 12345}, []Iface{
			{Index: 1, Name: "lo", Type: 24, MTU: 65536, Oper: 1, Up: true},
			{Index: 2, Name: "eth0", Type: 6, MTU: 1500, SpeedMbps: 1000, MAC: []byte{1, 2, 3, 4, 5, 6}, Oper: 1, Up: true, InOct: 5000000000, OutOct: 7},
		})
	}}
}

func TestOIDRoundTrip(t *testing.T) {
	for _, s := range []string{"1.3.6.1.2.1.1.1.0", "1.3.6.1.4.1.8072.3.2.10", "1.3.6.1.2.1.31.1.1.1.6.200"} {
		o := MustOID(s)
		d, err := decOID(encOID(o))
		if err != nil || d.String() != s {
			t.Fatalf("%s -> %v %v", s, d, err)
		}
	}
	for _, n := range []int64{0, 1, 127, 128, 255, 256, -1, -128, -129, 65535, 1 << 31} {
		if got := decInt(encInt(n)); got != n {
			t.Fatalf("int %d -> %d", n, got)
		}
	}
}

func TestGetNextAndWalkOrder(t *testing.T) {
	a := agent()
	got := parse(t, a.Handle(request(1, "s3cret-comm", pduGet, 77, 0, 0, "1.3.6.1.2.1.1.5.0", "1.3.6.1.2.1.1.99.0")))
	if got.pdu != pduResp || got.id != 77 || len(got.vars) != 2 {
		t.Fatalf("%+v", got)
	}
	if string(got.vars[0].Val.Bytes) != "gate" || got.vars[1].Val.Tag != tNoObject {
		t.Fatalf("%+v", got.vars)
	}
	got = parse(t, a.Handle(request(1, "s3cret-comm", pduNext, 5, 0, 0, "1.3.6.1.2.1.1")))
	if got.vars[0].OID.String() != "1.3.6.1.2.1.1.1.0" {
		t.Fatalf("%v", got.vars[0].OID)
	}
	// Walk everything with GETNEXT; the order must rise and end cleanly.
	cur := "1.3"
	var prev OID
	n := 0
	for {
		got = parse(t, a.Handle(request(1, "s3cret-comm", pduNext, 1, 0, 0, cur)))
		v := got.vars[0]
		if v.Val.Tag == tEndOfMib {
			break
		}
		if prev != nil && v.OID.Cmp(prev) <= 0 {
			t.Fatalf("not rising: %v after %v", v.OID, prev)
		}
		prev, cur = v.OID, v.OID.String()
		n++
		if n > 500 {
			t.Fatal("walk never ends")
		}
	}
	if n != 8+2*21 {
		t.Fatalf("walked %d variables", n)
	}
}

func TestBulkAndCounters(t *testing.T) {
	a := agent()
	got := parse(t, a.Handle(request(1, "s3cret-comm", pduBulk, 9, 0, 5, "1.3.6.1.2.1.31.1.1.1.6")))
	if len(got.vars) != 5 {
		t.Fatalf("want 5 got %d", len(got.vars))
	}
	if got.vars[1].OID.String() != "1.3.6.1.2.1.31.1.1.1.6.2" || got.vars[1].Val.Tag != tCounter64 || got.vars[1].Val.U != 5000000000 {
		t.Fatalf("%+v", got.vars[1])
	}
	// The 32-bit counter wraps, as it should.
	got = parse(t, a.Handle(request(1, "s3cret-comm", pduGet, 1, 0, 0, "1.3.6.1.2.1.2.2.1.10.2")))
	if got.vars[0].Val.U != 5000000000&0xffffffff {
		t.Fatalf("%d", got.vars[0].Val.U)
	}
	got = parse(t, a.Handle(request(1, "s3cret-comm", pduGet, 1, 0, 0, "1.3.6.1.2.1.2.2.1.5.2")))
	if got.vars[0].Val.Tag != tGauge32 || got.vars[0].Val.U != 1000000000 {
		t.Fatalf("speed %+v", got.vars[0])
	}
}

func TestStaysSilentForStrangers(t *testing.T) {
	a := agent()
	if a.Handle(request(1, "public", pduGet, 1, 0, 0, "1.3.6.1.2.1.1.1.0")) != nil {
		t.Fatal("answered the wrong community")
	}
	if a.Handle(request(0, "s3cret-comm", pduGet, 1, 0, 0, "1.3.6.1.2.1.1.1.0")) != nil {
		t.Fatal("answered SNMP v1")
	}
	if a.Handle(request(3, "s3cret-comm", pduGet, 1, 0, 0, "1.3.6.1.2.1.1.1.0")) != nil {
		t.Fatal("answered v3 framing")
	}
	// A set request is never honoured: it is a type the agent does not handle.
	if a.Handle(request(1, "s3cret-comm", 0xa3, 1, 0, 0, "1.3.6.1.2.1.1.5.0")) != nil {
		t.Fatal("answered a set request")
	}
	if (&Agent{Vars: agent().Vars}).Handle(request(1, "", pduGet, 1, 0, 0, "1.3.6.1.2.1.1.1.0")) != nil {
		t.Fatal("an empty community must never match")
	}
	for _, junk := range [][]byte{nil, {0x30}, {0x30, 0x84, 0xff, 0xff, 0xff, 0xff}, bytes.Repeat([]byte{0xff}, 40)} {
		if a.Handle(junk) != nil {
			t.Fatal("answered junk")
		}
	}
}

func TestServeOverUDP(t *testing.T) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Skip("no udp")
	}
	defer pc.Close()
	go agent().Serve(pc)
	c, _ := net.Dial("udp", pc.LocalAddr().String())
	defer c.Close()
	c.Write(request(1, "s3cret-comm", pduGet, 42, 0, 0, "1.3.6.1.2.1.1.1.0"))
	buf := make([]byte, 2048)
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := c.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if got := parse(t, buf[:n]); string(got.vars[0].Val.Bytes) != "Gatehouse test" {
		t.Fatalf("%+v", got)
	}
}

func TestReadIfaces(t *testing.T) {
	root := t.TempDir()
	d := filepath.Join(root, "sys/class/net/eth0")
	os.MkdirAll(filepath.Join(d, "statistics"), 0o755)
	w := func(name, v string) { os.WriteFile(filepath.Join(d, name), []byte(v+"\n"), 0o644) }
	w("ifindex", "2")
	w("type", "1")
	w("mtu", "1500")
	w("speed", "1000")
	w("address", "aa:bb:cc:00:11:22")
	w("operstate", "up")
	w("flags", "0x1003")
	w("statistics/rx_bytes", "100")
	w("statistics/tx_bytes", "200")
	ifs := ReadIfaces(root)
	if len(ifs) != 1 {
		t.Fatalf("%v", ifs)
	}
	f := ifs[0]
	if f.Index != 2 || f.Type != 6 || f.SpeedMbps != 1000 || !f.Up || f.Oper != 1 || f.InOct != 100 || f.OutOct != 200 || !bytes.Equal(f.MAC, []byte{0xaa, 0xbb, 0xcc, 0, 0x11, 0x22}) {
		t.Fatalf("%+v", f)
	}
}

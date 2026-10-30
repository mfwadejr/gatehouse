package flowexp

import (
	"testing"
	"time"
)

const sample = `ipv4     2 tcp      6 431999 ESTABLISHED src=192.168.1.20 dst=93.184.216.34 sport=50000 dport=443 packets=10 bytes=1500 src=93.184.216.34 dst=203.0.113.5 sport=443 dport=50000 packets=12 bytes=9000 [ASSURED] mark=0 zone=0 use=2
ipv4     2 udp      17 25 src=192.168.1.20 dst=1.1.1.1 sport=40000 dport=53 packets=1 bytes=70 src=1.1.1.1 dst=203.0.113.5 sport=53 dport=40000 packets=1 bytes=120 mark=0 use=1
ipv4     2 icmp     1 29 src=192.168.1.20 dst=8.8.8.8 type=8 code=0 id=1 packets=1 bytes=84 src=8.8.8.8 dst=203.0.113.5 type=0 code=0 id=1 packets=1 bytes=84 mark=0 use=1
ipv4     2 tcp      6 100 ESTABLISHED src=127.0.0.1 dst=127.0.0.1 sport=1 dport=2 packets=1 bytes=1 src=127.0.0.1 dst=127.0.0.1 sport=2 dport=1 packets=1 bytes=1
ipv6     10 tcp     6 100 ESTABLISHED src=fe80::1 dst=fe80::2 sport=1 dport=2 packets=1 bytes=1 src=fe80::2 dst=fe80::1 sport=2 dport=1 packets=1 bytes=1
garbage
`

func TestParseConntrack(t *testing.T) {
	cs := ParseConntrack(sample)
	if len(cs) != 3 {
		t.Fatalf("want 3 connections, got %d", len(cs))
	}
	c := cs[0]
	if c.Proto != 6 || c.Src.String() != "192.168.1.20" || c.DPort != 443 || c.OBytes != 1500 || c.RBytes != 9000 || c.RDst.String() != "203.0.113.5" || c.RSPort != 443 {
		t.Fatalf("bad parse: %+v", c)
	}
	if cs[2].Proto != 1 || cs[2].OBytes != 84 {
		t.Fatalf("icmp: %+v", cs[2])
	}
}

func TestTrackerReportsEndAndLongFlows(t *testing.T) {
	tr := &Tracker{Active: time.Minute}
	t0 := time.Unix(1000, 0)
	c := ParseConntrack(sample)[:1]
	if r := tr.Poll(t0, c); len(r) != 0 {
		t.Fatal("a new connection reports nothing yet")
	}
	c[0].OBytes, c[0].OPackets = 3000, 20
	r := tr.Poll(t0.Add(30*time.Second), c)
	if len(r) != 0 {
		t.Fatalf("too early: %v", r)
	}
	r = tr.Poll(t0.Add(61*time.Second), c)
	if len(r) != 2 || r[0].Octets != 3000 || r[0].Packets != 20 {
		t.Fatalf("long flow: %+v", r)
	}
	// After the first report only new traffic counts.
	c[0].OBytes, c[0].OPackets = 3300, 22
	tr.Poll(t0.Add(65*time.Second), c)
	r = tr.Poll(t0.Add(70*time.Second), nil)
	if len(r) != 1 || r[0].Octets != 300 || r[0].Packets != 2 {
		t.Fatalf("end: %+v", r)
	}
	if tr.Open() != 0 {
		t.Fatal("ended flow should be forgotten")
	}
}

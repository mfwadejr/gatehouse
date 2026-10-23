package dhcp

import (
	"net"
	"testing"
	"time"
)

// Exercises the real socket path over loopback using INFORM, whose reply is unicast to ciaddr.
func TestServerSocket(t *testing.T) {
	e := newEngine(t)
	e.db.Exec(`INSERT INTO dhcp_scopes(id,name,iface,mode,subnet,range_start,range_end,router,lease_seconds)
		VALUES(9,'lo','lo','serve','127.0.0.0/24','127.0.0.100','127.0.0.110','127.0.0.1',600)`)
	if err := e.Reload(); err != nil {
		t.Fatal(err)
	}
	s := NewServer(e, "127.0.0.1:0")
	s.Logf = t.Logf
	if err := s.Start(); err != nil {
		t.Skipf("cannot listen: %v", err)
	}
	defer s.Stop()

	c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	p := pkt(Inform, "aa:bb:cc:00:00:55", nil)
	p.CIAddr = net.IPv4(127, 0, 0, 1).To4()
	if _, err := c.WriteTo(p.Marshal(), s.LocalAddr()); err != nil {
		t.Fatal(err)
	}
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 1500)
	n, _, err := c.ReadFrom(buf)
	if err != nil {
		t.Fatalf("no reply: %v", err)
	}
	r, err := Parse(buf[:n])
	if err != nil || r.Type() != Ack || r.XID != p.XID {
		t.Fatalf("bad reply: %+v %v", r, err)
	}
}

func TestDestination(t *testing.T) {
	req := pkt(Request, "aa:bb:cc:00:00:01", nil)
	ack := &Packet{Options: map[byte][]byte{OptMsgType: {Ack}}}
	if d := destination(req, ack, &net.UDPAddr{IP: net.IPv4zero, Port: 68}); !d.IP.Equal(net.IPv4bcast) || d.Port != 68 {
		t.Fatalf("broadcast: %v", d)
	}
	req.CIAddr = net.IPv4(192, 168, 1, 5).To4()
	if d := destination(req, ack, &net.UDPAddr{IP: req.CIAddr, Port: 68}); !d.IP.Equal(req.CIAddr) {
		t.Fatalf("renewal unicast: %v", d)
	}
	req.GIAddr = net.IPv4(10, 0, 5, 1).To4()
	if d := destination(req, ack, &net.UDPAddr{IP: req.GIAddr, Port: 67}); !d.IP.Equal(req.GIAddr) || d.Port != 67 {
		t.Fatalf("relay: %v", d)
	}
}

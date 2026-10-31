package dnssrv

import (
	"context"
	"net"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"

	"github.com/miekg/dns"
)

// tunnelResolver is a small DNS server standing in for the resolver reached through the tunnel.
func tunnelResolver(t *testing.T) (addr string, hits *atomic.Int32, stop func()) {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	hits = new(atomic.Int32)
	srv := &dns.Server{PacketConn: pc, Handler: dns.HandlerFunc(func(w dns.ResponseWriter, r *dns.Msg) {
		hits.Add(1)
		m := new(dns.Msg)
		m.SetReply(r)
		m.Answer = []dns.RR{&dns.A{Hdr: dns.RR_Header{Name: r.Question[0].Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300}, A: net.ParseIP("198.51.100.7").To4()}}
		w.WriteMsg(m)
	})}
	go srv.ActivateAndServe()
	return pc.LocalAddr().String(), hits, func() { srv.Shutdown() }
}

// viaServer is a server whose normal pool counts its calls and whose tunnel pool really talks to
// the local resolver. The recorded devices say which tunnel the sockets were tied to.
func viaServer(t *testing.T, tunnelAddr string, route func(string) Path) (*Server, *atomic.Int32, *[]string) {
	t.Helper()
	var normal atomic.Int32
	s, _ := newTestServer(t, &normal)
	s.pool.Set([]*Upstream{{ID: 1, Name: "Quad9", Addr: tunnelAddr, Proto: "udp"}})
	var mu sync.Mutex
	var devs []string
	old := bind
	bind = func(dev string) func(string, string, syscall.RawConn) error {
		mu.Lock()
		devs = append(devs, dev)
		mu.Unlock()
		return nil
	}
	t.Cleanup(func() { bind = old })
	// the normal pool answers from a stub, so a call there means "left the normal way"
	s.pool.exchange = func(ctx context.Context, u *Upstream, req *dns.Msg) (*dns.Msg, error) {
		normal.Add(1)
		m := new(dns.Msg)
		m.SetReply(req)
		m.Answer = []dns.RR{&dns.A{Hdr: dns.RR_Header{Name: req.Question[0].Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300}, A: net.ParseIP("203.0.113.9").To4()}}
		return m, nil
	}
	s.Route = route
	return s, &normal, &devs
}

func TestDevicesOnAVPNAskThroughItWithACacheOfTheirOwn(t *testing.T) {
	addr, hits, stop := tunnelResolver(t)
	defer stop()
	s, normal, devs := viaServer(t, addr, func(c string) Path {
		if c == "192.168.1.20" {
			return Path{Dev: "tun0"}
		}
		return Path{}
	})
	r := func(client string) *dns.Msg {
		q := new(dns.Msg)
		q.SetQuestion("example.org.", dns.TypeA)
		return s.Resolve(client, q)
	}
	if a := r("192.168.1.20"); len(a.Answer) != 1 || a.Answer[0].(*dns.A).A.String() != "198.51.100.7" {
		t.Fatalf("answer: %v", a)
	}
	if normal.Load() != 0 || hits.Load() != 1 {
		t.Fatalf("the lookup left the wrong way: normal=%d tunnel=%d", normal.Load(), hits.Load())
	}
	if len(*devs) == 0 || (*devs)[0] != "tun0" {
		t.Fatalf("sockets were not tied to the tunnel: %v", *devs)
	}
	r("192.168.1.20") // from the tunnel's own cache
	if hits.Load() != 1 {
		t.Fatalf("the tunnel cache was not used: %d", hits.Load())
	}
	// A device that does not use the VPN must not be answered from the tunnel's cache.
	if a := r("192.168.1.30"); a.Answer[0].(*dns.A).A.String() != "203.0.113.9" || normal.Load() != 1 {
		t.Fatalf("the normal device got a tunnel answer: %v normal=%d", a, normal.Load())
	}
}

func TestKillSwitchHoldsLookupsAndTheirAbsenceFallsBack(t *testing.T) {
	addr, hits, stop := tunnelResolver(t)
	s, normal, _ := viaServer(t, addr, func(string) Path { return Path{Dev: "tun0", Hold: true} })
	q := new(dns.Msg)
	q.SetQuestion("held.example.org.", dns.TypeA)
	if a := s.Resolve("192.168.1.20", q); a.Rcode != dns.RcodeServerFailure || normal.Load() != 0 || hits.Load() != 0 {
		t.Fatalf("a held lookup got out: rcode=%d normal=%d tunnel=%d", a.Rcode, normal.Load(), hits.Load())
	}
	// The tunnel's resolver stops answering. Without the kill switch DNS carries on the normal way.
	stop()
	s.Route = func(string) Path { return Path{Dev: "tun0"} }
	q2 := new(dns.Msg)
	q2.SetQuestion("fallback.example.org.", dns.TypeA)
	if a := s.Resolve("192.168.1.20", q2); a.Rcode != dns.RcodeSuccess || normal.Load() != 1 {
		t.Fatalf("no fallback: rcode=%d normal=%d", a.Rcode, normal.Load())
	}
	// With the kill switch on, the same failure is a failure and nothing leaves the normal way.
	s.Route = func(string) Path { return Path{Dev: "tun0", Strict: true} }
	q3 := new(dns.Msg)
	q3.SetQuestion("strict.example.org.", dns.TypeA)
	if a := s.Resolve("192.168.1.20", q3); a.Rcode != dns.RcodeServerFailure || normal.Load() != 1 {
		t.Fatalf("a strict lookup fell back: rcode=%d normal=%d", a.Rcode, normal.Load())
	}
}

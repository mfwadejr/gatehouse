package dnssrv

import (
	"context"
	"net"
	"sync/atomic"
	"testing"

	"github.com/miekg/dns"
)

func TestDifferentServerForChosenNames(t *testing.T) {
	var normal, office atomic.Int32
	s, _ := newTestServer(t, &normal)
	s.SetForwards([]FwdRule{{Domain: "office.example", Servers: []string{"10.0.0.2:53"}}})
	s.fwd.Load().rules[0].pool.exchange = func(ctx context.Context, u *Upstream, req *dns.Msg) (*dns.Msg, error) {
		office.Add(1)
		m := new(dns.Msg)
		m.SetReply(req)
		m.Answer = []dns.RR{&dns.A{Hdr: dns.RR_Header{Name: req.Question[0].Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60}, A: net.ParseIP("10.0.0.50").To4()}}
		return m, nil
	}
	a := ask(s, "files.office.example.", dns.TypeA)
	if len(a.Answer) != 1 || a.Answer[0].(*dns.A).A.String() != "10.0.0.50" || office.Load() != 1 || normal.Load() != 0 {
		t.Fatalf("office name did not go to the office server: %v office=%d normal=%d", a.Answer, office.Load(), normal.Load())
	}
	// The domain itself too, and a second ask is served from the cache.
	ask(s, "office.example.", dns.TypeA)
	ask(s, "files.office.example.", dns.TypeA)
	if office.Load() != 2 {
		t.Fatalf("cache was not used: %d", office.Load())
	}
	// Another name goes the usual way, and so does a name that only ends in the same letters.
	ask(s, "www.example.org.", dns.TypeA)
	ask(s, "notoffice.example.", dns.TypeA)
	if normal.Load() != 2 {
		t.Fatalf("other names should use the usual servers: %d", normal.Load())
	}
	// Your own names still come first.
	if a := ask(s, "nas.lan.", dns.TypeA); len(a.Answer) != 1 || a.Answer[0].(*dns.A).A.String() != "192.168.1.10" {
		t.Fatalf("local name: %v", a.Answer)
	}
	// Taking the rule away sends the name the usual way again.
	s.SetForwards(nil)
	before := normal.Load()
	ask(s, "files.office.example.", dns.TypeA)
	if normal.Load() != before+1 {
		t.Fatal("the rule was not removed")
	}
}

func TestTheLongestDomainWins(t *testing.T) {
	s, _ := newTestServer(t, new(atomic.Int32))
	s.SetForwards([]FwdRule{{Domain: "example", Servers: []string{"10.0.0.1:53"}}, {Domain: "office.example", Servers: []string{"10.0.0.2:53"}}})
	p := s.forwardPool("a.office.example.")
	if p == nil || p.ups[0].Addr != "10.0.0.2:53" {
		t.Fatalf("wrong pool: %v", p)
	}
}

func TestForwardRulesAreCheckedInPlainWords(t *testing.T) {
	good, err := CleanFwdRule(FwdRule{Domain: " *.Office.Example. ", Servers: []string{"10.0.0.2", "[fd00::2]:5353", "10.0.0.2:53"}})
	if err != nil || good.Domain != "office.example" || len(good.Servers) != 2 || good.Servers[0] != "10.0.0.2:53" || good.Servers[1] != "[fd00::2]:5353" {
		t.Fatalf("%+v %v", good, err)
	}
	for _, bad := range []FwdRule{
		{Domain: "", Servers: []string{"10.0.0.2"}},
		{Domain: "has space.example", Servers: []string{"10.0.0.2"}},
		{Domain: "office.example"},
		{Domain: "office.example", Servers: []string{"dns.example.com"}},
		{Domain: "office.example", Servers: []string{"10.0.0.2:99x"}},
		{Domain: "-bad.example", Servers: []string{"10.0.0.2"}},
	} {
		if _, err := CleanFwdRule(bad); err == nil {
			t.Fatalf("accepted %+v", bad)
		}
	}
}

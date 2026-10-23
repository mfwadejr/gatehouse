package dnssrv

import (
	"testing"
	"time"

	"github.com/miekg/dns"
)

func TestRateLimiter(t *testing.T) {
	r := NewRateLimiter(10, 20)
	now := time.Unix(1000, 0)
	r.now = func() time.Time { return now }
	for i := 0; i < 20; i++ {
		if !r.Allow("a") {
			t.Fatalf("burst denied at %d", i)
		}
	}
	if r.Allow("a") {
		t.Fatal("over burst allowed")
	}
	if !r.Allow("b") {
		t.Fatal("other client affected")
	}
	now = now.Add(time.Second) // refills 10 tokens
	n := 0
	for r.Allow("a") {
		n++
	}
	if n != 10 {
		t.Fatalf("refill gave %d", n)
	}
	if !r.Allow("127.0.0.1") {
		t.Fatal("loopback must be exempt")
	}
}

func a(name, ip string, ttl uint32) dns.RR {
	rr, _ := dns.NewRR(name + " " + itoa(ttl) + " IN A " + ip)
	return rr
}

func itoa(n uint32) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}

func TestCacheTTLAndNegative(t *testing.T) {
	c := NewCache(2)
	now := time.Unix(1000, 0)
	c.now = func() time.Time { return now }
	q := dns.Question{Name: "example.com.", Qtype: dns.TypeA, Qclass: dns.ClassINET}
	m := new(dns.Msg)
	m.SetQuestion(q.Name, q.Qtype)
	m.Answer = []dns.RR{a("example.com.", "1.2.3.4", 100)}
	c.Put(q, false, m)
	now = now.Add(30 * time.Second)
	got, ok := c.Get(q, false)
	if !ok || got.Answer[0].Header().Ttl != 70 {
		t.Fatalf("ttl decrement: %v %v", ok, got)
	}
	now = now.Add(80 * time.Second)
	if _, ok := c.Get(q, false); ok {
		t.Fatal("expired entry served")
	}
	// DO bit is part of the key.
	c.Put(q, true, m)
	if _, ok := c.Get(q, false); ok {
		t.Fatal("do/non-do mixed")
	}
	// Negative caching uses SOA.
	nx := new(dns.Msg)
	nx.SetQuestion("nope.example.com.", dns.TypeA)
	nx.Rcode = dns.RcodeNameError
	soa, _ := dns.NewRR("example.com. 3600 IN SOA ns. host. 1 2 3 4 60")
	nx.Ns = []dns.RR{soa}
	c.Put(nx.Question[0], false, nx)
	if _, ok := c.Get(nx.Question[0], false); !ok {
		t.Fatal("negative not cached")
	}
	// SERVFAIL is never cached.
	sf := new(dns.Msg)
	sf.SetQuestion("bad.example.com.", dns.TypeA)
	sf.Rcode = dns.RcodeServerFailure
	c.Put(sf.Question[0], false, sf)
	if _, ok := c.Get(sf.Question[0], false); ok {
		t.Fatal("servfail cached")
	}
	// LRU eviction with max=2.
	for _, n := range []string{"a.test.", "b.test.", "c.test."} {
		mm := new(dns.Msg)
		mm.SetQuestion(n, dns.TypeA)
		mm.Answer = []dns.RR{a(n, "1.1.1.1", 300)}
		c.Put(mm.Question[0], false, mm)
	}
	if c.Len() != 2 {
		t.Fatalf("len %d", c.Len())
	}
}

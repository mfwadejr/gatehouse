package dnssrv

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func TestScopedHookBlocksAndFailsOpen(t *testing.T) {
	var calls atomic.Int32
	s, _ := newTestServer(t, &calls)
	ask := func() *dns.Msg {
		r := new(dns.Msg)
		r.SetQuestion("blocked.example.org.", dns.TypeA)
		return s.handle("192.168.1.5", r, time.Now(), false)
	}
	// no hook: answered
	if m := ask(); m.Rcode != dns.RcodeSuccess || len(m.Answer) == 0 {
		t.Fatalf("expected an answer, got %v", m)
	}
	// a hook that blocks this one name
	s.Scoped = func(_ string, _ ClientInfo, name string) string {
		if name == "blocked.example.org" {
			return "blocked app: Test"
		}
		return ""
	}
	if m := ask(); len(m.Answer) > 0 && m.Answer[0].(*dns.A).A.String() == "203.0.113.9" {
		t.Fatalf("should have been blocked: %v", m)
	}
	// a hook that crashes must not stop the lookup
	s.Scoped = func(string, ClientInfo, string) string { panic("boom") }
	if m := ask(); m.Rcode != dns.RcodeSuccess || len(m.Answer) == 0 {
		t.Fatalf("a crashing hook stopped the lookup: %v", m)
	}
}

func TestListHitAndNamesWorkForAListNoProfileUses(t *testing.T) {
	var calls atomic.Int32
	s, m := newTestServer(t, &calls)
	id, err := m.AddLocal("Gambling", "0.0.0.0 bets.example.net\n0.0.0.0 casino.example.net\n", "hosts")
	if err != nil {
		t.Fatal(err)
	}
	// no profile uses the list, so a lookup is not blocked by the normal filter ...
	if r := ask(s, "bets.example.net.", dns.TypeA); len(r.Answer) == 0 || r.Answer[0].(*dns.A).A.String() != "203.0.113.9" {
		t.Fatalf("list blocked something no profile asked for: %v", r)
	}
	// ... but a scope rule can ask the list directly
	if _, ok := s.ListHit("bets.example.net", id); !ok {
		t.Fatal("list hit missing")
	}
	if _, ok := s.ListHit("fine.example.net", id); ok {
		t.Fatal("unexpected hit")
	}
	rows, total, err := m.Names(id, "casino", 10)
	if err != nil || total != 2 || len(rows) != 1 || rows[0].Name != "casino.example.net" {
		t.Fatalf("names: %v %d %v", rows, total, err)
	}
}

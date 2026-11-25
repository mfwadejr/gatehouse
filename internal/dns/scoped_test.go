package dnssrv

import (
	"context"
	"net/http"
	"net/http/httptest"
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

func TestAnExceptionLetsAListedNameThroughButNeverAWrittenBlock(t *testing.T) {
	var calls atomic.Int32
	s, m := newTestServer(t, &calls)
	m.AllowPrivate = true
	if err := m.SeedDefaults(); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ads.example.net\ntrack.example.net\n"))
	}))
	defer ts.Close()
	id, err := m.Add("test", ts.URL+"/list.txt", "domains", 24)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Refresh(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if err := m.Reload(); err != nil {
		t.Fatal(err)
	}
	blocked := func(name string) bool {
		a := ask(s, name, dns.TypeA)
		return len(a.Answer) == 1 && a.Answer[0].(*dns.A).A.IsUnspecified()
	}
	if !blocked("ads.example.net.") || !blocked("track.example.net.") {
		t.Fatal("the list should block both names")
	}
	// An exception for one name, from the hook, lets only that name through.
	s.ScopedAllow = func(_ string, _ ClientInfo, name string) string {
		if name == "ads.example.net" {
			return "ads.example.net"
		}
		return ""
	}
	if blocked("ads.example.net.") {
		t.Fatal("the exception did not let the name through")
	}
	if !blocked("track.example.net.") {
		t.Fatal("the exception must not cover other names")
	}
	// A block the administrator wrote on purpose is not overridden.
	if err := m.AddCustom("ads.example.net", "block", "mine"); err != nil {
		t.Fatal(err)
	}
	if err := m.Reload(); err != nil {
		t.Fatal(err)
	}
	if !blocked("ads.example.net.") {
		t.Fatal("a written block was overridden by an exception")
	}
	// A hook that crashes means no exception: the list still blocks.
	s.ScopedAllow = func(string, ClientInfo, string) string { panic("boom") }
	if !blocked("track.example.net.") {
		t.Fatal("a crashing hook stopped the block")
	}
}

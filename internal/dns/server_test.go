package dnssrv

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/miekg/dns"

	"gatehouse/internal/store"
)

type fakeDir struct{ info ClientInfo }

func (f fakeDir) Client(string) ClientInfo { return f.info }

type fakeNames struct{}

func (fakeNames) LookupHost(n string) []net.IP {
	if n == "printer" {
		return []net.IP{net.ParseIP("192.168.1.50")}
	}
	return nil
}
func (fakeNames) LookupAddr(ip net.IP) (string, bool) {
	if ip.String() == "192.168.1.50" {
		return "printer", true
	}
	return "", false
}

func newTestServer(t *testing.T, calls *atomic.Int32) (*Server, *Manager) {
	t.Helper()
	db, err := store.OpenConfig(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	pool := NewPool(nil)
	pool.Set([]*Upstream{{ID: 1, Name: "fake", Addr: "192.0.2.1:53", Proto: "udp"}})
	pool.exchange = func(ctx context.Context, u *Upstream, req *dns.Msg) (*dns.Msg, error) {
		calls.Add(1)
		m := new(dns.Msg)
		m.SetReply(req)
		q := req.Question[0]
		if q.Name == "nope.example.org." {
			m.Rcode = dns.RcodeNameError
			return m, nil
		}
		if q.Qtype == dns.TypeA {
			m.Answer = []dns.RR{&dns.A{Hdr: dns.RR_Header{Name: q.Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300}, A: net.ParseIP("203.0.113.9").To4()}}
		}
		return m, nil
	}
	s := NewServer(DefaultOptions(), pool, fakeDir{ClientInfo{Name: "laptop", GroupID: 1, Filtering: true}}, fakeNames{}, nil)
	s.local.p.Store(buildLocal("lan", []LocalRecord{{Name: "nas.lan", Type: "A", Value: "192.168.1.10"}}))
	m := NewManager(db, t.TempDir(), &s.eng)
	return s, m
}

func ask(s *Server, name string, qt uint16) *dns.Msg {
	r := new(dns.Msg)
	r.SetQuestion(name, qt)
	return s.Resolve("192.168.1.20", r)
}

func TestForwardAndCache(t *testing.T) {
	var calls atomic.Int32
	s, _ := newTestServer(t, &calls)
	a := ask(s, "example.org.", dns.TypeA)
	if a.Rcode != dns.RcodeSuccess || len(a.Answer) != 1 {
		t.Fatalf("bad answer: %v", a)
	}
	ask(s, "example.org.", dns.TypeA)
	if calls.Load() != 1 {
		t.Fatalf("second query should be cached, upstream calls = %d", calls.Load())
	}
	if nx := ask(s, "nope.example.org.", dns.TypeA); nx.Rcode != dns.RcodeNameError {
		t.Fatalf("want NXDOMAIN, got %d", nx.Rcode)
	}
}

func TestLocalAnswers(t *testing.T) {
	var calls atomic.Int32
	s, _ := newTestServer(t, &calls)
	if a := ask(s, "nas.lan.", dns.TypeA); len(a.Answer) != 1 || a.Answer[0].(*dns.A).A.String() != "192.168.1.10" {
		t.Fatalf("local record: %v", a)
	}
	if a := ask(s, "printer.lan.", dns.TypeA); len(a.Answer) != 1 {
		t.Fatalf("lease name: %v", a)
	}
	if a := ask(s, "50.1.168.192.in-addr.arpa.", dns.TypePTR); len(a.Answer) != 1 {
		t.Fatalf("ptr: %v", a)
	}
	if a := ask(s, "ghost.lan.", dns.TypeA); a.Rcode != dns.RcodeNameError {
		t.Fatalf("unknown local name must be NXDOMAIN, got %d", a.Rcode)
	}
	if a := ask(s, "9.9.9.10.in-addr.arpa.", dns.TypePTR); a.Rcode != dns.RcodeNameError {
		t.Fatalf("private reverse must not leak, got %d", a.Rcode)
	}
	if calls.Load() != 0 {
		t.Fatalf("local queries reached upstream: %d", calls.Load())
	}
}

func TestBlocklistEndToEnd(t *testing.T) {
	var calls atomic.Int32
	s, m := newTestServer(t, &calls)
	m.AllowPrivate = true
	if err := m.SeedDefaults(); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ads.example.net\ntracker.example.org\n"))
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
	// The test client is in group 1, which must have the new list via the Default group.
	a := ask(s, "ads.example.net.", dns.TypeA)
	if len(a.Answer) != 1 || !a.Answer[0].(*dns.A).A.IsUnspecified() {
		t.Fatalf("expected a null answer, got %v", a)
	}
	if calls.Load() != 0 {
		t.Fatal("blocked query reached upstream")
	}
	// Subdomains are blocked too.
	if a := ask(s, "x.tracker.example.org.", dns.TypeA); len(a.Answer) != 1 || !a.Answer[0].(*dns.A).A.IsUnspecified() {
		t.Fatalf("subdomain not blocked: %v", a)
	}
	// A custom allow rule wins.
	if err := m.AddCustom("ads.example.net", "allow", "test"); err != nil {
		t.Fatal(err)
	}
	if a := ask(s, "ads.example.net.", dns.TypeA); a.Answer[0].(*dns.A).A.IsUnspecified() {
		t.Fatal("allow rule did not override the list")
	}
	ex := m.Explain("tracker.example.org", 1)
	if !ex.Blocked || ex.Reason != "list" {
		t.Fatalf("explain: %+v", ex)
	}
}

func TestBlockModes(t *testing.T) {
	var calls atomic.Int32
	s, m := newTestServer(t, &calls)
	if err := m.SeedDefaults(); err != nil {
		t.Fatal(err)
	}
	if _, err := m.AddLocal("mine", "blocked.example\n", "domains"); err != nil {
		t.Fatal(err)
	}
	o := s.Options()
	o.BlockMode = "nxdomain"
	s.Apply(o, nil)
	if a := ask(s, "blocked.example.", dns.TypeA); a.Rcode != dns.RcodeNameError {
		t.Fatalf("nxdomain mode: %d", a.Rcode)
	}
	o.BlockMode = "refused"
	s.Apply(o, nil)
	if a := ask(s, "blocked.example.", dns.TypeA); a.Rcode != dns.RcodeRefused {
		t.Fatalf("refused mode: %d", a.Rcode)
	}
}

func TestRateLimitRefuses(t *testing.T) {
	var calls atomic.Int32
	s, _ := newTestServer(t, &calls)
	o := s.Options()
	o.RateLimit, o.RateBurst = 1, 3
	s.Apply(o, nil)
	var events atomic.Int32
	s.OnRateLimited = func(string) { events.Add(1) }
	refused := 0
	for i := 0; i < 20; i++ {
		if ask(s, "example.org.", dns.TypeA).Rcode == dns.RcodeRefused {
			refused++
		}
	}
	if refused < 10 {
		t.Fatalf("expected most queries refused, got %d", refused)
	}
	if events.Load() != 1 {
		t.Fatalf("security event should fire once per minute, got %d", events.Load())
	}
}

func TestCanary(t *testing.T) {
	var calls atomic.Int32
	s, _ := newTestServer(t, &calls)
	if a := ask(s, "use-application-dns.net.", dns.TypeA); a.Rcode != dns.RcodeNameError {
		t.Fatalf("canary: %d", a.Rcode)
	}
}

func TestListenAndQuery(t *testing.T) {
	var calls atomic.Int32
	s, _ := newTestServer(t, &calls)
	if err := s.Start([]string{"127.0.0.1:0"}); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	addrs := s.Addrs()
	if len(addrs) < 2 {
		t.Fatalf("want udp and tcp listeners, got %v", addrs)
	}
	for _, network := range []string{"udp", "tcp"} {
		var addr string
		for _, a := range addrs {
			if a.Network() == network {
				addr = a.String()
			}
		}
		c := &dns.Client{Net: network}
		r := new(dns.Msg)
		r.SetQuestion("nas.lan.", dns.TypeA)
		resp, _, err := c.Exchange(r, addr)
		if err != nil || len(resp.Answer) != 1 {
			t.Fatalf("%s: %v %v", network, resp, err)
		}
	}
}

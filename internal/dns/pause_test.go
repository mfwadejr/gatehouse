package dnssrv

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func TestPausingBlockingLetsNamesThroughThenBlocksAgain(t *testing.T) {
	var calls atomic.Int32
	s, m := newTestServer(t, &calls)
	m.AllowPrivate = true
	if err := m.SeedDefaults(); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ads.example.net\n")) }))
	defer ts.Close()
	id, err := m.Add("test", ts.URL+"/l.txt", "domains", 24)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Refresh(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	if err := m.Reload(); err != nil {
		t.Fatal(err)
	}
	blocked := func() bool {
		a := ask(s, "ads.example.net.", dns.TypeA)
		return len(a.Answer) == 1 && a.Answer[0].(*dns.A).A.IsUnspecified()
	}
	if !blocked() {
		t.Fatal("expected the name to be blocked before the pause")
	}
	s.PauseBlocking(time.Now().Add(5 * time.Minute))
	if on, until := s.BlockingPaused(time.Now()); !on || until.IsZero() {
		t.Fatalf("pause not reported: %v %v", on, until)
	}
	if blocked() {
		t.Fatal("the name was blocked during a pause")
	}
	// A pause that has run out counts as over.
	s.PauseBlocking(time.Now().Add(-time.Second))
	if on, _ := s.BlockingPaused(time.Now()); on {
		t.Fatal("an expired pause still counts")
	}
	if !blocked() {
		t.Fatal("blocking did not come back when the pause ran out")
	}
	// Until resumed.
	s.PauseBlocking(time.Time{})
	if on, until := s.BlockingPaused(time.Now()); !on || !until.IsZero() || blocked() {
		t.Fatal("pause until resumed did not hold")
	}
	s.ResumeBlocking()
	if !blocked() {
		t.Fatal("resume did not switch blocking back on")
	}
}

func TestPrivateRelayNamesAreRefusedOnlyWhenAsked(t *testing.T) {
	var calls atomic.Int32
	s, _ := newTestServer(t, &calls)
	if a := ask(s, "mask.icloud.com.", dns.TypeA); a.Rcode != dns.RcodeSuccess {
		t.Fatalf("Private Relay name refused while the switch was off: %d", a.Rcode)
	}
	o := s.Options()
	o.PrivateRelay = true
	s.Apply(o, nil)
	for _, n := range []string{"mask.icloud.com.", "mask-h2.icloud.com.", "mask-canary.cloudflare-dns.com."} {
		if a := ask(s, n, dns.TypeA); a.Rcode != dns.RcodeNameError {
			t.Fatalf("%s: %d", n, a.Rcode)
		}
	}
	if a := ask(s, "www.icloud.com.", dns.TypeA); a.Rcode != dns.RcodeSuccess {
		t.Fatal("an ordinary iCloud name was refused")
	}
}

func TestBlockedAnswersCarryTheChosenMemoryTime(t *testing.T) {
	var calls atomic.Int32
	s, m := newTestServer(t, &calls)
	m.AllowPrivate = true
	m.AddCustom("blocked.example.net", "block", "test")
	m.Reload()
	o := s.Options()
	o.BlockTTL = 7
	s.Apply(o, nil)
	a := ask(s, "blocked.example.net.", dns.TypeA)
	if len(a.Answer) != 1 || a.Answer[0].Header().Ttl != 7 {
		t.Fatalf("expected a 7 second answer, got %v", a.Answer)
	}
}

package dnssrv

import (
	"testing"

	"github.com/miekg/dns"
	"sync/atomic"
)

func TestNamePatternsMatchTheWholeName(t *testing.T) {
	got, err := TryPattern("glob", "ads*.example.com", []string{"ads.example.com", "ads12.example.com", "x.ads1.example.com", "badads.example.com", "ads1.example.org"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "ads.example.com" || got[1] != "ads12.example.com" {
		t.Fatalf("matched %v", got)
	}
	got, _ = TryPattern("glob", "track?.example.com", []string{"track1.example.com", "track12.example.com"})
	if len(got) != 1 || got[0] != "track1.example.com" {
		t.Fatalf("? matched %v", got)
	}
	got, _ = TryPattern("re", `^(ads|track)[0-9]+\.`, []string{"ads1.example.com", "track22.x.net", "www.ads1.com", "adsx.example.com"})
	if len(got) != 2 {
		t.Fatalf("regexp matched %v", got)
	}
}

func TestBadAndDangerousPatternsAreRefusedInPlainWords(t *testing.T) {
	for _, c := range []struct{ kind, text string }{
		{"glob", ""}, {"glob", "ads example.com"}, {"glob", "ads/*.com"}, {"glob", "*"}, {"glob", "*.*"},
		{"re", "("}, {"re", ".*"}, {"re", "^$"}, {"re", "[a-z"}, {"re", ".+"}, {"nonsense", "a"},
	} {
		if _, err := CompilePattern(c.kind, c.text); err == nil {
			t.Errorf("accepted %s %q", c.kind, c.text)
		}
	}
	long := make([]byte, 300)
	for i := range long {
		long[i] = 'a'
	}
	if _, err := CompilePattern("glob", string(long)); err == nil {
		t.Error("accepted a very long pattern")
	}
}

func TestPatternRulesBlockAndAllowWinsOverThem(t *testing.T) {
	var calls atomic.Int32
	s, m := newTestServer(t, &calls)
	m.AllowPrivate = true
	if err := m.AddPattern("glob", "ads*.example.net", "block", "test"); err != nil {
		t.Fatal(err)
	}
	blocked := func(n string) bool {
		a := ask(s, n, dns.TypeA)
		return len(a.Answer) == 1 && a.Answer[0].(*dns.A).A.IsUnspecified()
	}
	if !blocked("ads7.example.net.") || blocked("www.example.net.") || blocked("x.ads7.example.net.") {
		t.Fatal("the pattern did not block exactly what it says")
	}
	if ex := m.Explain("ads7.example.net", 1); !ex.Blocked || ex.Reason != "custom-block" || ex.Rule != "pattern ads*.example.net" {
		t.Fatalf("explain: %+v", ex)
	}
	// A plain name allow beats the pattern, and so does an allow pattern.
	m.AddCustom("ads7.example.net", "allow", "")
	if blocked("ads7.example.net.") {
		t.Fatal("a name allow did not beat the pattern block")
	}
	m.AddPattern("glob", "ads9*.example.net", "allow", "")
	if blocked("ads99.example.net.") || !blocked("ads8.example.net.") {
		t.Fatal("an allow pattern did not beat the block pattern")
	}
	// Dangerous or broken ones never get stored.
	if err := m.AddPattern("re", ".*", "block", ""); err == nil {
		t.Fatal("a match-everything pattern was stored")
	}
}

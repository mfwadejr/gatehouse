package dnssrv

import (
	"strings"
	"testing"
)

func collect(t *testing.T, in, format string) ([]Rule, ParseStats) {
	t.Helper()
	var rules []Rule
	st, err := Parse(strings.NewReader(in), format, func(r Rule) { rules = append(rules, r) })
	if err != nil {
		t.Fatal(err)
	}
	return rules, st
}

func TestParseDomains(t *testing.T) {
	rules, st := collect(t, "# comment\nads.example.com\n\nTracker.Example.NET.  # trailing\nnot a domain\n1.2.3.4\nnodots\n", FormatAuto)
	if st.Format != FormatDomains || len(rules) != 2 {
		t.Fatalf("format=%s rules=%v", st.Format, rules)
	}
	if rules[1].Domain != "tracker.example.net" {
		t.Fatalf("normalise: %q", rules[1].Domain)
	}
}

func TestParseHosts(t *testing.T) {
	in := "127.0.0.1 localhost\n0.0.0.0 ads.one.com ads.two.com\n::1 ip6-localhost\n0.0.0.0 0.0.0.0\n# x\n127.0.0.1 evil.org # note\n"
	rules, st := collect(t, in, FormatAuto)
	if st.Format != FormatHosts {
		t.Fatalf("format %s", st.Format)
	}
	got := []string{}
	for _, r := range rules {
		got = append(got, r.Domain)
	}
	want := "ads.one.com,ads.two.com,evil.org"
	if strings.Join(got, ",") != want {
		t.Fatalf("got %v want %s", got, want)
	}
}

func TestParseABP(t *testing.T) {
	in := "! Title: test\n[Adblock Plus 2.0]\n||ads.example.com^\n@@||good.example.com^\n||track.net^$important\n||path.com/ads^\n/regex/\n##.banner\n||cdn.example.org^$third-party\n"
	rules, st := collect(t, in, FormatAuto)
	if st.Format != FormatABP {
		t.Fatalf("format %s", st.Format)
	}
	var block, allow []string
	for _, r := range rules {
		if r.Allow {
			allow = append(allow, r.Domain)
		} else {
			block = append(block, r.Domain)
		}
	}
	if strings.Join(block, ",") != "ads.example.com,cdn.example.org" || strings.Join(allow, ",") != "good.example.com" {
		t.Fatalf("block=%v allow=%v", block, allow)
	}
	if st.Skipped < 3 {
		t.Fatalf("skipped %d", st.Skipped)
	}
}

func TestIndexSubdomainAndExceptions(t *testing.T) {
	b := newIndexBuilder()
	b.setName(0, "ads")
	b.setName(1, "strict")
	b.add(0, "ads", Rule{Domain: "doubleclick.net"})
	b.add(1, "strict", Rule{Domain: "tiktok.com"})
	b.add(0, "ads", Rule{Domain: "good.doubleclick.net", Allow: true})
	ix := b.ix
	if i, rule, ok := ix.Lookup("x.y.doubleclick.net", 1<<0); !ok || i != 0 || rule != "doubleclick.net" {
		t.Fatalf("subdomain block failed: %v %v %v", i, rule, ok)
	}
	if _, _, ok := ix.Lookup("doubleclick.net.evil.com", 1<<0); ok {
		t.Fatal("suffix matched wrongly")
	}
	if _, _, ok := ix.Lookup("tiktok.com", 1<<0); ok {
		t.Fatal("list not in mask must not block")
	}
	if _, _, ok := ix.Lookup("tiktok.com", 1<<1); !ok {
		t.Fatal("list in mask must block")
	}
	if _, _, ok := ix.Lookup("a.good.doubleclick.net", 1<<0); ok {
		t.Fatal("exception ignored")
	}
}

func TestCustomRulesAllowWins(t *testing.T) {
	c := newCustom()
	c.block["example.com"] = ""
	c.allow["safe.example.com"] = ""
	if a, _ := c.Check("x.example.com"); a != "block" {
		t.Fatal("block failed")
	}
	if a, _ := c.Check("deep.safe.example.com"); a != "allow" {
		t.Fatal("allow should win")
	}
	if a, _ := c.Check("other.org"); a != "" {
		t.Fatal("unexpected match")
	}
}

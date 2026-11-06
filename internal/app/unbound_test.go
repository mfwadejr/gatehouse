package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func ubAnswers(good *bool) {
	ubExchange = func(ctx context.Context, m *dns.Msg) (*dns.Msg, error) {
		if !*good {
			return nil, errors.New("read udp: i/o timeout")
		}
		r := new(dns.Msg)
		r.SetReply(m)
		rr, _ := dns.NewRR(". 3600 IN NS a.root-servers.net.")
		r.Answer = []dns.RR{rr}
		return r, nil
	}
}

func TestResolveDirectlyFallsBackToForwardersAndComesBack(t *testing.T) {
	c, fn, _ := siteBox(t, "192.168.1.1/24")
	a := c.app
	good := true
	oldEx := ubExchange
	ubAnswers(&good)
	t.Cleanup(func() { ubExchange = oldEx })
	fn.mu.Lock()
	fn.unbound.Installed = true
	fn.mu.Unlock()
	// not installed yet: refused, and the setting is unchanged
	fn.mu.Lock()
	fn.unboundErr = errors.New("Unbound is not installed on this machine yet")
	fn.mu.Unlock()
	if rc, _, _ := c.call("PUT", "/api/dns/resolver", map[string]any{"mode": "direct"}); rc == 200 {
		t.Fatal("must refuse when Unbound cannot be set up")
	}
	if a.resolveMode() != "forward" {
		t.Fatal("the mode must stay as it was")
	}
	fn.mu.Lock()
	fn.unboundErr = nil
	fn.mu.Unlock()
	if rc, _, raw := c.call("PUT", "/api/dns/resolver", map[string]any{"mode": "direct", "dnssec": true}); rc != 200 {
		t.Fatalf("%d %s", rc, raw)
	}
	if !a.directActive() {
		t.Fatal("direct should be active")
	}
	ups := a.DNS.Pool().Status()
	if len(ups) != 1 || !strings.Contains(ups[0].Name, "Unbound") {
		t.Fatalf("pool should hold only Unbound: %+v", ups)
	}
	// Unbound stops answering: three bad probes in a row and the forwarders take over
	good = false
	for i := 0; i < ubFailsToFallback; i++ {
		a.ubObserve(a.ubProbe(context.Background()))
	}
	if a.directActive() {
		t.Fatal("should have fallen back")
	}
	if ups := a.DNS.Pool().Status(); len(ups) < 2 || strings.Contains(ups[0].Name, "Unbound") {
		t.Fatalf("the encrypted forwarders should be back: %+v", ups)
	}
	v := a.resolverView()
	if !v.Fallback || v.Active != "forwarders" || v.Reason == "" {
		t.Fatalf("%+v", v)
	}
	// it comes back after a few good answers, not after one
	good = true
	a.ubObserve(a.ubProbe(context.Background()))
	if a.directActive() {
		t.Fatal("one good answer is not enough")
	}
	for i := 1; i < ubOKsToReturn; i++ {
		a.ubObserve(a.ubProbe(context.Background()))
	}
	if !a.directActive() {
		t.Fatal("should be back on Unbound")
	}
	time.Sleep(10 * time.Millisecond)
}

func TestADNSSECFailureIsExplained(t *testing.T) {
	c, fn, _ := siteBox(t, "192.168.1.1/24")
	a := c.app
	fn.mu.Lock()
	fn.unbound.Installed = true
	fn.mu.Unlock()
	good := true
	oldEx := ubExchange
	ubAnswers(&good)
	t.Cleanup(func() { ubExchange = oldEx })
	c.call("PUT", "/api/dns/resolver", map[string]any{"mode": "direct", "dnssec": true})
	q := new(dns.Msg)
	q.SetQuestion("broken.example.", dns.TypeA)
	// with checking switched off the name is found (the fake answers everything): so it was the signatures
	if why := a.explainFailure(q); !strings.Contains(why, "DNSSEC") {
		t.Fatalf("expected a DNSSEC explanation, got %q", why)
	}
	// when the forwarders are in use there is nothing to explain
	c.call("PUT", "/api/dns/resolver", map[string]any{"mode": "forward"})
	if why := a.explainFailure(q); why != "" {
		t.Fatalf("no explanation without Unbound: %q", why)
	}
}

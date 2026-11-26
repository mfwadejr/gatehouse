package app

import (
	"net"
	"testing"

	"github.com/miekg/dns"
)

func aRec(name, ip string) dns.RR {
	return &dns.A{Hdr: dns.RR_Header{Name: name + ".", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60}, A: net.ParseIP(ip)}
}

func TestLogNamesForAddresses(t *testing.T) {
	var c nameCache
	// A name whose answer lists a public resolver must not become that resolver's label.
	c.remember("ping.ui.com", []dns.RR{aRec("ping.ui.com", "8.8.8.8"), aRec("ping.ui.com", "1.1.1.1"), aRec("ping.ui.com", "203.0.113.9")})
	if got := c.get("8.8.8.8"); got != "Google Public DNS" {
		t.Fatalf("8.8.8.8 is %q", got)
	}
	if got := c.get("1.1.1.1"); got != "Cloudflare DNS" {
		t.Fatalf("1.1.1.1 is %q", got)
	}
	if got := c.get("9.9.9.9"); got != "Quad9 DNS" {
		t.Fatalf("9.9.9.9 is %q", got)
	}
	if got := c.get("2001:4860:4860::8888"); got != "Google Public DNS" {
		t.Fatalf("Google's IPv6 address is %q", got)
	}
	// An ordinary address still gets the name that was asked for.
	if got := c.get("203.0.113.9"); got != "ping.ui.com" {
		t.Fatalf("203.0.113.9 is %q", got)
	}
	// An address that many different names point at gets no guess.
	for _, n := range []string{"a.example", "b.example", "c.example", "d.example"} {
		c.remember(n, []dns.RR{aRec(n, "198.51.100.7")})
	}
	if got := c.get("198.51.100.7"); got != "" {
		t.Fatalf("a shared address was labelled %q", got)
	}
	// One name for one address is still used.
	c.remember("only.example", []dns.RR{aRec("only.example", "198.51.100.8")})
	if got := c.get("198.51.100.8"); got != "only.example" {
		t.Fatalf("198.51.100.8 is %q", got)
	}
}

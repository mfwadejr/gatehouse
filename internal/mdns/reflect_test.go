package mdns

import (
	"testing"

	"github.com/miekg/dns"
)

func TestPlanIsBothWaysAndSkipsSelf(t *testing.T) {
	p := Plan([][2][]string{{{"br-home"}, {"eth2", "eth3"}}, {{"br-home"}, {"br-home"}}})
	if len(p["br-home"]) != 2 || len(p["eth2"]) != 1 || p["eth2"][0] != "br-home" || len(p["eth3"]) != 1 {
		t.Fatalf("%v", p)
	}
	if _, ok := p["x"]; ok {
		t.Fatal("unexpected entry")
	}
}

func pkt(t *testing.T, name string, ptr string) []byte {
	m := new(dns.Msg)
	if ptr == "" {
		m.SetQuestion(name, dns.TypePTR)
	} else {
		m.Response = true
		m.Answer = []dns.RR{&dns.PTR{Hdr: dns.RR_Header{Name: name, Rrtype: dns.TypePTR, Class: dns.ClassINET, Ttl: 120}, Ptr: ptr}}
	}
	b, err := m.Pack()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestWantedFiltersByService(t *testing.T) {
	printer := pkt(t, "_ipp._tcp.local.", "")
	cast := pkt(t, "_googlecast._tcp.local.", "")
	ans := pkt(t, "_ipp._tcp.local.", "Office Printer._ipp._tcp.local.")
	all := pkt(t, "_services._dns-sd._udp.local.", "")
	if !Wanted(printer, nil) || !Wanted(cast, nil) {
		t.Fatal("no list copies everything")
	}
	svc := []string{"_ipp._tcp"}
	if !Wanted(printer, svc) || !Wanted(ans, svc) || !Wanted(all, svc) {
		t.Fatal("printer traffic must be copied")
	}
	if Wanted(cast, svc) {
		t.Fatal("a Chromecast question must not be copied when only printers are shared")
	}
	if Wanted([]byte("garbage"), svc) {
		t.Fatal("garbage must not be copied")
	}
}

func TestSetConfigWithNothingDoesNotListen(t *testing.T) {
	var r Reflector
	r.SetConfig(Config{})
	r.SetConfig(Config{Links: map[string][]string{"does-not-exist0": {"nor-this0"}}}) // logs, does not panic
	r.Stop()
}

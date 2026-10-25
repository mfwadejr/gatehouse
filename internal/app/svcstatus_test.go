package app

import (
	"net"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

func TestServicesTextSaysWhatDHCPAndDNSAreDoing(t *testing.T) {
	a, err := New(Config{DataDir: t.TempDir(), WebAddr: freeAddr(t), DNSAddrs: []string{freeAddr(t)}, Net: &fakeNet{}, FW: &fakeFW{}})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.Config.Exec(`INSERT INTO dhcp_scopes(name,iface,subnet,range_start,range_end,router) VALUES('Home','br0','192.168.1.0/24','192.168.1.100','192.168.1.200','')`)
	a.Config.Exec(`INSERT INTO dhcp_leases(scope_id,ip,mac,start,expires,state) SELECT id,'192.168.1.101','aa:bb:cc:00:00:01',?,?,'active' FROM dhcp_scopes`, time.Now().Unix(), time.Now().Add(time.Hour).Unix())

	// A tiny DNS server that answers every question, so the lookup test has something to reach.
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	go func() {
		buf := make([]byte, 512)
		for {
			n, from, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			var m dnsmessage.Message
			if m.Unpack(buf[:n]) != nil || len(m.Questions) == 0 {
				continue
			}
			m.Response, m.RCode = true, dnsmessage.RCodeSuccess
			m.Answers = []dnsmessage.Resource{{Header: dnsmessage.ResourceHeader{Name: m.Questions[0].Name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET, TTL: 60}, Body: &dnsmessage.AResource{A: [4]byte{93, 184, 216, 34}}}}
			out, _ := m.Pack()
			pc.WriteTo(out, from)
		}
	}()
	out := ServicesText(a.Config, pc.LocalAddr().String())
	for _, want := range []string{"DHCP", "Home", "br0", "1 address in use", "DNS", "answered in"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	// With nothing answering, it says so plainly.
	bad := ServicesText(a.Config, "127.0.0.1:1")
	if !strings.Contains(bad, "FAILED") {
		t.Errorf("a dead resolver must be reported:\n%s", bad)
	}
}

func TestPortListeningFindsAnOpenSocket(t *testing.T) {
	l, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	port := l.LocalAddr().(*net.UDPAddr).Port
	if !portListening("udp", port) {
		t.Fatal("an open UDP socket was not found")
	}
	if portListening("udp", 1) {
		t.Fatal("a port nobody has was reported open")
	}
}

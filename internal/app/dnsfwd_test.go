package app

import (
	"net"
	"testing"

	"github.com/miekg/dns"
)

func TestDifferentServerRulesAreCheckedSavedAndTested(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	// A small DNS server on this machine plays "the office server".
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &dns.Server{PacketConn: pc, Handler: dns.HandlerFunc(func(w dns.ResponseWriter, r *dns.Msg) {
		m := new(dns.Msg)
		m.SetReply(r)
		if r.Question[0].Name == "gone.example." {
			m.Rcode = dns.RcodeNameError
		}
		w.WriteMsg(m)
	})}
	go srv.ActivateAndServe()
	defer srv.Shutdown()
	addr := pc.LocalAddr().String()

	if rc, _, _ := c.call("PUT", "/api/dns/forwards", map[string]any{"rules": []map[string]any{{"domain": "office.example", "servers": []string{"not an address"}}}}); rc == 200 {
		t.Fatal("a bad server address was accepted")
	}
	if rc, _, _ := c.call("PUT", "/api/dns/forwards", map[string]any{"rules": []map[string]any{{"domain": "a.example", "servers": []string{"10.0.0.2"}}, {"domain": "A.example.", "servers": []string{"10.0.0.3"}}}}); rc == 200 {
		t.Fatal("the same domain twice was accepted")
	}
	if rc, _, raw := c.call("PUT", "/api/dns/forwards", map[string]any{"rules": []map[string]any{{"domain": " Office.Example ", "servers": []string{"10.0.0.2"}}}}); rc != 200 {
		t.Fatalf("save: %d %s", rc, raw)
	}
	_, m, _ := c.call("GET", "/api/dns/forwards", nil)
	rules, _ := m["rules"].([]any)
	if len(rules) != 1 || rules[0].(map[string]any)["domain"] != "office.example" {
		t.Fatalf("not saved as expected: %v", m)
	}
	if rc, res, raw := c.call("POST", "/api/dns/forwards/test", map[string]any{"domain": "office.example", "servers": []string{addr}}); rc != 200 || res["ok"] != true {
		t.Fatalf("test: %d %s", rc, raw)
	}
	if _, res, _ := c.call("POST", "/api/dns/forwards/test", map[string]any{"domain": "gone.example", "servers": []string{addr}}); res["ok"] != true || res["verdict"] == "" {
		t.Fatalf("a missing domain should still show the server is reachable: %v", res)
	}
	if _, res, _ := c.call("POST", "/api/dns/forwards/test", map[string]any{"domain": "office.example", "servers": []string{"127.0.0.1:1"}}); res["ok"] == true {
		t.Fatalf("a closed port should not look fine: %v", res)
	}
}

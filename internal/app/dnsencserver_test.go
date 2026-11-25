package app

import (
	"crypto/tls"
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func TestEncryptedDNSForDevices(t *testing.T) {
	c, _, ff := siteBoxWith(t, "192.168.1.1/24", func(cfg *Config) {
		cfg.EncDoTAddr, cfg.EncDoHAddr = "127.0.0.1:0", "127.0.0.1:0"
	})
	a := c.app
	if a == nil {
		t.Skip("no app")
	}
	addVLAN(t, a, "eth1.20", "Guests", 20, "eth1", false)
	// The internet port and unknown names are refused; nothing chosen is refused.
	if rc, _, _ := c.call("PUT", "/api/dns/encrypted-server", map[string]any{"on": true, "ifaces": []string{"eth0"}}); rc == 200 {
		t.Fatal("internet port accepted")
	}
	if rc, _, _ := c.call("PUT", "/api/dns/encrypted-server", map[string]any{"on": true, "ifaces": []string{}}); rc == 200 {
		t.Fatal("no networks accepted")
	}
	if rc, _, raw := c.call("PUT", "/api/dns/encrypted-server", map[string]any{"on": true, "ifaces": []string{"eth1"}}); rc != 200 {
		t.Fatalf("%d %s", rc, raw)
	}
	if !strings.Contains(ff.script, `iifname "eth1" tcp dport { 853, 8443 } accept comment "protected: encrypted dns for devices"`) {
		t.Fatalf("firewall rule missing:\n%s", ff.script)
	}
	a.encsrv.mu.Lock()
	dotL, dohL := a.encsrv.srv.DoTAddr(), a.encsrv.srv.DoHAddr()
	a.encsrv.mu.Unlock()
	if dotL == "" || dohL == "" {
		t.Fatalf("not listening: %q %q", dotL, dohL)
	}
	// DNS-over-TLS.
	q := new(dns.Msg)
	q.SetQuestion("example.org.", dns.TypeA)
	cl := &dns.Client{Net: "tcp-tls", TLSConfig: &tls.Config{InsecureSkipVerify: true}, Timeout: 5 * time.Second}
	if r, _, err := cl.Exchange(q, dotL); err != nil || r == nil {
		t.Fatalf("dot: %v", err)
	}
	// DNS-over-HTTPS, both ways of asking.
	wire, _ := q.Pack()
	hc := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	resp, err := hc.Get("https://" + dohL + "/dns-query?dns=" + base64.RawURLEncoding.EncodeToString(wire))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	m := new(dns.Msg)
	if resp.StatusCode != 200 || m.Unpack(b) != nil || len(m.Question) != 1 {
		t.Fatalf("doh get: %d", resp.StatusCode)
	}
	resp, err = hc.Post("https://"+dohL+"/dns-query", "application/dns-message", strings.NewReader(string(wire)))
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("doh post: %v", err)
	}
	resp.Body.Close()
	if resp, err = hc.Post("https://"+dohL+"/dns-query", "text/plain", strings.NewReader("x")); err != nil || resp.StatusCode != 415 {
		t.Fatalf("wrong media type: %v", err)
	}
	// A bad certificate upload is refused and changes nothing.
	if rc, _, _ := c.call("POST", "/api/dns/encrypted-server/cert", map[string]string{"cert": "nope", "key": "nope"}); rc == 200 {
		t.Fatal("bad certificate accepted")
	}
	if a.encUploaded() {
		t.Fatal("a refused certificate was kept")
	}
	// Off again: the rule goes and the ports close.
	if rc, _, raw := c.call("PUT", "/api/dns/encrypted-server", map[string]any{"on": false, "ifaces": []string{"eth1"}}); rc != 200 {
		t.Fatalf("%d %s", rc, raw)
	}
	if strings.Contains(ff.script, "encrypted dns for devices") {
		t.Fatal("rule stayed after switching off")
	}
	if cn, err := net.DialTimeout("tcp", dotL, time.Second); err == nil {
		cn.Close()
		t.Fatal("still listening")
	}
}

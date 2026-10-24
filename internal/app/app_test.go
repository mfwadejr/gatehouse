package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/cookiejar"
	"testing"
	"time"

	"github.com/miekg/dns"

	"gatehouse/internal/auth"
)

type client struct {
	t    *testing.T
	base string
	hc   *http.Client
	csrf string
	app  *App // set by the boxes that build their own app, for tests that need to reach inside
}

func (c *client) call(method, path string, body any) (int, map[string]any, []byte) {
	c.t.Helper()
	var b []byte
	if body != nil {
		b, _ = json.Marshal(body)
	}
	req, _ := http.NewRequest(method, c.base+path, bytes.NewReader(b))
	if c.csrf != "" {
		req.Header.Set("X-CSRF-Token", c.csrf)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	buf.ReadFrom(resp.Body)
	var m map[string]any
	json.Unmarshal(buf.Bytes(), &m)
	return resp.StatusCode, m, buf.Bytes()
}

func freeAddr(t *testing.T) string {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().String()
}

func TestDaemonEndToEnd(t *testing.T) {
	web, dnsAddr := freeAddr(t), freeAddr(t)
	cfg := Config{DataDir: t.TempDir(), WebAddr: web, DNSAddrs: []string{dnsAddr}}
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if _, err := a.Auth.CreateUser("admin", "correct horse battery", "admin"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()
	defer func() { cancel(); <-done }()

	jar, _ := cookiejar.New(nil)
	c := &client{t: t, base: "http://" + web, hc: &http.Client{Jar: jar}}
	for i := 0; i < 50; i++ {
		if resp, err := c.hc.Get(c.base + "/api/health"); err == nil {
			resp.Body.Close()
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Sign in and enrol.
	_, m, _ := c.call("POST", "/api/auth/login", map[string]string{"username": "admin", "password": "correct horse battery"})
	c.csrf = m["csrf"].(string)
	_, m, _ = c.call("POST", "/api/auth/enrol/begin", nil)
	if code, _, _ := c.call("POST", "/api/auth/enrol/confirm", map[string]string{"code": auth.TOTPCodeAt(m["secret"].(string), time.Now())}); code != 200 {
		t.Fatalf("enrol: %d", code)
	}

	// A local DNS record is served over the real socket.
	if code, _, body := c.call("POST", "/api/dns/records", map[string]any{"name": "nas.lan", "type": "A", "value": "192.168.1.10"}); code != 200 {
		t.Fatalf("add record: %d %s", code, body)
	}
	r := new(dns.Msg)
	r.SetQuestion("nas.lan.", dns.TypeA)
	resp, _, err := (&dns.Client{Net: "udp"}).Exchange(r, dnsAddr)
	if err != nil || len(resp.Answer) != 1 || resp.Answer[0].(*dns.A).A.String() != "192.168.1.10" {
		t.Fatalf("dns answer: %v %v", resp, err)
	}

	// A custom block rule changes the answer.
	if code, _, _ := c.call("POST", "/api/dns/rules", map[string]string{"domain": "bad.example", "action": "block"}); code != 200 {
		t.Fatal("add rule")
	}
	r.SetQuestion("bad.example.", dns.TypeA)
	resp, _, err = (&dns.Client{Net: "udp"}).Exchange(r, dnsAddr)
	if err != nil || len(resp.Answer) != 1 || !resp.Answer[0].(*dns.A).A.IsUnspecified() {
		t.Fatalf("blocked answer: %v %v", resp, err)
	}
	_, _, body := c.call("GET", "/api/dns/explain?domain=bad.example&group=1", nil)
	if !bytes.Contains(body, []byte(`"blocked":true`)) {
		t.Fatalf("explain: %s", body)
	}

	// The query log picks the queries up.
	time.Sleep(1200 * time.Millisecond)
	_, _, body = c.call("GET", "/api/dns/log?limit=10", nil)
	if !bytes.Contains(body, []byte("bad.example")) {
		t.Fatalf("log: %s", body)
	}

	// DHCP scope validation and creation.
	bad := map[string]any{"name": "LAN", "iface": "eth1", "mode": "serve", "subnet": "192.168.1.0/24", "range_start": "192.168.2.10", "range_end": "192.168.1.200", "enabled": true}
	if code, _, _ := c.call("PUT", "/api/dhcp/scopes", bad); code != 400 {
		t.Fatalf("invalid scope accepted: %d", code)
	}
	bad["range_start"] = "192.168.1.100"
	if code, _, body := c.call("PUT", "/api/dhcp/scopes", bad); code != 200 {
		t.Fatalf("scope: %d %s", code, body)
	}
	if code, _, body := c.call("GET", "/api/dhcp/scopes", nil); code != 200 || !bytes.Contains(body, []byte("192.168.1.100")) {
		t.Fatalf("scopes: %d %s", code, body)
	}
	// Settings reject nonsense.
	if code, _, _ := c.call("PUT", "/api/dns/settings", map[string]any{"block_mode": "explode", "local_domain": "lan", "cache_size": 1000, "canary": true}); code != 400 {
		t.Fatalf("bad settings accepted: %d", code)
	}
	// Defaults seeded for the console: groups, lists, upstreams.
	_, _, body = c.call("GET", "/api/dns/groups", nil)
	if !bytes.Contains(body, []byte("Default")) {
		t.Fatalf("groups: %s", body)
	}
	_, _, body = c.call("GET", "/api/dns/upstreams", nil)
	if !bytes.Contains(body, []byte("Cloudflare")) {
		t.Fatalf("upstreams: %s", body)
	}
}

package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDDNSDuckUpdateSealedAndSetsEndpoint(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	a := c.app
	if a == nil {
		t.Skip("no app")
	}
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.RawQuery
		w.Write([]byte("OK"))
	}))
	defer srv.Close()
	a.ddns.duckBase = srv.URL
	if rc, _, _ := c.call("PUT", "/api/remote", map[string]any{"provider": "duckdns", "host": "bad name!", "secret": "tok"}); rc == 200 {
		t.Fatal("bad name accepted")
	}
	if rc, _, _ := c.call("PUT", "/api/remote", map[string]any{"provider": "duckdns", "host": "myhome"}); rc == 200 {
		t.Fatal("missing token accepted")
	}
	rc, m, raw := c.call("PUT", "/api/remote", map[string]any{"provider": "duckdns", "host": "myhome", "secret": "tok-123"})
	if rc != 200 || m["status"] != "ok" {
		t.Fatalf("%d %s", rc, raw)
	}
	if !strings.Contains(got, "domains=myhome") || !strings.Contains(got, "token=tok-123") {
		t.Fatalf("query %q", got)
	}
	if a.setting(vpnEndpointSetting, "") != "myhome.duckdns.org" {
		t.Fatal("endpoint not set")
	}
	sealed := a.setting("ddns.secret", "")
	if sealed == "" || strings.Contains(sealed, "tok-123") {
		t.Fatal("secret not sealed")
	}
	_, _, raw = c.call("GET", "/api/remote", nil)
	if strings.Contains(string(raw), "tok-123") || !strings.Contains(string(raw), `"has_secret":true`) {
		t.Fatalf("GET leaks or hides: %s", raw)
	}
	// A refusal from the service is reported in plain words, not as success.
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("KO")) })
	_, m, _ = c.call("PUT", "/api/remote", map[string]any{"provider": "duckdns", "host": "myhome"})
	if m["warning"] == nil {
		t.Fatalf("KO not reported: %v", m)
	}
}

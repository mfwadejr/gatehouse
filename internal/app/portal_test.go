package app

import (
	"gatehouse/internal/fw"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGuestSignInPage(t *testing.T) {
	c, _, ff := siteBoxWith(t, "192.168.1.1/24", func(cfg *Config) { cfg.PortalAddr = "127.0.0.1:0" })
	a := c.app
	if a == nil {
		t.Skip("no app")
	}
	addVLAN(t, a, "eth1.20", "Guests", 20, "eth1", false)
	// The management network and the internet port can never be put behind the page.
	if rc, _, raw := c.call("PUT", "/api/portal", map[string]any{"on": true, "ifaces": []string{"eth1"}, "title": "Hi", "hours": 8}); rc == 200 {
		t.Fatalf("management network accepted: %s", raw)
	}
	if rc, _, _ := c.call("PUT", "/api/portal", map[string]any{"on": true, "ifaces": []string{"eth0"}, "title": "Hi", "hours": 8}); rc == 200 {
		t.Fatal("internet port accepted")
	}
	if rc, _, _ := c.call("PUT", "/api/portal", map[string]any{"on": true, "ifaces": []string{}, "title": "Hi", "hours": 8}); rc == 200 {
		t.Fatal("no networks accepted")
	}
	if rc, _, _ := c.call("PUT", "/api/portal", map[string]any{"on": true, "ifaces": []string{"eth1.20"}, "title": "Hi", "hours": 0}); rc == 200 {
		t.Fatal("zero hours accepted")
	}
	if rc, _, raw := c.call("PUT", "/api/portal", map[string]any{"on": true, "ifaces": []string{"eth1.20"}, "title": "Cabin <wifi>", "terms": "Be kind.\n\nNo bad things.", "hours": 4}); rc != 200 {
		t.Fatalf("%d %s", rc, raw)
	}
	if !strings.Contains(ff.script, `ip saddr != @portal_ok tcp dport 80 counter redirect to :8099`) || !strings.Contains(ff.script, `"eth1.20"`) {
		t.Fatalf("the firewall was not set up for the page:\n%s", ff.script)
	}
	h := a.portalHandler()
	get := func(method, path, remote string) (int, string) {
		req := httptest.NewRequest(method, path, nil)
		req.RemoteAddr = remote
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		b, _ := io.ReadAll(rr.Result().Body)
		return rr.Code, string(b)
	}
	// A phone's connectivity check, whatever the address, gets the page (escaped), with the terms.
	code, body := get("GET", "/generate_204", "192.168.20.50:4000")
	if code != 200 || !strings.Contains(body, "Cabin &lt;wifi&gt;") || !strings.Contains(body, "Be kind.") || !strings.Contains(body, "I agree and connect") {
		t.Fatalf("page: %d %s", code, body)
	}
	// Someone not on a guest network cannot sign themselves in.
	if code, _ := get("POST", "/accept", "192.168.1.50:4000"); code != http.StatusForbidden {
		t.Fatalf("a device on the home network signed in: %d", code)
	}
	if code, body := get("POST", "/accept", "192.168.20.50:4000"); code != 200 || !strings.Contains(body, "connected") {
		t.Fatalf("accept: %d %s", code, body)
	}
	fill := a.portalFill(map[string][]string{})
	if len(fill.Elems) != 1 || !strings.HasPrefix(fill.Elems[0], "192.168.20.50 timeout ") || !strings.HasSuffix(fill.Elems[0], "s") {
		t.Fatalf("signed-in set: %v", fill.Elems)
	}
	// The set the firewall loads carries it, with time left.
	if m := portalSetFromModel(t, a); m == nil || len(m.Elems) != 1 {
		t.Fatalf("the firewall set does not hold the guest")
	}
	// A device known by hardware address keeps its sign-in when its address changes.
	a.portal.mu.Lock()
	a.portal.sessions["mac:aa:bb:cc:00:00:01"] = a.portal.sessions["ip:192.168.20.50"]
	a.portal.mu.Unlock()
	f2 := a.portalFill(map[string][]string{"aa:bb:cc:00:00:01": {"192.168.20.77"}})
	if !strings.Contains(strings.Join(f2.Elems, " "), "192.168.20.77 timeout") {
		t.Fatalf("by hardware address: %v", f2.Elems)
	}
	// Signing everyone out empties the set.
	if rc, _, _ := c.call("POST", "/api/portal/clear", nil); rc != 200 {
		t.Fatal("clear failed")
	}
	if f := a.portalFill(map[string][]string{}); len(f.Elems) != 0 {
		t.Fatalf("still signed in: %v", f.Elems)
	}
	// Off removes the rules again.
	if rc, _, raw := c.call("PUT", "/api/portal", map[string]any{"on": false, "ifaces": []string{"eth1.20"}, "title": "x", "hours": 4}); rc != 200 {
		t.Fatalf("%d %s", rc, raw)
	}
	if strings.Contains(ff.script, "portal_ok") {
		t.Fatal("the portal rules stayed after switching off")
	}
}

func portalSetFromModel(t *testing.T, a *App) *fw.SetFill {
	t.Helper()
	m, err := fw.Load(a.Config)
	if err != nil {
		t.Fatal(err)
	}
	f, ok := a.setContents(m, false)[fw.PortalSet]
	if !ok {
		return nil
	}
	return &f
}

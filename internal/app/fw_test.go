package app

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/cookiejar"
	"sync"
	"testing"
	"time"

	"gatehouse/internal/auth"
	"gatehouse/internal/fw"
)

type fakeFW struct {
	mu       sync.Mutex
	script   string
	token    string
	pending  bool
	checked  []string
	sets     map[string]fw.SetFill
	counters map[string]fw.Counter
}

func (f *fakeFW) Check(s string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checked = append(f.checked, s)
	return nil
}
func (f *fakeFW) Apply(s string, w time.Duration, a string) (string, time.Time, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.pending {
		return "", time.Time{}, fw.ErrPending
	}
	f.script, f.token, f.pending = s, "tok123", true
	return f.token, time.Now().Add(w), nil
}
func (f *fakeFW) Confirm(t string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.pending || t != f.token {
		return errors.New("that confirmation does not match the pending change")
	}
	f.pending = false
	return nil
}
func (f *fakeFW) LoadSets(fills map[string]fw.SetFill) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sets == nil {
		f.sets = map[string]fw.SetFill{}
	}
	for k, v := range fills {
		if old, ok := f.sets[k]; ok && !v.Replace {
			v.Elems = append(old.Elems, v.Elems...)
		}
		f.sets[k] = v
	}
	return nil
}
func (f *fakeFW) Counters() (map[string]fw.Counter, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.counters, nil
}
func (f *fakeFW) Rollback() error { f.mu.Lock(); defer f.mu.Unlock(); f.pending = false; return nil }
func (f *fakeFW) Pending() (string, time.Time, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.token, time.Now().Add(time.Minute), f.pending
}

func TestFirewallFlow(t *testing.T) {
	web, dnsAddr := freeAddr(t), freeAddr(t)
	ff := &fakeFW{}
	a, err := New(Config{DataDir: t.TempDir(), WebAddr: web, DNSAddrs: []string{dnsAddr}, FW: ff})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.Auth.CreateUser("admin", "correct horse battery", "admin")
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
	_, m, _ := c.call("POST", "/api/auth/login", map[string]string{"username": "admin", "password": "correct horse battery"})
	c.csrf = m["csrf"].(string)
	_, m, _ = c.call("POST", "/api/auth/enrol/begin", nil)
	secret := m["secret"].(string)
	c.call("POST", "/api/auth/enrol/confirm", map[string]string{"code": auth.TOTPCodeAt(secret, time.Now())})

	// A first network that cannot manage the device is refused, so a lockout can never be saved.
	code, _, body := c.call("PUT", "/api/fw/zones", map[string]any{"name": "Guests", "kind": "guest", "ifaces": []string{"eth2"}, "internet": true})
	if code != 400 || !bytes.Contains(body, []byte("locked out")) {
		t.Fatalf("lockout configuration saved: %d %s", code, body)
	}
	// Injection through names is refused too.
	if code, _, _ := c.call("PUT", "/api/fw/zones", map[string]any{"name": "x\" ; flush ruleset", "kind": "trusted", "ifaces": []string{"eth1"}, "management": true}); code != 400 {
		t.Fatalf("hostile name accepted: %d", code)
	}
	code, _, body = c.call("PUT", "/api/fw/zones", map[string]any{"name": "Home", "kind": "trusted", "ifaces": []string{"eth1"}, "management": true, "internet": true, "gateway": true, "intra_allow": true})
	if code != 200 {
		t.Fatalf("home: %d %s", code, body)
	}
	if code, _, body = c.call("PUT", "/api/fw/zones", map[string]any{"name": "Guests", "kind": "guest", "ifaces": []string{"eth2"}, "internet": true, "gateway": true}); code != 200 {
		t.Fatalf("guests: %d %s", code, body)
	}
	// Taking management away from the only managing network is refused.
	var homeID float64
	_, _, body = c.call("GET", "/api/fw/zones", nil)
	if !bytes.Contains(body, []byte("Home")) {
		t.Fatalf("zones: %s", body)
	}
	homeID = 1
	if code, _, _ = c.call("PUT", "/api/fw/zones", map[string]any{"id": homeID, "name": "Home", "kind": "trusted", "ifaces": []string{"eth1"}, "management": false, "gateway": true}); code != 400 {
		t.Fatalf("management removal accepted: %d", code)
	}
	// An exception rule, and one that points at nothing.
	if code, _, body = c.call("PUT", "/api/fw/rules", map[string]any{"name": "Print", "from": "Guests", "to": "Home", "dst": "192.168.1.50", "service": "print", "action": "accept", "enabled": true}); code != 200 {
		t.Fatalf("rule: %d %s", code, body)
	}
	if code, _, _ = c.call("PUT", "/api/fw/rules", map[string]any{"name": "Bad", "from": "Nowhere", "to": "Home", "service": "print", "action": "accept", "enabled": true}); code != 400 {
		t.Fatalf("rule to unknown network accepted: %d", code)
	}
	_, _, body = c.call("GET", "/api/fw/overview", nil)
	if !bytes.Contains(body, []byte("Guests: devices can reach the internet")) || !bytes.Contains(body, []byte(`"dirty":true`)) {
		t.Fatalf("overview: %s", body)
	}

	// Applying asks for a code only when the person switched that on (it is off by default).
	a.Config.SetSetting("fw.stepup", "1")
	if code, _, _ = c.call("POST", "/api/fw/apply", map[string]string{"code": "000000"}); code != 403 {
		t.Fatalf("apply without a valid code: %d", code)
	}
	// The code used at enrolment is spent; use the next step's code.
	next := auth.TOTPCodeAt(secret, time.Now().Add(30*time.Second))
	code, m, body = c.call("POST", "/api/fw/apply", map[string]string{"code": next})
	if code != 200 || m["token"] != "tok123" {
		t.Fatalf("apply: %d %s", code, body)
	}
	if !bytes.Contains([]byte(ff.script), []byte("protected: management console")) {
		t.Fatal("applied script lacks the protected rule")
	}
	// A second apply while one is pending is refused.
	if code, _, _ = c.call("POST", "/api/fw/apply", map[string]string{"code": auth.TOTPCodeAt(secret, time.Now().Add(60*time.Second))}); code != 409 {
		t.Fatalf("stacked apply: %d", code)
	}
	if code, _, body = c.call("POST", "/api/fw/confirm", map[string]string{"token": "wrong"}); code != 400 {
		t.Fatalf("wrong token: %d %s", code, body)
	}
	if code, _, body = c.call("POST", "/api/fw/confirm", map[string]string{"token": "tok123"}); code != 200 {
		t.Fatalf("confirm: %d %s", code, body)
	}
	_, _, body = c.call("GET", "/api/fw/overview", nil)
	if !bytes.Contains(body, []byte(`"dirty":false`)) {
		t.Fatalf("still dirty after confirm: %s", body)
	}
	_, _, body = c.call("GET", "/api/fw/history", nil)
	if !bytes.Contains(body, []byte(`"status":"confirmed"`)) {
		t.Fatalf("history: %s", body)
	}
}

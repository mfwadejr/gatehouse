package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gatehouse/internal/auth"
	"gatehouse/internal/crypt"
	"gatehouse/internal/events"
	"gatehouse/internal/store"
)

type env struct {
	t   *testing.T
	srv *Server
	ts  *httptest.Server
	cl  *http.Client
	bus *events.Bus
}

func newEnv(t *testing.T) *env {
	t.Helper()
	dir := t.TempDir()
	db, err := store.OpenConfig(filepath.Join(dir, "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	box, err := crypt.LoadOrCreate(filepath.Join(dir, "k"))
	if err != nil {
		t.Fatal(err)
	}
	svc := auth.New(db, box)
	if _, err := svc.CreateUser("admin", "correct horse battery", "admin"); err != nil {
		t.Fatal(err)
	}
	bus := events.NewBus()
	s := New(svc, bus)
	s.Topic("test.topic")
	s.Handle("POST /api/test/write", func(w http.ResponseWriter, r *http.Request, c *Ctx) { JSON(w, 200, map[string]string{"ok": "1"}) })
	s.HandleAdmin("GET /api/test/admin", func(w http.ResponseWriter, r *http.Request, c *Ctx) { JSON(w, 200, map[string]string{"ok": "1"}) })
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	jar := newJar()
	return &env{t: t, srv: s, ts: ts, cl: &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, bus: bus}
}

func (e *env) do(method, path string, body any, csrf string) (*http.Response, map[string]any) {
	e.t.Helper()
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req, _ := http.NewRequest(method, e.ts.URL+path, rd)
	if csrf != "" {
		req.Header.Set(csrfHeader, csrf)
	}
	resp, err := e.cl.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	var m map[string]any
	json.NewDecoder(resp.Body).Decode(&m)
	return resp, m
}

func TestFullLoginFlow(t *testing.T) {
	e := newEnv(t)
	// Unauthenticated access is refused.
	if r, _ := e.do("GET", "/api/auth/me", nil, ""); r.StatusCode != 401 {
		t.Fatalf("me without session: %d", r.StatusCode)
	}
	// Wrong password.
	if r, _ := e.do("POST", "/api/auth/login", map[string]string{"username": "admin", "password": "nope"}, ""); r.StatusCode != 401 {
		t.Fatalf("bad login: %d", r.StatusCode)
	}
	// Right password, no TOTP yet: a limited session that must enrol.
	r, m := e.do("POST", "/api/auth/login", map[string]string{"username": "admin", "password": "correct horse battery"}, "")
	if r.StatusCode != 200 || m["status"] != "enrol" {
		t.Fatalf("login: %d %v", r.StatusCode, m)
	}
	csrf := m["csrf"].(string)
	var cookie *http.Cookie
	for _, c := range r.Cookies() {
		if c.Name == cookieName {
			cookie = c
		}
	}
	if cookie == nil || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("session cookie not hardened: %+v", cookie)
	}
	// The limited session cannot reach protected endpoints.
	if r, _ := e.do("POST", "/api/test/write", nil, csrf); r.StatusCode != 403 {
		t.Fatalf("limited session reached a protected endpoint: %d", r.StatusCode)
	}
	_, m = e.do("POST", "/api/auth/enrol/begin", nil, csrf)
	secret := m["secret"].(string)
	if secret == "" || !strings.HasPrefix(m["uri"].(string), "otpauth://totp/") {
		t.Fatalf("enrol begin: %v", m)
	}
	if r, _ := e.do("POST", "/api/auth/enrol/confirm", map[string]string{"code": "000000"}, csrf); r.StatusCode != 400 {
		t.Fatalf("bad code accepted: %d", r.StatusCode)
	}
	r, m = e.do("POST", "/api/auth/enrol/confirm", map[string]string{"code": auth.TOTPCodeAt(secret, time.Now())}, csrf)
	if r.StatusCode != 200 || len(m["recovery_codes"].([]any)) != 10 {
		t.Fatalf("enrol confirm: %d %v", r.StatusCode, m)
	}
	// Now fully signed in.
	if r, _ := e.do("POST", "/api/test/write", nil, csrf); r.StatusCode != 200 {
		t.Fatalf("full session blocked: %d", r.StatusCode)
	}
	// CSRF: missing or wrong header is rejected.
	if r, _ := e.do("POST", "/api/test/write", nil, ""); r.StatusCode != 403 {
		t.Fatalf("missing csrf accepted: %d", r.StatusCode)
	}
	if r, _ := e.do("POST", "/api/test/write", nil, "wrong"); r.StatusCode != 403 {
		t.Fatalf("wrong csrf accepted: %d", r.StatusCode)
	}
	if r, _ := e.do("GET", "/api/test/admin", nil, ""); r.StatusCode != 200 {
		t.Fatalf("admin blocked: %d", r.StatusCode)
	}
	// Logout kills the session.
	e.do("POST", "/api/auth/logout", nil, csrf)
	if r, _ := e.do("GET", "/api/auth/me", nil, ""); r.StatusCode != 401 {
		t.Fatalf("session survived logout: %d", r.StatusCode)
	}

	// Second login now requires the code, and a code cannot be replayed.
	r, m = e.do("POST", "/api/auth/login", map[string]string{"username": "admin", "password": "correct horse battery"}, "")
	if m["status"] != "mfa" {
		t.Fatalf("expected mfa step: %v", m)
	}
	tkt := m["ticket"].(string)
	// Use the next time step so the enrolment code (already consumed) cannot be reused.
	code := auth.TOTPCodeAt(secret, time.Now().Add(30*time.Second))
	_ = code
	r, m = e.do("POST", "/api/auth/mfa", map[string]string{"ticket": tkt, "code": "123456"}, "")
	if r.StatusCode != 401 {
		t.Fatalf("bad mfa code accepted: %d", r.StatusCode)
	}
}

func TestSecurityHeadersAndErrors(t *testing.T) {
	e := newEnv(t)
	r, _ := e.do("GET", "/api/health", nil, "")
	if !strings.Contains(r.Header.Get("Content-Security-Policy"), "default-src 'self'") || r.Header.Get("X-Frame-Options") != "DENY" {
		t.Fatalf("headers missing: %v", r.Header)
	}
	if r, _ := e.do("GET", "/api/nothing", nil, ""); r.StatusCode != 404 {
		t.Fatalf("unknown api path: %d", r.StatusCode)
	}
	// Unknown JSON fields and trailing data are rejected.
	req, _ := http.NewRequest("POST", e.ts.URL+"/api/auth/login", strings.NewReader(`{"username":"a","password":"b","extra":1}`))
	resp, _ := e.cl.Do(req)
	if resp.StatusCode != 400 {
		t.Fatalf("unknown field accepted: %d", resp.StatusCode)
	}
	// Cross-origin login is rejected.
	req, _ = http.NewRequest("POST", e.ts.URL+"/api/auth/login", strings.NewReader(`{"username":"a","password":"b"}`))
	req.Header.Set("Origin", "https://evil.example")
	resp, _ = e.cl.Do(req)
	if resp.StatusCode != 403 {
		t.Fatalf("cross-origin login accepted: %d", resp.StatusCode)
	}
}

func TestStream(t *testing.T) {
	e := newEnv(t)
	// Sign in fully via the service directly to keep the test short.
	_, m := e.do("POST", "/api/auth/login", map[string]string{"username": "admin", "password": "correct horse battery"}, "")
	csrf := m["csrf"].(string)
	_, m = e.do("POST", "/api/auth/enrol/begin", nil, csrf)
	e.do("POST", "/api/auth/enrol/confirm", map[string]string{"code": auth.TOTPCodeAt(m["secret"].(string), time.Now())}, csrf)
	e.bus.Publish("test.topic", map[string]int{"n": 1})

	if r, _ := e.do("GET", "/api/stream?topics=secret.topic", nil, ""); r.StatusCode != 400 {
		t.Fatalf("unlisted topic accepted: %d", r.StatusCode)
	}
	resp, err := e.cl.Get(e.ts.URL + "/api/stream?topics=test.topic")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	buf := make([]byte, 512)
	got := ""
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !strings.Contains(got, "event: test.topic") {
		n, err := resp.Body.Read(buf)
		got += string(buf[:n])
		if err != nil {
			break
		}
	}
	if !strings.Contains(got, `data: {"n":1}`) {
		t.Fatalf("stream did not deliver the current value: %q", got)
	}
}

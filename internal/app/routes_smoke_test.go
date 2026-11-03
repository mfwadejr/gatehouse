package app

import (
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Every route the console serves, found by reading the source, so a route added later is covered
// without anyone remembering to list it here.
var reRoute = regexp.MustCompile(`"(GET|POST|PUT|PATCH|DELETE) (/api/[^"\s]*)"`)
var reAdminRoute = regexp.MustCompile(`HandleAdmin\("(GET|POST|PUT|PATCH|DELETE) (/api/[^"\s]*)"`)
var rePathParam = regexp.MustCompile(`\{[^}]+\}`)

type route struct{ method, path string }

func sourceRoutes(t *testing.T, re *regexp.Regexp) []route {
	t.Helper()
	seen := map[string]bool{}
	var out []route
	for _, dir := range []string{".", "../api"} {
		files, _ := filepath.Glob(filepath.Join(dir, "*.go"))
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") {
				continue
			}
			b, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			for _, m := range re.FindAllStringSubmatch(string(b), -1) {
				p := rePathParam.ReplaceAllString(m[2], "1")
				k := m[1] + " " + p
				if !seen[k] && p != "/api/x" {
					seen[k] = true
					out = append(out, route{m[1], p})
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].path+out[i].method < out[j].path+out[j].method })
	return out
}

// Routes that are open on purpose: signing in, the health check, first-time setup.
var openRoutes = map[string]bool{
	"POST /api/auth/login": true, "POST /api/auth/mfa": true, "GET /api/health": true,
	"GET /api/install/info": true, "GET /api/setup": true, "POST /api/setup": true, "POST /api/auth/ssh-verify": true,
}

// Anything that would run a real command, change the power state or hold a connection open is left
// out of the blind calls below. They are covered by their own tests.
func skipBlind(r route) bool {
	for _, p := range []string{"/api/term/", "/api/stream", "/api/system/power", "/api/system/update", "/api/diag/run", "/api/diag/restart", "/api/system/reset", "/api/backups/upload"} {
		if strings.HasPrefix(r.path, p) {
			return true
		}
	}
	return false
}

func TestEveryRouteRefusesAStranger(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	stranger := &client{t: t, base: c.base, hc: &http.Client{}}
	routes := sourceRoutes(t, reRoute)
	if len(routes) < 250 {
		t.Fatalf("found only %d routes in the source; the pattern may have broken", len(routes))
	}
	for _, r := range routes {
		if openRoutes[r.method+" "+r.path] || skipBlind(r) && r.path != "/api/stream" {
			continue
		}
		var body any
		if r.method != "GET" {
			body = map[string]any{}
		}
		rc, _, raw := stranger.call(r.method, r.path, body)
		if rc != http.StatusUnauthorized && rc != http.StatusForbidden {
			t.Errorf("%s %s answered a signed-out caller with %d: %.80s", r.method, r.path, rc, raw)
		}
	}
}

func TestEveryAdminRouteRefusesAViewer(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	if _, err := c.app.Auth.CreateUser("viewer", "another long phrase here", "viewer"); err != nil {
		t.Fatal(err)
	}
	jar, _ := cookiejar.New(nil)
	v := &client{t: t, base: c.base, hc: &http.Client{Jar: jar}}
	_, m, _ := v.call("POST", "/api/auth/login", map[string]string{"username": "viewer", "password": "another long phrase here"})
	tok, _ := m["csrf"].(string)
	if tok == "" {
		t.Fatalf("viewer could not sign in: %v", m)
	}
	v.csrf = tok
	routes := sourceRoutes(t, reAdminRoute)
	if len(routes) < 120 {
		t.Fatalf("found only %d admin routes in the source", len(routes))
	}
	for _, r := range routes {
		if skipBlind(r) {
			continue
		}
		var body any
		if r.method != "GET" {
			body = map[string]any{}
		}
		if rc, _, raw := v.call(r.method, r.path, body); rc != http.StatusForbidden {
			t.Errorf("%s %s let a viewer through: %d %.80s", r.method, r.path, rc, raw)
		}
	}
}

// No route may fall over (a 5xx or a panic) when asked with nothing useful, and every JSON answer
// must be valid JSON. This is the blind smoke test; the routes that matter most have their own.
func TestNoRouteFallsOverOnAnEmptyBox(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	for _, r := range sourceRoutes(t, reRoute) {
		if openRoutes[r.method+" "+r.path] || skipBlind(r) || r.method != "GET" {
			continue
		}
		rc, _, raw := c.call(r.method, r.path, nil)
		if rc >= 500 {
			t.Errorf("GET %s fell over: %d %.120s", r.path, rc, raw)
			continue
		}
		if rc == 200 && len(raw) > 0 && (raw[0] == '{' || raw[0] == '[') && !json.Valid(raw) {
			t.Errorf("GET %s answered invalid JSON: %.120s", r.path, raw)
		}
	}
}

func TestEmptyBodiesAreRefusedNotCrashed(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	for _, r := range sourceRoutes(t, reAdminRoute) {
		if skipBlind(r) || r.method == "GET" {
			continue
		}
		rc, _, raw := c.call(r.method, r.path, map[string]any{})
		if rc >= 500 {
			t.Errorf("%s %s fell over on an empty request: %d %.120s", r.method, r.path, rc, raw)
		}
	}
}

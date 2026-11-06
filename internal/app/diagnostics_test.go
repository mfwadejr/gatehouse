package app

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"nopewarden/internal/netd"
)

func TestDiagnosticsShowServicesLogsAndRestart(t *testing.T) {
	c, fn, _ := siteBoxWith(t, "192.168.1.1/24", nil)
	fn.mu.Lock()
	fn.diagUnits = []netd.DiagService{{Unit: "nopewarden-suricata", Title: "Threat watching", State: "failing", Reason: "It keeps stopping and being restarted."}}
	fn.mu.Unlock()
	rc, m, raw := c.call("GET", "/api/diag/services", nil)
	if rc != 200 || len(m["services"].([]any)) != 1 {
		t.Fatalf("services: %d %s", rc, raw)
	}
	rc, m, _ = c.call("GET", "/api/diag/log?unit=nopewarden-suricata&lines=50", nil)
	if rc != 200 || !strings.Contains(m["log"].(string), "log of nopewarden-suricata") {
		t.Fatalf("log: %d %v", rc, m)
	}
	if rc, _, _ := c.call("GET", "/api/diag/log?unit=bogus", nil); rc == 200 {
		t.Fatal("a service that is not on the list was shown")
	}
	if rc, _, _ := c.call("POST", "/api/diag/restart", map[string]any{"unit": "ssh"}); rc != 200 {
		t.Fatal("restart refused")
	}
	fn.mu.Lock()
	got := fn.restarted
	fn.mu.Unlock()
	if got != "ssh" {
		t.Fatalf("restarted %q", got)
	}
	rc, _, bundle := c.call("GET", "/api/diag/bundle", nil)
	if rc != 200 || !strings.Contains(string(bundle), "Settings (secrets hidden)") {
		t.Fatalf("bundle: %d", rc)
	}
}

func TestSettingsInTheBundleHideSecrets(t *testing.T) {
	a := newScopeApp(t)
	a.Config.SetSetting("some.api_token", "abc123")
	a.Config.SetSetting("update.check_hours", "12")
	out := a.settingsForBundle()
	if strings.Contains(out, "abc123") || !strings.Contains(out, "update.check_hours = 12") {
		t.Fatalf("%s", out)
	}
}

func TestTerminalIsOffUntilSwitchedOnAndAsksAgainAfterFifteenMinutes(t *testing.T) {
	c, fn, _ := siteBoxWith(t, "192.168.1.1/24", nil)
	_, m, _ := c.call("GET", "/api/term", nil)
	if m["enabled"] != false {
		t.Fatal("the terminal must be off by default")
	}
	open := func(pw string) int {
		rc, _, _ := c.call("POST", "/api/term/open", map[string]any{"rows": 24, "cols": 80, "password": pw})
		return rc
	}
	if open("correct horse battery") == 200 {
		t.Fatal("opened while switched off")
	}
	// switching on needs the password
	if rc, _, _ := c.call("PUT", "/api/term", map[string]any{"enabled": true}); rc == 200 {
		t.Fatal("switched on without the password")
	}
	if rc, _, raw := c.call("PUT", "/api/term", map[string]any{"enabled": true, "password": "correct horse battery"}); rc != 200 {
		t.Fatalf("switch on: %d %s", rc, raw)
	}
	// the password was just checked, so opening soon after does not ask again
	if _, m, _ := c.call("GET", "/api/term", nil); m["confirmed"] != true {
		t.Fatal("a recent check was not remembered")
	}
	rc, m, raw := c.call("POST", "/api/term/open", map[string]any{"rows": 24, "cols": 80})
	if rc != 200 {
		t.Fatalf("open soon after the check: %d %s", rc, raw)
	}
	id := m["id"].(string)
	// after 15 minutes it asks again, and a wrong password is refused
	termConfirmed.Lock()
	for k := range termConfirmed.m {
		termConfirmed.m[k] = time.Now().Add(-termConfirmFor - time.Minute)
	}
	termConfirmed.Unlock()
	if _, m, _ := c.call("GET", "/api/term", nil); m["confirmed"] != false {
		t.Fatal("an old check was still counted")
	}
	if open("") == 200 || open("wrong") == 200 {
		t.Fatal("opened without the right password")
	}
	if rc, _, raw := c.call("POST", "/api/term/open", map[string]any{"rows": 24, "cols": 80, "password": "correct horse battery"}); rc != 200 {
		t.Fatalf("open with the password: %d %s", rc, raw)
	}
	rc, m, _ = c.call("GET", "/api/term/"+id+"/out?since=3", nil)
	d, _ := base64.StdEncoding.DecodeString(m["data"].(string))
	if rc != 200 || string(d) != "hello" || m["next"].(float64) != 8 {
		t.Fatalf("out: %d %v", rc, m)
	}
	c.call("POST", "/api/term/"+id+"/in", map[string]any{"data": base64.StdEncoding.EncodeToString([]byte("ls\n"))})
	fn.mu.Lock()
	wrote := fn.termWrote
	fn.mu.Unlock()
	if wrote != "ls\n" {
		t.Fatalf("wrote %q", wrote)
	}
	// someone else's or unknown terminal
	if rc, _, _ := c.call("GET", "/api/term/nope/out", nil); rc == 200 {
		t.Fatal("read a terminal that is not open")
	}
	if rc, _, _ := c.call("DELETE", "/api/term/"+id, nil); rc != 200 {
		t.Fatal("close failed")
	}
	if rc, _, _ := c.call("GET", "/api/term/"+id+"/out", nil); rc == 200 {
		t.Fatal("read after close")
	}
	// switching it off stops everything
	c.call("PUT", "/api/term", map[string]any{"enabled": false})
	if open("correct horse battery") == 200 {
		t.Fatal("opened after being switched off")
	}
}

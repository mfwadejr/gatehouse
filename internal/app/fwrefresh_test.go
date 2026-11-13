package app

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
	"time"

	"nopewarden/internal/auth"
	"nopewarden/internal/fw"
)

// After an update that only changes how the rules are written, the settings are still the applied
// ones: Review must say so, and the program applies the refreshed rules itself. A real change of
// settings must never be applied this way.
func TestRefreshAfterUpdateOnlyWhenSettingsAreUnchanged(t *testing.T) {
	refreshSettle = 0
	web, dnsAddr := freeAddr(t), freeAddr(t)
	ff := &fakeFW{counters: map[string]fw.Counter{"default": {Packets: 1}}}
	a, err := New(Config{DataDir: t.TempDir(), WebAddr: web, DNSAddrs: []string{dnsAddr}, FW: ff, Version: "0.9.4.31"})
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
	for i := 0; i < 300; i++ {
		if resp, err := c.hc.Get(c.base + "/api/health"); err == nil {
			resp.Body.Close()
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	_, m, _ := c.call("POST", "/api/auth/login", map[string]string{"username": "admin", "password": "correct horse battery"})
	c.csrf = m["csrf"].(string)
	_, m, _ = c.call("POST", "/api/auth/enrol/begin", nil)
	c.call("POST", "/api/auth/enrol/confirm", map[string]string{"code": auth.TOTPCodeAt(m["secret"].(string), time.Now())})
	if code, _, body := c.call("PUT", "/api/fw/zones", map[string]any{"name": "Home", "kind": "trusted", "ifaces": []string{"eth1"}, "management": true, "internet": true, "gateway": true, "intra_allow": true}); code != 200 {
		t.Fatalf("home: %d %s", code, body)
	}

	// The live revision was written by an older version with different text, from these very settings.
	mod, script, err := a.currentRuleset()
	if err != nil {
		t.Fatal(err)
	}
	a.Config.Exec(`UPDATE fw_revisions SET status='superseded'`)
	a.Config.Exec(`INSERT INTO fw_revisions(created_at,author,summary,ruleset,status,model) VALUES(?,?,?,?,'confirmed',?)`,
		time.Now().Unix(), "admin", "old", "# written by an older version\n"+script, revisionPrint(mod))
	a.Config.SetSetting("fw.rules_version", "0.9.4.30")

	st := a.fwStatus()
	if st["dirty"] != true || st["refresh_only"] != true {
		t.Fatalf("an update that only rewrites the rules should wait as a refresh: %v", st)
	}
	a.refreshRulesIfNeeded()
	ff.mu.Lock()
	applied := ff.script != ""
	ff.mu.Unlock()
	if !applied {
		t.Fatal("the refreshed rules were not applied")
	}
	if st = a.fwStatus(); st["dirty"] != false {
		t.Fatalf("nothing should be waiting after the refresh: %v", st)
	}
	if a.setting("fw.rules_version", "") != "0.9.4.31" {
		t.Fatal("the live rules were not stamped with this version")
	}
	var who string
	a.Config.QueryRow(`SELECT author FROM fw_revisions WHERE status='confirmed'`).Scan(&who)
	if who != "NopeWarden" {
		t.Fatalf("author %q", who)
	}

	// A person's own change is not a refresh and is never applied by itself.
	if code, _, body := c.call("PUT", "/api/fw/zones", map[string]any{"name": "Guests", "kind": "guest", "ifaces": []string{"eth2"}, "internet": true, "gateway": true}); code != 200 {
		t.Fatalf("guests: %d %s", code, body)
	}
	a.Config.SetSetting("fw.rules_version", "0.9.4.30")
	ff.mu.Lock()
	ff.script = ""
	ff.mu.Unlock()
	if st = a.fwStatus(); st["dirty"] != true || st["refresh_only"] != false {
		t.Fatalf("a changed setting is a real change: %v", st)
	}
	a.refreshRulesIfNeeded()
	ff.mu.Lock()
	got := ff.script
	ff.mu.Unlock()
	if strings.TrimSpace(got) != "" {
		t.Fatal("a real change was applied without being asked")
	}
	// Settings that were never fingerprinted are left for one manual commit.
	a.Config.Exec(`UPDATE fw_revisions SET model='' WHERE status='confirmed'`)
	if a.rulesRefreshOnly() {
		t.Fatal("an unfingerprinted revision cannot be called a refresh")
	}
}

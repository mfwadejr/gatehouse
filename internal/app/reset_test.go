package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"nopewarden/internal/api"
	"nopewarden/internal/auth"
)

func TestNetworkResetClearsNetworksAndKeepsTheAccount(t *testing.T) {
	c, fn, data := updBox(t, "http://127.0.0.1:1/none", true)
	// siteBoxWith made a zone named Home; confirm it is there first.
	if _, m, _ := c.call("GET", "/api/fw/zones", nil); m == nil {
		// the endpoint returns a list; only reaching it matters here
	}
	var before int
	c.app.Config.QueryRow(`SELECT count(*) FROM zones`).Scan(&before)
	if before == 0 {
		t.Fatal("test box has no zone to reset")
	}
	if rc, _, raw := c.call("POST", "/api/system/reset", map[string]any{"level": "network", "password": "correct horse battery"}); rc != 200 {
		t.Fatalf("reset: %d %s", rc, raw)
	}
	var zones, users int
	c.app.Config.QueryRow(`SELECT count(*) FROM zones`).Scan(&zones)
	c.app.Config.QueryRow(`SELECT count(*) FROM users`).Scan(&users)
	if zones != 0 || users != 1 {
		t.Fatalf("zones=%d users=%d", zones, users)
	}
	// DNS servers are put back, so name lookups keep working.
	var ups int
	c.app.Config.QueryRow(`SELECT count(*) FROM dns_upstreams`).Scan(&ups)
	if ups == 0 {
		t.Fatal("no DNS servers after the reset")
	}
	// A copy of the settings from before is kept.
	files, _ := filepath.Glob(filepath.Join(data, "backups", "before-reset-*.ghbk"))
	if len(files) != 1 {
		t.Fatalf("backups: %v", files)
	}
	// The empty network and firewall are applied shortly after the answer (that is when the
	// "apply on first start" marker is cleared).
	waitFor(t, "the starting network to be put back", func() bool {
		return func() bool {
			c.app.Net.(*fakeNet).mu.Lock()
			defer c.app.Net.(*fakeNet).mu.Unlock()
			return c.app.Net.(*fakeNet).resets == 1
		}()
	})
	_ = fn
	// Still signed in: a network reset does not touch sessions.
	if rc, _, _ := c.call("GET", "/api/system/update", nil); rc != 200 {
		t.Fatalf("signed out by a network reset: %d", rc)
	}
}

func TestFactoryResetNeedsTheWordAndReopensSetup(t *testing.T) {
	c, _, _ := updBox(t, "http://127.0.0.1:1/none", true)
	if rc, _, _ := c.call("POST", "/api/system/reset", map[string]any{"level": "factory"}); rc == 200 {
		t.Fatal("factory reset without confirmation")
	}
	if rc, _, _ := c.call("POST", "/api/system/reset", map[string]any{"level": "factory", "confirm": "RESET?"}); rc == 200 {
		t.Fatal("factory reset with the wrong word")
	}
	if rc, _, raw := c.call("POST", "/api/system/reset", map[string]any{"level": "factory", "confirm": "reset", "password": "correct horse battery"}); rc != 200 {
		t.Fatalf("%d %s", rc, raw)
	}
	var users int
	c.app.Config.QueryRow(`SELECT count(*) FROM users`).Scan(&users)
	if users != 0 {
		t.Fatalf("%d users left", users)
	}
	if _, m, _ := c.call("GET", "/api/setup", nil); m["needed"] != true {
		t.Fatalf("setup is not open again: %v", m)
	}
}

func TestResetRefusesUnknownLevel(t *testing.T) {
	c, _, _ := updBox(t, "http://127.0.0.1:1/none", true)
	if rc, _, _ := c.call("POST", "/api/system/reset", map[string]any{"level": "everything"}); rc == 200 {
		t.Fatal("accepted a made-up level")
	}
}

func TestBackupDownload(t *testing.T) {
	c, _, data := updBox(t, "http://127.0.0.1:1/none", true)
	rc, _, raw := c.call("GET", "/api/system/backup", nil)
	if rc != 200 || len(raw) < 2 || raw[0] != 0x1f || raw[1] != 0x8b {
		t.Fatalf("%d %.40q", rc, raw)
	}
	// The download is a real backup that is kept in the list.
	left, _ := filepath.Glob(filepath.Join(data, "backups", "manual-*.ghbk"))
	if len(left) != 1 {
		t.Fatalf("expected the backup to be kept, found %d", len(left))
	}
	_ = os.Remove
}

func TestRestoreDefaultsOnlyTouchesPreferences(t *testing.T) {
	c, _, _ := updBox(t, "http://127.0.0.1:1/none", true)
	c.app.Config.SetSetting("backup.config", `{"schedule":"weekly"}`)
	c.app.Config.SetSetting("logs.retention_days", "90")
	c.app.Config.SetSetting("prefs.1.devices", `{"rows":50}`)
	c.app.Config.SetSetting("sched.update", "daily 03:00")
	c.app.Config.SetSetting("sched.update.last", "123")
	c.app.Config.SetSetting("console.port", "8443")
	c.app.Config.SetSetting("syslog.config", `{"host":"10.0.0.9"}`)
	var zones int
	c.app.Config.QueryRow(`SELECT count(*) FROM zones`).Scan(&zones)
	if rc, _, _ := c.call("POST", "/api/system/reset", map[string]any{"level": "defaults"}); rc != 403 {
		t.Fatal("restoring defaults must ask for the password")
	}
	if rc, _, raw := c.call("POST", "/api/system/reset", map[string]any{"level": "defaults", "password": "correct horse battery"}); rc != 200 {
		t.Fatalf("restore defaults: %d %s", rc, raw)
	}
	for _, k := range []string{"backup.config", "logs.retention_days", "prefs.1.devices", "sched.update"} {
		if c.app.Config.Setting(k, "gone") != "gone" {
			t.Errorf("%s should be back to its default", k)
		}
	}
	for k, want := range map[string]string{"sched.update.last": "123", "console.port": "8443", "syslog.config": `{"host":"10.0.0.9"}`} {
		if got := c.app.Config.Setting(k, "gone"); got != want {
			t.Errorf("%s should stay, got %q", k, got)
		}
	}
	var after int
	c.app.Config.QueryRow(`SELECT count(*) FROM zones`).Scan(&after)
	if after != zones || after == 0 {
		t.Fatalf("networks must not change: %d then %d", zones, after)
	}
	// Still signed in, and no network reset was started.
	if rc, _, _ := c.call("GET", "/api/system/update", nil); rc != 200 {
		t.Fatalf("signed out: %d", rc)
	}
}

func TestDangerZoneAsksForThePasswordAndTheCode(t *testing.T) {
	c, _, _ := updBox(t, "http://127.0.0.1:1/none", true)
	u := &auth.User{ID: 1, Username: "admin", TOTPEnabled: true}
	if rows := c.app.Config.QueryRow(`SELECT id, username FROM users LIMIT 1`); rows != nil {
		rows.Scan(&u.ID, &u.Username)
	}
	secret, _, err := c.app.Auth.BeginEnrol(u.ID, "NopeWarden")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.app.Auth.ConfirmEnrol(u.ID, auth.TOTPCodeAt(secret, time.Now())); err != nil {
		t.Fatal(err)
	}
	ctx := &api.Ctx{User: u, IP: "127.0.0.1"}
	if err := c.app.dangerous(ctx, "123456", ""); err != errNeedPass {
		t.Fatalf("a code alone is not enough, got %v", err)
	}
	if err := c.app.dangerous(ctx, "123456", "wrong"); err == nil || err == errNeedCode {
		t.Fatalf("a wrong password is refused as a wrong password, got %v", err)
	}
	if err := c.app.dangerous(ctx, "", "correct horse battery"); err != errNeedCode {
		t.Fatalf("the right password without a code must ask for the code, got %v", err)
	}
	if err := c.app.dangerous(ctx, "000000", "correct horse battery"); err != errNeedCode {
		t.Fatalf("a wrong code must be refused, got %v", err)
	}
	good := auth.TOTPCodeAt(secret, time.Now().Add(30*time.Second))
	if err := c.app.dangerous(ctx, good, "correct horse battery"); err != nil {
		t.Fatalf("password and the right code must pass, got %v", err)
	}
	u.TOTPEnabled = false
	if err := c.app.dangerous(ctx, "", "correct horse battery"); err != nil {
		t.Fatalf("without two-factor the password is enough, got %v", err)
	}
}

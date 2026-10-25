package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	if rc, _, raw := c.call("POST", "/api/system/reset", map[string]any{"level": "network"}); rc != 200 {
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
	files, _ := filepath.Glob(filepath.Join(data, "backups", "before-reset-*.db"))
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
	if rc, _, raw := c.call("POST", "/api/system/reset", map[string]any{"level": "factory", "confirm": "reset"}); rc != 200 {
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
	if rc != 200 || !strings.HasPrefix(string(raw), "SQLite format 3") {
		t.Fatalf("%d %.40q", rc, raw)
	}
	// The copy is removed just after the answer has been sent, so give the handler a moment.
	waitFor(t, "the temporary copy to be removed", func() bool {
		left, _ := filepath.Glob(filepath.Join(data, "backups", "download-*.db"))
		return len(left) == 0
	})
	_ = os.Remove
}

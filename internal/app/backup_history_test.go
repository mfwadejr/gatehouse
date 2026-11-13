package app

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func tarNames(t *testing.T, p string) map[string]bool {
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	tr := tar.NewReader(zr)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		out[h.Name] = true
	}
}

func TestBackupHistoryRecordsWhoDidWhatAndLogsAreOptIn(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	_, m, _ := c.call("POST", "/api/backups", nil)
	name := m["name"].(string)
	p := filepath.Join(c.app.backupDir(), name)
	if tarNames(t, p)["logs.db"] {
		t.Fatal("logs must not be in a backup unless asked for")
	}
	if rc, _, raw := c.call("PUT", "/api/backups/config", map[string]any{"schedule": "daily", "hour": 2, "keep": 7, "include_history": true}); rc != 200 {
		t.Fatalf("config: %d %s", rc, raw)
	}
	_, m, _ = c.call("POST", "/api/backups", nil)
	if !tarNames(t, filepath.Join(c.app.backupDir(), m["name"].(string)))["logs.db"] {
		t.Fatal("logs should be in the backup when asked for")
	}
	// The backup with logs still checks and lists.
	if rc, _, raw := c.call("POST", "/api/backups/"+m["name"].(string)+"/check", map[string]any{"password": ""}); rc != 200 {
		t.Fatalf("check: %d %s", rc, raw)
	}
	c.call("DELETE", "/api/backups/"+name, nil)
	rc, h, raw := c.call("GET", "/api/backups/history", nil)
	if rc != 200 {
		t.Fatalf("history: %d %s", rc, raw)
	}
	ev := h["events"].([]any)
	if len(ev) < 3 {
		t.Fatalf("expected made, made, deleted; got %s", raw)
	}
	first := ev[0].(map[string]any)
	if first["action"] != "deleted" || first["actor"] == "" || first["actor"] == "system" {
		t.Fatalf("newest should be the delete by a person: %v", first)
	}
}

package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBackupIsCheckedWhenMadeAndRestoreBringsItBack(t *testing.T) {
	a, _ := newSpeedApp(t)
	p, err := a.saveBackup("manual")
	if err != nil {
		t.Fatal(err)
	}
	m := a.readMeta(p)
	if m.Report == nil || !m.Report.OK {
		t.Fatalf("a fresh backup must pass its checks: %+v", m.Report)
	}
	if m.Report.Contents.Networks < 0 || len(m.Report.Checks) < 5 {
		t.Fatalf("checks: %+v", m.Report.Checks)
	}
	// Change something, then restore.
	if _, err := a.Config.Exec(`DELETE FROM wan_links WHERE name='LTE'`); err != nil {
		t.Fatal(err)
	}
	res, err := a.restoreBackup(p, "", "tester")
	if err != nil {
		t.Fatal(err)
	}
	var n int
	a.Config.QueryRow(`SELECT COUNT(*) FROM wan_links`).Scan(&n)
	if n != 2 {
		t.Fatalf("restore did not bring the connection back, have %d", n)
	}
	if res.Safety == "" || res.KeyChanged {
		t.Fatalf("%+v", res)
	}
	if _, err := os.Stat(filepath.Join(a.backupDir(), res.Safety)); err != nil {
		t.Fatalf("the safety copy is missing: %v", err)
	}
}

func TestDamagedBackupFailsItsChecksAndIsNeverRestored(t *testing.T) {
	a, _ := newSpeedApp(t)
	p, _ := a.saveBackup("manual")
	raw, _ := os.ReadFile(p)
	raw[len(raw)/2] ^= 0xff
	os.WriteFile(p, raw, 0o600)
	rep, _ := a.verifyBackup(p, "")
	if rep.OK {
		t.Fatal("a damaged file passed")
	}
	if _, err := a.restoreBackup(p, "", "tester"); err == nil {
		t.Fatal("a damaged backup was restored")
	}
	if _, err := a.verifyBackup(filepath.Join(t.TempDir(), "nope.ghbk"), ""); err != nil {
		t.Fatal(err)
	}
}

func TestOnlyTheChosenNumberOfBackupsAreKept(t *testing.T) {
	a, _ := newSpeedApp(t)
	a.Config.SetSetting("backup.config", `{"schedule":"daily","hour":2,"keep":2}`)
	dir := a.backupDir()
	os.MkdirAll(dir, 0o700)
	for _, n := range []string{"auto-20260101-020000", "auto-20260102-020000", "auto-20260103-020000", "manual-20260104-020000"} {
		os.WriteFile(filepath.Join(dir, n+".ghbk"), []byte("x"), 0o600)
	}
	// The older of the four was checked by hand, so it stays.
	a.writeMeta(filepath.Join(dir, "auto-20260101-020000.ghbk"), bkMeta{HandChecked: 5})
	a.pruneBackups("manual")
	var left []string
	files, _ := filepath.Glob(filepath.Join(dir, "*.ghbk"))
	for _, f := range files {
		left = append(left, filepath.Base(f))
	}
	got := strings.Join(left, ",")
	if !strings.Contains(got, "auto-20260101") || !strings.Contains(got, "manual-20260104") || strings.Contains(got, "auto-20260102") {
		t.Fatalf("kept: %s", got)
	}
}

func TestPasswordProtectedBackupOpensOnlyWithThePassword(t *testing.T) {
	enc, err := encrypt([]byte("secret settings"), "correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decrypt(enc, "wrong"); err == nil {
		t.Fatal("the wrong password worked")
	}
	if pt, err := decrypt(enc, "correct horse"); err != nil || string(pt) != "secret settings" {
		t.Fatalf("%q %v", pt, err)
	}
}

func TestRestoreCoversEveryTable(t *testing.T) {
	a, _ := newSpeedApp(t)
	rows, err := a.Config.Query(`SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		t.Fatal(err)
	}
	known := map[string]bool{"settings": true, "schema_version": true, "sessions": true}
	for _, t := range append(append([]string{}, restoreTables...), restoreAccountTables...) {
		known[t] = true
	}
	for rows.Next() {
		var n string
		rows.Scan(&n)
		if !known[n] {
			t.Errorf("table %s is not handled by restore; a restore would leave it out of date", n)
		}
	}
	rows.Close()
}

func TestScheduledBackupHappensOnceADay(t *testing.T) {
	a, _ := newSpeedApp(t)
	a.Config.SetSetting("backup.config", `{"schedule":"daily","hour":0,"keep":5}`)
	now := time.Now()
	a.backupTick(now)
	a.backupTick(now.Add(time.Hour))
	files, _ := filepath.Glob(filepath.Join(a.backupDir(), "auto-*.ghbk"))
	if len(files) != 1 {
		t.Fatalf("expected one scheduled backup, got %d", len(files))
	}
}

func TestBackupsThroughTheConsole(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	rc, m, raw := c.call("POST", "/api/backups", nil)
	if rc != 200 || m["name"] == nil {
		t.Fatalf("make: %d %s", rc, raw)
	}
	name := m["name"].(string)
	if rc, m, raw := c.call("POST", "/api/backups/"+name+"/check", map[string]any{"password": ""}); rc != 200 || m["report"] == nil {
		t.Fatalf("check: %d %s", rc, raw)
	}
	if rc, _, _ := c.call("POST", "/api/backups/"+name+"/hand-check", map[string]any{}); rc != 200 {
		t.Fatal("hand check refused for a backup that passed")
	}
	if rc, _, _ := c.call("POST", "/api/backups/../../etc/passwd/check", map[string]any{}); rc == 200 {
		t.Fatal("a path outside the backups was accepted")
	}
	if rc, _, _ := c.call("POST", "/api/backups/"+name+"/restore", map[string]any{"confirm": "nope"}); rc != 400 {
		t.Fatalf("restore without the confirmation word must be refused, got %d", rc)
	}
	rc, m, raw = c.call("GET", "/api/backups", nil)
	if rc != 200 || len(m["backups"].([]any)) < 1 {
		t.Fatalf("list: %d %s", rc, raw)
	}
}

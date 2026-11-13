package app

import (
	"testing"
	"time"
)

func TestRetentionKeepsAdminActivityLongerAndManualClearSparesIt(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	old := time.Now().Add(-100 * 24 * time.Hour).Unix()
	c.app.Logs.Exec(`DELETE FROM events`)
	ins := func(area, actor, title string, ts int64) {
		c.app.Logs.Exec(`INSERT INTO events(ts,area,severity,actor,title,detail) VALUES(?,?,?,?,?,?)`, ts, area, "info", actor, title, "")
	}
	ins("system", "system", "old general", old)
	ins("admin", "admin", "old sign-in", old)
	ins("system", "system", "new general", time.Now().Unix())
	count := func(q string) int {
		var n int
		c.app.Logs.QueryRow(q).Scan(&n)
		return n
	}
	// Preview first: only the old general row is past the 30 day rule; the old sign-in is inside 365.
	rc, m, raw := c.call("GET", "/api/logs/cleanup-preview", nil)
	if rc != 200 {
		t.Fatalf("preview: %d %s", rc, raw)
	}
	w := m["would_remove"].(map[string]any)
	if w["events"].(float64) != 1 || w["admin"].(float64) != 0 {
		t.Fatalf("preview wrong: %v", w)
	}
	if rc, _, raw := c.call("POST", "/api/logs/prune", nil); rc != 200 {
		t.Fatalf("prune: %d %s", rc, raw)
	}
	if count(`SELECT COUNT(*) FROM events WHERE title='old general'`) != 0 || count(`SELECT COUNT(*) FROM events WHERE title='old sign-in'`) != 1 {
		t.Fatal("general activity should age out at 30 days and admin activity should stay")
	}
	// Admin activity cannot be cleared by hand; clearing needs the password.
	if rc, _, _ := c.call("POST", "/api/logs/clear", map[string]any{"kinds": []string{"admin"}, "password": "correct horse battery"}); rc != 400 {
		t.Fatalf("admin activity must not be clearable, got %d", rc)
	}
	if rc, _, _ := c.call("POST", "/api/logs/clear", map[string]any{"kinds": []string{"events"}}); rc != 403 {
		t.Fatalf("clearing without the password must be refused, got %d", rc)
	}
	if rc, _, raw := c.call("POST", "/api/logs/clear", map[string]any{"kinds": []string{"events"}, "password": "correct horse battery"}); rc != 200 {
		t.Fatalf("clear: %d %s", rc, raw)
	}
	if count(`SELECT COUNT(*) FROM events WHERE title='new general'`) != 0 {
		t.Fatal("general activity should have been cleared")
	}
	if count(`SELECT COUNT(*) FROM events WHERE title='old sign-in'`) != 1 {
		t.Fatal("admin activity must survive a manual clear")
	}
	if count(`SELECT COUNT(*) FROM events WHERE title='Logs were cleared by hand'`) != 1 {
		t.Fatal("the clear itself must be written to admin activity")
	}
}

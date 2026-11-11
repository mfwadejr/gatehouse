package app

import (
	"testing"
	"time"
)

// A change that was never confirmed is marked undone once its time is well over, and one that is
// still inside its time (or was kept) is left alone.
func TestUnconfirmedChangeIsMarkedUndone(t *testing.T) {
	a, _ := newSpeedApp(t)
	now := time.Now().Unix()
	old := now - int64(confirmWindow/time.Second) - 30
	ins := func(at int64, status string) int64 {
		res, err := a.Config.Exec(`INSERT INTO fw_revisions(created_at,author,summary,ruleset,status) VALUES(?,?,?,?,?)`, at, "marvin", "x", "r", status)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		return id
	}
	stale, fresh, kept := ins(old, "pending"), ins(now, "pending"), ins(old, "confirmed")
	a.settleRevisions()
	status := func(id int64) string {
		var s string
		a.Config.QueryRow(`SELECT status FROM fw_revisions WHERE version=?`, id).Scan(&s)
		return s
	}
	if status(stale) != "rolled_back" || status(fresh) != "pending" || status(kept) != "confirmed" {
		t.Fatalf("stale=%s fresh=%s kept=%s", status(stale), status(fresh), status(kept))
	}
}

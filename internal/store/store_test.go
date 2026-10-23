package store

import (
	"path/filepath"
	"testing"
)

func TestMigrateIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 2; i++ {
		c, err := OpenConfig(filepath.Join(dir, "config.db"))
		if err != nil {
			t.Fatalf("config open #%d: %v", i, err)
		}
		var n int
		if err := c.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table'`).Scan(&n); err != nil || n < 20 {
			t.Fatalf("expected many tables, got %d err=%v", n, err)
		}
		c.Close()
		l, err := OpenLogs(filepath.Join(dir, "logs.db"))
		if err != nil {
			t.Fatalf("logs open #%d: %v", i, err)
		}
		l.Close()
	}
}

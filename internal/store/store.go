// Package store opens the two SQLite databases and applies their migrations.
package store

import (
	"database/sql"
	"fmt"
	"net/url"
	"strings"

	_ "github.com/mattn/go-sqlite3"
)

// DB wraps a *sql.DB with the helpers the rest of the program uses.
type DB struct {
	*sql.DB
	Path string
}

func open(path string, maxConns int) (*DB, error) {
	q := url.Values{}
	q.Set("_journal_mode", "WAL")
	q.Set("_busy_timeout", "5000")
	q.Set("_foreign_keys", "on")
	q.Set("_synchronous", "NORMAL")
	dsn := "file:" + path + "?" + q.Encode()
	if path == ":memory:" {
		dsn = "file::memory:?cache=shared&_foreign_keys=on"
	}
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(maxConns)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return &DB{DB: db, Path: path}, nil
}

func migrate(db *DB, name string, steps []string) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_version(id INTEGER PRIMARY KEY CHECK (id=1), version INTEGER NOT NULL)`); err != nil {
		return err
	}
	var cur int
	err := db.QueryRow(`SELECT version FROM schema_version WHERE id=1`).Scan(&cur)
	if err == sql.ErrNoRows {
		if _, err := db.Exec(`INSERT INTO schema_version(id,version) VALUES(1,0)`); err != nil {
			return err
		}
		cur = 0
	} else if err != nil {
		return err
	}
	for i := cur; i < len(steps); i++ {
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		for _, stmt := range splitStatements(steps[i]) {
			if _, err := tx.Exec(stmt); err != nil {
				tx.Rollback()
				return fmt.Errorf("%s migration %d: %w\n%s", name, i+1, err, stmt)
			}
		}
		if _, err := tx.Exec(`UPDATE schema_version SET version=? WHERE id=1`, i+1); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// splitStatements splits on semicolons that end a line. Migrations are written so this is safe.
func splitStatements(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ";\n") {
		part = strings.TrimSpace(part)
		part = strings.TrimSuffix(part, ";")
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

// OpenConfig opens config.db (single writer connection keeps SQLite simple and safe).
func OpenConfig(path string) (*DB, error) {
	db, err := open(path, 1)
	if err != nil {
		return nil, err
	}
	if err := migrate(db, "config", configMigrations); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// OpenLogs opens logs.db. Reads may run in parallel with the batching writer.
func OpenLogs(path string) (*DB, error) {
	db, err := open(path, 4)
	if err != nil {
		return nil, err
	}
	if err := migrate(db, "logs", logMigrations); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

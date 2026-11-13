package app

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestStorageReportAndPruneLong(t *testing.T) {
	a := newScopeApp(t)
	old := time.Now().Add(-400 * 24 * time.Hour).Unix()
	now := time.Now().Unix()
	a.Logs.Exec(`INSERT INTO threat_alerts(ts,sid,signature,plain) VALUES(?,1,'a','old'),(?,2,'b','new')`, old, now)
	a.Logs.Exec(`INSERT INTO outages(start,end) VALUES(?,?),(?,0)`, old, old+60, now)
	for i := 1; i <= 260; i++ {
		a.Config.Exec(`INSERT INTO fw_revisions(version,created_at,ruleset,status) VALUES(?,?,'x','superseded')`, 1000+i, now)
	}
	a.Config.Exec(`INSERT INTO fw_revisions(version,created_at,ruleset,status) VALUES(1,?,'x','confirmed'),(2,?,'x','pending')`, now, now)
	a.pruneLong()
	var n int
	a.Logs.QueryRow(`SELECT COUNT(*) FROM threat_alerts`).Scan(&n)
	if n != 1 {
		t.Fatalf("alerts left %d", n)
	}
	a.Logs.QueryRow(`SELECT COUNT(*) FROM outages`).Scan(&n)
	if n != 1 {
		t.Fatalf("outages left %d (the open one must stay)", n)
	}
	a.Config.QueryRow(`SELECT COUNT(*) FROM fw_revisions`).Scan(&n)
	if n != 202 {
		t.Fatalf("revisions left %d, want 200 newest plus confirmed and pending", n)
	}
	rep := a.storageReport()
	tabs, _ := rep["tables"].([]storRow)
	if len(tabs) != 11 {
		t.Fatalf("tables reported: %d", len(tabs))
	}
	for _, r := range tabs {
		if r.Policy == "" {
			t.Fatalf("%s has no rule", r.Key)
		}
	}
}

func TestAdminActivityAndSignInAudit(t *testing.T) {
	a := newScopeApp(t)
	if _, err := a.Auth.CreateUser("marvin", "correct horse battery", "admin"); err != nil {
		t.Fatal(err)
	}
	a.event("marvin", "info", "Added the network Guests", "")
	a.event("system", "info", "Country address lists updated", "")
	a.auditEvent("mallory", "warning", "Sign-in failed", "From 203.0.113.9: wrong password.")
	rows, total := a.adminActivity("", 0, 50, 0)
	if total != 2 || len(rows) != 2 {
		t.Fatalf("want the user's change and the failed sign-in, got %d: %+v", total, rows)
	}
	if r, _ := a.adminActivity("failed 203.0.113.9", 0, 50, 0); len(r) != 1 || r[0].Who != "mallory" {
		t.Fatalf("search by several words: %+v", r)
	}
	if r, _ := a.adminActivity("guests marvin", 0, 50, 0); len(r) != 1 {
		t.Fatalf("search across who and what: %+v", r)
	}
	// Special characters are matched as plain text, never as a pattern, and never break the query.
	for _, q := range []string{"%", "_", `'; DROP TABLE events; --`, `\`} {
		a.adminActivity(q, 0, 50, 0)
	}
	if _, total := a.adminActivity("", 0, 50, 0); total != 2 {
		t.Fatalf("events table damaged: %d", total)
	}
}

func TestSelfHealthVerdict(t *testing.T) {
	a := newScopeApp(t)
	h := a.selfHealth()
	if h["text"] == "" || h["level"] == nil {
		t.Fatalf("no verdict: %v", h)
	}
	if facts, _ := h["facts"].([]healthFact); len(facts) < 4 {
		t.Fatalf("facts: %v", h["facts"])
	}
	// Five warnings in the last hour make the verdict ask for a look, and the issue says where.
	for i := 0; i < 6; i++ {
		a.event("system", "warning", "Something odd", "")
	}
	h = a.selfHealth()
	issues, _ := h["issues"].([]healthIssue)
	found := false
	for _, i := range issues {
		if i.To == "logs/activity" {
			found = true
		}
	}
	if !found || h["level"] == "good" {
		t.Fatalf("repeated warnings were not raised: %v", h)
	}
}

func TestFirewallFailureStaysVisibleUntilAChangeApplies(t *testing.T) {
	a := newScopeApp(t)
	a.noteFWFailure(fmt.Errorf("nft: syntax error"), "marvin")
	st := a.fwStatus()
	if st["last_error"] != "nft: syntax error" {
		t.Fatalf("status should carry the failure: %v", st)
	}
	h := a.selfHealth()
	if h["level"] != "bad" {
		t.Fatalf("Diagnostics should say there is a problem: %v", h["level"])
	}
	a.clearFWFailure()
	if _, still := a.fwStatus()["last_error"]; still {
		t.Fatal("failure should clear once a change applies")
	}
}

func TestFailedSavesAreNotSilent(t *testing.T) {
	a := newScopeApp(t)
	if _, err := a.Logs.Exec(`INSERT INTO no_such_table VALUES(1)`); err == nil {
		t.Fatal("expected an error")
	}
	n, last, _ := a.Logs.WriteFailures(time.Hour)
	if n != 1 || last == "" {
		t.Fatalf("n=%d last=%q", n, last)
	}
	h := a.selfHealth()
	if !strings.Contains(fmt.Sprint(h["issues"]), "saves failed") || h["level"] != "bad" {
		t.Fatalf("health: %v", h)
	}
}

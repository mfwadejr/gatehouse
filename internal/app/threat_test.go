package app

import (
	"fmt"
	"testing"
	"time"
)

func evLine(sid int, cat string) []byte {
	return []byte(fmt.Sprintf(`{"event_type":"alert","src_ip":"192.168.1.20","dest_ip":"203.0.113.9","dest_port":443,"proto":"TCP","alert":{"signature_id":%d,"signature":"ET TEST sig %d","category":%q,"severity":2}}`, sid, sid, cat))
}

func countAlerts(a *App) (n int) {
	a.Logs.QueryRow(`SELECT COUNT(*) FROM threat_alerts`).Scan(&n)
	return
}

func TestThreatAlertsAreReadTunedAndFailOpen(t *testing.T) {
	a := newScopeApp(t)
	now := time.Now()
	// ignores lines that are not alerts, or are garbage
	for _, l := range [][]byte{[]byte(`{"event_type":"flow"}`), []byte(`not json`), []byte(`{"event_type":"alert","alert":{}}`)} {
		if a.threatIngest(l, now) {
			t.Fatalf("kept %s", l)
		}
	}
	if !a.threatIngest(evLine(1000, "A Network Trojan was detected"), now) {
		t.Fatal("real alert dropped")
	}
	var plain string
	a.Logs.QueryRow(`SELECT plain FROM threat_alerts`).Scan(&plain)
	if plain != "A device may be infected and talking to a known bad server." {
		t.Fatalf("plain words: %q", plain)
	}
	var ev int
	a.Logs.QueryRow(`SELECT COUNT(*) FROM events WHERE area='threat'`).Scan(&ev)
	if ev != 1 {
		t.Fatalf("log event: %d", ev)
	}
	// let it through: not kept again
	a.Config.Exec(`INSERT INTO threat_allowed(sid,mac,until) VALUES(1000,'',0)`)
	if a.threatIngest(evLine(1000, "Trojan"), now) {
		t.Fatal("allowed check still alerts")
	}
	// an allow that has run out no longer applies
	a.Config.Exec(`DELETE FROM threat_allowed`)
	a.Config.Exec(`INSERT INTO threat_allowed(sid,mac,until) VALUES(1000,'',?)`, now.Add(-time.Hour).Unix())
	if !a.threatIngest(evLine(1000, "Trojan"), now) {
		t.Fatal("expired allow still hides it")
	}
	// a noisy check is quieted after too many, and the person is told once
	for i := 0; i < noisyPerWindow+5; i++ {
		a.threatIngest(evLine(2000, "Misc activity"), now)
	}
	var auto int
	a.Config.QueryRow(`SELECT COUNT(*) FROM threat_allowed WHERE sid=2000 AND auto=1`).Scan(&auto)
	if auto != 1 {
		t.Fatalf("noisy check not quieted: %d", auto)
	}
	if got := countAlerts(a); got > noisyPerWindow+3 {
		t.Fatalf("kept too many: %d", got)
	}
	// off: nothing is kept
	before := countAlerts(a)
	a.Config.SetSetting(threatModeSetting, "off")
	if a.threatIngest(evLine(3000, "Trojan"), now) || countAlerts(a) != before {
		t.Fatal("kept an alert while off")
	}
}

func TestThreatEngineMissingIsPlain(t *testing.T) {
	a := newScopeApp(t)
	a.Config.SetSetting(threatEveSetting, "/nonexistent/eve.json")
	if a.threatEngine()["state"] != "missing" {
		t.Fatal("a missing engine should say so")
	}
}

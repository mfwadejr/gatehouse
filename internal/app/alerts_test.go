package app

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestQuietHoursCrossMidnight(t *testing.T) {
	q := alertQuiet{Enabled: true, From: "22:00", To: "07:00"}
	at := func(h, m int) time.Time { return time.Date(2026, 10, 30, h, m, 0, 0, time.UTC) }
	for _, c := range []struct {
		h, m int
		want bool
	}{{23, 0, true}, {2, 30, true}, {6, 59, true}, {7, 0, false}, {12, 0, false}, {21, 59, false}, {22, 0, true}} {
		if got := inQuiet(q, at(c.h, c.m)); got != c.want {
			t.Errorf("%d:%02d got %v want %v", c.h, c.m, got, c.want)
		}
	}
	if inQuiet(alertQuiet{Enabled: false, From: "22:00", To: "07:00"}, at(23, 0)) {
		t.Error("off must never be quiet")
	}
	if !inQuiet(alertQuiet{Enabled: true, From: "09:00", To: "17:00"}, at(12, 0)) || inQuiet(alertQuiet{Enabled: true, From: "09:00", To: "17:00"}, at(18, 0)) {
		t.Error("same-day window")
	}
}

func TestEmailMessageCannotInjectHeaders(t *testing.T) {
	m := string(emailMessage("a@b.c", "d@e.f", "Hello\r\nBcc: evil@x.y", "body"))
	if strings.Contains(m, "\r\nBcc:") {
		t.Fatalf("header injected:\n%s", m)
	}
}

func TestRepairEvents(t *testing.T) {
	if !repairEvent("system", "Gatehouse was updated") || !repairEvent("wifi", "Wi-Fi on wlan0 is working again") || repairEvent("system", "Console port changed") {
		t.Fatal("wrong classification")
	}
}

func TestAlertsSendOnlyWhatIsOnAndHideSecrets(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	got := make(chan string, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got <- r.URL.Path + "|" + r.Header.Get("Title") + "|" + r.Header.Get("Authorization") + "|" + string(b)
	}))
	defer srv.Close()

	// A bad ntfy setup is refused with a reason.
	rc, _, raw := c.call("PUT", "/api/alerts", map[string]any{"ntfy": map[string]any{"enabled": true, "server": srv.URL}, "email": map[string]any{"security": "starttls"}})
	if rc == 200 || !strings.Contains(string(raw), "topic") {
		t.Fatalf("%d %s", rc, raw)
	}
	rc, m, raw := c.call("PUT", "/api/alerts", map[string]any{
		"ntfy":  map[string]any{"enabled": true, "server": srv.URL, "topic": "gate", "new_token": "tk_secret"},
		"email": map[string]any{"security": "starttls", "port": 587},
		"on":    map[string]bool{"new_device": true},
	})
	if rc != 200 {
		t.Fatalf("%d %s", rc, raw)
	}
	if strings.Contains(string(raw), "tk_secret") || m["ntfy"].(map[string]any)["has_token"] != true {
		t.Fatalf("secret leaked or not stored: %s", raw)
	}
	// The sealed token is not the plain one.
	if strings.Contains(c.app.setting("alerts.config", ""), "tk_secret") {
		t.Fatal("token stored in plain text")
	}
	a := c.app
	a.alert("internet", "should not send", "")
	a.alert("new_device", "A new device joined: Phone", "details")
	select {
	case s := <-got:
		if !strings.HasPrefix(s, "/gate|A new device joined: Phone|Bearer tk_secret|details") {
			t.Fatalf("%s", s)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("nothing sent")
	}
	select {
	case s := <-got:
		t.Fatalf("a switched-off alert was sent: %s", s)
	case <-time.After(300 * time.Millisecond):
	}
	// The test button reports success, and a wrong channel is refused.
	if rc, _, raw = c.call("POST", "/api/alerts/test", map[string]any{"channel": "ntfy"}); rc != 200 {
		t.Fatalf("%d %s", rc, raw)
	}
	<-got
	if rc, _, _ = c.call("POST", "/api/alerts/test", map[string]any{"channel": "email"}); rc == 200 {
		t.Fatal("an email test with no mail server must fail")
	}
	// Quiet hours hold the message, and it is not sent while held.
	c.call("PUT", "/api/alerts", map[string]any{
		"ntfy":  map[string]any{"enabled": true, "server": srv.URL, "topic": "gate"},
		"email": map[string]any{"security": "starttls", "port": 587},
		"on":    map[string]bool{"new_device": true},
		"quiet": map[string]any{"enabled": true, "from": "00:00", "to": "23:59"},
	})
	a.alert("new_device", "held one", "")
	select {
	case s := <-got:
		t.Fatalf("sent during quiet hours: %s", s)
	case <-time.After(300 * time.Millisecond):
	}
	if v := a.alertView(); v.Held != 1 {
		t.Fatalf("held: %d", v.Held)
	}
}

package app

import (
	"strings"
	"testing"
)

func TestWifiPayloadEscapes(t *testing.T) {
	got := wifiPayload(`Cafe;"1"`, `pa:ss,w\d`, true)
	want := `WIFI:T:WPA;S:Cafe\;\"1\";P:pa\:ss\,w\\d;H:true;;`
	if got != want {
		t.Fatalf("%s\nwant %s", got, want)
	}
	if wifiPayload("Open", "", false) != "WIFI:T:nopass;S:Open;;" {
		t.Fatal(wifiPayload("Open", "", false))
	}
}

func TestWifiCardSavedSealedAndPrinted(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	a := c.app
	if a == nil {
		t.Skip("no app")
	}
	if rc, _, _ := c.call("PUT", "/api/wificards", map[string]any{"ssid": "Guest", "psk": "short"}); rc == 200 {
		t.Fatal("short password accepted")
	}
	if rc, _, raw := c.call("PUT", "/api/wificards", map[string]any{"ssid": "Guest <b>", "psk": "welcome-home-1"}); rc != 200 {
		t.Fatalf("%d %s", rc, raw)
	}
	var sealed string
	a.Config.QueryRow(`SELECT psk FROM wifi_cards`).Scan(&sealed)
	if sealed == "" || strings.Contains(sealed, "welcome") {
		t.Fatal("password not sealed")
	}
	rc, _, raw := c.call("GET", "/api/wificards", nil)
	if rc != 200 || strings.Contains(string(raw), "welcome") {
		t.Fatalf("list leaks: %s", raw)
	}
	rc, _, raw = c.call("GET", "/api/wificards/print?src=card:1", nil)
	if rc != 200 || !strings.Contains(string(raw), "welcome-home-1") || strings.Contains(string(raw), "<b>") || !strings.Contains(string(raw), "data:image/png") {
		t.Fatalf("%d %s", rc, raw)
	}
	if rc, _, _ = c.call("GET", "/api/wificards/qr?src=card:99", nil); rc == 200 {
		t.Fatal("unknown card")
	}
}

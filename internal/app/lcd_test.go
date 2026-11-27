package app

import (
	"strings"
	"testing"
)

func TestFrontPanelSettingsAndPages(t *testing.T) {
	c, fn, _ := siteBox(t, "192.168.1.1/24")
	a := c.app

	rc, m, raw := c.call("GET", "/api/lcd", nil)
	if rc != 200 {
		t.Fatalf("get: %d %s", rc, raw)
	}
	cfg := m["config"].(map[string]any)
	if cfg["enabled"] != false || cfg["rotate"].(float64) != 4 {
		t.Fatalf("defaults wrong: %v", cfg)
	}
	if rc, _, _ := c.call("POST", "/api/lcd/install", map[string]any{}); rc != 200 || !fn.lcd.Installed {
		t.Fatal("install did not start")
	}
	// A bad driver or a bad button action is refused with a reason.
	bad := map[string]any{"enabled": true, "driver": "no such; driver", "screens": []string{"name"}, "rotate": 4}
	if rc, _, _ := c.call("PUT", "/api/lcd/config", bad); rc == 200 {
		t.Fatal("a bad driver was accepted")
	}
	bad = map[string]any{"enabled": true, "driver": "hd44780", "screens": []string{"name"}, "rotate": 4, "buttons": map[string]string{"Up": "explode"}}
	if rc, _, _ := c.call("PUT", "/api/lcd/config", bad); rc == 200 {
		t.Fatal("a bad button action was accepted")
	}
	good := map[string]any{"enabled": true, "driver": "hd44780", "settings": map[string]string{"ConnectionType": "4bit", "Size": "20x2"},
		"screens": []string{"name", "networks", "dhcp", "system", "alerts", "internet"}, "rotate": 5, "buttons": map[string]string{"Enter": "hold"}}
	if rc, _, raw := c.call("PUT", "/api/lcd/config", good); rc != 200 {
		t.Fatalf("save: %d %s", rc, raw)
	}
	if fn.lcdPlan.Driver != "hd44780" || !fn.lcdPlan.Enabled {
		t.Fatalf("plan not applied: %+v", fn.lcdPlan)
	}
	if got := a.lcdConfig(); got.Rotate != 5 || got.Buttons["Enter"] != "hold" {
		t.Fatalf("not stored: %+v", got)
	}
	// Every screen builds lines that fit a 20 x 2 display after fitting.
	pages, scr := a.lcdPages(a.lcdConfig(), 20, 2)
	if len(pages) == 0 || len(pages) != len(scr) {
		t.Fatalf("pages: %v %v", pages, scr)
	}
	seen := map[string]bool{}
	for i, p := range pages {
		seen[scr[i]] = true
		if len(p) == 0 {
			t.Fatalf("empty page for %s", scr[i])
		}
	}
	for _, id := range []string{"name", "dhcp", "system", "alerts"} {
		if !seen[id] {
			t.Fatalf("screen %s missing", id)
		}
	}
	if !strings.Contains(pages[0][0], "NopeWarden") {
		t.Fatalf("first page: %v", pages[0])
	}
	// Detect passes the report through.
	if rc, _, _ := c.call("POST", "/api/lcd/detect", map[string]any{}); rc != 200 {
		t.Fatal("detect failed")
	}
	// Remove switches it off.
	if rc, _, _ := c.call("POST", "/api/lcd/remove", map[string]any{}); rc != 200 || a.lcdConfig().Enabled {
		t.Fatal("remove did not switch it off")
	}
	// No test is running, so asking for a pattern says so.
	if rc, _, _ := c.call("POST", "/api/lcd/test/pattern", map[string]any{"kind": "numbers"}); rc == 200 {
		t.Fatal("a pattern was drawn with no test running")
	}
	if rc, m, _ := c.call("GET", "/api/lcd/test/keys?since=0", nil); rc != 200 || m["running"] != false {
		t.Fatal("keys with no test")
	}
}

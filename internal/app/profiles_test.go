package app

import (
	"strings"
	"testing"
	"time"
)

func TestProfilesLevelsPauseAndHours(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	a := c.app
	if a == nil {
		t.Skip("box has no app")
	}
	rc, m, raw := c.call("GET", "/api/profiles", nil)
	if rc != 200 || m["presets"] == nil {
		t.Fatalf("%d %s", rc, raw)
	}
	// A bad window is refused with a reason.
	rc, _, raw = c.call("PUT", "/api/profiles", map[string]any{"name": "Kids2", "level": "basic", "hours": map[string]any{"on": true, "days": []int{}, "from": "21:00", "to": "07:00"}})
	if rc == 200 || !strings.Contains(string(raw), "off-hours") {
		t.Fatalf("%d %s", rc, raw)
	}
	// A name with spaces is made safe for firewall rules.
	rc, m, raw = c.call("PUT", "/api/profiles", map[string]any{"name": "Smart home", "level": "custom", "categories": []string{"malware"}, "safe_search": true,
		"hours": map[string]any{"on": true, "days": []int{0, 1, 2, 3, 4, 5, 6}, "from": "00:00", "to": "23:59"}})
	if rc != 200 {
		t.Fatalf("%d %s", rc, raw)
	}
	id := int64(m["id"].(float64))
	ps, err := a.profileList()
	if err != nil {
		t.Fatal(err)
	}
	var got *profileOut
	for i := range ps {
		if ps[i].ID == id {
			got = &ps[i]
		}
	}
	if got == nil || got.Name != "Smart-home" || got.Level != "custom" || len(got.Categories) != 1 || got.Categories[0] != "malware" || !got.SafeSearch || !got.Hours.On {
		t.Fatalf("%+v", got)
	}
	// Switching to Off keeps nothing of the categories.
	rc, _, raw = c.call("PUT", "/api/profiles", map[string]any{"id": id, "name": "Smart-home", "level": "off"})
	if rc != 200 {
		t.Fatalf("%d %s", rc, raw)
	}
	ps, _ = a.profileList()
	for _, p := range ps {
		if p.ID == id && (p.Level != "off" || len(p.Categories) != 0) {
			t.Fatalf("%+v", p)
		}
	}
	// Pausing a device, then resuming.
	a.Config.Exec(`INSERT INTO devices(mac,name) VALUES('aa:bb:cc:dd:ee:09','tv')`)
	var did int64
	a.Config.QueryRow(`SELECT id FROM devices WHERE mac='aa:bb:cc:dd:ee:09'`).Scan(&did)
	if rc, _, raw = c.call("POST", "/api/devices/"+itoa(did)+"/pause", map[string]any{"minutes": 30}); rc != 200 {
		t.Fatalf("%d %s", rc, raw)
	}
	if !strings.Contains(a.pausedSig(), "aa:bb:cc:dd:ee:09") {
		t.Fatal("not paused")
	}
	c.call("POST", "/api/devices/"+itoa(did)+"/pause", map[string]any{"minutes": 0})
	if strings.Contains(a.pausedSig(), "aa:bb:cc:dd:ee:09") {
		t.Fatal("still paused")
	}
	if rc, _, _ = c.call("POST", "/api/devices/9999/pause", map[string]any{"minutes": 5}); rc == 200 {
		t.Fatal("unknown device accepted")
	}
}

func TestPauseUntil(t *testing.T) {
	now := time.Unix(1000, 0)
	if pauseUntil(0, now) != 0 || pauseUntil(5, now) != 1300 || pauseUntil(-1, now) <= now.Unix()+86400*365 {
		t.Fatal("pause time")
	}
}

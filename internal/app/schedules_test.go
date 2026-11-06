package app

import (
	"encoding/json"
	"testing"
)

func TestSchedulesCanBeListedChangedAndRefused(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	_, _, raw := c.call("GET", "/api/schedules", nil)
	var list []struct{ ID, Spec, Plain string }
	json.Unmarshal(raw, &list)
	if len(list) != 4 {
		t.Fatalf("want 4 jobs: %s", raw)
	}
	if rc, _, out := c.call("PUT", "/api/schedules/threat", map[string]any{"spec": "every:6"}); rc != 200 {
		t.Fatalf("%d %s", rc, out)
	}
	if rc, _, _ := c.call("PUT", "/api/schedules/threat", map[string]any{"spec": "every:0"}); rc == 200 {
		t.Fatal("0 hours must be refused")
	}
	if rc, _, _ := c.call("PUT", "/api/schedules/updates", map[string]any{"spec": "off"}); rc == 200 {
		t.Fatal("updates check cannot be switched off here")
	}
	if rc, _, _ := c.call("PUT", "/api/schedules/nope", map[string]any{"spec": "every:6"}); rc != 404 {
		t.Fatal("unknown job is 404")
	}
	_, _, raw = c.call("GET", "/api/schedules", nil)
	json.Unmarshal(raw, &list)
	for _, j := range list {
		if j.ID == "threat" && j.Plain != "Every 6 hours" {
			t.Fatal(j)
		}
	}
}

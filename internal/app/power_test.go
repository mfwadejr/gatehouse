package app

import "testing"

func TestPowerRoute(t *testing.T) {
	c, fn, _ := updBox(t, "http://127.0.0.1:1/none", true)
	for _, a := range []string{"reboot", "poweroff", "restart-gatehouse"} {
		if rc, _, raw := c.call("POST", "/api/system/power", map[string]any{"action": a}); rc != 200 {
			t.Fatalf("%s: %d %s", a, rc, raw)
		}
	}
	if rc, _, _ := c.call("POST", "/api/system/power", map[string]any{"action": "format-disk"}); rc == 200 {
		t.Fatal("a made-up action was accepted")
	}
	fn.mu.Lock()
	got := append([]string(nil), fn.powerCalls...)
	fn.mu.Unlock()
	if len(got) != 3 || got[0] != "reboot" || got[1] != "poweroff" || got[2] != "restart-gatehouse" {
		t.Fatalf("%v", got)
	}
}

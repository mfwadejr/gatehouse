package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestThreatInstallIsAskedForAndWatchNeedsIt(t *testing.T) {
	c, fn, _ := siteBoxWith(t, "192.168.1.1/24", nil)
	// Not installed: the page says so, and Watch is refused.
	rc, m, raw := c.call("GET", "/api/threat", nil)
	if rc != 200 || m["mode"] != "off" {
		t.Fatalf("default: %d %s", rc, raw)
	}
	if rc, _, _ := c.call("PUT", "/api/threat/settings", map[string]any{"mode": "watch"}); rc == 200 {
		t.Fatal("Watch was accepted without the program installed")
	}
	// The preview names the packages and the size, and warns about a small machine.
	fn.mu.Lock()
	fn.threatInfo.DiskFreeMB, fn.threatInfo.MemoryMB, fn.threatInfo.CPUs = 100, 1024, 1
	fn.mu.Unlock()
	rc, m, raw = c.call("POST", "/api/threat/install/preview", nil)
	if rc != 200 {
		t.Fatalf("preview: %d %s", rc, raw)
	}
	if w, _ := m["warnings"].([]any); len(w) != 3 {
		t.Fatalf("expected 3 warnings: %s", raw)
	}
	if rc, _, raw := c.call("POST", "/api/threat/install", nil); rc != 200 {
		t.Fatalf("install: %d %s", rc, raw)
	}
	fn.mu.Lock()
	fn.threatInfo.Installed, fn.threatInfo.State = true, "idle"
	fn.mu.Unlock()
	if rc, _, raw := c.call("PUT", "/api/threat/settings", map[string]any{"mode": "watch", "preset": "strict", "cpu_percent": 80}); rc != 200 {
		t.Fatalf("watch: %d %s", rc, raw)
	}
	fn.mu.Lock()
	last := fn.threatPlans[len(fn.threatPlans)-1]
	fn.mu.Unlock()
	if !last.Enabled || last.Preset != "strict" || last.CPUPercent != 80 {
		t.Fatalf("the engine was handed %+v", last)
	}
	// Bad values are refused.
	for _, b := range []map[string]any{{"preset": "wild"}, {"cpu_percent": 5}, {"memory_mb": 10}, {"mode": "protect"}, {"eve_path": "../x"}} {
		if rc, _, _ := c.call("PUT", "/api/threat/settings", b); rc == 200 {
			t.Fatalf("accepted %v", b)
		}
	}
	// A failed start leaves it Off, so the page never claims it is on.
	c.call("PUT", "/api/threat/settings", map[string]any{"mode": "off"})
	fn.mu.Lock()
	fn.threatErr = os.ErrPermission
	fn.mu.Unlock()
	if rc, _, _ := c.call("PUT", "/api/threat/settings", map[string]any{"mode": "watch"}); rc == 200 {
		t.Fatal("a failed start was reported as fine")
	}
	_, m, _ = c.call("GET", "/api/threat", nil)
	if m["mode"] != "off" {
		t.Fatalf("mode after failed start: %v", m["mode"])
	}
	fn.mu.Lock()
	fn.threatErr = nil
	fn.mu.Unlock()
	// Rules and removal reach the helper.
	c.call("POST", "/api/threat/rules/update", nil)
	c.call("POST", "/api/threat/rules/rollback", nil)
	c.call("POST", "/api/threat/remove", nil)
	fn.mu.Lock()
	got := strings.Join(fn.threatCalls, ",")
	fn.mu.Unlock()
	if got != "install,update,rollback,remove" {
		t.Fatalf("calls: %s", got)
	}
}

func TestThreatWatchdogPausesAHungryEngineAndReportsAStoppedOne(t *testing.T) {
	a := newScopeApp(t)
	fn := a.Net.(*fakeNet)
	a.Config.SetSetting(threatModeSetting, "watch")
	a.Config.SetSetting(threatCPUSetting, "50")
	now := time.Now()
	info := netdThreat{Installed: true, Running: true}
	// 100% of a core for 11 minutes, against a 50% limit.
	for i := 0; i <= 11; i++ {
		info.CPUNSec = uint64(i) * 60 * 1e9
		a.threatWatchTick(info, now.Add(time.Duration(i)*time.Minute))
	}
	if a.threatMode() != "off" || a.Config.Setting(threatWhySetting, "") == "" {
		t.Fatal("a hungry engine was not switched off with a reason")
	}
	fn.mu.Lock()
	n := len(fn.threatPlans)
	enabled := fn.threatPlans[n-1].Enabled
	fn.mu.Unlock()
	if n == 0 || enabled {
		t.Fatal("the engine was not told to stop")
	}
	var ev int
	a.Logs.QueryRow(`SELECT COUNT(*) FROM events WHERE title LIKE 'Threat watching was switched off%'`).Scan(&ev)
	if ev != 1 {
		t.Fatalf("log events: %d", ev)
	}
	// A quiet engine is left alone.
	a.Config.SetSetting(threatModeSetting, "watch")
	for i := 0; i <= 30; i++ {
		info.CPUNSec = uint64(i) * 60 * 1e9 / 10
		a.threatWatchTick(info, now.Add(time.Hour+time.Duration(i)*time.Minute))
	}
	if a.threatMode() != "watch" {
		t.Fatal("a quiet engine was switched off")
	}
	// A stopped engine is reported once, not every minute.
	info.Running = false
	for i := 0; i < 8; i++ {
		a.threatWatchTick(info, now.Add(3*time.Hour+time.Duration(i)*time.Minute))
	}
	a.Logs.QueryRow(`SELECT COUNT(*) FROM events WHERE title='Threat watching is not running'`).Scan(&ev)
	if ev != 1 {
		t.Fatalf("not-running events: %d", ev)
	}
}

func TestThreatRuleSearchSwitchAndTuningRoundTrip(t *testing.T) {
	c, fn, _ := siteBoxWith(t, "192.168.1.1/24", nil)
	rules := filepath.Join(t.TempDir(), "suricata.rules")
	os.WriteFile(rules, []byte("alert tcp any any -> any any (msg:\"ET MALWARE Evil Beacon\"; sid:2001; rev:1;)\n#alert tcp any any -> any any (msg:\"ET INFO Chatty\"; sid:2002; rev:1;)\nalert udp any any -> any 53 (msg:\"ET DNS Odd\"; sid:2003; rev:1;)\n"), 0o644)
	old := threatRulesPath
	threatRulesPath = rules
	t.Cleanup(func() { threatRulesPath = old })
	rc, m, raw := c.call("GET", "/api/threat/rules?q=beacon", nil)
	if rc != 200 || len(m["rules"].([]any)) != 1 {
		t.Fatalf("search: %d %s", rc, raw)
	}
	if rc, _, raw := c.call("POST", "/api/threat/rules/2001", map[string]any{"on": false}); rc != 200 {
		t.Fatalf("switch off: %d %s", rc, raw)
	}
	_, m, _ = c.call("GET", "/api/threat/rules?q=2001", nil)
	if r := m["rules"].([]any); len(r) != 1 || r[0].(map[string]any)["on"] != false {
		t.Fatalf("rule not shown as off: %v", m)
	}
	// Export, change, import: the tuning comes back.
	rc, _, exp := c.call("GET", "/api/threat/export", nil)
	if rc != 200 || !strings.Contains(string(exp), "2001") {
		t.Fatalf("export: %d %s", rc, exp)
	}
	c.call("POST", "/api/threat/rules/2001", map[string]any{"on": true})
	var doc map[string]any
	if err := json.Unmarshal(exp, &doc); err != nil {
		t.Fatal(err)
	}
	if rc, _, raw := c.call("POST", "/api/threat/import", doc); rc != 200 {
		t.Fatalf("import: %d %s", rc, raw)
	}
	_, m, _ = c.call("GET", "/api/threat/rules?q=2001", nil)
	if r := m["rules"].([]any); r[0].(map[string]any)["on"] != false {
		t.Fatal("imported tuning not applied")
	}
	if rc, _, _ := c.call("POST", "/api/threat/import", map[string]any{"hello": 1}); rc == 200 {
		t.Fatal("accepted a file that is not a tuning file")
	}
	_ = fn
	if rc, m, _ := c.call("GET", "/api/threat/groups", nil); rc != 200 || len(m["groups"].([]any)) == 0 {
		t.Fatal("no rule groups listed")
	}
}

package app

import (
	"strings"
	"testing"
	"time"

	"nopewarden/internal/netd"
	"nopewarden/internal/threatcfg"
)

func inlineBox(t *testing.T) (*client, *fakeNet) {
	c, fn, _ := siteBoxWith(t, "192.168.1.1/24", nil)
	fn.mu.Lock()
	fn.threatInfo.Installed, fn.threatInfo.State, fn.threatInfo.CPUs, fn.threatInfo.MemoryMB = true, "idle", 4, 8192
	fn.mu.Unlock()
	if rc, _, raw := c.call("PUT", "/api/threat/settings", map[string]any{"mode": "inline"}); rc != 200 {
		t.Fatalf("inline: %d %s", rc, raw)
	}
	return c, fn
}

func has(l []string, g string) bool {
	for _, x := range l {
		if x == g {
			return true
		}
	}
	return false
}

func lastPlan(fn *fakeNet) threatcfg.Plan {
	fn.mu.Lock()
	defer fn.mu.Unlock()
	return fn.threatPlans[len(fn.threatPlans)-1]
}

func TestInlineStartsWithOnlyTheClearestGroupsOnBlock(t *testing.T) {
	c, fn := inlineBox(t)
	p := lastPlan(fn)
	if !p.Enabled || !p.Inline {
		t.Fatalf("%+v", p)
	}
	got := strings.Join(p.BlockGroups, ",")
	for _, want := range []string{"malware", "exploit", "exploit_kit"} {
		if !strings.Contains(got, want) {
			t.Fatalf("%s should block by default: %s", want, got)
		}
	}
	for _, not := range []string{"scan", "policy", "p2p", "games", "chat", "dns", "user_agents", "info"} {
		if strings.Contains(","+got+",", ","+not+",") {
			t.Fatalf("%s must start on Alert, not Block", not)
		}
	}
	rc, m, _ := c.call("GET", "/api/threat/inline", nil)
	if rc != 200 || m["on"] != true {
		t.Fatal(m)
	}
	rc, m, _ = c.call("GET", "/api/threat", nil)
	if m["inline"] != true || m["block"] == true {
		t.Fatalf("inline replaces address blocking: %v", m)
	}
}

func TestInlineCategoriesAreOffAlertOrBlock(t *testing.T) {
	c, fn := inlineBox(t)
	set := func(g, lv string) {
		if rc, _, raw := c.call("PUT", "/api/threat/inline/category", map[string]any{"group": g, "level": lv}); rc != 200 {
			t.Fatalf("%s %s: %d %s", g, lv, rc, raw)
		}
	}
	set("scan", "block")
	if !has(lastPlan(fn).BlockGroups, "scan") {
		t.Fatal("scan should block")
	}
	set("malware", "alert")
	p := lastPlan(fn)
	if has(p.BlockGroups, "malware") {
		t.Fatal("malware was set to Alert")
	}
	set("exploit", "off")
	p = lastPlan(fn)
	if has(p.BlockGroups, "exploit") {
		t.Fatalf("a group that is off cannot block: %v", p.BlockGroups)
	}
	for _, g := range threatcfg.EffectiveOff(p) {
		if g == "exploit" {
			return
		}
	}
	t.Fatal("exploit should be off")
}

func TestInlineStopBlockingNeverCheckAndUndo(t *testing.T) {
	c, fn := inlineBox(t)
	if rc, _, _ := c.call("POST", "/api/threat/inline/stop-blocking", map[string]any{"sid": 2019401}); rc != 200 {
		t.Fatal("stop blocking")
	}
	if p := lastPlan(fn); len(p.AlertOnlySIDs) != 1 || p.AlertOnlySIDs[0] != 2019401 {
		t.Fatalf("%v", p.AlertOnlySIDs)
	}
	if rc, _, _ := c.call("POST", "/api/threat/inline/never", map[string]any{"addr": "192.168.1.50"}); rc != 200 {
		t.Fatal("never check")
	}
	if rc, _, _ := c.call("POST", "/api/threat/inline/never", map[string]any{"addr": "banana"}); rc == 200 {
		t.Fatal("a bad address is refused")
	}
	if p := lastPlan(fn); len(p.NeverCheck) != 1 || p.NeverCheck[0] != "192.168.1.50" {
		t.Fatalf("%v", p.NeverCheck)
	}
	_, m, _ := c.call("GET", "/api/threat/inline", nil)
	if len(m["alert_only"].([]any)) != 1 || len(m["never"].([]any)) != 1 {
		t.Fatalf("the Changes you made list: %v", m)
	}
	c.call("DELETE", "/api/threat/inline/stop-blocking/2019401", nil)
	c.call("DELETE", "/api/threat/inline/never?addr=192.168.1.50", nil)
	if p := lastPlan(fn); len(p.AlertOnlySIDs) != 0 || len(p.NeverCheck) != 0 {
		t.Fatalf("undo: %+v", p)
	}
}

func TestInlineBackToWatchOnly(t *testing.T) {
	c, fn := inlineBox(t)
	if rc, _, _ := c.call("PUT", "/api/threat/settings", map[string]any{"mode": "watch"}); rc != 200 {
		t.Fatal("back to watch")
	}
	if p := lastPlan(fn); p.Inline || !p.Enabled {
		t.Fatalf("watching stays on, queueing stops: %+v", p)
	}
	if c.app.threatInline() {
		t.Fatal("inline stayed on")
	}
}

func TestInlineKeepingUpAndFallingBehind(t *testing.T) {
	c, _ := inlineBox(t)
	a := c.app
	now := time.Now()
	run := func(drops int64, listening bool, at time.Time) {
		info := netd.ThreatInfo{Installed: true, Running: listening}
		info.Queue = netd.QueueStats{Listening: listening, QueueDropped: drops}
		a.inlineTick(info, at)
	}
	run(0, true, now)
	if s, _ := a.inlineState(now); s != "keeping_up" {
		t.Fatal(s)
	}
	run(5, true, now.Add(time.Minute))
	if s, txt := a.inlineState(now.Add(time.Minute)); s != "falling_behind" || !strings.Contains(txt, "Falling behind") {
		t.Fatal(s, txt)
	}
	// It recovers once the counter stops growing for ten minutes.
	run(5, true, now.Add(12*time.Minute))
	if s, _ := a.inlineState(now.Add(12 * time.Minute)); s != "keeping_up" {
		t.Fatal(s)
	}
	run(5, false, now.Add(13*time.Minute))
	if s, txt := a.inlineState(now.Add(13 * time.Minute)); s != "stopped" || !strings.Contains(txt, "keeps working") {
		t.Fatal(s, txt)
	}
}

func TestInlineSizeWarnings(t *testing.T) {
	c, _ := inlineBox(t)
	w := c.app.inlineSize(netd.ThreatInfo{CPUs: 1, MemoryMB: 2048, Virt: "kvm"})
	if len(w) != 3 {
		t.Fatalf("%v", w)
	}
	if len(c.app.inlineSize(netd.ThreatInfo{CPUs: 4, MemoryMB: 8192})) != 0 {
		t.Fatal("a big machine gets no warning")
	}
}

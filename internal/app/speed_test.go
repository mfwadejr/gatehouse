package app

import (
	"errors"
	"testing"
	"time"
)

func newSpeedApp(t *testing.T) (*App, *fakeNet) {
	t.Helper()
	fn := &fakeNet{}
	a, err := New(Config{DataDir: t.TempDir(), WebAddr: freeAddr(t), DNSAddrs: []string{freeAddr(t)}, Net: fn, FW: &fakeFW{}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	if _, err := a.Config.Exec(`INSERT INTO wan_links(name,iface,type,enabled,priority) VALUES('Fibre','eth0','dhcp',1,1),('LTE','usb0','dhcp',1,2)`); err != nil {
		t.Fatal(err)
	}
	return a, fn
}

func TestSpeedTestGoesOutThroughTheConnectionAndIsSaved(t *testing.T) {
	a, fn := newSpeedApp(t)
	row, err := a.runSpeed("Fibre", "manual", false)
	if err != nil || row.DownBps != 412e6 || row.Grade != "A" || row.ID == 0 {
		t.Fatalf("%+v %v", row, err)
	}
	if len(fn.speedCalls) != 1 || fn.speedCalls[0].Dev != "eth0" || fn.speedCalls[0].Quick {
		t.Fatalf("call: %+v", fn.speedCalls)
	}
	h := a.speedHistory("Fibre", 0)
	if len(h) != 1 || h[0].Source != "manual" || h[0].Link != "Fibre" {
		t.Fatalf("history: %+v", h)
	}
	if got := a.speedHistory("LTE", 0); len(got) != 0 {
		t.Fatalf("other link: %+v", got)
	}
}

func TestMeteredConnectionGetsTheSmallTestUnlessAskedOtherwise(t *testing.T) {
	a, fn := newSpeedApp(t)
	cfg := speedConfig{Links: map[string]speedLink{}}
	yes := true
	cfg.Links["LTE"] = speedLink{Metered: &yes}
	a.setSpeedConfigForTest(cfg)
	if _, err := a.runSpeed("LTE", "manual", false); err != nil {
		t.Fatal(err)
	}
	if _, err := a.runSpeed("LTE", "manual", true); err != nil {
		t.Fatal(err)
	}
	if !fn.speedCalls[0].Quick || fn.speedCalls[1].Quick {
		t.Fatalf("quick flags: %+v", fn.speedCalls)
	}
}

func TestFailedSpeedTestIsKeptWithItsReason(t *testing.T) {
	a, fn := newSpeedApp(t)
	fn.speedErr = errors.New("this connection has no route to the internet right now")
	if _, err := a.runSpeed("Fibre", "manual", false); err == nil {
		t.Fatal("must fail")
	}
	h := a.speedHistory("", 0)
	if len(h) != 1 || h[0].Error == "" || h[0].DownBps != 0 {
		t.Fatalf("%+v", h)
	}
}

func TestSpeedTestRefusesUnknownConnectionAndRunsOneAtATime(t *testing.T) {
	a, _ := newSpeedApp(t)
	if _, err := a.runSpeed("Nope", "manual", false); err == nil {
		t.Fatal("unknown link")
	}
	a.speed.mu.Lock()
	a.speed.running = true
	a.speed.mu.Unlock()
	if _, err := a.runSpeed("Fibre", "manual", false); !errors.Is(err, errSpeedBusy) {
		t.Fatalf("want busy, got %v", err)
	}
}

func TestOldSpeedTestsAreRemovedAfterAYear(t *testing.T) {
	a, _ := newSpeedApp(t)
	old := time.Now().Add(-366 * 24 * time.Hour).Unix()
	a.Logs.Exec(`INSERT INTO speedtests(ts,link) VALUES(?,?),(?,?)`, old, "Fibre", time.Now().Unix(), "Fibre")
	a.pruneSpeed()
	if h := a.speedHistory("", 0); len(h) != 1 {
		t.Fatalf("kept %d", len(h))
	}
}

func TestSpeedRoutesSaveSettingsAndStartATest(t *testing.T) {
	c, fn, _ := siteBoxWith(t, "192.168.1.1/24", nil)
	var wan string
	if err := c.app.Config.QueryRow(`SELECT name FROM wan_links WHERE enabled=1 ORDER BY priority LIMIT 1`).Scan(&wan); err != nil {
		t.Fatal(err)
	}
	// The server list has its own calls; a stray server in the schedule save is ignored, not an error.
	if rc, _, raw := c.call("PUT", "/api/speedtest", map[string]any{"server": "http://insecure", "hour": 3}); rc != 200 {
		t.Fatalf("saving with a stray server field failed: %s", raw)
	}
	if got := c.app.speedConfig().Server; got == "http://insecure" {
		t.Fatal("the stray server field changed the default server")
	}
	if rc, _, raw := c.call("PUT", "/api/speedtest", map[string]any{"hour": 30}); rc == 200 {
		t.Fatalf("bad hour accepted: %s", raw)
	}
	if rc, _, raw := c.call("PUT", "/api/speedtest", map[string]any{"schedule": true, "hour": 4, "links": map[string]any{wan: map[string]any{"down_mbps": 500, "up_mbps": 50, "metered": false}}}); rc != 200 {
		t.Fatalf("save: %d %s", rc, raw)
	}
	rc, body, _ := c.call("GET", "/api/speedtest", nil)
	links, _ := body["links"].([]any)
	if rc != 200 || body["schedule"] != true || body["hour"] != float64(4) || len(links) != 1 {
		t.Fatalf("get: %d %v", rc, body)
	}
	if rc, _, raw := c.call("POST", "/api/speedtest/run", map[string]any{"link": "Missing"}); rc == 202 {
		t.Fatalf("unknown link started: %s", raw)
	}
	if rc, _, raw := c.call("POST", "/api/speedtest/run", map[string]any{"link": wan}); rc != 202 {
		t.Fatalf("run: %d %s", rc, raw)
	}
	waitFor(t, "the test to finish and be saved", func() bool { return len(c.app.speedHistory(wan, 0)) == 1 })
	rc, _, raw := c.call("GET", "/api/speedtest/history?link="+wan+"&days=7", nil)
	if rc != 200 || len(raw) < 20 {
		t.Fatalf("history: %d %s", rc, raw)
	}
	fn.mu.Lock()
	n := len(fn.speedCalls)
	fn.mu.Unlock()
	if n != 1 {
		t.Fatalf("calls %d", n)
	}
}

func TestSpeedServerListRules(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	if rc, _, _ := c.call("POST", "/api/speedtest/servers", map[string]any{"name": "x", "url": "http://insecure.example"}); rc != 400 {
		t.Fatalf("http must be refused, got %d", rc)
	}
	if rc, _, _ := c.call("PUT", "/api/speedtest/default", map[string]any{"url": "https://not-in-list.example"}); rc != 400 {
		t.Fatalf("a default outside the list must be refused, got %d", rc)
	}
	if rc, _, _ := c.call("PUT", "/api/speedtest/default", map[string]any{"url": ""}); rc != 200 {
		t.Fatalf("resetting to Cloudflare failed: %d", rc)
	}
	_, m, raw := c.call("GET", "/api/speedtest", nil)
	if l, _ := m["servers"].([]any); len(l) != 1 {
		t.Fatalf("Cloudflare must always be listed: %s", raw)
	}
}

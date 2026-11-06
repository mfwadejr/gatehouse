package app

import (
	"fmt"
	"os"
	"testing"
	"time"

	"nopewarden/internal/wanmon"
	"nopewarden/web"
)

// TestDemoConsole is a picture-taking tool, not a test: with NW_DEMO set it starts the real console on
// 127.0.0.1:8099 with made-up data and waits, so screenshots of the website and the help can be taken
// from the real screens. NW_SCENE picks the state: main, review, setup.
func TestDemoConsole(t *testing.T) {
	if os.Getenv("NW_DEMO") == "" {
		t.Skip("picture-taking tool; set NW_DEMO=1")
	}
	scene := os.Getenv("NW_SCENE")
	if scene == "" {
		scene = "main"
	}
	arpTable = func() map[string]arpEntry {
		out := map[string]arpEntry{}
		for i := 1; i <= 7; i++ {
			out[fmt.Sprintf("192.168.1.%d", 109+i)] = arpEntry{fmt.Sprintf("aa:bb:cc:00:00:%02x", i), "eth1"}
		}
		return out
	}
	defer func() { arpTable = arpTableReal }()
	c, fn, _ := siteBoxWith(t, "192.168.1.1/24", func(cfg *Config) {
		cfg.Static = web.Handler()
	})
	fn.mu.Lock()
	fn.wan = map[string]wanmon.State{
		"Fibre": {Up: true, Gateway: "192.0.2.1", RTT: 6200 * time.Microsecond, Since: time.Now().Add(-72 * time.Hour), Active: true, Checked: true, Health: "working", GwRTT: 2 * time.Millisecond, Last: time.Now()},
		"Cable": {Up: true, Gateway: "198.51.100.1", RTT: 14 * time.Millisecond, Since: time.Now().Add(-72 * time.Hour), Checked: true, Health: "working", GwRTT: 4 * time.Millisecond, Last: time.Now()},
	}
	fn.mu.Unlock()
	seedDemo(t, c, scene)
	fmt.Println("DEMO READY", scene, c.base)
	time.Sleep(time.Duration(envInt("NW_SECONDS", 600)) * time.Second)
}

func envInt(k string, def int) int {
	var n int
	if _, err := fmt.Sscan(os.Getenv(k), &n); err != nil || n == 0 {
		return def
	}
	return n
}

func seedDemo(t *testing.T, c *client, scene string) {
	must := func(method, path string, body any) map[string]any {
		rc, m, raw := c.call(method, path, body)
		if rc != 200 {
			t.Fatalf("%s %s: %d %s", method, path, rc, raw)
		}
		return m
	}
	must("PUT", "/api/net/wans", map[string]any{"name": "Cable", "iface": "eth2", "type": "dhcp", "enabled": true})
	must("PUT", "/api/dhcp/scopes", map[string]any{"name": "lan", "iface": "eth1", "mode": "serve", "subnet": "192.168.1.0/24",
		"range_start": "192.168.1.100", "range_end": "192.168.1.200", "router": "192.168.1.1", "lease_seconds": 86400, "enabled": true})
	var scope int64
	c.app.Config.QueryRow(`SELECT id FROM dhcp_scopes WHERE name='lan'`).Scan(&scope)
	names := []string{"Living room TV", "Kitchen PC", "Marvin's laptop", "Office printer", "Front door camera", "Kids tablet", "Game console"}
	icons := []string{"tv", "computer", "laptop", "printer", "camera", "tablet", "game"}
	now := time.Now().Unix()
	for i, n := range names {
		mac := fmt.Sprintf("aa:bb:cc:00:00:%02x", i+1)
		must("POST", "/api/devices", map[string]any{"mac": mac, "name": n})
		c.app.Config.Exec(`INSERT INTO dhcp_leases(scope_id,ip,mac,hostname,client_id,start,expires,state) VALUES(?,?,?,?,?,?,?,'active')`,
			scope, fmt.Sprintf("192.168.1.%d", 110+i), mac, "", "", now, now+86400)
		c.app.Config.Exec(`UPDATE devices SET icon=? WHERE mac=?`, icons[i], mac)
	}
	c.app.DHCP.Reload()
	if scene == "setup" {
		return
	}
	must("PUT", "/api/routes", map[string]any{"name": "Usenet through the backup line", "enabled": true, "target_kind": "links", "target": "Cable,Fibre", "who": "net:Home", "where": "dom:news.example.com", "service": "tcp/563"})
	must("PUT", "/api/routes", map[string]any{"name": "Time stays on Fibre", "enabled": true, "target_kind": "wan", "target": "Fibre", "who": "net:Home", "service": "ntp"})
	must("PUT", "/api/blocks", map[string]any{"kind": "app", "value": "tiktok", "scope": "everyone", "enabled": true})
	apply := func() {
		for _, k := range []string{"net", "fw", "dns"} {
			c.call("POST", "/api/"+k+"/apply", map[string]any{})
			_, m, _ := c.call("GET", "/api/applystate", nil)
			if p, _ := m[k].(map[string]any); p != nil {
				if pend, _ := p["pending"].(map[string]any); pend != nil {
					c.call("POST", "/api/"+k+"/confirm", map[string]any{"token": pend["token"]})
				}
			}
		}
	}
	if scene == "review" {
		apply()
		for _, r := range []map[string]any{
			{"name": "Kids offline at night", "from": "Home", "to": "Internet", "action": "drop", "enabled": true},
			{"name": "Guests cannot reach the printer", "from": "Home", "to": "Home", "dst": "192.168.1.113", "action": "drop", "enabled": true},
			{"name": "Open Plex to the living room TV", "from": "Internet", "to": "Home", "dst": "192.168.1.110", "service": "tcp/32400", "action": "accept", "enabled": true},
		} {
			c.call("PUT", "/api/fw/rules", r)
		}
		return
	}
	must("PUT", "/api/fw/rules", map[string]any{"name": "Cameras stay home", "from": "Home", "to": "Internet", "dst": "203.0.113.9", "action": "drop", "enabled": true})
	apply()
}

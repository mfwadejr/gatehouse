package app

import (
	"fmt"
	"os"
	"testing"
	"time"

	dnssrv "nopewarden/internal/dns"
	"nopewarden/internal/netd"
	"nopewarden/internal/vpn"
	"nopewarden/internal/wanmon"
	"nopewarden/web"
)

// TestDemoConsole is a picture-taking tool, not a test: with NW_DEMO set it starts the real console on
// 127.0.0.1:8099 with made-up data and waits, so screenshots of the website and the help can be taken
// from the real screens. NW_SCENE picks the state: main, review, setup, wizard (nothing set up, the first-run steps show), engine (main, with the threat
// engine installed and watching, so the screens that only exist then can be seen).
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
	if os.Getenv("NW_BALANCE") == "live" { // the monitor marks every link in a balancing group as carrying traffic
		x := fn.wan["Cable"]
		x.Active = true
		fn.wan["Cable"] = x
	}
	fn.mu.Unlock()
	if scene == "wizard" { // nothing set up yet, so the first-run steps show
		c.app.Config.Exec(`DELETE FROM zones`)
		c.app.Config.Exec(`DELETE FROM wan_links`)
	} else {
		seedDemo(t, c, scene)
	}
	if os.Getenv("NW_BALANCE") != "" { // both connections share the load, half each
		_, wm, _ := c.call("GET", "/api/net/wans", nil)
		var ms []map[string]any
		for _, x := range wm["wans"].([]any) {
			ms = append(ms, map[string]any{"wan_id": x.(map[string]any)["id"], "weight": 50, "enabled": true})
		}
		c.call("PUT", "/api/net/default-group", map[string]any{"label": "Default", "mode": "balance", "members": ms})
	}
	if scene == "engine" {
		fn.mu.Lock()
		fn.threatInfo = netd.ThreatInfo{Installed: true, Version: "7.0.8", Running: true, State: "idle", DiskFreeMB: 20000, MemoryMB: 8192, CPUs: 4,
			RulesUpdated: time.Now().Add(-6 * time.Hour).Unix(), RulesCount: 48213, CPUNSec: 4_000_000_000, MemoryBytes: 310 << 20}
		fn.mu.Unlock()
		c.call("PUT", "/api/threat/settings", map[string]any{"mode": "protect"})
		// Made-up blocks from several kinds of alert, so the Blocked right now list can be seen with many.
		{
			why := []string{"ET SCAN Nmap Scripting Engine User-Agent Detected", "ET SCAN Potential SSH Scan", "ET DROP Dshield Block Listed Source", "ET POLICY Possible Brute Force on RDP", "ET EXPLOIT Apache Log4j RCE Attempt"}
			var rows []threatBlockRow
			for i := 0; i < 64; i++ {
				w := why[(i*7+i/5)%len(why)]
				rows = append(rows, threatBlockRow{IP: fmt.Sprintf("203.0.113.%d", 10+i), Until: time.Now().Add(time.Duration(10+i) * time.Minute).Unix(), At: time.Now().Add(-time.Duration(2+i*3) * time.Minute).Unix(), SID: 2000000 + int64(i), Why: w, Device: "Living room TV"})
			}
			c.app.threatBlockSave(rows)
		}
		// Drives and certificates, so Diagnostics and the menu dots have something to say.
		fn.mu.Lock()
		fn.disk = netd.DiskReport{Tool: true, CanInstall: true, At: time.Now().Unix(), Drives: []netd.Drive{
			{Name: "nvme0n1", Model: "Samsung SSD 980 1TB", Kind: "NVMe SSD", Bytes: 1000204886016, Word: "Healthy", Reason: "The drive reports no problems.", Temp: 41, Wear: 3, Test: &netd.SelfTest{Kind: "Short", Result: "Passed"},
				Lines: []netd.DiskLine{{Label: "Drive's own verdict", Value: "Passed"}, {Label: "Temperature", Value: "41 °C"}, {Label: "Powered on", Value: "1.2 years (437 days)"}, {Label: "Wear", Value: "3% of its rated life used"}, {Label: "Spare area left", Value: "100%"}, {Label: "Media errors", Value: "0"}}},
			{Name: "sda", Model: "WDC WD10EZEX", Kind: "Hard disk", Bytes: 1000204886016, Word: "Warning", Level: "warn", Reason: "The drive has moved 8 sectors to its spare area. Keep an eye on it.", Temp: 38, Wear: -1, Counters: map[string]int64{"realloc": 8, "pending": 0}, Test: &netd.SelfTest{Kind: "Extended", Result: "Passed"},
				Lines: []netd.DiskLine{{Label: "Drive's own verdict", Value: "Passed"}, {Label: "Temperature", Value: "38 °C"}, {Label: "Sectors moved to spare", Value: "8"}, {Label: "Sectors waiting to be moved", Value: "0"}}, Missing: []string{"wear"}},
			{Name: "vda", Model: "Virtual disk", Kind: "Virtual disk", Bytes: 21474836480, Word: "Not reported", Reason: "This is a virtual disk. Its health belongs to the machine that provides it."},
		}}
		fn.ups = netd.UPSStatus{Tool: true, Answering: true, Maker: "American Power Conversion", Model: "Back-UPS ES 700G", Raw: "OL", Charge: 100, Runtime: 38 * 60, Load: 27, InputV: 231, BatteryV: 13.6}
		fn.mu.Unlock()
		c.app.Logs.Exec(`INSERT INTO disk_counters(ts,drive,key,value) VALUES(?,?,?,?)`, time.Now().Add(-8*24*time.Hour).Unix(), "sda", "realloc", 5)
		pc := c.app.powerConfig()
		pc.On = os.Getenv("NW_UPS_OFF") == ""
		c.app.savePowerConfig(pc)
		now := time.Now()
		ca, _ := vpn.NewCA(now.AddDate(-10, 0, 40))
		srv, _ := ca.ServerCert(now.AddDate(-10, 0, 40))
		gone, _ := ca.ServerCert(now.AddDate(-10, 0, -3))
		c.app.Config.Exec(`INSERT INTO vpn_ovpn(name,label,mode,ca_cert,srv_cert,created_at) VALUES('tun9','Office VPN','server',?,?,1)`, ca.CertPEM, srv.CertPEM)
		c.app.Config.Exec(`INSERT INTO vpn_ovpn_users(ovpn_id,name,serial,cert,created_at) SELECT id,'Sam''s laptop','9',?,1 FROM vpn_ovpn WHERE name='tun9'`, gone.CertPEM)
	}
	// NW_BACKUPS=25 makes that many backups, so lists that run to several pages can be seen.
	if n := envInt("NW_BACKUPS", 0); n > 0 {
		c.call("PUT", "/api/backups/config", map[string]any{"schedule": "daily", "hour": 2, "keep": n + 10})
		for i := 0; i < n; i++ {
			c.call("POST", "/api/backups", map[string]any{})
			time.Sleep(1100 * time.Millisecond) // a backup is named by the second
		}
	}
	if os.Getenv("NW_GUEST") != "" { // a guest network, so the sign-in page card has something to pick
		if rc, _, raw := c.call("POST", "/api/networks/add", map[string]any{"name": "Visitors", "port": "eth3", "vlan": 30, "kind": "guest", "internet": true, "intra_allow": false, "lease": "2h"}); rc != 200 {
			t.Fatalf("guest network: %d %s", rc, raw)
		}
	}
	if os.Getenv("NW_GUEST") != "" { // a few of your own names
		for _, r := range [][2]string{{"printer.lan", "192.168.1.60"}, {"nas.lan", "192.168.1.20"}, {"firewall.lan", "192.168.1.1"}} {
			c.call("POST", "/api/dns/records", map[string]any{"name": r[0], "type": "A", "value": r[1], "ttl": 300})
		}
	}
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
	if os.Getenv("NW_LOGS") != "" { // a few lookups and connections, with the detail a real box would have filled in
		seedDemoLogs(c)
	}
	must("PUT", "/api/fw/rules", map[string]any{"name": "Cameras stay home", "from": "Home", "to": "Internet", "dst": "203.0.113.9", "action": "drop", "enabled": true})
	apply()
}

// seedDemoLogs puts a few log rows in so the opened-row detail can be looked at.
func seedDemoLogs(c *client) {
	now := time.Now().Unix()
	c.app.storeFW([]netd.FWEvent{
		{TS: now - 40, Verdict: "allow", Tag: "rule:Home:Internet:Web", In: "eth1", Out: "eth0", Src: "192.168.1.110", Dst: "142.250.80.46", Proto: "tcp", SPort: 51000, DPort: 443},
		{TS: now - 30, Verdict: "drop", Tag: "default:fwd", In: "eth1", Out: "eth0", Src: "192.168.1.115", Dst: "203.0.113.9", Proto: "tcp", SPort: 51001, DPort: 25},
	})
	// Blocked attempts from the internet over the last month, for the Threats front-door card.
	scanners := []struct {
		addr  string
		ports []int
	}{
		{"198.51.100.7", []int{22, 23, 80, 443, 445, 3389, 8080, 5060, 21, 25, 110, 143, 993, 1883, 8443, 9100, 631, 554}},
		{"203.0.113.50", []int{22}},
		{"192.0.2.77", []int{3389, 445}},
		{"198.18.0.9", []int{23, 2323}},
	}
	for day := 0; day < 30; day++ {
		for si, sc := range scanners {
			for k := 0; k < (day*7+si*13)%9+1; k++ {
				p := sc.ports[(day+k)%len(sc.ports)]
				c.app.Logs.Exec(`INSERT INTO fw_events(ts,action,from_zone,from_addr,to_zone,to_addr,proto,dport) VALUES(?,?,?,?,?,?,?,?)`,
					now-int64(day)*86400-int64(k)*600-int64(si)*60, "drop", "Internet", sc.addr, "Home", "192.0.2.2", "tcp", p)
			}
		}
	}
	c.app.Logs.Exec(`UPDATE fw_events SET outcome='Completed',bytes_out=2600,bytes_in=1480000,dur=25 WHERE dport=443`)
	c.app.QLog.Add(dnssrv.LogEntry{TS: now - 20, Client: "192.168.1.110", ClientName: "Living room TV", QName: "api.example.com", QType: "A", Result: "forwarded", RCode: "NOERROR", Upstream: "1.1.1.1", LatencyUS: 14200})
	c.app.QLog.Add(dnssrv.LogEntry{TS: now - 10, Client: "192.168.1.112", ClientName: "Marvin's laptop", QName: "nothing-here.example.net", QType: "AAAA", Result: "forwarded", RCode: "NXDOMAIN", Upstream: "1.1.1.1", LatencyUS: 31800})
	c.app.Logs.Exec(`INSERT INTO dns_queries(ts,client,client_name,qname,qtype,result,rcode,upstream,latency_us,rule,out_if) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, now-5, "192.168.1.112", "Marvin's laptop", "cdn.games.example.com", "A", "blocked", "NOERROR", "", 0, "Ads and trackers: games.example.com", "")
	c.app.Logs.Exec(`INSERT INTO dns_queries(ts,client,client_name,qname,qtype,result,rcode,upstream,latency_us,rule,out_if) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, now-30, "192.168.1.112", "Marvin's laptop", "api.example.com", "A", "forwarded", "NOERROR", "1.1.1.1", 14200, "", "")
	// A busy minute of lookups for the live list, with a repeated name, and a laptop that also talks to
	// Google's DNS directly and tries encrypted DNS, for Find traffic and the DNS check.
	names := []string{"api.example.com", "cdn.example.com", "mail.example.org", "time.apple.com", "ocsp.digicert.com", "tracker.ads.example", "update.example.net", "video.example.tv"}
	for i := 0; i < 70; i++ {
		who, ip := "Living room TV", "192.168.1.110"
		if i%3 == 0 {
			who, ip = "Marvin's laptop", "192.168.1.112"
		}
		res, rule := "forwarded", ""
		if names[i%len(names)] == "tracker.ads.example" {
			res, rule = "blocked", "Ads and trackers: ads.example"
		} else if i%5 == 0 {
			res = "cached"
		}
		for _, qt := range []string{"A", "AAAA"} {
			if qt == "AAAA" && i%4 != 0 {
				continue
			}
			c.app.Logs.Exec(`INSERT INTO dns_queries(ts,client,client_name,qname,qtype,result,rcode,upstream,latency_us,rule) VALUES(?,?,?,?,?,?,?,?,?,?)`,
				now-int64(i)*55/10, ip, who, names[i%len(names)], qt, res, "NOERROR", "1.1.1.1", 12000+i*300, rule)
		}
	}
	var direct []netd.FWEvent
	for i := 0; i < 9; i++ {
		direct = append(direct, netd.FWEvent{TS: now - int64(i)*1500, Verdict: "allow", Tag: "internet:Home", In: "eth1", Out: "eth0", Src: "192.168.1.112", Dst: "8.8.8.8", Proto: "tcp", SPort: 52000 + i, DPort: 443})
	}
	direct = append(direct,
		netd.FWEvent{TS: now - 700, Verdict: "drop", Tag: "rule:dns:dot", In: "eth1", Out: "eth0", Src: "192.168.1.112", Dst: "1.1.1.1", Proto: "tcp", SPort: 53000, DPort: 853},
		netd.FWEvent{TS: now - 50, Verdict: "allow", Tag: "rule:Home:Internet:Web", In: "eth1", Out: "eth0", Src: "192.168.1.112", Dst: "198.51.100.200", Proto: "tcp", SPort: 53001, DPort: 443},
		netd.FWEvent{TS: now - 15, Verdict: "allow", Tag: "rule:Home:Internet:Web", In: "eth1", Out: "eth0", Src: "192.168.1.112", Dst: "198.51.100.201", Proto: "tcp", SPort: 53002, DPort: 8443},
	)
	c.app.storeFW(direct)
	c.app.Logs.Exec(`UPDATE fw_events SET outcome='Completed',bytes_out=900,bytes_in=4300,dur=2 WHERE to_addr='8.8.8.8'`)
	c.app.Logs.Exec(`UPDATE fw_events SET outcome='Timed out',bytes_out=60,bytes_in=0,dur=0 WHERE to_addr='198.51.100.200'`)
	c.app.Logs.Exec(`UPDATE fw_events SET outcome='Still open',bytes_out=5100,bytes_in=88000,dur=14 WHERE to_addr='198.51.100.201'`)
	time.Sleep(1500 * time.Millisecond)
}

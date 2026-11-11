package app

import (
	"strings"
	"testing"
	"time"

	"nopewarden/internal/fw"
)

func fakeCounters(t *testing.T, vals map[string][2]uint64) {
	old := readCounter
	readCounter = func(dev, name string) (uint64, bool) {
		v, ok := vals[dev]
		if !ok {
			return 0, false
		}
		if name == "rx_bytes" {
			return v[0], true
		}
		return v[1], true
	}
	t.Cleanup(func() { readCounter = old })
}

func twoConnections(t *testing.T, c *client) (string, string) {
	t.Helper()
	_, _, body := c.call("GET", "/api/net/wans", nil)
	if !strings.Contains(string(body), "Fibre") {
		if rc, _, raw := c.call("PUT", "/api/net/wans", map[string]any{"name": "Fibre", "iface": "eth0", "type": "dhcp", "enabled": true}); rc != 200 {
			t.Fatalf("%d %s", rc, raw)
		}
	}
	if rc, _, raw := c.call("PUT", "/api/net/wans", map[string]any{"name": "Backup", "iface": "eth2", "type": "dhcp", "priority": 2, "enabled": true}); rc != 200 {
		t.Fatalf("%d %s", rc, raw)
	}
	return "Fibre", "Backup"
}

func TestUsageIsCountedPerConnectionAndPerDevice(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	a := c.app
	f, b := twoConnections(t, c)
	devs := a.wanDevices()
	df, db := devs[f], devs[b]
	if df == "" || db == "" || df == db {
		t.Fatalf("devices: %v", devs)
	}
	fakeCounters(t, map[string][2]uint64{df: {1000, 100}, db: {5000, 500}})
	arp := map[string]arpEntry{"192.168.1.20": {MAC: "aa:bb:cc:dd:ee:01", Dev: "br0"}}
	now := time.Now()
	cur := map[string]fw.Counter{}
	a.devt.mu.Lock()
	a.sampleLinks(&a.devt, cur, nil, arp, now.Unix()/300*300, now, true)
	a.devt.mu.Unlock()

	// A minute later: Fibre moved 4000 down and 400 up; the device used each connection.
	fakeCounters(t, map[string][2]uint64{df: {5000, 500}, db: {5000, 500}})
	key := func(link, dir string) string {
		return fw.AcctWanPrefix + fw.WanKey(devs[link]) + ":" + dir + ":192.168.1.20"
	}
	cur[key(f, "up")] = fw.Counter{Bytes: 300}
	cur[key(f, "down")] = fw.Counter{Bytes: 3000}
	cur[key(b, "down")] = fw.Counter{Bytes: 700}
	a.devt.mu.Lock()
	a.sampleLinks(&a.devt, cur, nil, arp, now.Unix()/300*300, now.Add(time.Minute), false)
	a.devt.mu.Unlock()

	var up, down int64
	a.Logs.QueryRow(`SELECT COALESCE(SUM(up),0),COALESCE(SUM(down),0) FROM link_traffic WHERE link=?`, f).Scan(&up, &down)
	if up != 400 || down != 4000 {
		t.Fatalf("Fibre totals %d up %d down", up, down)
	}
	a.Logs.QueryRow(`SELECT COALESCE(SUM(up),0),COALESCE(SUM(down),0) FROM link_traffic WHERE link=?`, b).Scan(&up, &down)
	if up != 0 || down != 0 {
		t.Fatalf("Backup moved nothing: %d %d", up, down)
	}
	a.Logs.QueryRow(`SELECT COALESCE(SUM(up),0),COALESCE(SUM(down),0) FROM link_dev_traffic WHERE link=? AND mac='aa:bb:cc:dd:ee:01'`, b).Scan(&up, &down)
	if up != 0 || down != 700 {
		t.Fatalf("the device used Backup for 700 down: %d %d", up, down)
	}
	a.Logs.QueryRow(`SELECT COALESCE(SUM(up),0),COALESCE(SUM(down),0) FROM link_dev_traffic WHERE link=? AND mac='aa:bb:cc:dd:ee:01'`, f).Scan(&up, &down)
	if up != 300 || down != 3000 {
		t.Fatalf("the device used Fibre for 300 up, 3000 down: %d %d", up, down)
	}

	// The same readings through the API.
	v, err := a.usageFor(f, "this_month", "", "")
	if err != nil || v.Up != 400 || v.Down != 4000 || len(v.Top) != 1 || v.Top[0].Up != 300 {
		t.Fatalf("%+v %v", v, err)
	}
	if v.Note == "" {
		// Counting began today, so a month that began earlier carries the note.
		if time.Now().Day() > 1 {
			t.Fatalf("with two connections, earlier history cannot be split: %q", v.Note)
		}
	}
	if v, _ := a.usageFor(b, "last_7", "", ""); v.Up != 0 || v.Down != 0 {
		t.Fatalf("%+v", v)
	}
}

func TestUsagePeriodsAndPerConnectionLimits(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	a := c.app
	f, b := twoConnections(t, c)
	// A limit belongs to a connection: none can be saved for all of them.
	if rc, _, _ := c.call("PUT", "/api/usage/limit?link=", map[string]any{"on": true, "cap_gb": 100, "start_day": 1}); rc == 200 {
		t.Fatal("a limit for no connection must be refused")
	}
	if rc, _, raw := c.call("PUT", "/api/usage/limit?link="+f, map[string]any{"on": true, "cap_gb": 100, "start_day": 15}); rc != 200 {
		t.Fatalf("%d %s", rc, raw)
	}
	if rc, _, raw := c.call("PUT", "/api/usage/limit?link="+b, map[string]any{"on": true, "cap_gb": 50, "start_day": 3}); rc != 200 {
		t.Fatalf("%d %s", rc, raw)
	}
	vf, _ := a.usageFor(f, "billing", "", "")
	vb, _ := a.usageFor(b, "billing", "", "")
	if vf.CapGB != 100 || vf.Start != 15 || vb.CapGB != 50 || vb.Start != 3 || vf.From == vb.From && time.Now().Day() != 3 && time.Now().Day() != 15 {
		t.Fatalf("each connection keeps its own limit and billing day: %+v %+v", vf, vb)
	}
	// All connections: calendar ranges only. Billing quietly becomes this month.
	va, err := a.usageFor("", "billing", "", "")
	if err != nil || va.Range != "this_month" || va.On {
		t.Fatalf("%+v %v", va, err)
	}
	// Ranges.
	for rng, days := range map[string]int{"last_7": 7, "last_30": 30} {
		v, err := a.usageFor("", rng, "", "")
		if err != nil || int((v.To-v.From)/86400) != days {
			t.Fatalf("%s: %+v %v", rng, v, err)
		}
	}
	v, err := a.usageFor("", "custom", "2026-10-03", "2026-10-05")
	if err != nil || (v.To-v.From)/86400 != 3 {
		t.Fatalf("custom range includes both ends: %+v %v", v, err)
	}
	if _, err := a.usageFor("", "custom", "nope", "2026-10-05"); err == nil {
		t.Fatal("a bad date must be refused")
	}
	if _, err := a.usageFor("", "custom", "2024-01-01", "2026-10-05"); err == nil {
		t.Fatal("a range over a year must be refused")
	}
	lm, _ := a.usageFor("", "last_month", "", "")
	tm, _ := a.usageFor("", "this_month", "", "")
	if lm.To != tm.From {
		t.Fatalf("last month ends where this month begins: %d %d", lm.To, tm.From)
	}
	// The API takes the same words.
	if rc, m, raw := c.call("GET", "/api/usage?link="+f+"&range=last_7", nil); rc != 200 || m["range"] != "last_7" {
		t.Fatalf("%d %s", rc, raw)
	}
}

func TestSingleConnectionKeepsEarlierHistory(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	a := c.app
	links := a.usageLinks()
	if len(links) != 1 {
		t.Skipf("expected one connection, have %v", links)
	}
	v, _ := a.usageFor(links[0], "last_30", "", "")
	a.Logs.Exec(`INSERT INTO traffic(ts,mac,up,down) VALUES(?,'',?,?)`, v.From+3600, int64(2e9), int64(3e9))
	a.Logs.Exec(`INSERT INTO traffic(ts,mac,up,down) VALUES(?,'aa:bb:cc:dd:ee:09',?,?)`, v.From+3600, int64(1e9), int64(1e9))
	v, _ = a.usageFor(links[0], "last_30", "", "")
	if v.Up != 2e9 || v.Down != 3e9 || len(v.Top) != 1 || v.Note != "" {
		t.Fatalf("history from before counting by connection is the one connection's: %+v", v)
	}
}

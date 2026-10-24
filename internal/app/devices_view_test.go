package app

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"gatehouse/internal/fw"
)

const stationSample = `Station 5c:cf:7f:11:22:33 (on wlan0)
	inactive time:	40 ms
	rx bytes:	123456
	signal:  	-52 [-53, -55] dBm
	tx bitrate:	866.7 MBit/s VHT-MCS 9 80MHz short GI VHT-NSS 2
Station aa:bb:cc:dd:ee:ff (on wlan0)
	signal:  	-71 dBm
	tx bitrate:	72.2 MBit/s MCS 7
`

func TestStationDumpAndChannel(t *testing.T) {
	st := ParseStationDump(stationSample)
	if len(st) != 2 || st["5c:cf:7f:11:22:33"].Signal != -52 || st["5c:cf:7f:11:22:33"].Rate != "867 Mb/s" || st["aa:bb:cc:dd:ee:ff"].Signal != -71 {
		t.Fatalf("%+v", st)
	}
	band, ch := ParseChannel("Interface wlan0\n\tchannel 36 (5180 MHz), width: 80 MHz, center1: 5210 MHz\n")
	if band != "5" || ch != 36 {
		t.Fatal(band, ch)
	}
	if b, c := ParseChannel("channel 6 (2437 MHz), width: 20 MHz"); b != "2.4" || c != 6 {
		t.Fatal(b, c)
	}
}

func TestDeviceTrafficIsCountedPerDevice(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	a := c.app
	mac := "aa:bb:cc:dd:ee:01"
	a.Config.Exec(`INSERT INTO dhcp_scopes(id,name,iface,subnet,range_start,range_end,router) VALUES(1,'lan','eth1','192.168.1.0/24','192.168.1.100','192.168.1.200','192.168.1.1')`)
	a.Config.Exec(`INSERT INTO dhcp_leases(scope_id,ip,mac,start,expires,state) VALUES(1,'192.168.1.150',?,?,?, 'active')`, mac, time.Now().Unix(), time.Now().Add(time.Hour).Unix())
	a.DHCP.Reload()
	if rc, _, raw := c.call("POST", "/api/devices", map[string]any{"mac": mac, "name": "Kitchen TV"}); rc != 200 {
		t.Fatalf("%d %s", rc, raw)
	}

	now := time.Now()
	// What happened before the first look is not counted.
	a.sampleDevices(map[string]fw.Counter{
		fw.AcctPrefix + "up:192.168.1.150":   {Bytes: 1000},
		fw.AcctPrefix + "down:192.168.1.150": {Bytes: 9000},
	}, now)
	a.sampleDevices(map[string]fw.Counter{
		fw.AcctPrefix + "up:192.168.1.150":   {Bytes: 3000},
		fw.AcctPrefix + "down:192.168.1.150": {Bytes: 69000},
	}, now.Add(10*time.Second))
	rt := a.devRates()["192.168.1.150"]
	if rt.Up != 200 || rt.Down != 6000 {
		t.Fatalf("rates %+v", rt)
	}
	// A reload of the firewall restarts the counters: the new value is what was added.
	a.sampleDevices(map[string]fw.Counter{fw.AcctPrefix + "up:192.168.1.150": {Bytes: 500}}, now.Add(20*time.Second))
	u, _ := a.usageByDevice("1h")
	if len(u) != 1 || u[0].MAC != mac || u[0].Up != 2500 || u[0].Down != 60000 || u[0].Name != "Kitchen TV" {
		t.Fatalf("usage %+v", u)
	}
	if a.todayBytes()[mac][0] != 2500 {
		t.Fatal("today")
	}
	rc, m, _ := c.call("GET", "/api/traffic?range=1h&macs="+mac, nil)
	if rc != 200 || len(m["points"].([]any)) < 50 {
		t.Fatalf("series: %v", m)
	}

	// The device list carries speed, today's data, and the network.
	_, _, raw := c.call("GET", "/api/devices", nil)
	var out struct {
		Devices []struct {
			Name      string
			Network   string
			TodayDown int64 `json:"today_down"`
			Online    bool
			Vendor    string
			IconGuess string `json:"icon_guess"`
			Conn      string
		}
		Icons []string
	}
	json.Unmarshal(raw, &out)
	if len(out.Devices) != 1 || out.Devices[0].TodayDown != 60000 || !out.Devices[0].Online || out.Devices[0].Network != "Home" || out.Devices[0].Conn != "wired" || len(out.Icons) < 10 {
		t.Fatalf("list: %s", raw)
	}
}

func TestDeviceIconsAndDetails(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	mac := "aa:bb:cc:dd:ee:02"
	c.call("POST", "/api/devices", map[string]any{"mac": mac, "name": "Desk"})
	_, _, raw := c.call("GET", "/api/devices", nil)
	var out struct{ Devices []struct{ ID int64 } }
	json.Unmarshal(raw, &out)
	id := out.Devices[0].ID
	path := "/api/devices/" + istr(int(id))

	if rc, _, _ := c.call("PATCH", path, map[string]any{"icon": "tv"}); rc != 200 {
		t.Fatal("icon")
	}
	if rc, _, _ := c.call("PATCH", path, map[string]any{"icon": "../etc/passwd"}); rc == 200 {
		t.Fatal("bad icon name accepted")
	}
	png := base64.StdEncoding.EncodeToString(append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 40)...))
	rc, m, body := c.call("POST", "/api/device-icons", map[string]any{"name": "mine", "data": png})
	if rc != 200 {
		t.Fatalf("upload: %d %s", rc, body)
	}
	iid := int64(m["id"].(float64))
	for name, data := range map[string]string{
		"svg":   base64.StdEncoding.EncodeToString([]byte("<svg onload=alert(1)/>")),
		"big":   base64.StdEncoding.EncodeToString(append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 200<<10)...)),
		"empty": "",
		"junk":  "###",
	} {
		if rc, _, _ := c.call("POST", "/api/device-icons", map[string]any{"data": data}); rc == 200 {
			t.Errorf("%s accepted", name)
		}
	}
	if rc, _, _ := c.call("PATCH", path, map[string]any{"icon_id": iid}); rc != 200 {
		t.Fatal("pick upload")
	}
	_, _, raw = c.call("GET", "/api/devices", nil)
	if !strings.Contains(string(raw), `"icon_id":`+istr(int(iid))) {
		t.Fatalf("%s", raw)
	}
	// Details: rules that mention the device, and a reservation.
	c.call("PUT", "/api/fw/rules", map[string]any{"name": "Desk out", "from": "Home", "to": "Internet", "src": "dev:" + mac, "action": "accept", "enabled": true})
	_, m, _ = c.call("GET", path+"/details", nil)
	rules, _ := m["rules"].([]any)
	if len(rules) != 1 || rules[0] != "Desk out" {
		t.Fatalf("details: %v", m)
	}

	// Preferences are kept per person and only for known pages.
	if rc, _, _ := c.call("PUT", "/api/prefs/devices", map[string]any{"cols": []string{"name", "ip"}, "rows": 25}); rc != 200 {
		t.Fatal("prefs")
	}
	_, m, _ = c.call("GET", "/api/prefs/devices", nil)
	if m["rows"] != float64(25) {
		t.Fatalf("prefs: %v", m)
	}
	if rc, _, _ := c.call("PUT", "/api/prefs/other", map[string]any{"x": 1}); rc == 200 {
		t.Fatal("unknown pref key accepted")
	}
}

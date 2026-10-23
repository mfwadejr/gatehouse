package netd

import (
	"strings"
	"testing"
)

func TestParseScan(t *testing.T) {
	text := `BSS aa:bb:cc:dd:ee:01(on wlan0)
	freq: 2437.0
	signal: -48.00 dBm
	SSID: Home
	RSN:	 * Version: 1
		 * Authentication suites: PSK
BSS aa:bb:cc:dd:ee:02(on wlan0)
	freq: 5180.0
	signal: -60.00 dBm
	SSID: Home
	RSN:	 * Version: 1
		 * Authentication suites: PSK
BSS aa:bb:cc:dd:ee:03(on wlan0)
	freq: 2412
	signal: -70.00 dBm
	SSID: Cafe
BSS aa:bb:cc:dd:ee:04(on wlan0)
	freq: 2412
	signal: -50.00 dBm
	SSID:
`
	got := ParseScan(text)
	if len(got) != 2 || got[0].SSID != "Home" || got[0].Signal != -48 || got[0].Security != "wpa2" || got[1].SSID != "Cafe" || got[1].Security != "open" {
		t.Fatalf("%+v", got)
	}
}

func TestUPnPScript(t *testing.T) {
	s, err := UPnPScript([]UPnPMapping{{ID: 7, Proto: "udp", ExtPort: 40000, IntPort: 3074, IntAddr: "192.168.1.50", WanDev: "eth0"}})
	if err != nil || !strings.Contains(s, `iifname "eth0" udp dport 40000 counter dnat to 192.168.1.50:3074 comment "upnp:7"`) {
		t.Fatalf("%v\n%s", err, s)
	}
	if s, _ := UPnPScript(nil); strings.Contains(s, "chain") {
		t.Fatal("no mappings means no rules")
	}
	for name, m := range map[string]UPnPMapping{
		"privileged port": {ID: 1, Proto: "tcp", ExtPort: 22, IntPort: 22, IntAddr: "192.168.1.5", WanDev: "eth0"},
		"public target":   {ID: 1, Proto: "tcp", ExtPort: 5000, IntPort: 22, IntAddr: "8.8.8.8", WanDev: "eth0"},
		"injection":       {ID: 1, Proto: "tcp", ExtPort: 5000, IntPort: 22, IntAddr: "192.168.1.5", WanDev: "eth0\" accept #"},
		"bad protocol":    {ID: 1, Proto: "icmp", ExtPort: 5000, IntPort: 22, IntAddr: "192.168.1.5", WanDev: "eth0"},
	} {
		if _, err := UPnPScript([]UPnPMapping{m}); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

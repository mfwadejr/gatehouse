package netcfg

import (
	"strings"
	"testing"
)

func TestWifiAccessPointInBridge(t *testing.T) {
	c := &Config{
		Interfaces: []Interface{{Name: "br0", Kind: "bridge", Members: []string{"enp2s0", "wlan0"}, IPv4: "192.168.1.1/24", Enabled: true}},
		Wifi:       []Wifi{{Iface: "wlan0", Mode: "ap", SSID: "Home", PSK: "correct horse", Country: "US", Band: "5", Fast: true, WPA3: true, Enabled: true}},
	}
	o, err := Generate(c)
	if err != nil {
		t.Fatal(err)
	}
	h := o.Wifi["wlan0.hostapd.conf"]
	for _, want := range []string{"hw_mode=a", "channel=36", "ieee80211ac=1", "country_code=US", "wpa_key_mgmt=WPA-PSK SAE", "ssid=Home"} {
		if !strings.Contains(h, want) {
			t.Errorf("hostapd config lacks %q:\n%s", want, h)
		}
	}
	if len(o.PPPUnits) != 1 || o.PPPUnits[0] != "wifi-ap:wlan0" {
		t.Errorf("units = %v", o.PPPUnits)
	}
	if !strings.Contains(o.Networkd["gatehouse-wlan0.network"], "Bridge=br0") {
		t.Error("the access point card should join the bridge")
	}
}

func TestWifiClientAsInternet(t *testing.T) {
	c := &Config{
		WANs: []WAN{{Name: "Hotspot", Iface: "wlan0", Type: "dhcp", Priority: 2, Enabled: true}},
		Wifi: []Wifi{{Iface: "wlan0", Mode: "client", SSID: "Phone", PSK: "pa\"ss word1", Country: "GB", Enabled: true}},
	}
	o, err := Generate(c)
	if err != nil {
		t.Fatal(err)
	}
	s := o.Wifi["wlan0.supplicant.conf"]
	if !strings.Contains(s, "ssid=50686f6e65") || !strings.Contains(s, `psk="pa"ss word1"`) || !strings.Contains(s, "country=GB") {
		t.Errorf("supplicant config:\n%s", s)
	}
	if !strings.Contains(o.Networkd["gatehouse-wlan0.network"], "DHCP=ipv4") {
		t.Error("the joined network should hand out the internet address")
	}
}

func TestWifiRejectsMistakes(t *testing.T) {
	lan := Interface{Name: "wlan0", Kind: "phys", IPv4: "10.0.0.1/24", Enabled: true}
	cases := map[string]*Config{
		"password":      {Interfaces: []Interface{lan}, Wifi: []Wifi{{Iface: "wlan0", Mode: "ap", SSID: "x", PSK: "short", Country: "US", Band: "2.4", Enabled: true}}},
		"country":       {Interfaces: []Interface{lan}, Wifi: []Wifi{{Iface: "wlan0", Mode: "ap", SSID: "x", PSK: "longenough", Band: "2.4", Enabled: true}}},
		"open ap":       {Interfaces: []Interface{lan}, Wifi: []Wifi{{Iface: "wlan0", Mode: "ap", SSID: "x", Country: "US", Band: "2.4", Enabled: true}}},
		"no network":    {Wifi: []Wifi{{Iface: "wlan0", Mode: "ap", SSID: "x", PSK: "longenough", Country: "US", Band: "2.4", Enabled: true}}},
		"client lan":    {Interfaces: []Interface{lan}, Wifi: []Wifi{{Iface: "wlan0", Mode: "client", SSID: "x", PSK: "longenough", Country: "US", Enabled: true}}},
		"client no wan": {Wifi: []Wifi{{Iface: "wlan0", Mode: "client", SSID: "x", PSK: "longenough", Country: "US", Enabled: true}}},
	}
	for name, c := range cases {
		if _, err := Generate(c); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

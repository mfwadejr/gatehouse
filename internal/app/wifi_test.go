package app

import (
	"path/filepath"
	"strings"
	"testing"

	"gatehouse/internal/store"
)

func TestParseWifiCaps(t *testing.T) {
	info := `Wiphy phy0
	Band 1:
		Capabilities: 0x1ef
		Frequencies:
			* 2412 MHz [1] (20.0 dBm)
			* 2417 MHz [2] (20.0 dBm)
	Band 2:
		VHT Capabilities (0x33800132):
		Frequencies:
			* 5180 MHz [36] (23.0 dBm)
	Supported interface modes:
		 * IBSS
		 * managed
		 * AP
		 * monitor
	Supported commands:
		 * new_interface
`
	c := ParseWifiCaps(info)
	if !c.AP || !c.VHT || len(c.Bands) != 2 || c.Bands[0] != "2.4" || c.Bands[1] != "5" {
		t.Fatalf("%+v", c)
	}
	if ParseWifiCaps("Supported interface modes:\n\t * managed\n").AP {
		t.Fatal("a client-only card must not report AP")
	}
}

func TestParseIwLink(t *testing.T) {
	out := "Connected to 3c:a6:2f:11:22:33 (on wlp2s0)\n\tSSID: Home\n\tfreq: 2437\n\tsignal: -52 dBm\n\ttx bitrate: 72.2 MBit/s\n"
	ssid, sig, ok := ParseIwLink(out)
	if !ok || ssid != "Home" || sig != -52 {
		t.Fatalf("%q %d %v", ssid, sig, ok)
	}
	if _, _, ok := ParseIwLink("Not connected.\n"); ok {
		t.Fatal("a card that has not joined was reported as joined")
	}
}

func TestWifiServesFindsTheNetworkACardCarries(t *testing.T) {
	db, err := store.OpenConfig(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if wifiServes(db, "wlp3s0") != nil {
		t.Fatal("a card with no network must report none")
	}
	db.Exec(`INSERT INTO zones(name,kind) VALUES('Home','trusted')`)
	// The card is a member of the LAN bridge: the address is on the bridge.
	db.Exec(`INSERT INTO interfaces(name,kind,zone_id,ipv4_cidr,members,enabled) VALUES('br-lan','bridge',1,'192.168.50.1/24','eth1,wlp3s0',1)`)
	got := wifiServes(db, "wlp3s0")
	if got == nil || got.Name != "br-lan" || got.Zone != "Home" || got.Addr != "192.168.50.1/24" || got.Via != "br-lan" {
		t.Fatalf("bridge member: %+v", got)
	}
	// A card that is its own network reports itself.
	db.Exec(`INSERT INTO interfaces(name,kind,zone_id,ipv4_cidr,enabled) VALUES('wlan1','phys',1,'192.168.60.1/24',1)`)
	got = wifiServes(db, "wlan1")
	if got == nil || got.Name != "wlan1" || got.Via != "" || got.Addr != "192.168.60.1/24" {
		t.Fatalf("own network: %+v", got)
	}
	// A switched-off network does not count.
	db.Exec(`UPDATE interfaces SET enabled=0 WHERE name='br-lan'`)
	if wifiServes(db, "wlp3s0") != nil {
		t.Fatal("a disabled bridge does not carry the card")
	}
}

const worldDomainCard = `Wiphy phy0
	Band 1:
		Capabilities: 0x1ef
		HT Capabilities
		Frequencies:
			* 2412 MHz [1] (20.0 dBm)
			* 2437 MHz [6] (20.0 dBm)
			* 2462 MHz [11] (20.0 dBm)
			* 2467 MHz [12] (20.0 dBm) (no IR)
	Band 2:
		VHT Capabilities (0x33800132):
		Frequencies:
			* 5180 MHz [36] (20.0 dBm) (no IR)
			* 5200 MHz [40] (20.0 dBm) (no IR)
			* 5260 MHz [52] (20.0 dBm) (no IR, radar detection)
			* 5500 MHz [100] (disabled)
	Supported interface modes:
		 * managed
		 * AP
`

const usDomainCard = `Wiphy phy0
	Band 1:
		HT Capabilities
		Frequencies:
			* 2412 MHz [1] (30.0 dBm)
			* 2437 MHz [6] (30.0 dBm)
	Band 2:
		VHT Capabilities (0x33800132):
		Frequencies:
			* 5180 MHz [36] (23.0 dBm)
			* 5200 MHz [40] (23.0 dBm)
			* 5260 MHz [52] (23.0 dBm) (radar detection)
			* 5500 MHz [100] (23.0 dBm) (radar detection)
			* 5745 MHz [149] (30.0 dBm)
			* 5825 MHz [165] (30.0 dBm)
	Band 4:
		HE Iftypes: managed, AP
		Frequencies:
			* 5955 MHz [1] (12.0 dBm)
			* 6115 MHz [33] (12.0 dBm)
	Supported interface modes:
		 * managed
		 * AP
`

func TestWifiProbeOffersOnlyBandsTheCardCanBroadcastOn(t *testing.T) {
	// Before a country is set the card lists 5 GHz but may not start a network there.
	c := ParseWifiCaps(worldDomainCard)
	if got := strings.Join(c.Listed, ","); got != "2.4,5" {
		t.Fatalf("hardware bands: %s", got)
	}
	if got := strings.Join(c.ApBands, ","); got != "2.4" {
		t.Fatalf("broadcast bands under the world domain should be 2.4 only, got %s", got)
	}
	if got := strings.Join(c.Bands, ","); got != "2.4,5" {
		t.Fatalf("join bands: %s", got)
	}
	// With a country that allows it, 5 GHz opens up, radar channels still need detection.
	c = ParseWifiCaps(usDomainCard)
	if got := strings.Join(c.ApBands, ","); got != "2.4,5" {
		t.Fatalf("US broadcast bands: %s", got)
	}
	var radar, nonRadar int
	for _, ch := range c.Channels {
		if ch.Band == "5" && ch.Radar {
			radar++
		}
		if ch.Band == "5" && ch.Usable() {
			nonRadar++
		}
	}
	if radar != 2 || nonRadar != 4 {
		t.Fatalf("channels: radar=%d usable=%d", radar, nonRadar)
	}
	// 6 GHz is detected and can be joined, but is not offered for broadcasting yet.
	if !hasStr(c.Bands, "6") || hasStr(c.ApBands, "6") {
		t.Fatalf("6 GHz: bands=%v ap=%v", c.Bands, c.ApBands)
	}
	if c.Std != "ax" {
		t.Fatalf("fastest standard should be ax, got %q", c.Std)
	}
	if ParseWifiCaps(worldDomainCard).Std != "ac" {
		t.Fatal("a VHT card is Wi-Fi 5")
	}
	if ParseWifiCaps("").Bands == nil || ParseWifiCaps("").Channels == nil {
		t.Fatal("empty output must still give real lists")
	}
}

func TestRegCountry(t *testing.T) {
	out := "global\ncountry US: DFS-FCC\n\t(2402 - 2472 @ 40), (N/A, 30), (N/A)\n\nphy#0 (self-managed)\ncountry DE: DFS-ETSI\n\t(2402 - 2482 @ 40), (N/A, 20), (N/A)\n"
	if got := RegCountry(out, "phy0"); got != "DE" {
		t.Fatalf("a self-managed card uses its own country, got %q", got)
	}
	if got := RegCountry(out, "phy1"); got != "US" {
		t.Fatalf("another card uses the global one, got %q", got)
	}
	if RegCountry("", "phy0") != "" {
		t.Fatal("nothing known")
	}
}

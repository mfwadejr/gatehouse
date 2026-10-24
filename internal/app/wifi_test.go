package app

import "testing"

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

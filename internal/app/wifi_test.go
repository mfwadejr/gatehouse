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

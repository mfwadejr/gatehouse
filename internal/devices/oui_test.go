package devices

import "testing"

func TestVendorFromAddress(t *testing.T) {
	OUIPath = "testdata/oui.csv"
	ReloadOUI()
	defer func() { OUIPath = "/usr/share/ieee-data/oui.csv"; ReloadOUI() }()
	for mac, want := range map[string]string{
		"60:8b:0e:11:22:33": "Apple",
		"60-8B-0E-11-22-33": "Apple",
		"d0:37:45:00:00:01": "TP-LINK",
		"dc:a6:32:00:00:01": "Raspberry Pi Trading",
		"7c:89:56:00:00:01": "Samsung",
		"24:6f:28:00:00:01": "Espressif",
		"00:00:00:00:00:01": "",                // not in the list
		"da:a1:19:11:22:33": "Private address", // made up by the device
		"60:8b":             "",
	} {
		if got := Vendor(mac); got != want {
			t.Errorf("%s: got %q want %q", mac, got, want)
		}
	}
	if IsPrivateMAC("60:8b:0e:11:22:33") || !IsPrivateMAC("fa:00:00:00:00:00") {
		t.Fatal("private address test is wrong")
	}
}

func TestMissingListIsHarmless(t *testing.T) {
	OUIPath = "testdata/none.csv"
	ReloadOUI()
	defer func() { OUIPath = "/usr/share/ieee-data/oui.csv"; ReloadOUI() }()
	if Vendor("60:8b:0e:11:22:33") != "" {
		t.Fatal("a name from nowhere")
	}
}

func TestIconGuess(t *testing.T) {
	for _, c := range []struct{ v, n, h, want string }{
		{"Apple", "", "Marvins-iPhone", "phone"},
		{"Sonos", "", "", "speaker"},
		{"Hewlett Packard", "", "NPI1234 LaserJet", "printer"},
		{"Espressif", "", "", "iot"},
		{"Raspberry Pi Trading", "", "", "server"},
		{"Intel Corporate", "", "DESKTOP-4F", "computer"},
		{"", "", "", "device"},
	} {
		if got := GuessIcon(c.v, c.n, c.h); got != c.want {
			t.Errorf("%+v: got %s", c, got)
		}
	}
	if !ValidIcon("tv") || ValidIcon("../etc") {
		t.Fatal("icon names")
	}
}

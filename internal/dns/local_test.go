package dnssrv

import "testing"

func TestCleanRecord(t *testing.T) {
	good := []struct{ name, typ, val, wantName, wantVal string }{
		{"firewall.lan", "A", "192.168.50.1", "firewall.lan", "192.168.50.1"},
		{"firewall.lan", "a", "https://192.168.50.1/", "firewall.lan", "192.168.50.1"},
		{"https://NAS.lan/", "A", " http://192.168.1.10:8080/x ", "nas.lan", "192.168.1.10"},
		{"v6.lan", "AAAA", "fd00::10", "v6.lan", "fd00::10"},
		{"www.lan", "CNAME", "https://nas.lan/", "www.lan", "nas.lan"},
		{"mail.lan", "MX", "10 Mail.lan", "mail.lan", "10 mail.lan"},
		{"note.lan", "TXT", "hello there", "note.lan", "hello there"},
	}
	for _, c := range good {
		n, v, err := CleanRecord(c.name, c.typ, c.val)
		if err != nil || n != c.wantName || v != c.wantVal {
			t.Errorf("%v: got %q %q %v", c, n, v, err)
		}
	}
	bad := []struct{ name, typ, val string }{
		{"firewall.lan", "A", "not an address"},
		{"firewall.lan", "A", "fd00::1"},
		{"firewall.lan", "A", "192.168.50.999"},
		{"firewall.lan", "AAAA", "192.168.50.1"},
		{"firewall.lan", "CNAME", "192.168.1.1 extra"},
		{"firewall.lan", "MX", "mail.lan"},
		{"bad name", "A", "192.168.50.1"},
		{"x.lan", "SRV", "1"},
	}
	for _, c := range bad {
		if _, _, err := CleanRecord(c.name, c.typ, c.val); err == nil {
			t.Errorf("%v was accepted", c)
		}
	}
	// What an older version could have saved: flagged as unusable.
	if UsableRecord(LocalRecord{Name: "firewall.lan", Type: "A", Value: "https://192.168.50.1"}) {
		t.Error("a record with https:// in an A value counts as usable")
	}
	if !UsableRecord(LocalRecord{Name: "firewall.lan", Type: "A", Value: "192.168.50.1"}) {
		t.Error("a good record counts as unusable")
	}
}

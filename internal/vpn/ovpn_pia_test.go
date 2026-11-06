package vpn

import (
	"strings"
	"testing"
)

// A Private Internet Access profile (the real Switzerland file): it ends with disable-occ, which
// only turns off OpenVPN's options-consistency warning. It must import.
func TestImportProfileProviderWithDisableOCC(t *testing.T) {
	text := "client\ndev tun\nproto udp\nremote swiss.privacy.network 1198\nresolv-retry infinite\nnobind\npersist-key\npersist-tun\ncipher aes-128-cbc\nauth sha1\ntls-client\nremote-cert-tls server\nauth-user-pass\ncompress\nverb 1\nreneg-sec 0\n<ca>\n-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----\n</ca>\ndisable-occ\n"
	ip, err := ImportProfile(text)
	if err != nil {
		t.Fatalf("import refused: %v", err)
	}
	if !ip.NeedsAuth || ip.Remote != "swiss.privacy.network" {
		t.Fatalf("unexpected result: %+v", ip)
	}
}

func TestImportProfileStillRefusesCommands(t *testing.T) {
	_, err := ImportProfile("client\nremote a.example 1194\n<ca>\nx\n</ca>\nup /tmp/x.sh\n")
	if err == nil || !strings.Contains(err.Error(), "up") {
		t.Fatalf("a profile that runs a program must still be refused, got %v", err)
	}
}

// The files below are written to look like the shapes different providers use. They are made up for
// this test (they are not real provider files), so they show the importer copes with the common
// quirks, not that any one provider works.
func TestImportCopesWithCommonProfileShapes(t *testing.T) {
	ca := "<ca>\n-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----\n</ca>\n"
	cases := map[string]string{
		"byte-order mark and old line ends": "\ufeffclient\rdev tun\rproto udp\rremote a.example.net 1194\rauth-user-pass\r" + ca,
		"settings newer OpenVPN rejects":    "client\ndev tun\nremote a.example.net 1194\nncp-disable\ntls-remote a.example.net\nmlock\nroute-metric 5\nauth-user-pass\n" + ca,
		"tls-auth with key direction":       "client\ndev tun\nremote a.example.net 1194\nkey-direction 1\n" + ca + "<tls-auth>\n-----BEGIN OpenVPN Static key V1-----\nAAAA\n-----END OpenVPN Static key V1-----\n</tls-auth>\n",
	}
	for name, text := range cases {
		ip, err := ImportProfile(text)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if ip.Remote == "" {
			t.Errorf("%s: no server found", name)
		}
		for _, bad := range []string{"pass.txt", "ncp-disable", "tls-remote", "mlock"} {
			if strings.Contains(ip.Config, bad) {
				t.Errorf("%s: %q should have been dropped:\n%s", name, bad, ip.Config)
			}
		}
	}
	// What stays refused, with a reason a person can act on.
	if _, err := ImportProfile("client\nremote a.example.net 1194\nauth-user-pass /etc/openvpn/pass.txt\n" + ca); err == nil || !strings.Contains(err.Error(), "username and password") {
		t.Errorf("a password file must be refused with an explanation: %v", err)
	}
	if _, err := ImportProfile("client\n<connection>\nremote a.example.net 1194\n</connection>\n" + ca); err == nil || !strings.Contains(err.Error(), "one server's profile") {
		t.Errorf("several servers must be refused with an explanation: %v", err)
	}
	if _, err := ImportProfile("client\nremote a.example.net 1194\nup /tmp/x.sh\nscript-security 2\n" + ca); err == nil {
		t.Error("a program to run must still be refused")
	}
}

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

package vpn

import (
	"crypto/x509"
	"encoding/pem"
	"strings"
	"testing"
	"time"
)

func parseCert(t *testing.T, p string) *x509.Certificate {
	b, _ := pem.Decode([]byte(p))
	if b == nil {
		t.Fatal("no PEM")
	}
	c, err := x509.ParseCertificate(b.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestPKI(t *testing.T) {
	now := time.Now()
	ca, err := NewCA(now)
	if err != nil {
		t.Fatal(err)
	}
	srv, err := ca.ServerCert(now)
	if err != nil {
		t.Fatal(err)
	}
	cl, err := ca.ClientCert("Marvin phone", now)
	if err != nil {
		t.Fatal(err)
	}
	root := x509.NewCertPool()
	root.AddCert(parseCert(t, ca.CertPEM))
	if _, err := parseCert(t, srv.CertPEM).Verify(x509.VerifyOptions{Roots: root, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}); err != nil {
		t.Errorf("server cert does not verify: %v", err)
	}
	if _, err := parseCert(t, cl.CertPEM).Verify(x509.VerifyOptions{Roots: root, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err != nil {
		t.Errorf("client cert does not verify: %v", err)
	}
	// A client certificate must not pass as a server.
	if _, err := parseCert(t, cl.CertPEM).Verify(x509.VerifyOptions{Roots: root, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}); err == nil {
		t.Error("a client certificate was accepted as a server")
	}
	if _, err := ca.ClientCert("bad;name", now); err == nil {
		t.Error("bad device name accepted")
	}
	crlPEM, err := ca.CRL(2, []Revoked{{Serial: cl.Serial, At: now}}, now)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := pem.Decode([]byte(crlPEM))
	crl, err := x509.ParseRevocationList(b.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if err := crl.CheckSignatureFrom(parseCert(t, ca.CertPEM)); err != nil {
		t.Error(err)
	}
	if len(crl.RevokedCertificateEntries) != 1 || crl.RevokedCertificateEntries[0].SerialNumber.Cmp(parseCert(t, cl.CertPEM).SerialNumber) != 0 {
		t.Error("revoked certificate missing from the list")
	}
	if crl.NextUpdate.Before(now.AddDate(9, 0, 0)) {
		t.Error("revocation list expires too soon")
	}
	k, _ := StaticKey()
	if !strings.Contains(k, "BEGIN OpenVPN Static key V1") || strings.Count(k, "\n") != 21 {
		t.Errorf("static key format:\n%s", k)
	}
}

func TestServerConfigAndProfile(t *testing.T) {
	s := OVPNServer{Dev: "tun0", Proto: "udp", Port: 1194, Subnet: "10.9.0.0/24", DNS: "10.9.0.1", RouteAll: false,
		Routes: []string{"192.168.1.0/24"}, Dir: "/etc/openvpn/server/gatehouse-tun0"}
	c, err := s.Config()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"dev tun0", "proto udp4", "port 1194", "server 10.9.0.0 255.255.255.0", "dh none", "tls-crypt /etc/openvpn/server/gatehouse-tun0/tls-crypt.key",
		"crl-verify", "data-ciphers AES-256-GCM", `push "dhcp-option DNS 10.9.0.1"`, `push "route 192.168.1.0 255.255.255.0"`, "tls-version-min 1.2"} {
		if !strings.Contains(c, want) {
			t.Errorf("server config lacks %q\n%s", want, c)
		}
	}
	if strings.Contains(c, "redirect-gateway") {
		t.Error("pushed a default route without being asked")
	}
	s.RouteAll = true
	c, _ = s.Config()
	if !strings.Contains(c, "redirect-gateway def1") {
		t.Error("route-all missing")
	}
	s.Subnet = "10.9.0.0/23"
	if c, _ = s.Config(); !strings.Contains(c, "255.255.254.0") {
		t.Errorf("mask wrong:\n%s", c)
	}
	s.Port = 0
	if _, err := s.Config(); err == nil {
		t.Error("port 0 accepted")
	}
	p := Profile{Host: "vpn.example.net", Port: 1194, Proto: "udp", CA: "CA\n", Cert: "CERT\n", Key: "KEY\n", TLSCrypt: "TC\n"}.Text()
	for _, want := range []string{"remote vpn.example.net 1194", "remote-cert-tls server", "<ca>\nCA\n</ca>", "<tls-crypt>\nTC\n</tls-crypt>"} {
		if !strings.Contains(p, want) {
			t.Errorf("profile lacks %q", want)
		}
	}
}

const goodProfile = `client
dev tun
proto udp
remote nl-1.provider.example 1194
remote nl-2.provider.example 443
resolv-retry infinite
nobind
persist-key
persist-tun
remote-cert-tls server
cipher AES-256-GCM
auth SHA512
auth-user-pass
redirect-gateway def1
dhcp-option DNS 10.8.0.1
verb 3
<ca>
-----BEGIN CERTIFICATE-----
AAAA
-----END CERTIFICATE-----
</ca>
<tls-crypt>
-----BEGIN OpenVPN Static key V1-----
00
-----END OpenVPN Static key V1-----
</tls-crypt>
`

func TestImportProfile(t *testing.T) {
	ip, err := ImportProfile(goodProfile)
	if err != nil {
		t.Fatal(err)
	}
	if !ip.NeedsAuth || ip.Remote != "nl-1.provider.example" {
		t.Errorf("%+v", ip)
	}
	for _, gone := range []string{"redirect-gateway", "dhcp-option", "dev tun", "auth-user-pass"} {
		if strings.Contains(ip.Config, gone) {
			t.Errorf("imported config still has %q", gone)
		}
	}
	for _, kept := range []string{"remote nl-2.provider.example 443", "auth SHA512", "<ca>", "<tls-crypt>", "cipher AES-256-GCM"} {
		if !strings.Contains(ip.Config, kept) {
			t.Errorf("imported config lost %q", kept)
		}
	}
	full := OVPNClientConfig(ip, "tun1", "/etc/openvpn/client/gatehouse-tun1.auth")
	for _, want := range []string{"dev tun1", "route-nopull", "script-security 1", "auth-user-pass /etc/openvpn/client/gatehouse-tun1.auth"} {
		if !strings.Contains(full, want) {
			t.Errorf("final config lacks %q", want)
		}
	}
	// Anything that could run a program or read another file is refused.
	for _, bad := range []string{
		"up /tmp/evil.sh", "down /bin/sh", "plugin /usr/lib/x.so", "tls-verify /bin/true", "route-up /x",
		"auth-user-pass /etc/shadow", "config /etc/passwd", "management 127.0.0.1 7505", "cd /", "chroot /tmp",
		"client-connect /x", "learn-address /x", "ipchange /x", "up-restart", "askpass /etc/shadow", "pkcs12 /x",
	} {
		_, err := ImportProfile(strings.Replace(goodProfile, "verb 3", bad, 1))
		if err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
	if _, err := ImportProfile("client\nremote x 1\n"); err == nil {
		t.Error("profile without a certificate accepted")
	}
	if _, err := ImportProfile(strings.Replace(goodProfile, "<tls-crypt>", "<connection>\nremote z 1\n</connection>\n<tls-crypt>", 1)); err == nil {
		t.Error("connection block accepted")
	}
}

// An ExpressVPN-style profile: old harmless lines must import, not be refused.
func TestImportProfileLegacyLines(t *testing.T) {
	p := strings.Replace(goodProfile, "remote-cert-tls server", "ns-cert-type server\nkeysize 256\nkey-method 2\nfast-io\ncomp-lzo\ntls-timeout 5\nhand-window 30\nreplay-window 64 15\nremote-cert-tls server", 1)
	ip, err := ImportProfile(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(ip.Config, "ns-cert-type") || strings.Contains(ip.Config, "keysize") || strings.Contains(ip.Config, "comp-lzo") {
		t.Errorf("legacy lines were kept:\n%s", ip.Config)
	}
	if n := strings.Count(ip.Config, "remote-cert-tls server"); n != 1 {
		t.Errorf("remote-cert-tls written %d times, want once", n)
	}
	for _, kept := range []string{"tls-timeout 5", "hand-window 30", "replay-window 64 15"} {
		if !strings.Contains(ip.Config, kept) {
			t.Errorf("lost %q", kept)
		}
	}
	// ns-cert-type alone becomes the modern check.
	ip, err = ImportProfile(strings.Replace(goodProfile, "remote-cert-tls server", "ns-cert-type server", 1))
	if err != nil || !strings.Contains(ip.Config, "remote-cert-tls server") || strings.Contains(ip.Config, "ns-cert-type") {
		t.Errorf("ns-cert-type not translated: %v\n%s", err, ip.Config)
	}
}

// Several refused settings are all named in one message.
func TestImportProfileNamesEveryRefusedLine(t *testing.T) {
	_, err := ImportProfile(strings.Replace(goodProfile, "verb 3", "up /tmp/x.sh\nplugin /x.so\nup /tmp/y.sh", 1))
	if err == nil {
		t.Fatal("accepted")
	}
	for _, want := range []string{"2 settings", "up", "plugin"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message %q lacks %q", err, want)
		}
	}
	if _, err := ImportProfile(strings.Replace(goodProfile, "verb 3", "up /x", 1)); err == nil || !strings.Contains(err.Error(), `"up"`) {
		t.Errorf("single refusal message changed: %v", err)
	}
}

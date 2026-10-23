// Package tlsutil creates the console's self-signed certificate on first start.
package tlsutil

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

// Ensure returns paths to a certificate and key in dir, creating them if missing or if
// the certificate expires within 30 days.
func Ensure(dir, hostname string) (certPath, keyPath string, err error) {
	certPath, keyPath = filepath.Join(dir, "console.crt"), filepath.Join(dir, "console.key")
	if c, e := tls.LoadX509KeyPair(certPath, keyPath); e == nil {
		if leaf, e := x509.ParseCertificate(c.Certificate[0]); e == nil && time.Until(leaf.NotAfter) > 30*24*time.Hour {
			return certPath, keyPath, nil
		}
	}
	if err = os.MkdirAll(dir, 0o750); err != nil {
		return
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return
	}
	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 100))
	if hostname == "" {
		hostname = "gatehouse"
	}
	tpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: hostname, Organization: []string{"Gatehouse"}},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(5, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{hostname, hostname + ".lan", "gatehouse", "gatehouse.lan", "localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}
	// Include every address the device has now, so the certificate matches how people reach it.
	if addrs, e := net.InterfaceAddrs(); e == nil {
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok && !n.IP.IsLinkLocalUnicast() {
				tpl.IPAddresses = append(tpl.IPAddresses, n.IP)
			}
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		return
	}
	kb, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return
	}
	if err = os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb}), 0o600); err != nil {
		return
	}
	err = os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644)
	return
}

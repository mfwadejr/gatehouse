package vpn

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"
)

// A small certificate authority for OpenVPN: ECDSA P-256 throughout. It only ever signs the
// server's own certificate and the certificates of devices the administrator adds.

// CA is a certificate authority held as PEM text.
type CA struct {
	CertPEM string
	KeyPEM  string // secret
}

func serial() (*big.Int, error) {
	return rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
}

func keyPEM(k *ecdsa.PrivateKey) (string, error) {
	der, err := x509.MarshalPKCS8PrivateKey(k)
	if err != nil {
		return "", err
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})), nil
}

func certPEM(der []byte) string {
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

// NewCA creates a authority valid for ten years.
func NewCA(now time.Time) (*CA, error) {
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	sn, err := serial()
	if err != nil {
		return nil, err
	}
	t := &x509.Certificate{
		SerialNumber:          sn,
		Subject:               pkix.Name{CommonName: "Gatehouse VPN CA"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLenZero:        true,
	}
	der, err := x509.CreateCertificate(rand.Reader, t, t, &k.PublicKey, k)
	if err != nil {
		return nil, err
	}
	kp, err := keyPEM(k)
	if err != nil {
		return nil, err
	}
	return &CA{CertPEM: certPEM(der), KeyPEM: kp}, nil
}

func (c *CA) parse() (*x509.Certificate, *ecdsa.PrivateKey, error) {
	cb, _ := pem.Decode([]byte(c.CertPEM))
	kb, _ := pem.Decode([]byte(c.KeyPEM))
	if cb == nil || kb == nil {
		return nil, nil, errors.New("the certificate authority is damaged")
	}
	cert, err := x509.ParseCertificate(cb.Bytes)
	if err != nil {
		return nil, nil, err
	}
	k, err := x509.ParsePKCS8PrivateKey(kb.Bytes)
	if err != nil {
		return nil, nil, err
	}
	ek, ok := k.(*ecdsa.PrivateKey)
	if !ok {
		return nil, nil, errors.New("the certificate authority key is not ECDSA")
	}
	return cert, ek, nil
}

// Issued is a certificate with its private key.
type Issued struct {
	CertPEM string
	KeyPEM  string // secret
	Serial  string // hex
}

func (c *CA) issue(cn string, server bool, now time.Time) (*Issued, error) {
	ca, cak, err := c.parse()
	if err != nil {
		return nil, err
	}
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	sn, err := serial()
	if err != nil {
		return nil, err
	}
	eku, years := x509.ExtKeyUsageClientAuth, 5
	if server {
		eku, years = x509.ExtKeyUsageServerAuth, 10
	}
	t := &x509.Certificate{
		SerialNumber: sn,
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.AddDate(years, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{eku},
	}
	der, err := x509.CreateCertificate(rand.Reader, t, ca, &k.PublicKey, cak)
	if err != nil {
		return nil, err
	}
	kp, err := keyPEM(k)
	if err != nil {
		return nil, err
	}
	return &Issued{CertPEM: certPEM(der), KeyPEM: kp, Serial: hex.EncodeToString(sn.Bytes())}, nil
}

// ServerCert issues the router's own certificate.
func (c *CA) ServerCert(now time.Time) (*Issued, error) {
	return c.issue("gatehouse-server", true, now)
}

// ClientCert issues a certificate for one device.
func (c *CA) ClientCert(name string, now time.Time) (*Issued, error) {
	if !ValidName(name) {
		return nil, errors.New("a device name can use letters, numbers, spaces, dots and dashes")
	}
	return c.issue(name, false, now)
}

// ValidName reports whether name is acceptable for a device or VPN.
func ValidName(name string) bool {
	if len(name) == 0 || len(name) > 40 {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == ' ', r == '.', r == '-', r == '_':
		default:
			return false
		}
	}
	return strings.TrimSpace(name) == name
}

// Revoked is one revoked certificate.
type Revoked struct {
	Serial string // hex
	At     time.Time
}

// CRL builds the revocation list. It stays valid for ten years so that an idle router can
// never lock everyone out by letting it expire; it is rebuilt whenever something is revoked.
func (c *CA) CRL(number int64, revoked []Revoked, now time.Time) (string, error) {
	ca, cak, err := c.parse()
	if err != nil {
		return "", err
	}
	var entries []x509.RevocationListEntry
	for _, r := range revoked {
		b, err := hex.DecodeString(r.Serial)
		if err != nil {
			return "", fmt.Errorf("bad serial %q", r.Serial)
		}
		entries = append(entries, x509.RevocationListEntry{SerialNumber: new(big.Int).SetBytes(b), RevocationTime: r.At})
	}
	der, err := x509.CreateRevocationList(rand.Reader, &x509.RevocationList{
		Number:                    big.NewInt(number),
		ThisUpdate:                now.Add(-time.Hour),
		NextUpdate:                now.AddDate(10, 0, 0),
		RevokedCertificateEntries: entries,
	}, ca, cak)
	if err != nil {
		return "", err
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "X509 CRL", Bytes: der})), nil
}

// StaticKey makes an OpenVPN static key (used with tls-crypt) in its file format.
func StaticKey() (string, error) {
	b := make([]byte, 256)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	h := hex.EncodeToString(b)
	var sb strings.Builder
	sb.WriteString("#\n# 2048 bit OpenVPN static key\n#\n-----BEGIN OpenVPN Static key V1-----\n")
	for i := 0; i < len(h); i += 32 {
		sb.WriteString(h[i:i+32] + "\n")
	}
	sb.WriteString("-----END OpenVPN Static key V1-----\n")
	return sb.String(), nil
}

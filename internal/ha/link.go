package ha

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"time"
)

// The link between the two units is HTTPS with certificates each unit makes for itself. Neither
// trusts a certificate authority: each unit pins the other's certificate fingerprint. The primary's
// fingerprint travels in the pairing code; the standby's reaches the primary during the join step,
// proved by a keyed hash that only someone holding the code can make.

// LinkPort is the TCP port the units talk on.
const LinkPort = 8444

// Identity is a unit's certificate for the link.
type Identity struct {
	CertPEM string `json:"cert"`
	KeyPEM  string `json:"key"`
	FP      string `json:"fp"`
}

// Fingerprint is the SHA-256 of a certificate, in hex.
func Fingerprint(der []byte) string { s := sha256.Sum256(der); return hex.EncodeToString(s[:]) }

// NewIdentity makes a certificate that lasts ten years.
func NewIdentity(host string) (Identity, error) {
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return Identity{}, err
	}
	sn, _ := rand.Int(rand.Reader, big.NewInt(1<<62))
	t := &x509.Certificate{SerialNumber: sn, Subject: pkix.Name{CommonName: "gatehouse-ha-" + host},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(10, 0, 0),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth}}
	der, err := x509.CreateCertificate(rand.Reader, t, t, &k.PublicKey, k)
	if err != nil {
		return Identity{}, err
	}
	kb, _ := x509.MarshalECPrivateKey(k)
	return Identity{CertPEM: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		KeyPEM: string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb})), FP: Fingerprint(der)}, nil
}

func (id Identity) pair() (tls.Certificate, error) {
	return tls.X509KeyPair([]byte(id.CertPEM), []byte(id.KeyPEM))
}

// Proof is what a unit holding the pairing code can say about its own fingerprint.
func Proof(secret, fp string) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte("gatehouse-ha-join:" + fp))
	return hex.EncodeToString(m.Sum(nil))
}

func proofOK(secret, fp, got string) bool {
	return subtle.ConstantTimeCompare([]byte(Proof(secret, fp)), []byte(got)) == 1
}

// Status is what a unit tells its partner about itself.
type Status struct {
	Version string `json:"version"`
	Active  bool   `json:"active"` // holds the shared addresses right now
	Role    string `json:"role"`   // primary or standby: how it was paired
	// Applying is true while this unit is still loading a copy of the settings it was just sent.
	Applying bool   `json:"applying,omitempty"`
	Prefer   string `json:"prefer,omitempty"` // which unit (by how it was paired) should be active when both are healthy
	Seq      int64  `json:"seq"`              // newest settings snapshot it has
	Time     int64  `json:"time"`
	// Update is what this unit's updater is doing ("idle" when nothing). ResultTo and Result say
	// how the last update ended: Result is "ok" or "failed", ResultTo the version it aimed for.
	Update   string `json:"update,omitempty"`
	Result   string `json:"result,omitempty"`
	ResultTo string `json:"result_to,omitempty"`
	// WanMACs are the hardware addresses on the internet ports, by connection name, reported by the
	// active unit for a warm standby to copy.
	WanMACs map[string]string `json:"wan_macs,omitempty"`
}

// Overview is what the primary tells a unit that is about to join.
type Overview struct {
	Roles   []PortRole `json:"roles"`
	Version string     `json:"version"`
}

// Link is the server side of the link.
type Link struct {
	Self   Identity
	Secret string
	// Peer returns the pinned fingerprint of the partner, empty while not paired.
	Peer func() string
	// OnJoin records a standby that proved it holds the code.
	OnJoin     func(fp string, port int) error
	OnSnapshot func(body []byte, seq int64, version, mapping string) error
	// OnPrefer records which unit is the Primary (the one that should hold the role when both are
	// healthy). It comes from the partner only.
	OnPrefer func(role string) error
	Status   func() Status
	Overview func() Overview
	// OnHandover makes this unit step aside; OnLeave unpairs it. Both come from the partner only.
	OnHandover func() error
	OnLeave    func() error
	// OnUpdate takes a signed update package from the active partner and installs it here.
	OnUpdate func(pkg []byte, sig, version string) error
}

func (l *Link) tlsConfig() (*tls.Config, error) {
	c, err := l.Self.pair()
	if err != nil {
		return nil, err
	}
	return &tls.Config{Certificates: []tls.Certificate{c}, MinVersion: tls.VersionTLS13, ClientAuth: tls.RequireAnyClientCert}, nil
}

// Serve listens until ctx ends.
func (l *Link) Serve(ctx context.Context, ln net.Listener) error {
	cfg, err := l.tlsConfig()
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: l.Handler(), TLSConfig: cfg, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 5 * time.Minute, WriteTimeout: time.Minute}
	go func() { <-ctx.Done(); srv.Close() }()
	err = srv.Serve(tls.NewListener(ln, cfg))
	if ctx.Err() != nil {
		return nil
	}
	return err
}

// Handler is the set of calls the partner may make.
func (l *Link) Handler() http.Handler {
	mux := http.NewServeMux()
	fp := func(r *http.Request) string {
		if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
			return ""
		}
		return Fingerprint(r.TLS.PeerCertificates[0].Raw)
	}
	// known: the partner we are paired with. proved: a caller that holds the pairing code.
	known := func(r *http.Request) bool {
		p := l.Peer()
		return p != "" && subtle.ConstantTimeCompare([]byte(p), []byte(fp(r))) == 1
	}
	proved := func(r *http.Request) bool {
		f := fp(r)
		return f != "" && l.Secret != "" && proofOK(l.Secret, f, r.Header.Get("X-GH-Proof"))
	}
	deny := func(w http.ResponseWriter) { http.Error(w, "not paired with you", http.StatusForbidden) }
	mux.HandleFunc("GET /ha/overview", func(w http.ResponseWriter, r *http.Request) {
		if !known(r) && !proved(r) {
			deny(w)
			return
		}
		json.NewEncoder(w).Encode(l.Overview())
	})
	mux.HandleFunc("POST /ha/join", func(w http.ResponseWriter, r *http.Request) {
		if !proved(r) {
			deny(w)
			return
		}
		var port int
		fmt.Sscan(r.Header.Get("X-GH-Port"), &port)
		if err := l.OnJoin(fp(r), port); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		w.Write([]byte("ok"))
	})
	mux.HandleFunc("GET /ha/status", func(w http.ResponseWriter, r *http.Request) {
		if !known(r) {
			deny(w)
			return
		}
		json.NewEncoder(w).Encode(l.Status())
	})
	mux.HandleFunc("POST /ha/handover", func(w http.ResponseWriter, r *http.Request) {
		if !known(r) || l.OnHandover == nil {
			deny(w)
			return
		}
		if err := l.OnHandover(); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		w.Write([]byte("ok"))
	})
	mux.HandleFunc("POST /ha/leave", func(w http.ResponseWriter, r *http.Request) {
		if !known(r) || l.OnLeave == nil {
			deny(w)
			return
		}
		if err := l.OnLeave(); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		w.Write([]byte("ok"))
	})
	mux.HandleFunc("POST /ha/update", func(w http.ResponseWriter, r *http.Request) {
		if !known(r) || l.OnUpdate == nil {
			deny(w)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 256<<20))
		if err != nil {
			http.Error(w, "unreadable", http.StatusBadRequest)
			return
		}
		if err := l.OnUpdate(body, r.Header.Get("X-GH-Sig"), r.Header.Get("X-GH-Version")); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		w.Write([]byte("ok"))
	})
	mux.HandleFunc("POST /ha/prefer", func(w http.ResponseWriter, r *http.Request) {
		if !known(r) || l.OnPrefer == nil {
			deny(w)
			return
		}
		if err := l.OnPrefer(r.Header.Get("X-GH-Prefer")); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		w.Write([]byte("ok"))
	})
	mux.HandleFunc("POST /ha/snapshot", func(w http.ResponseWriter, r *http.Request) {
		if !known(r) {
			deny(w)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 64<<20))
		if err != nil {
			http.Error(w, "unreadable", http.StatusBadRequest)
			return
		}
		var seq int64
		fmt.Sscan(r.Header.Get("X-GH-Seq"), &seq)
		if err := l.OnSnapshot(body, seq, r.Header.Get("X-GH-Version"), r.Header.Get("X-GH-Map")); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		w.Write([]byte("ok"))
	})
	return mux
}

// DialMap, when set, rewrites the address a call connects to. Tests use it to reach a unit that
// has a made-up address; it is never set in production.
var DialMap func(addr string) string

// Call makes one request to the partner, pinning its certificate. proof is the pairing secret when
// the partner does not know this unit yet (discovery and joining).
func Call(ctx context.Context, addr string, self Identity, pin string, method, path string, body []byte, headers map[string]string, secret string) ([]byte, error) {
	c, err := self.pair()
	if err != nil {
		return nil, err
	}
	cfg := &tls.Config{Certificates: []tls.Certificate{c}, MinVersion: tls.VersionTLS13, InsecureSkipVerify: true,
		VerifyPeerCertificate: func(raw [][]byte, _ [][]*x509.Certificate) error {
			if len(raw) == 0 || subtle.ConstantTimeCompare([]byte(Fingerprint(raw[0])), []byte(pin)) != 1 {
				return errors.New("the other unit is not the one this pairing code belongs to")
			}
			return nil
		}}
	hc := &http.Client{Timeout: 2 * time.Minute, Transport: &http.Transport{TLSClientConfig: cfg, DisableKeepAlives: true}}
	if DialMap != nil {
		addr = DialMap(addr)
	}
	req, err := http.NewRequestWithContext(ctx, method, "https://"+addr+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if secret != "" {
		req.Header.Set("X-GH-Proof", Proof(secret, self.FP))
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("the other unit answered: %s", string(bytes.TrimSpace(out)))
	}
	return out, nil
}

package dnssrv

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func okReply(req *dns.Msg) *dns.Msg {
	m := new(dns.Msg)
	m.SetReply(req)
	return m
}

func twoUpstreams() []*Upstream {
	return []*Upstream{
		{ID: 1, Name: "Slow but mine", Addr: "192.0.2.1:53", Proto: "udp", Position: 1},
		{ID: 2, Name: "Fast", Addr: "192.0.2.2:53", Proto: "udp", Position: 2},
	}
}

func TestOrderedModeAsksInTheListOrder(t *testing.T) {
	p := NewPool(nil)
	p.Set(twoUpstreams())
	var asked []string
	p.exchange = func(ctx context.Context, u *Upstream, req *dns.Msg) (*dns.Msg, error) {
		asked = append(asked, u.Name)
		return okReply(req), nil
	}
	// Teach the pool that the second server is much faster.
	p.ups[0].record(120*time.Millisecond, nil, time.Now())
	p.ups[1].record(5*time.Millisecond, nil, time.Now())

	q := new(dns.Msg)
	q.SetQuestion("example.org.", dns.TypeA)
	if _, _, _, err := p.Exchange(context.Background(), q); err != nil {
		t.Fatal(err)
	}
	if asked[0] != "Fast" {
		t.Fatalf("fastest-first should ask the fast one first, asked %v", asked)
	}

	p.SetOrdered(true)
	asked = nil
	if _, _, _, err := p.Exchange(context.Background(), q); err != nil {
		t.Fatal(err)
	}
	if asked[0] != "Slow but mine" {
		t.Fatalf("in-my-order should ask the first in the list first, asked %v", asked)
	}

	// The next one is used only when the one above fails.
	asked = nil
	p.exchange = func(ctx context.Context, u *Upstream, req *dns.Msg) (*dns.Msg, error) {
		asked = append(asked, u.Name)
		if u.Name == "Slow but mine" {
			return nil, errors.New("i/o timeout")
		}
		return okReply(req), nil
	}
	if _, up, _, err := p.Exchange(context.Background(), q); err != nil || up.Name != "Fast" {
		t.Fatalf("did not fall through to the next server: %v %v", up, err)
	}
	if strings.Join(asked, ",") != "Slow but mine,Fast" {
		t.Fatalf("asked %v", asked)
	}
}

func TestStatusSaysWhyAServerIsSlowOrDown(t *testing.T) {
	p := NewPool(nil)
	p.Set(twoUpstreams())
	now := time.Now()
	p.ups[0].record(195*time.Millisecond, nil, now)
	st := p.Status()[0]
	if st.State != "degraded" || !strings.Contains(st.Reason, "195 ms") || !strings.Contains(st.Reason, "150 ms") {
		t.Fatalf("degraded reason: %+v", st)
	}
	for i := 0; i < 3; i++ {
		p.ups[1].record(0, errors.New("read tcp: i/o timeout"), now)
	}
	st = p.Status()[1]
	if st.State != "down" || !strings.Contains(st.Reason, "no reply in time") || st.RetryIn <= 0 || st.Failures != 3 {
		t.Fatalf("down reason: %+v", st)
	}
}

func selfSigned(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "dns.test"}, DNSNames: []string{"dns.test"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, _ := x509.ParseCertificate(der)
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, pool
}

type countListener struct {
	net.Listener
	n *atomic.Int64
}

func (c countListener) Accept() (net.Conn, error) {
	conn, err := c.Listener.Accept()
	if err == nil {
		c.n.Add(1)
	}
	return conn, err
}

func TestEncryptedConnectionsAreReused(t *testing.T) {
	cert, roots := selfSigned(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var accepted atomic.Int64
	srv := &dns.Server{Listener: tls.NewListener(countListener{ln, &accepted}, &tls.Config{Certificates: []tls.Certificate{cert}}),
		Handler: dns.HandlerFunc(func(w dns.ResponseWriter, r *dns.Msg) { w.WriteMsg(okReply(r)) })}
	go srv.ActivateAndServe()
	defer srv.Shutdown()

	p := NewPool(nil)
	p.roots = roots
	p.Set([]*Upstream{{ID: 1, Name: "dot", Addr: ln.Addr().String(), Proto: "tls", TLSName: "dns.test", Position: 1}})
	q := new(dns.Msg)
	q.SetQuestion("example.org.", dns.TypeA)
	for i := 0; i < 6; i++ {
		if _, _, _, err := p.Exchange(context.Background(), q); err != nil {
			t.Fatalf("query %d: %v", i, err)
		}
	}
	if got := accepted.Load(); got != 1 {
		t.Fatalf("six queries should share one connection, the server saw %d", got)
	}

	// A connection the server closed while idle is replaced without the person noticing.
	p.ups[0].mu.Lock()
	for _, ic := range p.ups[0].idle {
		ic.c.Close()
	}
	p.ups[0].mu.Unlock()
	if _, _, _, err := p.Exchange(context.Background(), q); err != nil {
		t.Fatalf("a stale connection must be retried on a fresh one: %v", err)
	}
	if got := accepted.Load(); got != 2 {
		t.Fatalf("expected one replacement connection, server saw %d", got)
	}

	// The Test button keeps connect time and answer time apart.
	r := p.Test(context.Background(), p.ups[0])
	if !r.OK || r.ConnectMS <= 0 || r.AnswerMS <= 0 {
		t.Fatalf("test result: %+v", r)
	}
	// An upstream that is removed gives its connections up.
	old := p.ups[0]
	p.Set(nil)
	old.mu.Lock()
	n := len(old.idle)
	old.mu.Unlock()
	if n != 0 {
		t.Fatalf("a removed server still holds %d open connections", n)
	}
}

func TestTestExplainsASlowConnectionAndAFailure(t *testing.T) {
	p := NewPool(nil)
	u := &Upstream{ID: 1, Name: "nothing here", Addr: "127.0.0.1:1", Proto: "udp", Position: 1}
	p.Set([]*Upstream{u})
	p.timeout = 300 * time.Millisecond
	r := p.Test(context.Background(), u)
	if r.OK || r.Error == "" || !strings.Contains(r.Verdict, "did not answer") {
		t.Fatalf("a dead server must be reported: %+v", r)
	}
}

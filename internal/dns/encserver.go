package dnssrv

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/miekg/dns"
)

// EncServer lets devices use this resolver over encrypted DNS: DNS-over-TLS (RFC 7858) and
// DNS-over-HTTPS (RFC 8484). Every question goes through the same pipeline as an ordinary lookup, so
// the lists, the cache, the rate limit and the query log all apply. It is off until it is started, and
// the firewall decides which networks can reach it.
type EncServer struct {
	s    *Server
	cert atomic.Pointer[tls.Certificate]

	mu   sync.Mutex
	dot  *dns.Server
	doh  *http.Server
	dotL net.Listener
	dohL net.Listener
}

// NewEncServer makes an encrypted-DNS front end for a resolver.
func NewEncServer(s *Server) *EncServer { return &EncServer{s: s} }

// SetCertificate swaps the certificate used for new connections.
func (e *EncServer) SetCertificate(c *tls.Certificate) { e.cert.Store(c) }

func (e *EncServer) tlsConfig() *tls.Config {
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		NextProtos: []string{"h2", "http/1.1", "dot"},
		GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
			if c := e.cert.Load(); c != nil {
				return c, nil
			}
			return nil, errors.New("no certificate")
		},
	}
}

// Start listens on the two addresses. Either may fail on its own (a port in use, say); the errors say
// which. Anything already running is stopped first.
func (e *EncServer) Start(dotAddr, dohAddr string) (dotErr, dohErr error) {
	e.Stop()
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.cert.Load() == nil {
		err := errors.New("there is no certificate yet")
		return err, err
	}
	if l, err := tls.Listen("tcp", dotAddr, e.tlsConfig()); err != nil {
		dotErr = err
	} else {
		srv := &dns.Server{Listener: l, Handler: e.s, IdleTimeout: func() time.Duration { return 30 * time.Second }, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second}
		e.dot, e.dotL = srv, l
		go srv.ActivateAndServe()
	}
	if l, err := tls.Listen("tcp", dohAddr, e.tlsConfig()); err != nil {
		dohErr = err
	} else {
		mux := http.NewServeMux()
		mux.HandleFunc("/dns-query", e.serveDoH)
		srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second}
		e.doh, e.dohL = srv, l
		go srv.Serve(l)
	}
	return dotErr, dohErr
}

// Stop closes both listeners.
func (e *EncServer) Stop() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.dot != nil {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		e.dot.ShutdownContext(ctx)
		cancel()
		e.dot = nil
	}
	if e.dotL != nil {
		e.dotL.Close()
		e.dotL = nil
	}
	if e.doh != nil {
		e.doh.Close()
		e.doh = nil
	}
	if e.dohL != nil {
		e.dohL.Close()
		e.dohL = nil
	}
}

// Listening says which of the two listeners are running.
func (e *EncServer) Listening() (dot, doh bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.dot != nil, e.doh != nil
}

func (e *EncServer) serveDoH(w http.ResponseWriter, r *http.Request) {
	var wire []byte
	switch r.Method {
	case http.MethodGet:
		b, err := base64.RawURLEncoding.DecodeString(r.URL.Query().Get("dns"))
		if err != nil || len(b) == 0 {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		wire = b
	case http.MethodPost:
		if r.Header.Get("Content-Type") != "application/dns-message" {
			http.Error(w, "unsupported media type", http.StatusUnsupportedMediaType)
			return
		}
		b, err := io.ReadAll(io.LimitReader(r.Body, 65536))
		if err != nil || len(b) == 0 {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		wire = b
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	req := new(dns.Msg)
	if err := req.Unpack(wire); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	resp := e.s.handle(ip, req, time.Now(), false)
	if resp == nil {
		http.Error(w, "no answer", http.StatusServiceUnavailable)
		return
	}
	out, err := resp.Pack()
	if err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	age := uint32(0)
	for i, rr := range resp.Answer {
		if t := rr.Header().Ttl; i == 0 || t < age {
			age = t
		}
	}
	w.Header().Set("Content-Type", "application/dns-message")
	w.Header().Set("Cache-Control", "max-age="+strconv.FormatUint(uint64(age), 10))
	w.Write(out)
}

// DoTAddr and DoHAddr give the address each listener is on ("" when it is not running).
func (e *EncServer) DoTAddr() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.dotL == nil {
		return ""
	}
	return e.dotL.Addr().String()
}

func (e *EncServer) DoHAddr() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.dohL == nil {
		return ""
	}
	return e.dohL.Addr().String()
}

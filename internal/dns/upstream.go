package dnssrv

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
)

// Upstream is one resolver the server forwards to.
type Upstream struct {
	ID      int64
	Name    string
	Addr    string // host:port for udp/tls, URL for https
	Proto   string // udp, tls, https
	TLSName string

	mu        sync.Mutex
	ewma      time.Duration
	samples   int
	fails     int
	downUntil time.Time
	lastErr   string
	lastOK    time.Time
	queries   uint64
}

// UpstreamStatus is the health snapshot shown in the console.
type UpstreamStatus struct {
	ID        int64   `json:"id"`
	Name      string  `json:"name"`
	Addr      string  `json:"addr"`
	Proto     string  `json:"proto"`
	State     string  `json:"state"` // healthy, degraded, down
	LatencyMS float64 `json:"latency_ms"`
	Queries   uint64  `json:"queries"`
	LastError string  `json:"last_error,omitempty"`
}

const degradedAbove = 150 * time.Millisecond

func (u *Upstream) record(d time.Duration, err error, now time.Time) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.queries++
	if err != nil {
		u.fails++
		u.lastErr = err.Error()
		if u.fails >= 2 {
			// Back off: 5s, 10s, ... capped at 60s.
			backoff := time.Duration(u.fails-1) * 5 * time.Second
			if backoff > time.Minute {
				backoff = time.Minute
			}
			u.downUntil = now.Add(backoff)
		}
		return
	}
	u.fails = 0
	u.lastErr = ""
	u.lastOK = now
	u.downUntil = time.Time{}
	if u.samples == 0 {
		u.ewma = d
	} else {
		u.ewma = (u.ewma*7 + d*3) / 10
	}
	u.samples++
}

func (u *Upstream) status(now time.Time) UpstreamStatus {
	u.mu.Lock()
	defer u.mu.Unlock()
	st := UpstreamStatus{ID: u.ID, Name: u.Name, Addr: u.Addr, Proto: u.Proto, Queries: u.queries,
		LatencyMS: float64(u.ewma.Microseconds()) / 1000, LastError: u.lastErr}
	switch {
	case now.Before(u.downUntil) || (u.fails >= 2 && u.samples == 0):
		st.State = "down"
	case u.ewma > degradedAbove:
		st.State = "degraded"
	default:
		st.State = "healthy"
	}
	return st
}

func (u *Upstream) usable(now time.Time) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return !now.Before(u.downUntil)
}

func (u *Upstream) score() time.Duration {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.samples == 0 {
		return 0 // untried upstreams get a chance first
	}
	return u.ewma
}

// Pool picks the best upstream and fails over to the next on error.
type Pool struct {
	mu        sync.RWMutex
	ups       []*Upstream
	timeout   time.Duration
	now       func() time.Time
	bootstrap []string
	doh       *http.Client
	exchange  func(ctx context.Context, u *Upstream, req *dns.Msg) (*dns.Msg, error)
}

func NewPool(bootstrap []string) *Pool {
	p := &Pool{timeout: 3 * time.Second, now: time.Now, bootstrap: bootstrap}
	p.exchange = p.realExchange
	p.doh = p.newDoHClient()
	return p
}

func (p *Pool) newDoHClient() *http.Client {
	resolver := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			d := net.Dialer{Timeout: 2 * time.Second}
			if len(p.bootstrap) == 0 {
				return d.DialContext(ctx, network, "1.1.1.1:53")
			}
			return d.DialContext(ctx, network, net.JoinHostPort(p.bootstrap[0], "53"))
		},
	}
	dialer := &net.Dialer{Timeout: 3 * time.Second, Resolver: resolver}
	return &http.Client{
		Timeout: 4 * time.Second,
		Transport: &http.Transport{
			DialContext:         dialer.DialContext,
			TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
			MaxIdleConnsPerHost: 4,
			IdleConnTimeout:     60 * time.Second,
			ForceAttemptHTTP2:   true,
		},
	}
}

// Set replaces the upstream list, keeping health data for unchanged entries.
func (p *Pool) Set(list []*Upstream) {
	p.mu.Lock()
	defer p.mu.Unlock()
	old := map[string]*Upstream{}
	for _, u := range p.ups {
		old[u.Proto+"|"+u.Addr] = u
	}
	for i, u := range list {
		if o, ok := old[u.Proto+"|"+u.Addr]; ok {
			o.ID, o.Name, o.TLSName = u.ID, u.Name, u.TLSName
			list[i] = o
		}
	}
	p.ups = list
}

// Status returns the health of every upstream.
func (p *Pool) Status() []UpstreamStatus {
	p.mu.RLock()
	defer p.mu.RUnlock()
	now := p.now()
	out := make([]UpstreamStatus, 0, len(p.ups))
	for _, u := range p.ups {
		out = append(out, u.status(now))
	}
	return out
}

var errNoUpstream = errors.New("no upstream available")

// Exchange sends the request to upstreams in order of measured latency until one answers.
func (p *Pool) Exchange(ctx context.Context, req *dns.Msg) (*dns.Msg, *Upstream, time.Duration, error) {
	p.mu.RLock()
	ups := append([]*Upstream(nil), p.ups...)
	p.mu.RUnlock()
	now := p.now()
	var avail, down []*Upstream
	for _, u := range ups {
		if u.usable(now) {
			avail = append(avail, u)
		} else {
			down = append(down, u)
		}
	}
	sort.SliceStable(avail, func(i, j int) bool { return avail[i].score() < avail[j].score() })
	order := append(avail, down...) // last resort: try ones marked down
	if len(order) == 0 {
		return nil, nil, 0, errNoUpstream
	}
	var lastErr error
	deadline := time.Now().Add(p.timeout * 2)
	for _, u := range order {
		if time.Now().After(deadline) {
			break
		}
		cctx, cancel := context.WithTimeout(ctx, p.timeout)
		start := time.Now()
		resp, err := p.exchange(cctx, u, req.Copy())
		cancel()
		d := time.Since(start)
		u.record(d, err, p.now())
		if err == nil {
			return resp, u, d, nil
		}
		lastErr = err
	}
	return nil, nil, 0, fmt.Errorf("all upstreams failed: %w", lastErr)
}

func (p *Pool) realExchange(ctx context.Context, u *Upstream, req *dns.Msg) (*dns.Msg, error) {
	switch u.Proto {
	case "https":
		return p.exchangeDoH(ctx, u, req)
	case "tls":
		c := &dns.Client{Net: "tcp-tls", Timeout: p.timeout, TLSConfig: &tls.Config{ServerName: u.TLSName, MinVersion: tls.VersionTLS12}}
		resp, _, err := c.ExchangeContext(ctx, req, u.Addr)
		return resp, err
	default:
		c := &dns.Client{Net: "udp", Timeout: p.timeout, UDPSize: 1232}
		resp, _, err := c.ExchangeContext(ctx, req, u.Addr)
		if err == nil && resp.Truncated {
			tc := &dns.Client{Net: "tcp", Timeout: p.timeout}
			resp, _, err = tc.ExchangeContext(ctx, req, u.Addr)
		}
		return resp, err
	}
}

func (p *Pool) exchangeDoH(ctx context.Context, u *Upstream, req *dns.Msg) (*dns.Msg, error) {
	req.Id = 0 // RFC 8484 recommends ID 0 for cache friendliness
	wire, err := req.Pack()
	if err != nil {
		return nil, err
	}
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, u.Addr, bytes.NewReader(wire))
	if err != nil {
		return nil, err
	}
	hr.Header.Set("Content-Type", "application/dns-message")
	hr.Header.Set("Accept", "application/dns-message")
	resp, err := p.doh.Do(hr)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("doh status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 65535))
	if err != nil {
		return nil, err
	}
	out := new(dns.Msg)
	if err := out.Unpack(body); err != nil {
		return nil, err
	}
	return out, nil
}

// Probe sends a cheap query to every upstream so health and latency stay current even
// when there is no traffic.
func (p *Pool) Probe(ctx context.Context) {
	p.mu.RLock()
	ups := append([]*Upstream(nil), p.ups...)
	p.mu.RUnlock()
	var wg sync.WaitGroup
	for _, u := range ups {
		wg.Add(1)
		go func(u *Upstream) {
			defer wg.Done()
			m := new(dns.Msg)
			m.SetQuestion(".", dns.TypeNS)
			cctx, cancel := context.WithTimeout(ctx, p.timeout)
			defer cancel()
			start := time.Now()
			_, err := p.exchange(cctx, u, m)
			u.record(time.Since(start), err, p.now())
		}(u)
	}
	wg.Wait()
}

// ValidateUpstream checks an address before it is stored.
func ValidateUpstream(proto, addr, tlsName string) error {
	switch proto {
	case "udp", "tls":
		host, port, err := net.SplitHostPort(addr)
		if err != nil || host == "" || port == "" {
			return errors.New("address must be host:port, for example 1.1.1.1:53")
		}
		if proto == "tls" && net.ParseIP(host) != nil && tlsName == "" {
			return errors.New("DNS-over-TLS to an IP address needs the server name for certificate checks")
		}
	case "https":
		u, err := url.Parse(addr)
		if err != nil || u.Scheme != "https" || u.Host == "" {
			return errors.New("DNS-over-HTTPS needs an https:// URL")
		}
		if strings.Contains(u.Host, " ") {
			return errors.New("invalid host")
		}
	default:
		return errors.New("protocol must be udp, tls or https")
	}
	return nil
}

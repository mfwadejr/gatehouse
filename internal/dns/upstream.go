package dnssrv

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
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
	// Position is the place in the person's own list (1 is first). In "in my order" mode it
	// decides which server is asked first.
	Position int

	mu        sync.Mutex
	idle      []idleConn // kept-open encrypted connections, newest last
	lastFail  time.Time
	failures  uint64
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
	Position  int     `json:"position"`
	// Reason says in plain words why a server is slow or down.
	Reason string `json:"reason,omitempty"`
	// Samples is how many answers the average is built from; Failures how many queries failed in all.
	Samples  int    `json:"samples"`
	Failures uint64 `json:"failures"`
	// LastOK and LastFail are unix seconds; zero means never.
	LastOK   int64 `json:"last_ok"`
	LastFail int64 `json:"last_fail"`
	// RetryIn is the seconds until a server marked down is tried again.
	RetryIn int `json:"retry_in,omitempty"`
	// Reused says encrypted connections are kept open between queries.
	Reused bool `json:"reused"`
}

const degradedAbove = 150 * time.Millisecond

// idleConn is an open encrypted connection waiting for the next query.
type idleConn struct {
	c  *dns.Conn
	at time.Time
}

const (
	idleKeep = 20 * time.Second // servers close idle connections soon; do not trust older ones
	idleMax  = 3
)

func (u *Upstream) record(d time.Duration, err error, now time.Time) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.queries++
	if err != nil {
		u.fails++
		u.failures++
		u.lastFail = now
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
		LatencyMS: float64(u.ewma.Microseconds()) / 1000, LastError: u.lastErr, Position: u.Position,
		Samples: u.samples, Failures: u.failures, Reused: u.Proto == "tls"}
	if !u.lastOK.IsZero() {
		st.LastOK = u.lastOK.Unix()
	}
	if !u.lastFail.IsZero() {
		st.LastFail = u.lastFail.Unix()
	}
	switch {
	case now.Before(u.downUntil) || (u.fails >= 2 && u.samples == 0):
		st.State = "down"
		if now.Before(u.downUntil) {
			st.RetryIn = int(u.downUntil.Sub(now).Seconds()) + 1
		}
		st.Reason = fmt.Sprintf("Did not answer the last %d queries: %s", u.fails, friendlyErr(u.lastErr))
	case u.ewma > degradedAbove:
		st.State = "degraded"
		st.Reason = fmt.Sprintf("Answers take %d ms on average, slower than the %d ms Gatehouse treats as slow. It still works, and is tried after faster ones.", u.ewma.Milliseconds(), degradedAbove.Milliseconds())
		if u.Proto != "udp" && u.Proto != "" {
			st.Reason += " A slow or mobile internet link (a hotspot, for example) makes every encrypted lookup slower."
		}
	case u.samples == 0:
		st.State = "healthy"
		st.Reason = "Not asked yet."
	default:
		st.State = "healthy"
	}
	return st
}

// friendlyErr turns a network error into something a person can act on.
func friendlyErr(e string) string {
	l := strings.ToLower(e)
	switch {
	case e == "":
		return "no reply"
	case strings.Contains(l, "i/o timeout") || strings.Contains(l, "deadline exceeded") || strings.Contains(l, "timeout"):
		return "no reply in time (the server or the connection to it is not answering)"
	case strings.Contains(l, "refused"):
		return "the server refused the connection"
	case strings.Contains(l, "no route") || strings.Contains(l, "unreachable"):
		return "the server cannot be reached from this connection"
	case strings.Contains(l, "certificate") || strings.Contains(l, "x509"):
		return "the server's certificate did not match its name (" + e + ")"
	case strings.Contains(l, "no such host"):
		return "the server's name could not be looked up"
	}
	return e
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
	ordered   atomic.Bool    // ask the servers in the person's own order instead of fastest first
	roots     *x509.CertPool // for tests: extra certificate roots
	dials     atomic.Int64   // how many new encrypted connections were opened (for tests)
}

// SetOrdered chooses how the servers are tried. False (the default) asks the fastest first;
// true asks them in the order of the list and moves on only when one fails.
func (p *Pool) SetOrdered(v bool) { p.ordered.Store(v) }

// Ordered reports whether the person's own order is used.
func (p *Pool) Ordered() bool { return p.ordered.Load() }

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
			o.ID, o.Name, o.TLSName, o.Position = u.ID, u.Name, u.TLSName, u.Position
			list[i] = o
		}
	}
	keep := map[*Upstream]bool{}
	for _, u := range list {
		keep[u] = true
	}
	for _, u := range p.ups {
		if !keep[u] {
			u.closeIdle()
		}
	}
	p.ups = list
}

// closeIdle drops every kept-open connection.
func (u *Upstream) closeIdle() {
	u.mu.Lock()
	idle := u.idle
	u.idle = nil
	u.mu.Unlock()
	for _, ic := range idle {
		ic.c.Close()
	}
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
	if p.ordered.Load() {
		// The person's own order: the list position decides, speed does not.
		sort.SliceStable(avail, func(i, j int) bool { return avail[i].Position < avail[j].Position })
		sort.SliceStable(down, func(i, j int) bool { return down[i].Position < down[j].Position })
	} else {
		sort.SliceStable(avail, func(i, j int) bool { return avail[i].score() < avail[j].score() })
	}
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
		return p.exchangeTLS(ctx, u, req)
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

func (p *Pool) tlsClient() *dns.Client {
	return &dns.Client{Net: "tcp-tls", Timeout: p.timeout}
}

func (p *Pool) tlsConfig(u *Upstream) *tls.Config {
	return &tls.Config{ServerName: u.TLSName, MinVersion: tls.VersionTLS12, RootCAs: p.roots}
}

// takeConn hands out a kept-open connection that is still young enough to trust.
func (u *Upstream) takeConn(now time.Time) *dns.Conn {
	u.mu.Lock()
	defer u.mu.Unlock()
	for len(u.idle) > 0 {
		ic := u.idle[len(u.idle)-1]
		u.idle = u.idle[:len(u.idle)-1]
		if now.Sub(ic.at) <= idleKeep {
			return ic.c
		}
		ic.c.Close()
	}
	return nil
}

func (u *Upstream) putConn(c *dns.Conn, now time.Time) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if len(u.idle) >= idleMax {
		c.Close()
		return
	}
	u.idle = append(u.idle, idleConn{c: c, at: now})
}

// exchangeTLS asks over an encrypted connection, reusing one kept open from an earlier query
// when it can. Opening a connection costs several round trips (connect, then the encryption
// handshake), which on a slow link is far longer than the question itself.
func (p *Pool) exchangeTLS(ctx context.Context, u *Upstream, req *dns.Msg) (*dns.Msg, error) {
	cl := p.tlsClient()
	cl.TLSConfig = p.tlsConfig(u)
	for attempt := 0; ; attempt++ {
		conn := u.takeConn(p.now())
		reused := conn != nil
		if conn == nil {
			var err error
			conn, err = cl.DialContext(ctx, u.Addr)
			if err != nil {
				return nil, err
			}
			p.dials.Add(1)
		}
		resp, _, err := cl.ExchangeWithConnContext(ctx, req, conn)
		if err != nil {
			conn.Close()
			if reused && attempt == 0 {
				continue // the server probably closed it while idle; try once on a fresh one
			}
			return nil, err
		}
		u.putConn(conn, p.now())
		return resp, nil
	}
}

func (p *Pool) exchangeDoH(ctx context.Context, u *Upstream, req *dns.Msg) (*dns.Msg, error) {
	return doDoH(ctx, p.doh, u, req)
}

func doDoH(ctx context.Context, client *http.Client, u *Upstream, req *dns.Msg) (*dns.Msg, error) {
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
	resp, err := client.Do(hr)
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

// TestResult is what asking one server right now showed, with the time to connect and the time
// to answer kept apart, because they have different causes.
type TestResult struct {
	OK        bool    `json:"ok"`
	ConnectMS float64 `json:"connect_ms"` // opening the connection (and the encryption handshake); 0 for plain UDP
	AnswerMS  float64 `json:"answer_ms"`  // the question and the answer on that connection
	Error     string  `json:"error,omitempty"`
	Verdict   string  `json:"verdict"`
}

// Get finds a server by its id.
func (p *Pool) Get(id int64) *Upstream {
	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, u := range p.ups {
		if u.ID == id {
			return u
		}
	}
	return nil
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

// Test asks one server a cheap question over a fresh connection and reports where the time went.
func (p *Pool) Test(ctx context.Context, u *Upstream) TestResult {
	ctx, cancel := context.WithTimeout(ctx, p.timeout*2)
	defer cancel()
	q := new(dns.Msg)
	q.SetQuestion(".", dns.TypeNS)
	var r TestResult
	var connect, answer time.Duration
	var err error
	switch u.Proto {
	case "tls":
		cl := p.tlsClient()
		cl.TLSConfig = p.tlsConfig(u)
		t0 := time.Now()
		var conn *dns.Conn
		conn, err = cl.DialContext(ctx, u.Addr)
		connect = time.Since(t0)
		if err == nil {
			p.dials.Add(1)
			t1 := time.Now()
			_, _, err = cl.ExchangeWithConnContext(ctx, q, conn)
			answer = time.Since(t1)
			if err == nil {
				u.putConn(conn, p.now())
			} else {
				conn.Close()
			}
		}
	case "https":
		client := p.newDoHClient()
		defer client.CloseIdleConnections()
		var ready time.Time
		t0 := time.Now()
		tctx := httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
			GotConn: func(httptrace.GotConnInfo) { ready = time.Now() },
		})
		_, err = doDoH(tctx, client, u, q)
		total := time.Since(t0)
		if !ready.IsZero() {
			connect = ready.Sub(t0)
		}
		answer = total - connect
	default:
		cl := &dns.Client{Net: "udp", Timeout: p.timeout, UDPSize: 1232}
		t0 := time.Now()
		_, _, err = cl.ExchangeContext(ctx, q, u.Addr)
		answer = time.Since(t0)
	}
	r.ConnectMS, r.AnswerMS = ms(connect), ms(answer)
	if err != nil {
		r.Error = friendlyErr(err.Error())
		r.Verdict = "The server did not answer: " + r.Error + "."
		u.record(0, err, p.now())
		return r
	}
	r.OK = true
	u.record(answer, nil, p.now())
	switch {
	case connect > 0 && connect > 3*answer && connect > 100*time.Millisecond:
		r.Verdict = "The server itself answers quickly, but opening the connection is slow. That points at the internet link, not the server. Gatehouse keeps connections open to avoid paying this for every lookup."
	case answer > degradedAbove:
		r.Verdict = "This server is slow to answer from here. Another server may suit this connection better."
	default:
		r.Verdict = "This server is answering normally."
	}
	return r
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

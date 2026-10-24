package dnssrv

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/miekg/dns"
)

// Options configure the resolver. They can be replaced while it runs with Apply.
type Options struct {
	BlockMode   string  // null (0.0.0.0 / ::), nxdomain, refused
	LocalDomain string  // e.g. "lan"
	CacheSize   int     // entries
	RateLimit   float64 // queries per second per client, 0 disables
	RateBurst   float64
	BlockTTL    uint32
	Canary      bool // answer NXDOMAIN for use-application-dns.net so browsers keep using system DNS
}

// DefaultOptions are used until the configuration is loaded.
func DefaultOptions() Options {
	return Options{BlockMode: "null", LocalDomain: "lan", CacheSize: 20000, RateLimit: 200, RateBurst: 400, BlockTTL: 60, Canary: true}
}

// Server is the DNS resolver: local names, filtering, cache, forwarding, logging.
type Server struct {
	opts  atomic.Pointer[Options]
	pool  *Pool
	cache *Cache
	rl    *RateLimiter
	eng   engineHolder
	local localHolder
	dir   Directory
	names Names
	qlog  *QueryLog
	stats *Stats

	// OnRateLimited is called at most once per client per minute.
	OnRateLimited func(client string)
	// OnAnswer is told about every successful answer that came from an upstream server or the
	// cache, so the firewall can follow the addresses of domains that rules refer to.
	OnAnswer  func(qname string, answers []dns.RR)
	limitedAt sync.Map // client -> unix minute

	mu      sync.Mutex
	servers []*dns.Server
}

// NewServer builds a resolver. dir, names and qlog may be nil in tests.
func NewServer(o Options, pool *Pool, dir Directory, names Names, qlog *QueryLog) *Server {
	s := &Server{pool: pool, cache: NewCache(o.CacheSize), rl: NewRateLimiter(o.RateLimit, o.RateBurst),
		dir: dir, names: names, qlog: qlog, stats: NewStats()}
	s.opts.Store(&o)
	return s
}

func (s *Server) Pool() *Pool                 { return s.pool }
func (s *Server) Cache() *Cache               { return s.cache }
func (s *Server) Stats() *Stats               { return s.stats }
func (s *Server) QueryLog() *QueryLog         { return s.qlog }
func (s *Server) Options() Options            { return *s.opts.Load() }
func (s *Server) engineHolder() *engineHolder { return &s.eng }

// NewManager returns a blocklist manager wired to this server's engine.
func (s *Server) NewManager(m *Manager) *Manager { m.eng = &s.eng; return m }

// Apply replaces the options, rate limits and local records.
func (s *Server) Apply(o Options, recs []LocalRecord) {
	s.opts.Store(&o)
	s.rl.Configure(o.RateLimit, o.RateBurst)
	s.local.p.Store(buildLocal(o.LocalDomain, recs))
	s.cache.Flush()
}

// SetLocalRecords swaps the administrator-defined records.
func (s *Server) SetLocalRecords(recs []LocalRecord) {
	s.local.p.Store(buildLocal(s.Options().LocalDomain, recs))
	s.cache.Flush()
}

func clientIP(w dns.ResponseWriter) string {
	switch a := w.RemoteAddr().(type) {
	case *net.UDPAddr:
		return a.IP.String()
	case *net.TCPAddr:
		return a.IP.String()
	}
	h, _, err := net.SplitHostPort(w.RemoteAddr().String())
	if err != nil {
		return w.RemoteAddr().String()
	}
	return h
}

// ServeDNS implements dns.Handler.
func (s *Server) ServeDNS(w dns.ResponseWriter, r *dns.Msg) {
	start := time.Now()
	resp := s.handle(clientIP(w), r, start, isUDP(w))
	if resp != nil {
		_ = w.WriteMsg(resp)
	}
}

func isUDP(w dns.ResponseWriter) bool {
	_, ok := w.RemoteAddr().(*net.UDPAddr)
	return ok
}

// Resolve runs the full pipeline for a request from the given client address.
// It is what ServeDNS calls and what tests use.
func (s *Server) Resolve(client string, r *dns.Msg) *dns.Msg {
	return s.handle(client, r, time.Now(), false)
}

func (s *Server) handle(client string, r *dns.Msg, start time.Time, udp bool) *dns.Msg {
	o := s.opts.Load()
	if len(r.Question) != 1 || r.Opcode != dns.OpcodeQuery {
		m := new(dns.Msg)
		m.SetRcode(r, dns.RcodeFormatError)
		if r.Opcode != dns.OpcodeQuery {
			m.SetRcode(r, dns.RcodeNotImplemented)
		}
		return m
	}
	q := r.Question[0]
	q.Name = strings.ToLower(q.Name)

	var ci ClientInfo
	if s.dir != nil {
		ci = s.dir.Client(client)
	} else {
		ci = ClientInfo{Filtering: true}
	}

	entry := LogEntry{TS: start.Unix(), Client: client, ClientName: ci.Name,
		QName: strings.TrimSuffix(q.Name, "."), QType: dns.TypeToString[q.Qtype]}
	finish := func(resp *dns.Msg, out Outcome, rule, upstream string) *dns.Msg {
		lat := time.Since(start)
		rc := dns.RcodeSuccess
		if resp != nil {
			rc = resp.Rcode
		}
		entry.Result, entry.Rule, entry.Upstream = string(out), rule, upstream
		entry.RCode = dns.RcodeToString[rc]
		entry.LatencyUS = lat.Microseconds()
		s.stats.Record(out, lat, rc == dns.RcodeNameError)
		if s.qlog != nil {
			s.qlog.Add(entry)
		}
		if s.OnAnswer != nil && resp != nil && rc == dns.RcodeSuccess && len(resp.Answer) > 0 && (out == OutForwarded || out == OutCached) {
			s.OnAnswer(strings.TrimSuffix(q.Name, "."), resp.Answer)
		}
		if resp != nil && udp {
			resp.Truncate(udpSize(r))
		}
		return resp
	}

	// 1. Rate limit.
	if !s.rl.Allow(client) {
		if s.OnRateLimited != nil {
			min := start.Unix() / 60
			if prev, ok := s.limitedAt.Load(client); !ok || prev.(int64) != min {
				s.limitedAt.Store(client, min)
				s.OnRateLimited(client)
			}
		}
		m := new(dns.Msg)
		m.SetRcode(r, dns.RcodeRefused)
		return finish(m, OutRateLimited, "", "")
	}

	// 2. Special names.
	if o.Canary && q.Name == "use-application-dns.net." {
		m := new(dns.Msg)
		m.SetRcode(r, dns.RcodeNameError)
		return finish(m, OutLocal, "canary", "")
	}

	// 3. Local records, leases, reverse lookups.
	if rrs, rc, ok := s.localAnswer(q); ok {
		m := new(dns.Msg)
		m.SetReply(r)
		m.Authoritative = false
		m.RecursionAvailable = true
		m.Rcode = rc
		m.Answer = rrs
		return finish(m, OutLocal, "", "")
	}

	// 4. Filtering.
	if ci.Filtering || s.dir == nil {
		e := s.eng.get()
		if !e.nofilter[ci.GroupID] {
			blocked, rule := false, ""
			switch a, rl := e.custom.Check(strings.TrimSuffix(q.Name, ".")); a {
			case "allow":
			case "block":
				blocked, rule = true, "custom: "+rl
			default:
				if i, rl, ok := e.ix.Lookup(strings.TrimSuffix(q.Name, "."), e.masks[ci.GroupID]); ok {
					blocked, rule = true, e.ix.ListName(i)+": "+rl
				}
			}
			if blocked {
				return finish(s.blockedReply(r, q, o), OutBlocked, rule, "")
			}
		}
	}

	do := false
	if opt := r.IsEdns0(); opt != nil {
		do = opt.Do()
	}

	// 5. Cache.
	if c, ok := s.cache.Get(q, do); ok {
		c.Id = r.Id
		c.Question = r.Question
		c.RecursionDesired = r.RecursionDesired
		return finish(c, OutCached, "", "")
	}

	// 6. Forward.
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	up, upName := s.pool, ""
	resp, u, _, err := up.Exchange(ctx, r)
	if err != nil {
		m := new(dns.Msg)
		m.SetRcode(r, dns.RcodeServerFailure)
		return finish(m, OutFailed, "", "")
	}
	if u != nil {
		upName = u.Name
	}
	resp.Id = r.Id
	s.cache.Put(q, do, resp)
	return finish(resp, OutForwarded, "", upName)
}

func udpSize(r *dns.Msg) int {
	if opt := r.IsEdns0(); opt != nil {
		if sz := int(opt.UDPSize()); sz >= 512 {
			return sz
		}
	}
	return dns.MinMsgSize
}

func (s *Server) blockedReply(r *dns.Msg, q dns.Question, o *Options) *dns.Msg {
	m := new(dns.Msg)
	switch o.BlockMode {
	case "nxdomain":
		m.SetRcode(r, dns.RcodeNameError)
		return m
	case "refused":
		m.SetRcode(r, dns.RcodeRefused)
		return m
	}
	m.SetReply(r)
	m.RecursionAvailable = true
	ttl := o.BlockTTL
	switch q.Qtype {
	case dns.TypeA:
		m.Answer = []dns.RR{&dns.A{Hdr: dns.RR_Header{Name: q.Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: ttl}, A: net.IPv4zero.To4()}}
	case dns.TypeAAAA:
		m.Answer = []dns.RR{&dns.AAAA{Hdr: dns.RR_Header{Name: q.Name, Rrtype: dns.TypeAAAA, Class: dns.ClassINET, Ttl: ttl}, AAAA: net.IPv6zero}}
	}
	return m
}

// Start listens on UDP and TCP for every address. Addresses are host:port.
// It returns once all listeners are bound, or an error and nothing left running.
func (s *Server) Start(addrs []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.servers) > 0 {
		return errors.New("dns: already started")
	}
	type pair struct {
		pc net.PacketConn
		l  net.Listener
	}
	var bound []pair
	closeAll := func() {
		for _, b := range bound {
			if b.pc != nil {
				b.pc.Close()
			}
			if b.l != nil {
				b.l.Close()
			}
		}
	}
	for _, a := range addrs {
		pc, err := net.ListenPacket("udp", a)
		if err != nil {
			closeAll()
			return fmt.Errorf("dns: listen udp %s: %w", a, err)
		}
		bound = append(bound, pair{pc: pc})
		l, err := net.Listen("tcp", a)
		if err != nil {
			closeAll()
			return fmt.Errorf("dns: listen tcp %s: %w", a, err)
		}
		bound = append(bound, pair{l: l})
	}
	for _, b := range bound {
		srv := &dns.Server{Handler: s, PacketConn: b.pc, Listener: b.l,
			ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second}
		started := make(chan struct{})
		srv.NotifyStartedFunc = func() { close(started) }
		go srv.ActivateAndServe()
		<-started
		s.servers = append(s.servers, srv)
	}
	return nil
}

// Stop shuts the listeners down.
func (s *Server) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, srv := range s.servers {
		srv.Shutdown()
	}
	s.servers = nil
}

// Addrs lists the bound addresses (useful when port 0 was requested).
func (s *Server) Addrs() []net.Addr {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []net.Addr
	for _, srv := range s.servers {
		if srv.PacketConn != nil {
			out = append(out, srv.PacketConn.LocalAddr())
		} else if srv.Listener != nil {
			out = append(out, srv.Listener.Addr())
		}
	}
	return out
}

// Run ticks the stats once per second until ctx ends, calling publish with a snapshot.
func (s *Server) Run(ctx context.Context, publish func(Snapshot)) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.stats.Tick()
			if publish != nil {
				snap := s.stats.Snapshot()
				snap.CacheSize = s.cache.Len()
				publish(snap)
			}
		}
	}
}

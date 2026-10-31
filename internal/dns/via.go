package dnssrv

import (
	"net"
	"sync"
	"syscall"
	"time"
)

// Path says how one client's lookups must travel. The zero value is the normal connection.
type Path struct {
	// Dev is the VPN tunnel the lookups go out through. Empty means the normal connection.
	Dev string
	// Strict means a failure through the tunnel is a failure: the lookup never falls back to the
	// normal connection (the kill switch is on).
	Strict bool
	// Hold means the lookup is refused outright: the tunnel is down and the kill switch is on.
	Hold bool
}

// viaSet keeps, for every tunnel, a pool whose sockets are tied to it and a cache of its own, so
// an answer fetched through the VPN is never mixed with one fetched on the normal connection
// (the two can differ by location).
type viaSet struct {
	mu     sync.Mutex
	pools  map[string]*Pool
	gen    map[string]uint64
	caches map[string]*Cache
}

// bind makes the dial control that ties sockets to a tunnel. Tests replace it.
var bind = BindControl

func (s *Server) viaPool(dev string) *Pool {
	s.via.mu.Lock()
	defer s.via.mu.Unlock()
	if s.via.pools == nil {
		s.via.pools, s.via.gen, s.via.caches = map[string]*Pool{}, map[string]uint64{}, map[string]*Cache{}
	}
	p := s.via.pools[dev]
	if p == nil {
		p = NewBoundPool(s.pool.bootstrap, bind(dev))
		s.via.pools[dev] = p
	}
	// follow the person's list: whenever the main list changes, copy it again
	if g := s.pool.gen.Load(); s.via.gen[dev] != g || len(p.ups) == 0 {
		p.Set(s.pool.cloneList())
		p.SetOrdered(s.pool.Ordered())
		s.via.gen[dev] = g
	}
	return p
}

func (s *Server) viaCache(dev string) *Cache {
	s.viaPool(dev)
	s.via.mu.Lock()
	defer s.via.mu.Unlock()
	c := s.via.caches[dev]
	if c == nil {
		c = NewCache(s.opts.Load().CacheSize)
		s.via.caches[dev] = c
	}
	return c
}

// flushVia empties the tunnel caches along with the main one.
func (s *Server) flushVia() {
	s.via.mu.Lock()
	defer s.via.mu.Unlock()
	for _, c := range s.via.caches {
		c.Flush()
	}
}

// NewBoundPool is a pool whose sockets are tied to one device by control.
func NewBoundPool(bootstrap []string, control func(network, address string, c syscall.RawConn) error) *Pool {
	p := &Pool{timeout: 3 * time.Second, now: time.Now, bootstrap: bootstrap, control: control}
	p.exchange = p.realExchange
	p.doh = p.newDoHClient()
	return p
}

// dialer is the dialer for plain and encrypted queries; nil means the default.
func (p *Pool) dialer() *net.Dialer {
	if p.control == nil {
		return nil
	}
	return &net.Dialer{Timeout: p.timeout, Control: p.control}
}

// cloneList copies the upstream list, without the health data, for another pool.
func (p *Pool) cloneList() []*Upstream {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]*Upstream, 0, len(p.ups))
	for _, u := range p.ups {
		out = append(out, &Upstream{ID: u.ID, Name: u.Name, Addr: u.Addr, Proto: u.Proto, TLSName: u.TLSName, Position: u.Position})
	}
	return out
}

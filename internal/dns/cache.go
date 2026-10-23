package dnssrv

import (
	"container/list"
	"sync"
	"time"

	"github.com/miekg/dns"
)

type cacheKey struct {
	name  string
	qtype uint16
	do    bool
}

type cacheEntry struct {
	key     cacheKey
	msg     *dns.Msg
	stored  time.Time
	expires time.Time
}

// Cache is a size-limited LRU cache of DNS responses honouring record TTLs.
type Cache struct {
	mu     sync.Mutex
	m      map[cacheKey]*list.Element
	ll     *list.List
	max    int
	minTTL time.Duration
	maxTTL time.Duration
	negTTL time.Duration
	now    func() time.Time
}

func NewCache(max int) *Cache {
	return &Cache{
		m: map[cacheKey]*list.Element{}, ll: list.New(), max: max,
		minTTL: 10 * time.Second, maxTTL: 6 * time.Hour, negTTL: time.Hour, now: time.Now,
	}
}

func keyFor(q dns.Question, do bool) cacheKey {
	return cacheKey{name: q.Name, qtype: q.Qtype, do: do}
}

// ttlOf returns how long a response may be cached, or 0 if it must not be.
func (c *Cache) ttlOf(m *dns.Msg) time.Duration {
	if m.Truncated || m.Rcode == dns.RcodeServerFailure || m.Rcode == dns.RcodeRefused || m.Rcode == dns.RcodeFormatError {
		return 0
	}
	var min uint32 = ^uint32(0)
	scan := func(rrs []dns.RR) {
		for _, rr := range rrs {
			if rr.Header().Rrtype == dns.TypeOPT {
				continue
			}
			if t := rr.Header().Ttl; t < min {
				min = t
			}
		}
	}
	if m.Rcode == dns.RcodeNameError || (m.Rcode == dns.RcodeSuccess && len(m.Answer) == 0) {
		// Negative answer: use the SOA minimum (RFC 2308).
		for _, rr := range m.Ns {
			if soa, ok := rr.(*dns.SOA); ok {
				t := soa.Minttl
				if soa.Hdr.Ttl < t {
					t = soa.Hdr.Ttl
				}
				d := time.Duration(t) * time.Second
				if d > c.negTTL {
					d = c.negTTL
				}
				return d
			}
		}
		return 30 * time.Second
	}
	scan(m.Answer)
	scan(m.Ns)
	if min == ^uint32(0) {
		return 0
	}
	d := time.Duration(min) * time.Second
	if d < c.minTTL {
		d = c.minTTL
	}
	if d > c.maxTTL {
		d = c.maxTTL
	}
	return d
}

// Put stores a response if it is cacheable.
func (c *Cache) Put(q dns.Question, do bool, m *dns.Msg) {
	ttl := c.ttlOf(m)
	if ttl <= 0 {
		return
	}
	k := keyFor(q, do)
	now := c.now()
	e := &cacheEntry{key: k, msg: m.Copy(), stored: now, expires: now.Add(ttl)}
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.m[k]; ok {
		el.Value = e
		c.ll.MoveToFront(el)
		return
	}
	c.m[k] = c.ll.PushFront(e)
	for c.ll.Len() > c.max {
		last := c.ll.Back()
		delete(c.m, last.Value.(*cacheEntry).key)
		c.ll.Remove(last)
	}
}

// Get returns a copy of the cached response with TTLs reduced by its age.
func (c *Cache) Get(q dns.Question, do bool) (*dns.Msg, bool) {
	k := keyFor(q, do)
	c.mu.Lock()
	el, ok := c.m[k]
	if !ok {
		c.mu.Unlock()
		return nil, false
	}
	e := el.Value.(*cacheEntry)
	now := c.now()
	if !now.Before(e.expires) {
		delete(c.m, k)
		c.ll.Remove(el)
		c.mu.Unlock()
		return nil, false
	}
	c.ll.MoveToFront(el)
	msg := e.msg.Copy()
	age := uint32(now.Sub(e.stored) / time.Second)
	c.mu.Unlock()
	for _, sec := range [][]dns.RR{msg.Answer, msg.Ns, msg.Extra} {
		for _, rr := range sec {
			h := rr.Header()
			if h.Rrtype == dns.TypeOPT {
				continue
			}
			if h.Ttl > age {
				h.Ttl -= age
			} else {
				h.Ttl = 1
			}
		}
	}
	return msg, true
}

// Len returns the number of cached entries.
func (c *Cache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ll.Len()
}

// Flush empties the cache.
func (c *Cache) Flush() {
	c.mu.Lock()
	c.m = map[cacheKey]*list.Element{}
	c.ll.Init()
	c.mu.Unlock()
}

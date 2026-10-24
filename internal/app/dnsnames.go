package app

import (
	"net/netip"
	"sync"

	"github.com/miekg/dns"
)

// The name a device asked for is the best label for the address it then connected to. This keeps
// the latest name seen for each address, so the log can say "netflix.com" instead of a number.
type nameCache struct {
	mu    sync.Mutex
	names map[netip.Addr]string
	order []netip.Addr // oldest first, a ring
	next  int
}

const nameCacheSize = 20000

func (c *nameCache) put(ip netip.Addr, name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.names == nil {
		c.names = make(map[netip.Addr]string, 1024)
	}
	if _, ok := c.names[ip]; !ok {
		if len(c.order) < nameCacheSize {
			c.order = append(c.order, ip)
		} else {
			delete(c.names, c.order[c.next])
			c.order[c.next] = ip
			c.next = (c.next + 1) % nameCacheSize
		}
	}
	c.names[ip] = name
}

func (c *nameCache) get(ip string) string {
	a, err := netip.ParseAddr(ip)
	if err != nil {
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.names[a.Unmap()]
}

// remember notes the addresses in a DNS answer under the name that was asked for.
func (c *nameCache) remember(qname string, answers []dns.RR) {
	for _, rr := range answers {
		var ip netip.Addr
		switch v := rr.(type) {
		case *dns.A:
			ip, _ = netip.AddrFromSlice(v.A.To4())
		case *dns.AAAA:
			ip, _ = netip.AddrFromSlice(v.AAAA)
		}
		if ip.IsValid() {
			c.put(ip, qname)
		}
	}
}

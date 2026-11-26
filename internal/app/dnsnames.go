package app

import (
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
)

// The name a device asked for is the best label for the address it then connected to. This keeps
// the latest name seen for each address, so the log can say "netflix.com" instead of a number.
type nameCache struct {
	mu    sync.Mutex
	names map[netip.Addr]string
	order []netip.Addr // oldest first, a ring
	next  int
	seen  map[netip.Addr][]nameAt // the few different names lately given for an address
}

type nameAt struct {
	name string
	at   time.Time
}

// An address that many different names point at (a shared host or a CDN front) is not a good label:
// the latest name would be a guess about someone else's lookup. After this many different names within
// the window the address gets no guessed name.
const (
	nameSharedAfter  = 4
	nameSharedWindow = 30 * time.Minute
)

// knownResolver names the best-known public DNS services, whatever a lookup once said about them. A
// device talking to 8.8.8.8 is talking to Google's DNS, not to the last site someone looked up.
func knownResolver(ip netip.Addr) string {
	s := ip.Unmap().String()
	switch {
	case s == "8.8.8.8" || s == "8.8.4.4" || strings.HasPrefix(s, "2001:4860:4860::"):
		return "Google Public DNS"
	case strings.HasPrefix(s, "1.1.1.") || s == "1.0.0.1" || s == "1.0.0.2" || s == "1.0.0.3" || strings.HasPrefix(s, "2606:4700:4700::"):
		return "Cloudflare DNS"
	case s == "9.9.9.9" || s == "9.9.9.10" || s == "9.9.9.11" || s == "149.112.112.112" || s == "149.112.112.10" || s == "149.112.112.11" || strings.HasPrefix(s, "2620:fe::"):
		return "Quad9 DNS"
	case strings.HasPrefix(s, "208.67.222.") || strings.HasPrefix(s, "208.67.220.") || strings.HasPrefix(s, "2620:119:35::") || strings.HasPrefix(s, "2620:119:53::"):
		return "OpenDNS"
	case strings.HasPrefix(s, "94.140.14.") || strings.HasPrefix(s, "94.140.15.") || strings.HasPrefix(s, "2a10:50c0::"):
		return "AdGuard DNS"
	case strings.HasPrefix(s, "185.228.168.") || strings.HasPrefix(s, "185.228.169."):
		return "CleanBrowsing DNS"
	}
	return ""
}

const nameCacheSize = 20000

func (c *nameCache) put(ip netip.Addr, name string) {
	if knownResolver(ip) != "" {
		return // named by what it is, not by a lookup
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	if c.seen == nil {
		c.seen = map[netip.Addr][]nameAt{}
	}
	recent := c.seen[ip][:0:0]
	have := false
	for _, x := range c.seen[ip] {
		if now.Sub(x.at) <= nameSharedWindow {
			if x.name == name {
				have = true
				x.at = now
			}
			recent = append(recent, x)
		}
	}
	if !have {
		recent = append(recent, nameAt{name, now})
		if len(recent) > nameSharedAfter+2 {
			recent = recent[len(recent)-(nameSharedAfter+2):]
		}
	}
	c.seen[ip] = recent
	if len(c.seen) > nameCacheSize*2 {
		c.seen = map[netip.Addr][]nameAt{ip: recent}
	}
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
	if k := knownResolver(a); k != "" {
		return k
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	a = a.Unmap()
	if len(c.seen[a]) >= nameSharedAfter && time.Since(c.seen[a][len(c.seen[a])-1].at) <= nameSharedWindow {
		return "" // shared by many names: no guess
	}
	return c.names[a]
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

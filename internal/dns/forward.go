package dnssrv

import (
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
)

// Names that use a different server ("conditional forwarding"). A rule says: names ending in this
// domain are asked of these servers, not of the usual ones. It is for things like an office network
// whose own names only its own DNS server knows. Your own names (nas.lan) and blocks still come
// first, and the answer is kept in the same cache as every other.

// FwdRule is one such rule.
type FwdRule struct {
	Domain  string   `json:"domain"`
	Servers []string `json:"servers"`
}

type fwdPool struct {
	domain string // lower case, with a trailing dot
	pool   *Pool
}

type fwdSet struct{ rules []fwdPool }

// CleanFwdRule checks one rule and returns it in the form NopeWarden keeps: a plain lower-case
// domain, and servers as address:port. The error is in plain words.
func CleanFwdRule(r FwdRule) (FwdRule, error) {
	d := strings.ToLower(strings.TrimSpace(r.Domain))
	d = strings.TrimPrefix(strings.TrimPrefix(d, "*."), ".")
	d = strings.TrimSuffix(d, ".")
	if d == "" || len(d) > 253 || strings.ContainsAny(d, " /:@") {
		return r, errors.New("type a domain such as office.example")
	}
	for _, l := range strings.Split(d, ".") {
		if l == "" || len(l) > 63 || strings.HasPrefix(l, "-") || strings.HasSuffix(l, "-") {
			return r, fmt.Errorf("%q is not a domain name", r.Domain)
		}
	}
	if len(r.Servers) == 0 {
		return r, errors.New("add the address of the server that knows these names")
	}
	if len(r.Servers) > 4 {
		return r, errors.New("use at most four servers for one domain")
	}
	out := FwdRule{Domain: d}
	seen := map[string]bool{}
	for _, s := range r.Servers {
		s = strings.TrimSpace(s)
		host, port, err := net.SplitHostPort(s)
		if err != nil {
			host, port = strings.Trim(s, "[]"), "53"
		}
		ip := net.ParseIP(host)
		if ip == nil {
			return r, fmt.Errorf("%q is not an address. Use the server's address, such as 10.0.0.2", s)
		}
		if p := strings.TrimLeft(port, "0123456789"); p != "" || port == "" || len(port) > 5 {
			return r, fmt.Errorf("%q is not a valid port", port)
		}
		a := net.JoinHostPort(ip.String(), port)
		if !seen[a] {
			seen[a] = true
			out.Servers = append(out.Servers, a)
		}
	}
	return out, nil
}

func buildFwd(rules []FwdRule) *fwdSet {
	set := &fwdSet{}
	for _, r := range rules {
		p := NewPool(nil)
		var ups []*Upstream
		for i, a := range r.Servers {
			ups = append(ups, &Upstream{ID: int64(i + 1), Name: a, Addr: a, Proto: "udp", Position: i + 1})
		}
		p.Set(ups)
		set.rules = append(set.rules, fwdPool{domain: r.Domain + ".", pool: p})
	}
	// The longest domain wins, so office.example beats example.
	sort.SliceStable(set.rules, func(i, j int) bool { return len(set.rules[i].domain) > len(set.rules[j].domain) })
	return set
}

// forwardPool returns the pool for the rule that covers name, or nil.
func (s *Server) forwardPool(name string) *Pool {
	f := s.fwd.Load()
	if f == nil {
		return nil
	}
	for _, r := range f.rules {
		if name == r.domain || strings.HasSuffix(name, "."+r.domain) {
			return r.pool
		}
	}
	return nil
}

// SetForwards replaces the rules. The cache is cleared so a name that was looked up the old way is
// asked again.
func (s *Server) SetForwards(rules []FwdRule) {
	s.fwd.Store(buildFwd(rules))
	s.cache.Flush()
	s.flushVia()
}

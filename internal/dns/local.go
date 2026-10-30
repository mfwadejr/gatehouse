package dnssrv

import (
	"errors"
	"net"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/miekg/dns"
)

// ClientInfo is what the server knows about the device behind a query.
type ClientInfo struct {
	Name      string
	MAC       string
	GroupID   int64
	Filtering bool
}

// Directory resolves a client address to a device. The DHCP and device tables implement it.
type Directory interface {
	Client(ip string) ClientInfo
}

// Names resolves hostnames from DHCP leases and static assignments.
type Names interface {
	LookupHost(name string) []net.IP // name is lowercase, without the local domain
	LookupAddr(ip net.IP) (string, bool)
}

// LocalRecord is an administrator-defined record.
type LocalRecord struct {
	Name  string
	Type  string
	Value string
	TTL   uint32
}

type localZone struct {
	domain  string // e.g. "lan"
	records map[string][]dns.RR
}

type localHolder struct{ p atomic.Pointer[localZone] }

func buildLocal(domain string, recs []LocalRecord) *localZone {
	z := &localZone{domain: strings.Trim(strings.ToLower(domain), "."), records: map[string][]dns.RR{}}
	for _, r := range recs {
		name := dns.Fqdn(strings.ToLower(r.Name))
		ttl := r.TTL
		if ttl == 0 {
			ttl = 300
		}
		var rr dns.RR
		var err error
		switch strings.ToUpper(r.Type) {
		case "A", "AAAA":
			rr, err = dns.NewRR(name + " " + utoa(ttl) + " IN " + strings.ToUpper(r.Type) + " " + r.Value)
		case "CNAME", "PTR":
			rr, err = dns.NewRR(name + " " + utoa(ttl) + " IN " + strings.ToUpper(r.Type) + " " + dns.Fqdn(r.Value))
		case "TXT":
			rr, err = dns.NewRR(name + " " + utoa(ttl) + " IN TXT \"" + strings.ReplaceAll(r.Value, "\"", "'") + "\"")
		case "MX":
			rr, err = dns.NewRR(name + " " + utoa(ttl) + " IN MX " + r.Value)
		default:
			continue
		}
		if err != nil || rr == nil {
			continue
		}
		z.records[name] = append(z.records[name], rr)
	}
	return z
}

func utoa(n uint32) string {
	if n == 0 {
		return "0"
	}
	var b [10]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// answer builds a local response, or returns handled=false to continue down the pipeline.
func (s *Server) localAnswer(q dns.Question) (rrs []dns.RR, rcode int, handled bool) {
	name := strings.ToLower(q.Name)
	z := s.local.p.Load()
	trimmed := strings.TrimSuffix(name, ".")

	// Reverse lookups for addresses we know.
	if q.Qtype == dns.TypePTR {
		if ip := reverseToIP(name); ip != nil && s.names != nil {
			if host, ok := s.names.LookupAddr(ip); ok {
				dom := ""
				if z != nil && z.domain != "" {
					dom = "." + z.domain
				}
				rr, err := dns.NewRR(q.Name + " 300 IN PTR " + dns.Fqdn(host+dom))
				if err == nil {
					return []dns.RR{rr}, dns.RcodeSuccess, true
				}
			}
			if isPrivate(ip) {
				return nil, dns.RcodeNameError, true // never leak private reverse lookups upstream
			}
		}
	}
	if z != nil {
		if rs, ok := z.records[name]; ok {
			var out []dns.RR
			for _, rr := range rs {
				if rr.Header().Rrtype == q.Qtype || q.Qtype == dns.TypeANY {
					out = append(out, dns.Copy(rr))
				}
			}
			if len(out) == 0 {
				for _, rr := range rs { // CNAME answers any type
					if rr.Header().Rrtype == dns.TypeCNAME {
						out = append(out, dns.Copy(rr))
					}
				}
			}
			return out, dns.RcodeSuccess, true // possibly empty = NODATA
		}
	}
	if (q.Qtype == dns.TypeA || q.Qtype == dns.TypeAAAA) && s.names != nil {
		host := trimmed
		if z != nil && z.domain != "" {
			host = strings.TrimSuffix(host, "."+z.domain)
		}
		if !strings.Contains(host, ".") {
			ips := s.names.LookupHost(host)
			var out []dns.RR
			for _, ip := range ips {
				if q.Qtype == dns.TypeA && ip.To4() != nil {
					out = append(out, &dns.A{Hdr: dns.RR_Header{Name: q.Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60}, A: ip.To4()})
				}
				if q.Qtype == dns.TypeAAAA && ip.To4() == nil {
					out = append(out, &dns.AAAA{Hdr: dns.RR_Header{Name: q.Name, Rrtype: dns.TypeAAAA, Class: dns.ClassINET, Ttl: 60}, AAAA: ip})
				}
			}
			if len(ips) > 0 {
				return out, dns.RcodeSuccess, true
			}
		}
	}
	// Names in the local domain that we do not know are NXDOMAIN, not forwarded.
	if z != nil && z.domain != "" && (trimmed == z.domain || strings.HasSuffix(trimmed, "."+z.domain)) {
		return nil, dns.RcodeNameError, true
	}
	return nil, 0, false
}

func reverseToIP(name string) net.IP {
	name = strings.TrimSuffix(name, ".")
	if strings.HasSuffix(name, ".in-addr.arpa") {
		parts := strings.Split(strings.TrimSuffix(name, ".in-addr.arpa"), ".")
		if len(parts) != 4 {
			return nil
		}
		return net.ParseIP(parts[3] + "." + parts[2] + "." + parts[1] + "." + parts[0])
	}
	if strings.HasSuffix(name, ".ip6.arpa") {
		nib := strings.Split(strings.TrimSuffix(name, ".ip6.arpa"), ".")
		if len(nib) != 32 {
			return nil
		}
		var b strings.Builder
		for i := 31; i >= 0; i-- {
			b.WriteString(nib[i])
			if i%4 == 0 && i != 0 {
				b.WriteByte(':')
			}
		}
		return net.ParseIP(b.String())
	}
	return nil
}

func isPrivate(ip net.IP) bool {
	return ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast()
}

var reHostName = regexp.MustCompile(`^(\*\.)?[a-z0-9_]([a-z0-9_.-]*[a-z0-9_])?$`)

// CleanRecord checks one custom record the way the resolver will read it, and returns the
// value tidied up (a pasted web address loses its https:// and anything after the host).
// The error text is meant to be shown to the person as it is.
func CleanRecord(name, typ, value string) (string, string, error) {
	typ = strings.ToUpper(strings.TrimSpace(typ))
	value = strings.TrimSpace(value)
	if typ != "TXT" {
		value = stripWebAddress(value)
	}
	name = strings.ToLower(stripWebAddress(strings.TrimSpace(name)))
	if !reHostName.MatchString(name) {
		return "", "", errors.New("Names can use letters, numbers, dashes and dots, like nas.lan")
	}
	switch typ {
	case "A":
		ip := net.ParseIP(value)
		if ip == nil || ip.To4() == nil || strings.Contains(value, ":") {
			return "", "", errors.New("An A record points to one IPv4 address, like 192.168.50.10. Enter just the address, with no https:// and no slash")
		}
		return name, ip.To4().String(), nil
	case "AAAA":
		ip := net.ParseIP(value)
		if ip == nil || !strings.Contains(value, ":") {
			return "", "", errors.New("An AAAA record points to one IPv6 address, like fd00::10. Enter just the address")
		}
		return name, ip.String(), nil
	case "CNAME":
		if !reHostName.MatchString(strings.ToLower(value)) || strings.HasPrefix(value, "*") {
			return "", "", errors.New("A CNAME points to another name, like nas.lan. Enter just the name, with no https:// and no slash")
		}
		return name, strings.ToLower(value), nil
	case "MX":
		f := strings.Fields(value)
		if len(f) != 2 {
			return "", "", errors.New("An MX record is a number and a mail server name, like: 10 mail.lan")
		}
		if n, err := strconv.Atoi(f[0]); err != nil || n < 0 || n > 65535 || !reHostName.MatchString(strings.ToLower(f[1])) {
			return "", "", errors.New("An MX record is a number and a mail server name, like: 10 mail.lan")
		}
		return name, f[0] + " " + strings.ToLower(f[1]), nil
	case "TXT":
		if value == "" || len(value) > 255 {
			return "", "", errors.New("Text can be up to 255 characters")
		}
		return name, value, nil
	}
	return "", "", errors.New("type must be A, AAAA, CNAME, TXT or MX")
}

// stripWebAddress turns "https://192.168.50.1/" or "http://nas.lan:8080/x" into the bare host.
func stripWebAddress(v string) string {
	l := strings.ToLower(v)
	if i := strings.Index(l, "://"); i >= 0 && i <= 6 {
		v = v[i+3:]
		if j := strings.IndexAny(v, "/?#"); j >= 0 {
			v = v[:j]
		}
		if k := strings.LastIndex(v, ":"); k >= 0 && !strings.Contains(v[:k], ":") {
			v = v[:k]
		}
	}
	return strings.TrimRight(v, "/")
}

// UsableRecord reports whether the resolver can answer from a saved record. Records saved
// before CleanRecord existed may not be usable.
func UsableRecord(r LocalRecord) bool {
	z := buildLocal("", []LocalRecord{r})
	return len(z.records) > 0
}

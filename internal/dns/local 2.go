package dnssrv

import (
	"net"
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

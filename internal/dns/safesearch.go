package dnssrv

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/miekg/dns"
)

// Safe search: for devices in a profile that asks for it, the main search engines and YouTube are
// pointed at the address their own "safe" mode uses. The device still opens the usual website name;
// only the address behind the name changes. This only works while the device asks Gatehouse for
// names (encrypted DNS inside a browser, or a VPN, bypasses it).

var reGoogle = regexp.MustCompile(`^(www\.)?google\.(com|co\.[a-z]{2}|com\.[a-z]{2}|[a-z]{2})\.$`)

var safeSearchFixed = map[string]string{
	"www.bing.com.":             "strict.bing.com.",
	"bing.com.":                 "strict.bing.com.",
	"duckduckgo.com.":           "safe.duckduckgo.com.",
	"www.duckduckgo.com.":       "safe.duckduckgo.com.",
	"www.youtube.com.":          "restrictmoderate.youtube.com.",
	"m.youtube.com.":            "restrictmoderate.youtube.com.",
	"youtubei.googleapis.com.":  "restrictmoderate.youtube.com.",
	"youtube.googleapis.com.":   "restrictmoderate.youtube.com.",
	"www.youtube-nocookie.com.": "restrictmoderate.youtube.com.",
}

// SafeSearchTarget says where a name should point for safe search, or "" for names it does not touch.
func SafeSearchTarget(qname string) string {
	n := strings.ToLower(qname)
	if t, ok := safeSearchFixed[n]; ok {
		return t
	}
	if reGoogle.MatchString(n) {
		return "forcesafesearch.google.com."
	}
	return ""
}

// safeSearchReply answers a question for a name that safe search covers: a CNAME to the safe name
// followed by that name's own addresses.
func (s *Server) safeSearchReply(r *dns.Msg, q dns.Question, target string) *dns.Msg {
	m := new(dns.Msg)
	m.SetReply(r)
	m.RecursionAvailable = true
	m.Answer = []dns.RR{&dns.CNAME{Hdr: dns.RR_Header{Name: q.Name, Rrtype: dns.TypeCNAME, Class: dns.ClassINET, Ttl: 300}, Target: target}}
	req := new(dns.Msg)
	req.SetQuestion(target, q.Qtype)
	req.RecursionDesired = true
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if resp, _, _, err := s.pool.Exchange(ctx, req); err == nil && resp != nil {
		for _, rr := range resp.Answer {
			if rr.Header().Rrtype == q.Qtype || rr.Header().Rrtype == dns.TypeCNAME {
				m.Answer = append(m.Answer, rr)
			}
		}
	}
	return m
}

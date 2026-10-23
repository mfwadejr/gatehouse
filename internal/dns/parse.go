// Package dnssrv is the DNS server: filtering, cache, upstreams, logging.
package dnssrv

import (
	"bufio"
	"io"
	"net"
	"strings"
)

// Format names a blocklist syntax.
const (
	FormatAuto    = "auto"
	FormatDomains = "domains"
	FormatHosts   = "hosts"
	FormatABP     = "abp"
)

// Rule is one parsed entry.
type Rule struct {
	Domain string
	Allow  bool // exception rule (@@||example.com^)
}

// ParseStats reports what a parse found.
type ParseStats struct {
	Format  string
	Rules   int
	Allow   int
	Skipped int // lines in a syntax we do not support (regex, cosmetic, path rules)
}

// DetectFormat guesses the syntax from the first meaningful lines.
func DetectFormat(sample string) string {
	abp, hosts, dom := 0, 0, 0
	for _, line := range strings.Split(sample, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") || strings.HasPrefix(line, "[") {
			if strings.HasPrefix(line, "[Adblock") || strings.HasPrefix(line, "! ") {
				abp++
			}
			continue
		}
		switch {
		case strings.HasPrefix(line, "||") || strings.HasPrefix(line, "@@||"):
			abp += 2
		default:
			f := strings.Fields(line)
			if len(f) >= 2 && net.ParseIP(f[0]) != nil {
				hosts += 2
			} else if len(f) == 1 {
				dom++
			}
		}
	}
	switch {
	case abp > hosts && abp > dom:
		return FormatABP
	case hosts >= dom && hosts > 0:
		return FormatHosts
	default:
		return FormatDomains
	}
}

// NormalizeDomain lower-cases and validates a domain, returning "" if it is not usable.
func NormalizeDomain(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimSuffix(s, ".")
	if s == "" || len(s) > 253 || strings.ContainsAny(s, " /\\:@*?#") {
		return ""
	}
	if net.ParseIP(s) != nil {
		return ""
	}
	if !strings.Contains(s, ".") {
		return ""
	}
	for _, label := range strings.Split(s, ".") {
		if label == "" || len(label) > 63 {
			return ""
		}
	}
	return s
}

var hostsIgnore = map[string]bool{
	"localhost": true, "localhost.localdomain": true, "local": true, "broadcasthost": true,
	"ip6-localhost": true, "ip6-loopback": true, "ip6-localnet": true, "ip6-mcastprefix": true,
	"ip6-allnodes": true, "ip6-allrouters": true, "ip6-allhosts": true, "0.0.0.0": true,
}

// Parse reads a list in the given format (or detects it) and calls fn for each rule.
func Parse(r io.Reader, format string, fn func(Rule)) (ParseStats, error) {
	br := bufio.NewReaderSize(r, 1<<20)
	var st ParseStats
	var head []string
	var lines []string
	sc := bufio.NewScanner(br)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	// Detection needs a sample, so buffer the first few hundred lines.
	const sample = 400
	emit := func(line string, f string) {
		line = strings.TrimSpace(line)
		if line == "" {
			return
		}
		switch f {
		case FormatDomains:
			if i := strings.IndexAny(line, "#!"); i >= 0 {
				line = strings.TrimSpace(line[:i])
			}
			if d := NormalizeDomain(line); d != "" {
				st.Rules++
				fn(Rule{Domain: d})
			} else if line != "" {
				st.Skipped++
			}
		case FormatHosts:
			if strings.HasPrefix(line, "#") {
				return
			}
			if i := strings.Index(line, "#"); i >= 0 {
				line = strings.TrimSpace(line[:i])
			}
			fields := strings.Fields(line)
			if len(fields) < 2 || net.ParseIP(fields[0]) == nil {
				st.Skipped++
				return
			}
			for _, h := range fields[1:] {
				if hostsIgnore[strings.ToLower(h)] {
					continue
				}
				if d := NormalizeDomain(h); d != "" {
					st.Rules++
					fn(Rule{Domain: d})
				}
			}
		case FormatABP:
			if strings.HasPrefix(line, "!") || strings.HasPrefix(line, "[") || strings.HasPrefix(line, "#") {
				return
			}
			allow := false
			if strings.HasPrefix(line, "@@") {
				allow = true
				line = line[2:]
			}
			if !strings.HasPrefix(line, "||") {
				st.Skipped++
				return
			}
			line = line[2:]
			// Only plain domain rules: ||example.com^ optionally with $options we ignore.
			if i := strings.Index(line, "$"); i >= 0 {
				opts := line[i+1:]
				if strings.Contains(opts, "important") || strings.Contains(opts, "dnsrewrite") || strings.Contains(opts, "client=") || strings.Contains(opts, "denyallow") {
					st.Skipped++
					return
				}
				line = line[:i]
			}
			line = strings.TrimSuffix(line, "^")
			if strings.ContainsAny(line, "/*^|") {
				st.Skipped++
				return
			}
			d := NormalizeDomain(line)
			if d == "" {
				st.Skipped++
				return
			}
			if allow {
				st.Allow++
			} else {
				st.Rules++
			}
			fn(Rule{Domain: d, Allow: allow})
		}
	}
	chosen := format
	flush := func() {
		if chosen == "" || chosen == FormatAuto {
			chosen = DetectFormat(strings.Join(head, "\n"))
		}
		st.Format = chosen
		for _, l := range lines {
			emit(l, chosen)
		}
		lines = nil
	}
	n := 0
	detected := false
	for sc.Scan() {
		line := sc.Text()
		if !detected {
			lines = append(lines, line)
			head = append(head, line)
			n++
			if n >= sample {
				flush()
				detected = true
			}
			continue
		}
		emit(line, chosen)
	}
	if !detected {
		flush()
	}
	return st, sc.Err()
}

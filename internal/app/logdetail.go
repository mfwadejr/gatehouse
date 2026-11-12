package app

import (
	"strings"

	dnssrv "nopewarden/internal/dns"
)

// The extra detail shown when a log row is opened, and written to the CSV.

// ruleLabel names the rule that decided a connection, in words, from the comment the firewall keeps.
func ruleLabel(tag string) string {
	kind, rest, _ := strings.Cut(tag, ":")
	switch kind {
	case "rule":
		if p := strings.SplitN(rest, ":", 3); len(p) == 3 {
			return p[2]
		}
	case "internet":
		return "Internet access for " + rest
	case "trusted":
		return "Trusted network " + rest
	case "within":
		return "Inside " + rest
	case "killswitch":
		return "Kill switch for VPN " + rest
	case "vpn":
		return "VPN " + rest
	case "forward":
		return "Port forward"
	case "protected":
		return strings.TrimSpace(rest)
	case "default":
		switch rest {
		case "in":
			return "Default: nothing allows this to the router"
		case "dot":
			return "Default: encrypted DNS is not allowed"
		}
		return "Default: block what no rule allows"
	}
	return tag
}

// dnsAnsweredBy says who gave the answer to a lookup, in words.
func dnsAnsweredBy(r dnssrv.Row) string {
	switch r.Result {
	case "blocked":
		return "NopeWarden (a block list)"
	case "cached":
		return "NopeWarden (from memory)"
	case "local":
		return "NopeWarden (your own names)"
	case "rate_limited":
		return "NopeWarden (slowed down)"
	}
	if r.Upstream != "" {
		return r.Upstream
	}
	if r.Result == "failed" {
		return "No server answered"
	}
	return "An upstream DNS server"
}

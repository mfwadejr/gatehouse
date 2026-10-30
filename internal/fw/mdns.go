package fw

import (
	"encoding/json"
	"strings"
)

// Devices finding each other across networks (mDNS, Bonjour, AirPlay, Chromecast, printers).
// The person chooses pairs of networks that may see each other's devices. The reflector service
// copies the announcements between them; the firewall only has to let them reach the router.

type MDNSPair struct {
	A string `json:"a"`
	B string `json:"b"`
}

type MDNSPolicy struct {
	Pairs []MDNSPair `json:"pairs"`
	// Services limits what is copied (for example _ipp._tcp). Empty means everything.
	Services []string `json:"services"`
}

// MDNSFrom reads the saved choice.
func MDNSFrom(db Queryer) MDNSPolicy {
	var p MDNSPolicy
	if raw := setting(db, "fw.mdns", ""); raw != "" {
		json.Unmarshal([]byte(raw), &p)
	}
	return p
}

// MDNSZones lists the networks named in any pair, in order, without repeats.
func (p MDNSPolicy) Zones() []string {
	var out []string
	seen := map[string]bool{}
	for _, pr := range p.Pairs {
		for _, n := range []string{pr.A, pr.B} {
			if n != "" && !seen[n] {
				seen[n] = true
				out = append(out, n)
			}
		}
	}
	return out
}

// mdnsIfaces finds the interfaces of the networks that take part.
func mdnsIfaces(zones []Zone, p MDNSPolicy) []string {
	use := map[string]bool{}
	for _, n := range p.Zones() {
		use[strings.ToLower(n)] = true
	}
	var out []string
	for _, z := range zones {
		if z.Kind == KindWan || !use[strings.ToLower(z.Name)] {
			continue
		}
		out = append(out, z.Ifaces...)
	}
	return out
}

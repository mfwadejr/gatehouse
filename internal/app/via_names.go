package app

import (
	"net"
	"strings"
	"sync"
	"time"

	dnssrv "nopewarden/internal/dns"
	"nopewarden/internal/fw"
)

// DNS follows the route. When a route sends a website through a VPN, the lookup of that website's
// name goes through the same VPN (with its own cache), so the provider the VPN replaces never sees
// the question and the address that comes back is the one the VPN's side of the internet gives.
// Everything else about how a name is looked up stays as it was.

type routeNameCache struct {
	mu    sync.Mutex
	at    time.Time
	specs []fw.RouteSpec
}

var routeNames routeNameCache

// routeSpecsNow returns the routes as the firewall uses them, read at most every five seconds.
func (a *App) routeSpecsNow() []fw.RouteSpec {
	routeNames.mu.Lock()
	defer routeNames.mu.Unlock()
	if time.Since(routeNames.at) > 5*time.Second {
		routeNames.specs, _ = fw.RouteSpecs(a.Config)
		routeNames.at = time.Now()
	}
	return routeNames.specs
}

// forgetRouteNames makes the next lookup read the routes again (after any change to them).
func forgetRouteNames() {
	routeNames.mu.Lock()
	routeNames.at = time.Time{}
	routeNames.mu.Unlock()
}

// dnsPathForName is the first route that covers both this device and this name and sends traffic
// through a VPN.
func (a *App) dnsPathForName(client, qname string) (dnssrv.Path, bool) {
	ip := net.ParseIP(client)
	if ip == nil {
		return dnssrv.Path{}, false
	}
	qname = strings.ToLower(strings.TrimSuffix(qname, "."))
	loc := time.Now().Location()
	now := time.Now()
	for _, sp := range a.routeSpecsNow() {
		if sp.PausedUntil > now.Unix() || (sp.Sched != nil && !sp.Sched.Active(loc, now)) {
			continue
		}
		if len(sp.Srcs) > 0 {
			who := false
			for _, pref := range sp.Srcs {
				if prefixHas(pref, ip) {
					who = true
					break
				}
			}
			if !who {
				continue
			}
		}
		covered := false
		for _, tok := range strings.FieldsFunc(sp.Dst, func(r rune) bool { return r == ',' || r == ' ' }) {
			if strings.HasPrefix(tok, "dom:") && domainMatches(strings.ToLower(strings.TrimPrefix(tok, "dom:")), qname) {
				covered = true
				break
			}
		}
		if !covered {
			continue
		}
		if sp.TunnelIface == "" {
			return dnssrv.Path{}, false // a route over an internet connection leaves lookups as they are
		}
		for _, v := range a.vpnSnapshot().Views {
			if v.Dev != sp.TunnelIface || v.Role == "server" || !v.Enabled {
				continue
			}
			switch {
			case v.State == "up":
				return dnssrv.Path{Dev: v.Dev, Strict: sp.Kill || v.KillSwitch}, true
			case sp.Kill || v.KillSwitch:
				return dnssrv.Path{Dev: v.Dev, Hold: true}, true
			}
			return dnssrv.Path{}, false
		}
		return dnssrv.Path{}, false
	}
	return dnssrv.Path{}, false
}

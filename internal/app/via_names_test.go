package app

import (
	"testing"
	"time"

	"nopewarden/internal/fw"
)

func TestADeviceLooksAWebsiteUpThroughTheRoutesVPN(t *testing.T) {
	a, _ := newSpeedApp(t)
	view := func(state string, kill bool) {
		a.vpn.snap = vpnSnapshot{Views: []VPNView{{Dev: "wg0", Label: "PIA", Role: "client", Enabled: true, State: state, KillSwitch: kill}}}
	}
	setRoutes := func(sp ...fw.RouteSpec) {
		routeNames.mu.Lock()
		routeNames.specs, routeNames.at = sp, time.Now()
		routeNames.mu.Unlock()
	}
	t.Cleanup(forgetRouteNames)
	route := fw.RouteSpec{Name: "Sites", TunnelIface: "wg0", Srcs: []string{"192.168.50.0/24"}, Dst: "dom:xhamster.com"}
	setRoutes(route)
	view("up", false)
	if p, ok := a.dnsPathForName("192.168.50.20", "www.xhamster.com"); !ok || p.Dev != "wg0" || p.Strict || p.Hold {
		t.Fatalf("a covered name from a covered device goes through the VPN: %+v %v", p, ok)
	}
	if _, ok := a.dnsPathForName("192.168.50.20", "example.org"); ok {
		t.Fatal("a name the route does not cover is looked up as before")
	}
	if _, ok := a.dnsPathForName("192.168.60.9", "xhamster.com"); ok {
		t.Fatal("a device the route does not cover is left alone")
	}
	// Not up, kill switch off: lookups carry on the normal way so DNS keeps working.
	view("down", false)
	if p, ok := a.dnsPathForName("192.168.50.20", "xhamster.com"); ok {
		t.Fatalf("tunnel down, kill switch off: %+v", p)
	}
	// Not up, kill switch on (on the route): the lookup is held, not sent the normal way.
	route.Kill = true
	setRoutes(route)
	if p, ok := a.dnsPathForName("192.168.50.20", "xhamster.com"); !ok || !p.Hold {
		t.Fatalf("kill switch on: %+v %v", p, ok)
	}
	// Paused routes do nothing.
	route.PausedUntil = time.Now().Add(time.Hour).Unix()
	setRoutes(route)
	if _, ok := a.dnsPathForName("192.168.50.20", "xhamster.com"); ok {
		t.Fatal("a paused route must not steer lookups")
	}
	// A route over an internet connection (no tunnel) leaves lookups alone.
	setRoutes(fw.RouteSpec{Name: "Fibre", Srcs: []string{"192.168.50.0/24"}, Dst: "dom:xhamster.com"})
	if _, ok := a.dnsPathForName("192.168.50.20", "xhamster.com"); ok {
		t.Fatal("connection routes do not steer lookups")
	}
}

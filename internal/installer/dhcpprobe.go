package installer

import (
	"context"
	"net"
	"net/netip"
	"sort"
	"time"

	"nopewarden/internal/dhcp"
)

// Another DHCP server on the network a port is plugged into is the one thing that can quietly spoil an
// install: hand out addresses next to it and every computer on that network gets answers from two servers.
// So before asking, the installer listens, with the same probe the console's "Check for other servers"
// button uses (internal/dhcp.Probe): one DHCP discover out of the port, and the offers that come back. It
// never asks for an address (no request is sent), so nothing is leased and nothing on the network changes.

// Offer is one server's answer to the discover.
type Offer struct {
	Server netip.Addr   // the server's own address
	Given  netip.Prefix // the address it would lease, with its network size (invalid when the server sent no mask)
	Router netip.Addr
}

// Network is the network the offer is for, or the zero prefix when it did not say.
func (o Offer) Network() netip.Prefix {
	if !o.Given.IsValid() {
		return netip.Prefix{}
	}
	return o.Given.Masked()
}

// ProbeResult is what listening on one port found.
type ProbeResult struct {
	Port   string
	Offers []Offer
	// Err is set when the port could not be listened on (no permission, the socket was busy). The answer is
	// then "unknown", which is never a reason to stop the install.
	Err error
}

// Heard says whether any server answered.
func (r ProbeResult) Heard() bool { return len(r.Offers) > 0 }

// Servers lists the answering servers' addresses, lowest first.
func (r ProbeResult) Servers() []netip.Addr {
	var out []netip.Addr
	for _, o := range r.Offers {
		out = append(out, o.Server)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Less(out[j]) })
	return out
}

// SharesServer says whether the same server answered on both ports, which means both are on one network.
func SharesServer(a, b ProbeResult) (netip.Addr, bool) {
	for _, x := range a.Offers {
		for _, y := range b.Offers {
			if x.Server == y.Server {
				return x.Server, true
			}
		}
	}
	return netip.Addr{}, false
}

// offersFrom turns what the probe heard into offers.
func offersFrom(found []dhcp.RogueFinding) []Offer {
	var out []Offer
	for _, f := range found {
		srv, ok := netip.AddrFromSlice(f.Server.To4())
		if !ok {
			continue
		}
		o := Offer{Server: srv}
		if y, ok := netip.AddrFromSlice(f.Offer.To4()); ok && !y.IsUnspecified() && f.Mask != nil {
			if ones, bits := net.IPMask(f.Mask.To4()).Size(); bits == 32 && ones > 0 {
				o.Given = netip.PrefixFrom(y, ones)
			}
		}
		out = append(out, o)
	}
	return out
}

// ProbeDHCP listens on one port for DHCP servers for the wait time. The port must be up. Any failure to
// listen comes back in ProbeResult.Err and means "could not check".
func ProbeDHCP(port string, wait time.Duration) ProbeResult {
	ctx, cancel := context.WithTimeout(context.Background(), wait+2*time.Second)
	defer cancel()
	found, err := dhcp.Probe(ctx, port, nil, wait)
	return ProbeResult{Port: port, Offers: offersFrom(found), Err: err}
}

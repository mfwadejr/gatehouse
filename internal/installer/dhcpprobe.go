package installer

import (
	"bufio"
	"context"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
	// HasAddress is the IPv4 address the port already holds on the live system, when it has one. An address
	// that arrived by itself means something on that cable handed it out, even when no offer was heard.
	HasAddress netip.Addr
}

// Unsure says whether the answer is "could not tell": nothing was heard, yet the port already has an address.
// That is not the same as "no server": the live system got that address from somewhere.
func (r ProbeResult) Unsure() bool { return r.Err == nil && !r.Heard() && r.HasAddress.IsValid() }

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

// Where the live system keeps what it learned, and what the port holds now. Variables so tests can point
// them elsewhere.
var (
	leaseDir    = "/run/systemd/netif/leases" // systemd-networkd: one file per port, named by its index
	sysNetDir   = "/sys/class/net"
	currentAddr = func(port string) netip.Addr { // the IPv4 address a port holds now, if any
		ifi, err := net.InterfaceByName(port)
		if err != nil {
			return netip.Addr{}
		}
		addrs, _ := ifi.Addrs()
		for _, a := range addrs {
			if pf, err := netip.ParsePrefix(a.String()); err == nil && pf.Addr().Is4() && !pf.Addr().IsLinkLocalUnicast() && !pf.Addr().IsLoopback() {
				return pf.Addr()
			}
		}
		return netip.Addr{}
	}
)

// heldLease reads the lease the live system already holds on a port (its own DHCP client asked when the cable
// went in), which names the server that answered. It is the surest sign of a server on that cable, and does
// not depend on hearing a new offer in time.
func heldLease(port string) (Offer, bool) {
	idx, err := os.ReadFile(filepath.Join(sysNetDir, port, "ifindex"))
	if err != nil {
		return Offer{}, false
	}
	f, err := os.Open(filepath.Join(leaseDir, strings.TrimSpace(string(idx))))
	if err != nil {
		return Offer{}, false
	}
	defer f.Close()
	kv := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if k, v, ok := strings.Cut(sc.Text(), "="); ok {
			kv[k] = strings.TrimSpace(v)
		}
	}
	srv, err := netip.ParseAddr(kv["SERVER_ADDRESS"])
	if err != nil || !srv.Is4() || srv.IsUnspecified() {
		return Offer{}, false
	}
	o := Offer{Server: srv}
	if y, err := netip.ParseAddr(kv["ADDRESS"]); err == nil && y.Is4() {
		if m, err := netip.ParseAddr(kv["NETMASK"]); err == nil && m.Is4() {
			b := m.As4()
			if ones, bits := net.IPMask(b[:]).Size(); bits == 32 && ones > 0 {
				o.Given = netip.PrefixFrom(y, ones)
			}
		}
	}
	if r, err := netip.ParseAddr(kv["ROUTER"]); err == nil && r.Is4() {
		o.Router = r
	}
	return o, true
}

// ProbeDHCP listens on one port for DHCP servers for the wait time (sending the discover again every two
// seconds), and adds the server named by the lease the live system already holds there. The port must be up.
// Any failure to listen comes back in ProbeResult.Err and means "could not check"; a held lease still counts.
func ProbeDHCP(port string, wait time.Duration) ProbeResult {
	ctx, cancel := context.WithTimeout(context.Background(), wait+2*time.Second)
	defer cancel()
	found, err := dhcp.Probe(ctx, port, nil, wait)
	res := ProbeResult{Port: port, Offers: offersFrom(found), Err: err, HasAddress: currentAddr(port)}
	if o, ok := heldLease(port); ok {
		known := false
		for _, x := range res.Offers {
			if x.Server == o.Server {
				known = true
			}
		}
		if !known {
			res.Offers = append(res.Offers, o)
		}
		res.Err = nil // a server was found, so the check did not fail
	}
	return res
}

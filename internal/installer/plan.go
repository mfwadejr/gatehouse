package installer

import (
	"errors"
	"fmt"
	"net/netip"
	"os"
	"strings"
)

// What the person chose for the network during the install: which port is the internet and
// which address the management port has. It is left on the new system so the first start can
// serve that address on the management port straight away, and the browser setup starts from
// the same answers instead of asking again.

// InstallNetPath is where the choices are left on the installed system.
var InstallNetPath = "/var/lib/gatehouse/install-net"

// InstallNetFile is the same place, relative to the new system's root.
const InstallNetFile = "var/lib/gatehouse/install-net"

// InstallNet is what the installer remembered.
type InstallNet struct {
	WanPort  string // the internet port, found again by its MAC address
	MgmtAddr string // the management port's address with its size, like 192.168.50.1/24
}

// resolvePort finds a port by its MAC address, falling back to the name it had at install time.
func resolvePort(name, mac string) string {
	if mac != "" {
		for _, p := range WiredPorts() {
			if strings.EqualFold(p.MAC, mac) {
				return p.Name
			}
		}
	}
	return name
}

// ReadInstallNet returns what the installer left, or the zero value when nothing was chosen.
func ReadInstallNet() InstallNet {
	var n InstallNet
	b, err := os.ReadFile(InstallNetPath)
	if err != nil {
		return n
	}
	for _, l := range strings.Split(string(b), "\n") {
		f := strings.Fields(l)
		switch {
		case len(f) >= 2 && f[0] == "wan":
			mac := ""
			if len(f) > 2 {
				mac = f[2]
			}
			n.WanPort = resolvePort(f[1], mac)
		case len(f) >= 2 && f[0] == "mgmt":
			if p, err := ParseMgmtAddr(f[1]); err == nil {
				n.MgmtAddr = p.String()
			}
		}
	}
	return n
}

// installNetBody is the file's content for a request.
func (r *Request) installNetBody() string {
	var b strings.Builder
	if r.WanPort != "" {
		fmt.Fprintf(&b, "wan %s %s\n", r.WanPort, r.WanMAC)
	}
	if r.MgmtAddr != "" {
		fmt.Fprintf(&b, "mgmt %s\n", r.MgmtAddr)
	}
	return b.String()
}

// ParseMgmtAddr reads the management address. A bare address means a /24 network. It must be a
// private IPv4 address that is neither the network address nor the broadcast address.
func ParseMgmtAddr(s string) (netip.Prefix, error) {
	s = strings.TrimSpace(s)
	if !strings.Contains(s, "/") {
		s += "/24"
	}
	p, err := netip.ParsePrefix(s)
	if err != nil || !p.Addr().Is4() {
		return netip.Prefix{}, errors.New("enter an address like 192.168.50.1")
	}
	a := p.Addr()
	switch {
	case !a.IsPrivate():
		return netip.Prefix{}, errors.New("use a private address, such as one starting 192.168, 10. or 172.16")
	case p.Bits() < 16 || p.Bits() > 29:
		return netip.Prefix{}, errors.New("the network size must be between /16 and /29")
	case a == p.Masked().Addr():
		return netip.Prefix{}, errors.New("that is the network's own address; use one ending in .1, for example")
	}
	last := p.Masked().Addr().As4()
	host := 32 - p.Bits()
	n := uint32(last[0])<<24 | uint32(last[1])<<16 | uint32(last[2])<<8 | uint32(last[3])
	n |= (1 << host) - 1
	if a == netip.AddrFrom4([4]byte{byte(n >> 24), byte(n >> 16), byte(n >> 8), byte(n)}) {
		return netip.Prefix{}, errors.New("that is the broadcast address; choose another")
	}
	return p, nil
}

// mgmtCandidates are the addresses offered first. They avoid the ranges modems and routers
// usually use (192.168.0.x, 192.168.1.x, 192.168.100.x, 10.0.0.x), so a modem plugged in later
// does not land in the same range as the local network.
var mgmtCandidates = []string{"192.168.50.1/24", "192.168.77.1/24", "10.10.10.1/24", "172.16.50.1/24", "192.168.201.1/24"}

// SuggestMgmtAddr picks the first candidate that does not overlap anything in use.
func SuggestMgmtAddr(used []netip.Prefix) string {
	for _, c := range mgmtCandidates {
		p := netip.MustParsePrefix(c)
		clash := false
		for _, u := range used {
			if u.Masked().Overlaps(p.Masked()) {
				clash = true
			}
		}
		if !clash {
			return c
		}
	}
	return mgmtCandidates[0]
}

// ClashesWith reports which of the given networks overlap the management network.
func ClashesWith(addr netip.Prefix, used []netip.Prefix) bool {
	for _, u := range used {
		if u.Masked().Overlaps(addr.Masked()) {
			return true
		}
	}
	return false
}

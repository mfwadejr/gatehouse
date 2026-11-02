package app

import "net"

// ifaceAddrs lists the addresses a port has: IPv4 with its prefix, and global IPv6.
func ifaceAddrs(name string) []string {
	out := []string{}
	ifi, err := net.InterfaceByName(name)
	if err != nil {
		return out
	}
	as, _ := ifi.Addrs()
	for _, a := range as {
		ip, _, err := net.ParseCIDR(a.String())
		if err != nil || ip.IsLinkLocalUnicast() {
			continue
		}
		out = append(out, a.String())
	}
	return out
}

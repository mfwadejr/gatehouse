package ha

import (
	"errors"
	"fmt"
	"net/netip"
)

// The heartbeat cable: an optional cable between one spare port on each unit, used for the
// private conversation between them (settings copy, status, update hand-over and the connection
// table copy) so that conversation does not depend on the shared network. The shared network
// still works without it, and a cable that is unplugged never makes either unit take over: the
// takeover decision stays with the shared-address protocol on the shared network.

// HeartNet is the tiny network that exists only on the cable, chosen from the pair's id so both
// units agree without being told. It is a /30 inside 10.255.0.0/16, which no ordinary network uses.
func HeartNet(pairID string) netip.Prefix {
	h := 0
	for _, c := range pairID {
		h = (h*31 + int(c)) % 200
	}
	return netip.PrefixFrom(netip.AddrFrom4([4]byte{10, 255, byte(40 + h), 0}), 30)
}

// HeartAddrs are the primary's and the standby's own addresses on the cable.
func HeartAddrs(n netip.Prefix) (primary, standby netip.Addr) {
	b := n.Masked().Addr().As4()
	p, s := b, b
	p[3], s[3] = b[3]+1, b[3]+2
	return netip.AddrFrom4(p), netip.AddrFrom4(s)
}

// HeartOwn returns this unit's own address with the prefix, and the partner's address.
func HeartOwn(pairID string, role Role) (own netip.Prefix, peer netip.Addr, err error) {
	n := HeartNet(pairID)
	p, s := HeartAddrs(n)
	switch role {
	case Primary:
		return netip.PrefixFrom(p, 30), s, nil
	case Standby:
		return netip.PrefixFrom(s, 30), p, nil
	}
	return netip.Prefix{}, netip.Addr{}, errors.New("unknown role")
}

// HeartConntrackdConf is the connection-table copy settings when the cable carries it.
func HeartConntrackdConf(p Pairing, role Role, iface string) (string, error) {
	own, peer, err := HeartOwn(p.ID, role)
	if err != nil {
		return "", err
	}
	q := p
	q.Primary, q.Standby = own.Addr().String(), peer.String()
	if role == Standby {
		q.Primary, q.Standby = peer.String(), own.Addr().String()
	}
	if iface == "" || len(iface) > 15 {
		return "", fmt.Errorf("the cable's port is needed")
	}
	return ConntrackdConf(q, role, iface)
}

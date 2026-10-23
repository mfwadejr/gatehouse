// Package seed writes the network choices made in the installer into a new system's
// configuration, so that the first boot comes up working without further setup.
package seed

import (
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"strings"

	"gatehouse/internal/crypt"
	"gatehouse/internal/fw"
	"gatehouse/internal/store"
)

// LAN says how the port facing the person's own devices behaves.
type LAN struct {
	Port string `json:"port"`
	// Mode is "router" (Gatehouse hands out addresses and is the gateway) or "behind"
	// (another router does that and Gatehouse acts as firewall and DNS on its network).
	Mode string `json:"mode"`

	// Router mode.
	Address string `json:"address"` // this device on the LAN, for example 192.168.1.1/24

	// Behind mode. Addressing is "dhcp" or "static".
	Addressing string `json:"addressing"`
	Static     string `json:"static"` // CIDR
	Gateway    string `json:"gateway"`
	DNS        string `json:"dns"`
}

// WAN is the optional internet connection.
type WAN struct {
	Port      string `json:"port"`
	Type      string `json:"type"` // dhcp, static, pppoe
	Addr      string `json:"addr"`
	Gateway   string `json:"gateway"`
	DNS       string `json:"dns"`
	PPPoEUser string `json:"pppoe_user"`
	PPPoEPass string `json:"pppoe_pass"`
}

// Network is the whole choice.
type Network struct {
	LAN LAN  `json:"lan"`
	WAN *WAN `json:"wan,omitempty"`
	// Bridge lists extra ports that join the LAN port as one switch. Ports not listed anywhere
	// are left unused.
	Bridge []string `json:"bridge,omitempty"`
}

var rePort = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,14}$`)

// BridgeName is the name of the LAN bridge when extra ports join it.
const BridgeName = "br-lan"

// Validate checks everything before anything is written.
func (n *Network) Validate() error {
	l := &n.LAN
	if !rePort.MatchString(l.Port) {
		return errors.New("choose the port for your local network")
	}
	seen := map[string]bool{l.Port: true}
	if n.WAN != nil {
		if !rePort.MatchString(n.WAN.Port) {
			return errors.New("choose the port for the internet connection, or leave it out")
		}
		if seen[n.WAN.Port] {
			return errors.New("the internet and local network need different ports")
		}
		seen[n.WAN.Port] = true
		switch n.WAN.Type {
		case "dhcp":
		case "static":
			if _, err := netip.ParsePrefix(n.WAN.Addr); err != nil {
				return errors.New("the internet address must look like 203.0.113.10/24")
			}
			if g, err := netip.ParseAddr(n.WAN.Gateway); err != nil || !g.Is4() {
				return errors.New("enter the internet gateway address")
			}
		case "pppoe":
			if n.WAN.PPPoEUser == "" || n.WAN.PPPoEPass == "" {
				return errors.New("enter the user name and password from your internet provider")
			}
		default:
			return errors.New("choose how the internet connection gets its address")
		}
	}
	for _, p := range n.Bridge {
		if !rePort.MatchString(p) {
			return fmt.Errorf("%q is not a port name", p)
		}
		if seen[p] {
			return fmt.Errorf("port %s is already used", p)
		}
		seen[p] = true
	}
	switch l.Mode {
	case "router":
		p, err := netip.ParsePrefix(l.Address)
		if err != nil || !p.Addr().Is4() {
			return errors.New("the local network address must look like 192.168.1.1/24")
		}
		if !p.Addr().IsPrivate() {
			return errors.New("use a private network range such as 192.168.x.x, 10.x.x.x or 172.16-31.x.x")
		}
		if p.Bits() < 16 || p.Bits() > 29 {
			return errors.New("the local network size must be between /16 and /29")
		}
		if p.Addr() == p.Masked().Addr() {
			return errors.New("the address of this device cannot be the network address itself")
		}
	case "behind":
		if n.WAN != nil {
			return errors.New("when Gatehouse sits behind another router, that router provides the internet, so leave the internet port out")
		}
		switch l.Addressing {
		case "dhcp":
		case "static":
			p, err := netip.ParsePrefix(l.Static)
			if err != nil || !p.Addr().Is4() {
				return errors.New("the address must look like 192.168.0.50/24")
			}
			if g, err := netip.ParseAddr(l.Gateway); err != nil || !g.Is4() || !p.Contains(g) {
				return errors.New("enter your router's address, inside the same network")
			}
		default:
			return errors.New("choose automatic or fixed for this device's address")
		}
	default:
		return errors.New("choose how Gatehouse is used on your network")
	}
	return nil
}

// Suggest returns a private network that does not overlap any of used. The result is the
// address of this device within it, for example 192.168.1.1/24.
func Suggest(used []netip.Prefix) string {
	for _, c := range []string{"192.168.1.0/24", "192.168.50.0/24", "10.10.10.0/24", "172.16.20.0/24", "192.168.88.0/24", "10.20.30.0/24"} {
		p := netip.MustParsePrefix(c)
		clash := false
		for _, u := range used {
			if u.Overlaps(p) {
				clash = true
			}
		}
		if !clash {
			return firstHost(p)
		}
	}
	return "192.168.1.1/24"
}

func firstHost(p netip.Prefix) string {
	a := p.Masked().Addr().Next()
	return netip.PrefixFrom(a, p.Bits()).String()
}

// Range picks a sensible pool of addresses for DHCP inside the network of addr.
func Range(addr netip.Prefix) (start, end netip.Addr) {
	base := u32(addr.Masked().Addr())
	size := uint32(1) << (32 - addr.Bits())
	hosts := size - 2
	s := base + 1 + hosts*39/100
	e := base + size - 1 - 5
	if hosts < 30 {
		s, e = base+size/2, base+size-2
	}
	if s > e {
		s = e
	}
	return fromU32(s), fromU32(e)
}

func u32(a netip.Addr) uint32 {
	b := a.As4()
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}

func fromU32(v uint32) netip.Addr {
	return netip.AddrFrom4([4]byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)})
}

// Apply stores the choice. The first boot applies it without asking for confirmation, because
// there is nothing earlier to return to.
func Apply(db *store.DB, box *crypt.Box, n *Network) error {
	if err := n.Validate(); err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	l := n.LAN
	zone := fw.Zone{Name: "Home", Kind: fw.KindTrusted}
	fw.ApplyPreset(&zone)
	zone.Gateway = l.Mode == "router"
	lanName := l.Port
	members := ""
	if len(n.Bridge) > 0 {
		lanName = BridgeName
		members = strings.Join(append([]string{l.Port}, n.Bridge...), ",")
	}
	if _, err := fw.SaveZone(tx, fw.ZoneRow{Name: zone.Name, Kind: zone.Kind, Ifaces: []string{lanName}, Internet: zone.Internet,
		Management: zone.Management, IntraAllow: zone.IntraAllow, Gateway: zone.Gateway, DNSForce: true,
		Description: "Your own computers and phones"}); err != nil {
		return err
	}
	kind := "phys"
	if members != "" {
		kind = "bridge"
	}
	var addr, gw, dns string
	var dhcp int
	switch {
	case l.Mode == "router":
		addr = l.Address
	case l.Addressing == "static":
		addr, gw, dns = l.Static, l.Gateway, l.DNS
	default:
		dhcp = 1
	}
	if _, err := tx.Exec(`UPDATE interfaces SET kind=?, ipv4_cidr=?, dhcp=?, gateway=?, dns=?, members=?, enabled=1 WHERE name=?`,
		kind, addr, dhcp, gw, dns, members, lanName); err != nil {
		return err
	}

	if l.Mode == "router" {
		p := netip.MustParsePrefix(l.Address)
		s, e := Range(p)
		if _, err := tx.Exec(`INSERT INTO dhcp_scopes(name,iface,mode,subnet,range_start,range_end,router,dns,domain,lease_seconds,enabled)
			VALUES('Home',?,'serve',?,?,?,?,?,'lan',86400,1)`, lanName, p.Masked().String(), s.String(), e.String(), p.Addr().String(), p.Addr().String()); err != nil {
			return err
		}
	}
	if w := n.WAN; w != nil {
		sealed := ""
		if w.Type == "pppoe" {
			sealed = box.Seal(w.PPPoEPass)
		}
		if _, err := tx.Exec(`INSERT INTO wan_links(name,iface,type,ipv4_addr,ipv4_gw,dns,pppoe_user,pppoe_pass,role,priority,weight,enabled)
			VALUES('Internet',?,?,?,?,?,?,?,'primary',1,1,1)`, w.Port, w.Type, w.Addr, w.Gateway, w.DNS, w.PPPoEUser, sealed); err != nil {
			return err
		}
	}
	for k, v := range map[string]string{"net.autoapply": "1", "fw.stepup": "1"} {
		if _, err := tx.Exec(`INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, k, v); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Package bridgemode describes Gatehouse working as a transparent filtering bridge: it sits between
// another router and the local network, passes traffic through at the link level and filters it,
// instead of routing. Everything here is plain data and checks; nothing touches the machine.
//
// The management address lives on the bridge itself. Which side may reach the console is decided by
// the firewall (see fw.compileBridge): the inside side by default, the outside side never unless a
// subnet is named on purpose.
package bridgemode

import (
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"strings"
)

// Name is the bridge interface. It is fixed so that the firewall and the network files agree.
const Name = "br-gate"

var reName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,14}$`)

// Allow is one exception that lets new connections in from the outside to a device inside.
type Allow struct {
	Name  string `json:"name"`
	Proto string `json:"proto"` // tcp or udp
	Port  int    `json:"port"`
	To    string `json:"to"` // an inside address
}

// Config is the whole bridge mode setup.
type Config struct {
	Enabled bool     `json:"enabled"`
	Outside string   `json:"outside"` // the port toward the other router
	Inside  []string `json:"inside"`  // the ports toward the local network
	// OOB is an optional management-only port. It is never added to the bridge: it keeps its own
	// address, so the console stays reachable whatever the bridge does.
	OOB     string `json:"oob"`
	OOBAddr string `json:"oob_addr"` // address with prefix on the OOB port
	// The management address of the bridge.
	Mode    string `json:"mode"` // static or dhcp
	Addr    string `json:"addr"` // CIDR, for static
	Gateway string `json:"gateway"`
	DNS     string `json:"dns"`
	// Subnets on the outside that may also reach the console (for example a VPN or office range).
	Subnets []string `json:"subnets"`
	Allow   []Allow  `json:"allow"`
	// Stateful keeps new connections from the outside out of the inside (the default).
	Open bool `json:"open"` // true lets everything through unfiltered, for a trial
}

// Ports lists every port the bridge mode uses.
func (c Config) Ports() []string {
	p := append([]string{c.Outside}, c.Inside...)
	if c.OOB != "" {
		p = append(p, c.OOB)
	}
	return p
}

// Members are the ports that join the bridge.
func (c Config) Members() []string { return append([]string{c.Outside}, c.Inside...) }

// Validate checks a setup in plain words. It only checks the setup itself; whether the named ports
// exist on this machine is checked by CheckPorts.
func (c Config) Validate() error {
	if !c.Enabled {
		return nil
	}
	var errs []string
	add := func(f string, a ...any) { errs = append(errs, fmt.Sprintf(f, a...)) }
	if !reName.MatchString(c.Outside) {
		add("choose the port that goes to your other router")
	}
	if len(c.Inside) == 0 {
		add("choose at least one port for the local network")
	}
	seen := map[string]bool{}
	for _, p := range c.Ports() {
		if !reName.MatchString(p) {
			add("%q is not a usable port name", p)
			continue
		}
		if seen[p] {
			add("%s is used twice", p)
		}
		seen[p] = true
	}
	switch c.Mode {
	case "dhcp":
	case "static":
		pf, err := netip.ParsePrefix(c.Addr)
		if err != nil || !pf.Addr().Is4() {
			add("the management address must look like 192.168.1.2/24")
		} else if gw := strings.TrimSpace(c.Gateway); gw != "" {
			g, e := netip.ParseAddr(gw)
			if e != nil || !pf.Contains(g) {
				add("the router address must be inside the management network")
			}
		} else {
			add("enter your other router's address; without it Gatehouse cannot reach the internet for updates")
		}
	default:
		add("the management address must be fixed or automatic")
	}
	for _, d := range strings.FieldsFunc(c.DNS, func(r rune) bool { return r == ',' || r == ' ' }) {
		if _, err := netip.ParseAddr(d); err != nil {
			add("%q is not a DNS server address", d)
		}
	}
	for _, s := range c.Subnets {
		if _, err := netip.ParsePrefix(s); err != nil {
			add("%q is not a network like 10.8.0.0/24", s)
		}
	}
	if c.OOB != "" {
		if pf, err := netip.ParsePrefix(c.OOBAddr); err != nil || !pf.Addr().Is4() {
			add("the management-only port needs its own address like 192.168.99.1/24")
		} else if c.Mode == "static" {
			if bp, e := netip.ParsePrefix(c.Addr); e == nil && bp.Overlaps(pf) {
				add("the management-only port's network must differ from the bridge's network")
			}
		}
	}
	if len(c.Allow) > 32 {
		add("at most 32 exceptions")
	}
	for _, a := range c.Allow {
		if a.Proto != "tcp" && a.Proto != "udp" {
			add("exception %q: protocol must be tcp or udp", a.Name)
		}
		if a.Port < 1 || a.Port > 65535 {
			add("exception %q: port must be 1 to 65535", a.Name)
		}
		if ip, err := netip.ParseAddr(a.To); err != nil || !ip.Is4() {
			add("exception %q: %q is not an inside address", a.Name, a.To)
		}
		if strings.ContainsAny(a.Name, "\n\r\"\\") {
			add("exception name has characters that are not allowed")
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

// PortInfo is what is known about a port on this machine, for the checks.
type PortInfo struct {
	Name     string
	Wireless bool
	UsedFor  string // "internet", "network" or "" when free
}

// CheckPorts refuses setups that name missing or already-used ports, and returns advice that does
// not stop anything. current are the ports the person is managing from right now, if known.
func (c Config) CheckPorts(have []PortInfo, current []string) (warnings []string, err error) {
	if !c.Enabled {
		return nil, nil
	}
	by := map[string]PortInfo{}
	for _, p := range have {
		by[p.Name] = p
	}
	var errs []string
	for _, n := range c.Ports() {
		p, ok := by[n]
		if !ok {
			errs = append(errs, fmt.Sprintf("%s is not a port on this machine", n))
			continue
		}
		if p.Wireless {
			errs = append(errs, fmt.Sprintf("%s is a Wi-Fi card; a bridge needs wired ports", n))
		}
	}
	if len(errs) > 0 {
		return nil, errors.New(strings.Join(errs, "; "))
	}
	if len(current) > 0 {
		reach, outsideOnly := false, false
		for _, cp := range current {
			if cp == c.OOB {
				reach = true
			}
			for _, m := range c.Inside {
				if m == cp {
					reach = true
				}
			}
			if cp == c.Outside {
				outsideOnly = true
			}
		}
		switch {
		case reach:
		case outsideOnly:
			warnings = append(warnings, "You are managing from the port that will face your other router. Only the subnets you listed can reach the console after this change, so you may lose it; the 60-second way back will restore it.")
		default:
			warnings = append(warnings, "You are managing from a port that is not part of the bridge mode setup, so that connection will stop working; the 60-second way back will restore it.")
		}
	}
	if c.OOB == "" {
		warnings = append(warnings, "No management-only port is set. If the bridge is set up wrongly you can still use the 60-second way back, or the keyboard and screen on this machine.")
	}
	return warnings, nil
}

// Interface mirrors the fields the network file generator needs, without importing it.
type Interface struct {
	Name    string
	Kind    string
	Members []string
	IPv4    string
	DHCP    bool
	Gateway string
	DNS     string
	Enabled bool
}

// Interfaces gives the interfaces to configure: the bridge with its management address, and the
// optional management-only port on its own.
func (c Config) Interfaces() []Interface {
	br := Interface{Name: Name, Kind: "bridge", Members: c.Members(), Enabled: true}
	if c.Mode == "dhcp" {
		br.DHCP = true
	} else {
		br.IPv4, br.Gateway, br.DNS = c.Addr, strings.TrimSpace(c.Gateway), c.DNS
	}
	out := []Interface{br}
	if c.OOB != "" {
		out = append(out, Interface{Name: c.OOB, Kind: "phys", IPv4: c.OOBAddr, Enabled: true})
	}
	return out
}

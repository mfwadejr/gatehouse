// Package netcfg describes the machine's network interfaces and internet connections and
// turns that description into systemd-networkd and pppd configuration.
//
// Generation is a pure function of the description, so it is easy to test. Applying the
// result (writing files, reloading services) happens in the privileged helper.
package netcfg

import (
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"sort"
	"strings"
)

// Interface is a LAN-side port or VLAN.
type Interface struct {
	Name   string `json:"name"`
	Kind   string `json:"kind"` // phys, vlan or bridge
	Parent string `json:"parent"`
	// Members are the ports of a bridge: several ports behaving as one switch.
	Members []string `json:"members"`
	VLAN    int      `json:"vlan"`
	IPv4    string   `json:"ipv4"` // CIDR, for example 192.168.1.1/24
	// DHCP takes the address from another router instead (Gatehouse sits behind it). With a
	// fixed IPv4 address and no DHCP, Gateway and DNS say where that router is.
	DHCP    bool   `json:"dhcp"`
	Gateway string `json:"gateway"`
	DNS     string `json:"dns"`
	IPv6    string `json:"ipv6"`
	PD      bool   `json:"pd"` // receive a delegated IPv6 prefix from the internet link
	MTU     int    `json:"mtu"`
	Enabled bool   `json:"enabled"`
}

// WAN is one internet connection.
type WAN struct {
	Name      string `json:"name"`
	Iface     string `json:"iface"` // physical port
	Type      string `json:"type"`  // dhcp, static, pppoe
	VLAN      int    `json:"vlan"`  // 802.1Q tag on the port, 0 for none
	Addr      string `json:"addr"`  // static CIDR
	Gateway   string `json:"gateway"`
	DNS       string `json:"dns"`
	PPPoEUser string `json:"pppoe_user"`
	PPPoEPass string `json:"pppoe_pass"` // plaintext, present only while applying
	V6Mode    string `json:"v6_mode"`    // none, slaac, pd, static
	V6Addr    string `json:"v6_addr"`
	V6Gateway string `json:"v6_gateway"`
	Priority  int    `json:"priority"`
	MTU       int    `json:"mtu"`
	Enabled   bool   `json:"enabled"`
}

// Config is everything needed to produce files.
type Config struct {
	Interfaces []Interface `json:"interfaces"`
	WANs       []WAN       `json:"wans"`
	WireGuard  []WG        `json:"wireguard,omitempty"`
	Tunnels    []Tunnel    `json:"tunnels,omitempty"`
	Wifi       []Wifi      `json:"wifi,omitempty"`
}

// Output is the set of files to install.
type Output struct {
	Networkd map[string]string // file name in /etc/systemd/network
	PPPPeers map[string]string // file name in /etc/ppp/peers
	PPPUnits []string          // ppp links that must run
	Secrets  string            // lines for chap-secrets and pap-secrets
	Sysctl   string
	// Private holds secret files (keys), by file name inside the VPN key directory.
	Private map[string]string
	// Wifi holds the wireless card configuration files (they contain passphrases), by file name
	// inside WifiDir.
	Wifi map[string]string
}

var (
	reName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,14}$`)
	reUser = regexp.MustCompile(`^[A-Za-z0-9@._+\-]{1,64}$`)
)

// WanDevice is the interface a link uses on the wire; it must match the firewall's idea of it.
func WanDevice(w WAN) string {
	switch {
	case w.Type == "pppoe":
		return "ppp-" + SafeLinkName(w.Name)
	case w.VLAN > 0:
		return fmt.Sprintf("%s.%d", w.Iface, w.VLAN)
	}
	return w.Iface
}

// SafeLinkName reduces a display name to a short token for unit and interface names.
func SafeLinkName(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	out := b.String()
	if len(out) > 8 {
		out = out[:8]
	}
	return out
}

func prefix(s string, v6 bool) (netip.Prefix, error) {
	p, err := netip.ParsePrefix(s)
	if err != nil {
		return p, fmt.Errorf("%q is not an address with a prefix length, for example 192.168.1.1/24", s)
	}
	if p.Addr().Is6() != v6 {
		return p, fmt.Errorf("%q is the wrong kind of address", s)
	}
	return p, nil
}

func addr(s string, v6 bool) (netip.Addr, error) {
	a, err := netip.ParseAddr(s)
	if err != nil || a.Is6() != v6 {
		return a, fmt.Errorf("%q is not a valid address", s)
	}
	return a, nil
}

// Validate checks the description before anything is written.
func (c *Config) Validate() error {
	var errs []string
	add := func(f string, a ...any) { errs = append(errs, fmt.Sprintf(f, a...)) }
	names := map[string]bool{}
	for _, i := range c.Interfaces {
		if !reName.MatchString(i.Name) {
			add("interface name %q is not valid", i.Name)
			continue
		}
		if names[i.Name] {
			add("interface %s is listed twice", i.Name)
		}
		names[i.Name] = true
		switch i.Kind {
		case "phys":
		case "bridge":
			if len(i.Members) == 0 {
				add("%s: a bridge needs at least one port", i.Name)
			}
			for _, m := range i.Members {
				if !reName.MatchString(m) {
					add("%s: port name %q is not valid", i.Name, m)
				}
				if m == i.Name {
					add("%s: a bridge cannot contain itself", i.Name)
				}
			}
		case "vlan":
			if i.VLAN < 1 || i.VLAN > 4094 {
				add("%s: VLAN number must be between 1 and 4094", i.Name)
			}
			if !reName.MatchString(i.Parent) {
				add("%s: pick the port the VLAN rides on", i.Name)
			}
		default:
			add("%s: unknown kind %q", i.Name, i.Kind)
		}
		if i.IPv4 != "" {
			if _, err := prefix(i.IPv4, false); err != nil {
				add("%s: %v", i.Name, err)
			}
		}
		if i.IPv6 != "" {
			if _, err := prefix(i.IPv6, true); err != nil {
				add("%s: %v", i.Name, err)
			}
		}
		if i.DHCP && i.IPv4 != "" {
			add("%s: choose either an automatic address or a fixed one", i.Name)
		}
		if i.Gateway != "" {
			gw, err := netip.ParseAddr(i.Gateway)
			pf, perr := netip.ParsePrefix(i.IPv4)
			switch {
			case err != nil || !gw.Is4():
				add("%s: %q is not an IPv4 address for the router", i.Name, i.Gateway)
			case perr != nil || !pf.Contains(gw):
				add("%s: the router address must be inside this network", i.Name)
			}
		}
		for _, d := range strings.FieldsFunc(i.DNS, func(r rune) bool { return r == ',' || r == ' ' }) {
			if a, err := netip.ParseAddr(d); err != nil {
				add("%s: %q is not an address", i.Name, d)
			} else if a.IsUnspecified() {
				add("%s: %q cannot be a DNS server", i.Name, d)
			}
		}
		if i.MTU != 0 && (i.MTU < 576 || i.MTU > 9216) {
			add("%s: MTU must be between 576 and 9216", i.Name)
		}
	}
	for _, i := range c.Interfaces {
		if i.Kind != "bridge" {
			continue
		}
		for _, m := range i.Members {
			if names[m] {
				add("%s is in the bridge %s and also set up on its own", m, i.Name)
			}
		}
	}
	wnames := map[string]bool{}
	for _, w := range c.WANs {
		if w.Name == "" || len(w.Name) > 32 || strings.ContainsAny(w.Name, "\n\r\"'\\") {
			add("internet connection name %q is not valid", w.Name)
		}
		if SafeLinkName(w.Name) == "" {
			add("internet connection %q needs at least one letter or digit in its name", w.Name)
		}
		if wnames[SafeLinkName(w.Name)] {
			add("internet connection names must differ (%q)", w.Name)
		}
		wnames[SafeLinkName(w.Name)] = true
		if !reName.MatchString(w.Iface) {
			add("%s: port name %q is not valid", w.Name, w.Iface)
		}
		if w.VLAN < 0 || w.VLAN > 4094 {
			add("%s: VLAN number must be between 1 and 4094", w.Name)
		}
		switch w.Type {
		case "dhcp":
		case "static":
			if _, err := prefix(w.Addr, false); err != nil {
				add("%s: %v", w.Name, err)
			}
			if _, err := addr(w.Gateway, false); err != nil {
				add("%s: gateway: %v", w.Name, err)
			}
		case "pppoe":
			if !reUser.MatchString(w.PPPoEUser) {
				add("%s: PPPoE username has characters that are not allowed", w.Name)
			}
			if strings.ContainsAny(w.PPPoEPass, "\n\r\"\\") || w.PPPoEPass == "" && w.Enabled {
				add("%s: PPPoE password is missing or has characters that are not allowed", w.Name)
			}
		default:
			add("%s: unknown connection type %q", w.Name, w.Type)
		}
		switch w.V6Mode {
		case "", "none", "slaac", "pd":
		case "static":
			if _, err := prefix(w.V6Addr, true); err != nil {
				add("%s: %v", w.Name, err)
			}
			if _, err := addr(w.V6Gateway, true); err != nil {
				add("%s: IPv6 gateway: %v", w.Name, err)
			}
		default:
			add("%s: unknown IPv6 mode %q", w.Name, w.V6Mode)
		}
		for _, d := range strings.FieldsFunc(w.DNS, func(r rune) bool { return r == ',' || r == ' ' }) {
			if _, err := netip.ParseAddr(d); err != nil {
				add("%s: DNS server %q is not an address", w.Name, d)
			}
		}
		if w.MTU != 0 && (w.MTU < 576 || w.MTU > 9216) {
			add("%s: MTU must be between 576 and 9216", w.Name)
		}
	}
	errs = append(errs, c.validateWifi()...)
	errs = append(errs, c.validateWG()...)
	errs = append(errs, c.validateTunnels()...)
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

type file struct{ b strings.Builder }

func (f *file) l(format string, a ...any) { fmt.Fprintf(&f.b, format+"\n", a...) }

// Generate produces the configuration files.
func Generate(c *Config) (*Output, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	out := &Output{Networkd: map[string]string{}, PPPPeers: map[string]string{}, Private: map[string]string{}, Wifi: map[string]string{}}

	// Which physical ports carry VLANs, and which VLANs.
	vlansOn := map[string][]string{}
	netdevs := map[string]*file{}
	addVLAN := func(parent, name string, id int) {
		for _, v := range vlansOn[parent] {
			if v == name {
				return
			}
		}
		vlansOn[parent] = append(vlansOn[parent], name)
		f := &file{}
		f.l("[NetDev]\nName=%s\nKind=vlan\n\n[VLAN]\nId=%d", name, id)
		netdevs[name] = f
	}
	for _, i := range c.Interfaces {
		if i.Enabled && i.Kind == "vlan" {
			addVLAN(i.Parent, i.Name, i.VLAN)
		}
	}
	for _, w := range c.WANs {
		if w.Enabled && w.VLAN > 0 && w.Type != "pppoe" {
			addVLAN(w.Iface, WanDevice(w), w.VLAN)
		}
		if w.Enabled && w.Type == "pppoe" && w.VLAN > 0 {
			addVLAN(w.Iface, fmt.Sprintf("%s.%d", w.Iface, w.VLAN), w.VLAN)
		}
	}
	for _, i := range c.Interfaces {
		if i.Enabled && i.Kind == "bridge" {
			f := &file{}
			f.l("[NetDev]\nName=%s\nKind=bridge\n\n[Bridge]\nSTP=no", i.Name)
			netdevs[i.Name] = f
		}
	}
	for name, f := range netdevs {
		out.Networkd["gatehouse-"+name+".netdev"] = f.b.String()
	}

	// The uplink that delegated IPv6 prefixes come from.
	uplink := ""
	wans := append([]WAN(nil), c.WANs...)
	sort.SliceStable(wans, func(i, j int) bool { return wans[i].Priority < wans[j].Priority })
	for _, w := range wans {
		if w.Enabled && w.V6Mode == "pd" {
			uplink = WanDevice(w)
			break
		}
	}

	written := map[string]bool{}
	netFile := func(dev string) *file {
		f := &file{}
		f.l("[Match]\nName=%s\n", dev)
		return f
	}
	finish := func(dev string, f *file) {
		out.Networkd["gatehouse-"+dev+".network"] = f.b.String()
		written[dev] = true
	}

	// WAN ports.
	for _, w := range wans {
		if !w.Enabled {
			continue
		}
		prio := w.Priority
		if prio < 1 {
			prio = 1
		}
		metric := prio * 100
		switch w.Type {
		case "pppoe":
			peer := "gh-" + SafeLinkName(w.Name)
			p := &file{}
			p.l("# Generated by Gatehouse")
			p.l("plugin pppoe.so")
			dev := w.Iface
			if w.VLAN > 0 {
				dev = fmt.Sprintf("%s.%d", w.Iface, w.VLAN)
			}
			p.l("nic-%s", dev)
			p.l("user \"%s\"", w.PPPoEUser)
			p.l("ifname %s", WanDevice(w))
			p.l("defaultroute\ndefaultroute-metric %d\nreplacedefaultroute", metric)
			p.l("noipdefault\nusepeerdns\npersist\nmaxfail 0\nholdoff 5\nlcp-echo-interval 10\nlcp-echo-failure 3")
			p.l("noauth\nhide-password\nnodetach\nnoaccomp\nnopcomp\nnovj\nnobsdcomp\nnodeflate")
			mtu := w.MTU
			if mtu == 0 {
				mtu = 1492
			}
			p.l("mtu %d\nmru %d", mtu, mtu)
			if w.V6Mode == "slaac" || w.V6Mode == "pd" {
				p.l("+ipv6")
			}
			out.PPPPeers[peer] = p.b.String()
			out.PPPUnits = append(out.PPPUnits, SafeLinkName(w.Name))
			out.Secrets += fmt.Sprintf("\"%s\" * \"%s\" *\n", w.PPPoEUser, w.PPPoEPass)
			// The carrier port itself only needs to be up.
			if !written[dev] {
				f := netFile(dev)
				f.l("[Network]\nLinkLocalAddressing=no\nIPv6AcceptRA=no\nConfigureWithoutCarrier=yes")
				if dev == w.Iface {
					for _, v := range vlansOn[dev] {
						f.l("VLAN=%s", v)
					}
				}
				finish(dev, f)
			}
			continue
		}
		dev := WanDevice(w)
		f := netFile(dev)
		f.l("[Network]")
		v6 := w.V6Mode
		switch w.Type {
		case "dhcp":
			if v6 == "pd" || v6 == "slaac" {
				f.l("DHCP=yes")
			} else {
				f.l("DHCP=ipv4")
			}
		case "static":
			f.l("Address=%s", w.Addr)
		}
		switch v6 {
		case "pd", "slaac":
			f.l("IPv6AcceptRA=yes")
		case "static":
			f.l("Address=%s", w.V6Addr)
			f.l("IPv6AcceptRA=no")
		default:
			f.l("IPv6AcceptRA=no\nLinkLocalAddressing=ipv4")
		}
		if dev == w.Iface {
			for _, v := range vlansOn[dev] {
				f.l("VLAN=%s", v)
			}
		}
		f.l("ConfigureWithoutCarrier=no")
		for _, d := range strings.FieldsFunc(w.DNS, func(r rune) bool { return r == ',' || r == ' ' }) {
			f.l("DNS=%s", d)
		}
		if w.Type == "static" {
			f.l("\n[Route]\nGateway=%s\nMetric=%d", w.Gateway, metric)
		}
		if w.Type == "static" && w.V6Mode == "static" {
			f.l("\n[Route]\nGateway=%s\nMetric=%d", w.V6Gateway, metric)
		}
		if w.Type == "dhcp" {
			f.l("\n[DHCPv4]\nClientIdentifier=mac\nUseDNS=no\nUseNTP=no\nUseHostname=no\nUseDomains=no\nRouteMetric=%d", metric)
			f.l("\n[DHCPv6]\nUseDNS=no\nUseNTP=no\nUseHostname=no\nWithoutRA=solicit")
		}
		f.l("\n[IPv6AcceptRA]\nUseDNS=no\nRouteMetric=%d", metric)
		if w.MTU > 0 {
			f.l("\n[Link]\nMTUBytes=%d", w.MTU)
		}
		finish(dev, f)
		// The physical port of a tagged link only carries the VLAN.
		if w.VLAN > 0 && !written[w.Iface] {
			p := netFile(w.Iface)
			p.l("[Network]\nLinkLocalAddressing=no\nIPv6AcceptRA=no\nConfigureWithoutCarrier=yes")
			for _, v := range vlansOn[w.Iface] {
				p.l("VLAN=%s", v)
			}
			finish(w.Iface, p)
		}
	}

	// Ports that belong to a bridge only carry traffic into it.
	for _, i := range c.Interfaces {
		if !i.Enabled || i.Kind != "bridge" {
			continue
		}
		for _, m := range i.Members {
			if written[m] {
				continue
			}
			f := netFile(m)
			f.l("[Network]\nBridge=%s\nConfigureWithoutCarrier=yes\nLinkLocalAddressing=no\nIPv6AcceptRA=no", i.Name)
			finish(m, f)
		}
	}

	// LAN side.
	for _, i := range c.Interfaces {
		if !i.Enabled || written[i.Name] {
			continue
		}
		f := netFile(i.Name)
		f.l("[Network]")
		if i.IPv4 != "" {
			f.l("Address=%s", i.IPv4)
		}
		if i.DHCP {
			f.l("DHCP=ipv4")
		}
		for _, d := range strings.FieldsFunc(i.DNS, func(r rune) bool { return r == ',' || r == ' ' }) {
			f.l("DNS=%s", d)
		}
		if i.IPv6 != "" {
			f.l("Address=%s", i.IPv6)
		}
		if i.PD && uplink != "" {
			f.l("DHCPPrefixDelegation=yes\nIPv6SendRA=yes")
		} else if i.IPv6 != "" {
			f.l("IPv6SendRA=yes")
		}
		f.l("ConfigureWithoutCarrier=yes")
		f.l("LinkLocalAddressing=ipv6")
		if i.Kind == "phys" {
			for _, v := range vlansOn[i.Name] {
				f.l("VLAN=%s", v)
			}
		}
		if i.Gateway != "" {
			f.l("\n[Route]\nGateway=%s", i.Gateway)
		}
		if i.DHCP {
			f.l("\n[DHCPv4]\nClientIdentifier=mac\nUseDNS=no\nUseNTP=no\nUseHostname=no\nUseDomains=no")
		}
		if i.PD && uplink != "" {
			f.l("\n[DHCPPrefixDelegation]\nUplinkInterface=%s\nAnnounce=yes\nToken=::1", uplink)
		}
		if i.IPv6 != "" || (i.PD && uplink != "") {
			f.l("\n[IPv6SendRA]\nEmitDNS=yes\nDNS=_link_local\nManaged=no\nOtherInformation=no")
		}
		if i.MTU > 0 {
			f.l("\n[Link]\nMTUBytes=%d", i.MTU)
		}
		finish(i.Name, f)
	}
	// Ports that only carry VLANs but have no config of their own.
	var parents []string
	for p := range vlansOn {
		parents = append(parents, p)
	}
	sort.Strings(parents)
	for _, p := range parents {
		if written[p] {
			continue
		}
		f := netFile(p)
		f.l("[Network]\nLinkLocalAddressing=no\nIPv6AcceptRA=no\nConfigureWithoutCarrier=yes")
		for _, v := range vlansOn[p] {
			f.l("VLAN=%s", v)
		}
		finish(p, f)
	}

	c.generateWifi(out)
	c.generateWG(out)
	c.generateTunnels(out)

	out.Sysctl = "# Generated by Gatehouse\nnet.ipv4.ip_forward=1\nnet.ipv6.conf.all.forwarding=1\nnet.ipv4.conf.all.rp_filter=2\nnet.ipv4.conf.default.rp_filter=2\nnet.ipv4.tcp_syncookies=1\nnet.ipv4.conf.all.accept_redirects=0\nnet.ipv6.conf.all.accept_redirects=0\nnet.ipv4.conf.all.send_redirects=0\n"
	return out, nil
}

// CGNAT reports whether an IPv4 address is in space that an ISP shares between customers.
func CGNAT(ip string) (shared bool, why string) {
	a, err := netip.ParseAddr(ip)
	if err != nil || !a.Is4() {
		return false, ""
	}
	if netip.MustParsePrefix("100.64.0.0/10").Contains(a) {
		return true, "Your internet provider shares one public address between many customers (carrier-grade NAT). Port forwarding and remote access will not work from the internet."
	}
	if a.IsPrivate() {
		return true, "The address on your internet connection is private, so another router sits in front of this one. Port forwarding will not work until that router forwards ports to this device or is put in bridge mode."
	}
	return false, ""
}

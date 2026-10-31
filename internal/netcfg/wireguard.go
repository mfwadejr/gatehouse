package netcfg

import (
	"encoding/base64"
	"fmt"
	"net/netip"
	"regexp"
	"strings"
)

// VPNKeyDir is where networkd reads WireGuard keys from. Files there are readable by root
// and the systemd-network group only.
// Tunnel routing tables are 300..MaxTunnelTable. Their policy rules sit at priorities 100-999,
// ahead of the internet-connection rules (1100 and up), and each tunnel has room for 50 sources.
const (
	// MarkBase is the first firewall mark a route uses; MarkPrioBase the first rule priority for them.
	MarkBase         = 0x1000
	MarkPrioBase     = 20
	MaxMarks         = 16
	MaxTunnelTable   = 307
	MaxTunnelSources = 50
)

const VPNKeyDir = "/etc/gatehouse/vpn"

// WGPeer is the other end of a tunnel.
type WGPeer struct {
	Name       string   `json:"name"`
	PublicKey  string   `json:"public_key"`
	PSK        string   `json:"psk,omitempty"` // plaintext only while applying
	AllowedIPs []string `json:"allowed_ips"`
	Endpoint   string   `json:"endpoint"` // host:port, client mode
	Keepalive  int      `json:"keepalive"`
	Enabled    bool     `json:"enabled"`
}

// WG is one WireGuard interface.
type WG struct {
	Name       string   `json:"name"`
	Port       int      `json:"port"`    // listen port; 0 picks one for a client
	Address    string   `json:"address"` // this device's address inside the tunnel, CIDR
	PrivateKey string   `json:"private_key,omitempty"`
	MTU        int      `json:"mtu"`
	Peers      []WGPeer `json:"peers"`
	Enabled    bool     `json:"enabled"`
	// Mode is "server" (devices connect in) or "client" (this device connects out).
	Mode string `json:"mode"`
	// Table and Sources make a client tunnel carry the internet traffic of the listed
	// prefixes: those sources are looked up in the tunnel's own routing table, whose default
	// route is the tunnel. Everyone else keeps using the normal internet connection.
	Table   int      `json:"table"`
	Sources []string `json:"sources"`
	// Marks are firewall marks whose traffic also uses the table (routes that send only some
	// destinations through the tunnel).
	Marks []int `json:"marks,omitempty"`
}

var (
	reWG       = regexp.MustCompile(`^wg[0-9]{1,2}$`)
	reEndpoint = regexp.MustCompile(`^[A-Za-z0-9.\-]{1,253}:[0-9]{1,5}$|^\[[0-9A-Fa-f:]+\]:[0-9]{1,5}$`)
)

func validKey(k string) bool {
	b, err := base64.StdEncoding.DecodeString(k)
	return err == nil && len(b) == 32
}

func (c *Config) validateWG() []string {
	var errs []string
	add := func(f string, a ...any) { errs = append(errs, fmt.Sprintf(f, a...)) }
	names, ports := map[string]bool{}, map[int]bool{}
	for _, w := range c.WireGuard {
		if !reWG.MatchString(w.Name) {
			add("VPN interface name %q is not valid", w.Name)
			continue
		}
		if names[w.Name] {
			add("VPN %s is listed twice", w.Name)
		}
		names[w.Name] = true
		if !w.Enabled {
			continue
		}
		if p, err := netip.ParsePrefix(w.Address); err != nil || !p.Addr().Is4() {
			add("%s: the tunnel address must look like 10.8.0.1/24", w.Name)
		}
		if w.Port < 0 || w.Port > 65535 {
			add("%s: port must be between 1 and 65535", w.Name)
		}
		if w.Port > 0 {
			if ports[w.Port] {
				add("%s: port %d is already used by another VPN", w.Name, w.Port)
			}
			ports[w.Port] = true
		}
		if !validKey(w.PrivateKey) {
			add("%s: the private key is missing or damaged", w.Name)
		}
		if w.MTU != 0 && (w.MTU < 1280 || w.MTU > 9000) {
			add("%s: MTU must be between 1280 and 9000", w.Name)
		}
		if w.Mode != "" && w.Mode != "server" && w.Mode != "client" {
			add("%s: mode must be server or client", w.Name)
		}
		for _, mk := range w.Marks {
			if mk < MarkBase || mk >= MarkBase+MaxMarks {
				add("%s: route mark %d is out of range", w.Name, mk)
			}
		}
		if w.Table != 0 || len(w.Sources) > 0 || len(w.Marks) > 0 {
			if w.Table < 300 || w.Table > MaxTunnelTable {
				add("%s: routing table %d is out of range", w.Name, w.Table)
			}
			if len(w.Sources) > MaxTunnelSources {
				add("%s: too many devices or networks use this tunnel (the limit is %d)", w.Name, MaxTunnelSources)
			}
			for _, src := range w.Sources {
				if p, err := netip.ParsePrefix(src); err != nil || !p.Addr().Is4() {
					add("%s: %q is not an IPv4 address range", w.Name, src)
				}
			}
		}
		seen := map[string]bool{}
		for _, p := range w.Peers {
			if !p.Enabled {
				continue
			}
			if !validKey(p.PublicKey) {
				add("%s: %s has an invalid public key", w.Name, p.Name)
			}
			if seen[p.PublicKey] {
				add("%s: %s repeats another device's key", w.Name, p.Name)
			}
			seen[p.PublicKey] = true
			if p.PSK != "" && !validKey(p.PSK) {
				add("%s: %s has an invalid shared secret", w.Name, p.Name)
			}
			if p.Endpoint != "" && !reEndpoint.MatchString(p.Endpoint) {
				add("%s: %s has an invalid endpoint (use host:port)", w.Name, p.Name)
			}
			if p.Keepalive < 0 || p.Keepalive > 600 {
				add("%s: %s keepalive must be 0 to 600 seconds", w.Name, p.Name)
			}
			for _, a := range p.AllowedIPs {
				if _, err := netip.ParsePrefix(a); err != nil {
					add("%s: %s: %q is not an address range", w.Name, p.Name, a)
				}
			}
			if len(p.AllowedIPs) == 0 {
				add("%s: %s has no addresses assigned", w.Name, p.Name)
			}
		}
	}
	return errs
}

func (c *Config) generateWG(out *Output) {
	for _, w := range c.WireGuard {
		if !w.Enabled {
			continue
		}
		keyFile := w.Name + ".key"
		out.Private[keyFile] = w.PrivateKey + "\n"
		d := &file{}
		d.l("# Generated by Gatehouse\n[NetDev]\nName=%s\nKind=wireguard", w.Name)
		if w.MTU > 0 {
			d.l("MTUBytes=%d", w.MTU)
		}
		d.l("\n[WireGuard]")
		if w.Port > 0 {
			d.l("ListenPort=%d", w.Port)
		}
		d.l("PrivateKeyFile=%s/%s", VPNKeyDir, keyFile)
		for i, p := range w.Peers {
			if !p.Enabled {
				continue
			}
			d.l("\n[WireGuardPeer]\nPublicKey=%s\nAllowedIPs=%s", p.PublicKey, strings.Join(p.AllowedIPs, ","))
			if p.PSK != "" {
				pf := fmt.Sprintf("%s-peer%d.psk", w.Name, i)
				out.Private[pf] = p.PSK + "\n"
				d.l("PresharedKeyFile=%s/%s", VPNKeyDir, pf)
			}
			if p.Endpoint != "" {
				d.l("Endpoint=%s", p.Endpoint)
			}
			if p.Keepalive > 0 {
				d.l("PersistentKeepalive=%d", p.Keepalive)
			}
		}
		out.Networkd["gatehouse-"+w.Name+".netdev"] = d.b.String()
		n := &file{}
		n.l("# Generated by Gatehouse\n[Match]\nName=%s\n\n[Network]\nAddress=%s\nIPv6AcceptRA=no\nLinkLocalAddressing=no", w.Name, w.Address)
		writeTunnelRoutes(n, w.Name, w.Address, w.Table, w.Sources, w.Marks, peerRoutes(w))
		out.Networkd["gatehouse-"+w.Name+".network"] = n.b.String()
	}
}

// peerRoutes are the ranges reachable through the tunnel: everything its peers may send us.
func peerRoutes(w WG) []string {
	var out []string
	for _, p := range w.Peers {
		if p.Enabled {
			out = append(out, p.AllowedIPs...)
		}
	}
	return out
}

// writeTunnelRoutes adds the routes and policy rules of a tunnel to its .network file.
// Specific ranges (another site's network) go in the main table so every device can reach
// them. A default route goes in the tunnel's own table, and only the chosen sources look
// there. Destinations on our own networks are never sent through a tunnel.
func writeTunnelRoutes(n *file, dev, address string, table int, sources []string, marks []int, ranges []string) {
	own := netip.Prefix{}
	if p, err := netip.ParsePrefix(address); err == nil {
		own = p.Masked()
	}
	seen := map[string]bool{}
	for _, r := range ranges {
		p, err := netip.ParsePrefix(r)
		if err != nil || !p.Addr().Is4() || seen[p.Masked().String()] {
			continue
		}
		p = p.Masked()
		seen[p.String()] = true
		switch {
		case p.Bits() == 0:
			if table > 0 {
				n.l("\n[Route]\nDestination=0.0.0.0/0\nTable=%d", table)
			}
		case own.IsValid() && own.Contains(p.Addr()) && p.Bits() >= own.Bits():
			// covered by the address's own network
		default:
			n.l("\n[Route]\nDestination=%s", p)
		}
	}
	if table == 0 {
		return
	}
	// Traffic the router itself sends out of this tunnel (its health check) finds the tunnel.
	n.l("\n[RoutingPolicyRule]\nOutgoingInterface=%s\nTable=%d\nPriority=%d", dev, table, 100+(table-300))
	for i, src := range sources {
		p, err := netip.ParsePrefix(src)
		if err != nil {
			continue
		}
		prio := 200 + (table-300)*100 + i*2
		n.l("\n[RoutingPolicyRule]\nFrom=%s\nTable=main\nSuppressPrefixLength=0\nPriority=%d", p.Masked(), prio)
		n.l("\n[RoutingPolicyRule]\nFrom=%s\nTable=%d\nPriority=%d", p.Masked(), table, prio+1)
	}
	// Marked traffic (a route for some destinations only) is looked up the same way. These
	// rules sit ahead of the source rules, so a specific route wins over a general one.
	for _, mk := range marks {
		prio := MarkPrioBase + (mk-MarkBase)*2
		n.l("\n[RoutingPolicyRule]\nFirewallMark=0x%x\nTable=main\nSuppressPrefixLength=0\nPriority=%d", mk, prio)
		n.l("\n[RoutingPolicyRule]\nFirewallMark=0x%x\nTable=%d\nPriority=%d", mk, table, prio+1)
	}
}

// Tunnel is a device that another program creates (an OpenVPN client's tun device). The
// program owns its address; networkd only adds the routing that sends chosen sources into it.
type Tunnel struct {
	Name    string   `json:"name"`
	Table   int      `json:"table"`
	Sources []string `json:"sources"`
	Marks   []int    `json:"marks,omitempty"`
	Routes  []string `json:"routes"` // other networks reachable through it, for site-to-site use
	Enabled bool     `json:"enabled"`
}

var reTun = regexp.MustCompile(`^tun[0-9]{1,2}$`)

func (c *Config) validateTunnels() []string {
	var errs []string
	add := func(f string, a ...any) { errs = append(errs, fmt.Sprintf(f, a...)) }
	seen := map[string]bool{}
	for _, t := range c.Tunnels {
		if !reTun.MatchString(t.Name) {
			add("tunnel device name %q is not valid", t.Name)
			continue
		}
		if seen[t.Name] {
			add("tunnel %s is listed twice", t.Name)
		}
		seen[t.Name] = true
		if !t.Enabled {
			continue
		}
		for _, mk := range t.Marks {
			if mk < MarkBase || mk >= MarkBase+MaxMarks {
				add("%s: route mark %d is out of range", t.Name, mk)
			}
		}
		if (t.Table != 0 || len(t.Sources) > 0 || len(t.Marks) > 0) && (t.Table < 300 || t.Table > MaxTunnelTable) {
			add("%s: routing table %d is out of range", t.Name, t.Table)
		}
		if len(t.Sources) > MaxTunnelSources {
			add("%s: too many devices or networks use this tunnel (the limit is %d)", t.Name, MaxTunnelSources)
		}
		for _, src := range append(append([]string(nil), t.Sources...), t.Routes...) {
			if p, err := netip.ParsePrefix(src); err != nil || !p.Addr().Is4() {
				add("%s: %q is not an IPv4 address range", t.Name, src)
			}
		}
	}
	return errs
}

func (c *Config) generateTunnels(out *Output) {
	for _, t := range c.Tunnels {
		if !t.Enabled {
			continue
		}
		n := &file{}
		// KeepConfiguration=static: the program that made this device (OpenVPN) set its address
		// and its own route. Without this networkd, which now manages the device, can drop them as
		// foreign, leaving a tunnel that connects and carries nothing.
		n.l("# Generated by Gatehouse\n[Match]\nName=%s\n\n[Network]\nIPv6AcceptRA=no\nLinkLocalAddressing=no\nConfigureWithoutCarrier=yes\nKeepConfiguration=static", t.Name)
		ranges := append([]string(nil), t.Routes...)
		if t.Table > 0 {
			ranges = append(ranges, "0.0.0.0/0")
		}
		writeTunnelRoutes(n, t.Name, "", t.Table, t.Sources, t.Marks, ranges)
		out.Networkd["gatehouse-"+t.Name+".network"] = n.b.String()
	}
}

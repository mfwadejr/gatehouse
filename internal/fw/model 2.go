// Package fw turns a plain description of the networks into an nftables ruleset.
//
// The model is deliberately small. Each network has a kind (Trusted, Private, Guest,
// Isolated, Custom) that implies sensible rules; explicit rules only handle exceptions.
// Everything that reaches the generated script is validated first, so a name or address
// typed into the console can never change the structure of the ruleset.
package fw

// Zone kinds. Wan is the internet side; Vpn zones behave like Private unless changed.
const (
	KindTrusted  = "trusted"  // may reach everything; nothing may start a connection to it
	KindPrivate  = "private"  // internet only
	KindGuest    = "guest"    // internet only, and no access to the router beyond DNS and DHCP
	KindIsolated = "isolated" // internet optional (off by default), never other networks
	KindCustom   = "custom"   // only what the explicit rules allow
	KindWan      = "wan"
	KindVpn      = "vpn"
)

// Zone is a group of interfaces that share one policy. In the console it is a "network".
type Zone struct {
	Name        string
	Kind        string
	Ifaces      []string
	Internet    bool   // may reach the internet
	Management  bool   // may reach the console and recovery services
	UPnP        bool   // devices here may ask for port mappings
	IntraAllow  bool   // devices inside the zone may reach each other through the router
	Gateway     bool   // this box is the gateway and NATs for it; false when another router is
	SSH         bool   // allow SSH to the router from this zone
	DNSForce    bool   // devices here cannot use any DNS server except the enforced one
	Description string // shown in the console
}

// Rule is an explicit exception between two zones. To may be "self" for services on the
// router itself and "wan" for the internet.
type Rule struct {
	Name    string
	From    string
	To      string
	Src     string // any, address, CIDR, range, or an address group name
	Dst     string
	Service string // any, a name like https, or proto/port like tcp/8080 or udp/1000-2000
	Action  string // accept, drop, reject
	Limit   int    // packets per second, 0 for none
	Log     bool
	Enabled bool
}

// Forward is a port forward from a WAN to an internal address.
type Forward struct {
	Name    string
	WAN     string // WAN zone interface, or empty for all
	Proto   string // tcp, udp
	ExtPort string // 443 or 8000-8010
	IntAddr string
	IntPort string // empty means same as ExtPort
	Enabled bool
}

// Model is everything the compiler needs.
type Model struct {
	Zones    []Zone
	Rules    []Rule
	Forwards []Forward
	// Groups are named address lists usable in rules.
	Groups map[string][]string
	// ConsolePorts are the TCP ports of the web console, always open to management zones.
	ConsolePorts []int
	// DNSRedirect sends port 53 from client networks to the router's own resolver.
	DNSRedirect bool
	// DNSTarget is where forced DNS goes: empty for this router's own resolver, otherwise the
	// address of the server the administrator chose.
	DNSTarget string
	// BlockDoT drops DNS-over-TLS (port 853) toward the internet so clients cannot bypass filtering.
	BlockDoT bool
	// LogDrops writes dropped packets to the event log (rate limited).
	LogDrops bool
}

// ZoneByName returns a zone.
func (m *Model) ZoneByName(name string) (*Zone, bool) {
	for i := range m.Zones {
		if m.Zones[i].Name == name {
			return &m.Zones[i], true
		}
	}
	return nil, false
}

// WanIfaces lists every interface in a WAN zone.
func (m *Model) WanIfaces() []string {
	var out []string
	for _, z := range m.Zones {
		if z.Kind == KindWan {
			out = append(out, z.Ifaces...)
		}
	}
	return out
}

// DefaultConsolePorts is the console's usual port.
var DefaultConsolePorts = []int{443}

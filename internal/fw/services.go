package fw

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// PortSpec is one protocol and port range.
type PortSpec struct {
	Proto string // tcp, udp, icmp
	Lo    int
	Hi    int
}

// Named services. A name may stand for several protocols.
var services = map[string][]PortSpec{
	"dns":    {{"tcp", 53, 53}, {"udp", 53, 53}},
	"http":   {{"tcp", 80, 80}},
	"https":  {{"tcp", 443, 443}, {"udp", 443, 443}},
	"ssh":    {{"tcp", 22, 22}},
	"ntp":    {{"udp", 123, 123}},
	"smtp":   {{"tcp", 25, 25}, {"tcp", 465, 465}, {"tcp", 587, 587}},
	"imap":   {{"tcp", 143, 143}, {"tcp", 993, 993}},
	"ftp":    {{"tcp", 21, 21}},
	"rdp":    {{"tcp", 3389, 3389}, {"udp", 3389, 3389}},
	"smb":    {{"tcp", 445, 445}},
	"print":  {{"tcp", 631, 631}, {"tcp", 9100, 9100}},
	"mdns":   {{"udp", 5353, 5353}},
	"ping":   {{"icmp", 0, 0}},
	"web":    {{"tcp", 80, 80}, {"tcp", 443, 443}, {"udp", 443, 443}},
	"dhcp":   {{"udp", 67, 68}},
	"vpn-wg": {{"udp", 51820, 51820}},
}

// ServiceNames lists the names offered in the console.
func ServiceNames() []string {
	out := make([]string, 0, len(services))
	for k := range services {
		out = append(out, k)
	}
	return out
}

// ParseService turns "https", "tcp/8080" or "udp/1000-2000" into port specs. "any" yields nil.
func ParseService(s string) ([]PortSpec, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" || s == "any" {
		return nil, nil
	}
	if v, ok := services[s]; ok {
		return v, nil
	}
	proto, ports, found := strings.Cut(s, "/")
	if !found || (proto != "tcp" && proto != "udp") {
		return nil, fmt.Errorf("unknown service %q: use a name such as https, or tcp/8080", s)
	}
	lo, hi, err := parsePortRange(ports)
	if err != nil {
		return nil, err
	}
	return []PortSpec{{proto, lo, hi}}, nil
}

func parsePortRange(s string) (int, int, error) {
	a, b, isRange := strings.Cut(s, "-")
	lo, err := strconv.Atoi(strings.TrimSpace(a))
	if err != nil || lo < 1 || lo > 65535 {
		return 0, 0, fmt.Errorf("port %q is not valid", s)
	}
	hi := lo
	if isRange {
		hi, err = strconv.Atoi(strings.TrimSpace(b))
		if err != nil || hi < lo || hi > 65535 {
			return 0, 0, fmt.Errorf("port range %q is not valid", s)
		}
	}
	return lo, hi, nil
}

func (p PortSpec) portExpr() string {
	if p.Lo == p.Hi {
		return strconv.Itoa(p.Lo)
	}
	return fmt.Sprintf("%d-%d", p.Lo, p.Hi)
}

// matchExpr renders the nft match for the spec, for example "tcp dport 443".
func (p PortSpec) matchExpr() string {
	if p.Proto == "icmp" {
		return "meta l4proto { icmp, ipv6-icmp }"
	}
	return fmt.Sprintf("%s dport %s", p.Proto, p.portExpr())
}

// ParseServices reads a rule's "what for" field: one or more services, separated by commas, each
// a name, tcp/8080 style ports, or list:Name for a saved list of ports.
func ParseServices(s string, lists map[string][]string) ([]PortSpec, error) {
	var out []PortSpec
	any := false
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if name, ok := strings.CutPrefix(part, "list:"); ok {
			items, found := lists[name]
			if !found {
				return nil, fmt.Errorf("there is no list of ports called %q", name)
			}
			for _, it := range items {
				sp, err := ParseService(it)
				if err != nil {
					return nil, fmt.Errorf("list %q: %w", name, err)
				}
				if sp == nil {
					any = true
				}
				out = append(out, sp...)
			}
			continue
		}
		sp, err := ParseService(part)
		if err != nil {
			return nil, err
		}
		if sp == nil {
			any = true
		}
		out = append(out, sp...)
	}
	if any {
		return nil, nil
	}
	return out, nil
}

// String renders a spec the way people type it: tcp/443, udp/67-68, icmp.
func (p PortSpec) String() string {
	if p.Proto == "icmp" {
		return "icmp"
	}
	return p.Proto + "/" + p.portExpr()
}

// BuiltinService is a service Gatehouse knows by name.
type BuiltinService struct {
	Name  string   `json:"name"`
	Ports []string `json:"ports"`
}

// BuiltinServices lists the named services with their ports, sorted by name.
func BuiltinServices() []BuiltinService {
	out := make([]BuiltinService, 0, len(services))
	for name, specs := range services {
		b := BuiltinService{Name: name}
		for _, sp := range specs {
			b.Ports = append(b.Ports, sp.String())
		}
		out = append(out, b)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

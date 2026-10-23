package fw

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"regexp"
	"sort"
	"strings"
)

var (
	reName  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 _.-]{0,31}$`)
	reIface = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,15}$`)
)

// addrSet is a validated set of address literals, split by family.
type addrSet struct {
	v4, v6 []string
}

func (a addrSet) empty() bool { return len(a.v4) == 0 && len(a.v6) == 0 }

func (a *addrSet) add(lit string, is6 bool) {
	if is6 {
		a.v6 = append(a.v6, lit)
	} else {
		a.v4 = append(a.v4, lit)
	}
}

// parseAddrs validates "any", addresses, CIDRs, ranges and group names.
// It returns (nil, true, nil) for "any".
func parseAddrs(s string, groups map[string][]string, depth int) (set addrSet, any bool, err error) {
	s = strings.TrimSpace(s)
	if s == "" || strings.EqualFold(s, "any") {
		return addrSet{}, true, nil
	}
	if depth > 3 {
		return addrSet{}, false, errors.New("address groups are nested too deeply")
	}
	for _, part := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' }) {
		if members, ok := groups[part]; ok {
			sub, _, err := parseAddrs(strings.Join(members, ","), groups, depth+1)
			if err != nil {
				return addrSet{}, false, fmt.Errorf("group %q: %w", part, err)
			}
			set.v4 = append(set.v4, sub.v4...)
			set.v6 = append(set.v6, sub.v6...)
			continue
		}
		if lo, hi, isRange := strings.Cut(part, "-"); isRange {
			a, err1 := netip.ParseAddr(lo)
			b, err2 := netip.ParseAddr(hi)
			if err1 != nil || err2 != nil || a.Is4() != b.Is4() || b.Less(a) {
				return addrSet{}, false, fmt.Errorf("%q is not a valid address range", part)
			}
			set.add(a.String()+"-"+b.String(), a.Is6())
			continue
		}
		if strings.Contains(part, "/") {
			p, err := netip.ParsePrefix(part)
			if err != nil {
				return addrSet{}, false, fmt.Errorf("%q is not a valid network", part)
			}
			set.add(p.Masked().String(), p.Addr().Is6())
			continue
		}
		a, err := netip.ParseAddr(part)
		if err != nil {
			return addrSet{}, false, fmt.Errorf("%q is not an address, network or group", part)
		}
		set.add(a.String(), a.Is6())
	}
	if set.empty() {
		return addrSet{}, false, errors.New("no addresses given")
	}
	sort.Strings(set.v4)
	sort.Strings(set.v6)
	return set, false, nil
}

// Validate reports every problem in the model. A model that passes compiles to a safe ruleset.
func (m *Model) Validate() error {
	var errs []string
	add := func(f string, a ...any) { errs = append(errs, fmt.Sprintf(f, a...)) }

	seenZone := map[string]bool{}
	ifaceZone := map[string]string{}
	mgmt := 0
	for _, z := range m.Zones {
		if !reName.MatchString(z.Name) {
			add("network name %q is not allowed (letters, numbers, spaces, - _ .)", z.Name)
			continue
		}
		key := strings.ToLower(z.Name)
		if seenZone[key] {
			add("two networks are called %q", z.Name)
		}
		seenZone[key] = true
		switch z.Kind {
		case KindTrusted, KindPrivate, KindGuest, KindIsolated, KindCustom, KindWan, KindVpn:
		default:
			add("network %q has an unknown type %q", z.Name, z.Kind)
		}
		for _, i := range z.Ifaces {
			if !reIface.MatchString(i) {
				add("interface name %q is not valid", i)
				continue
			}
			if other, dup := ifaceZone[i]; dup {
				add("%s is in both %q and %q", i, other, z.Name)
			}
			ifaceZone[i] = z.Name
		}
		if z.Management {
			if z.Kind == KindWan {
				add("the internet side cannot be a management network")
			}
			if len(z.Ifaces) == 0 {
				add("management network %q has no ports", z.Name)
			}
			mgmt++
		}
	}
	if mgmt == 0 {
		add("at least one network must be allowed to manage this device, otherwise you would be locked out")
	}
	if len(m.ConsolePorts) == 0 {
		add("the console port is not set")
	}
	for _, p := range m.ConsolePorts {
		if p < 1 || p > 65535 {
			add("console port %d is not valid", p)
		}
	}
	for name, members := range m.Groups {
		if !reName.MatchString(name) {
			add("group name %q is not allowed", name)
			continue
		}
		if _, _, err := parseAddrs(strings.Join(members, ","), nil, 0); err != nil {
			add("group %q: %v", name, err)
		}
	}
	zoneOK := func(n string) bool {
		if n == "self" || n == "wan" {
			return true
		}
		return seenZone[strings.ToLower(n)]
	}
	seenRule := map[string]bool{}
	for _, r := range m.Rules {
		if !reName.MatchString(r.Name) {
			add("rule name %q is not allowed", r.Name)
		}
		if seenRule[r.From+">"+r.To+">"+r.Name] {
			add("rule %q appears twice between %s and %s", r.Name, r.From, r.To)
		}
		seenRule[r.From+">"+r.To+">"+r.Name] = true
		if !zoneOK(r.From) || r.From == "self" {
			add("rule %q starts from an unknown network %q", r.Name, r.From)
		}
		if !zoneOK(r.To) {
			add("rule %q goes to an unknown network %q", r.Name, r.To)
		}
		switch r.Action {
		case "accept", "drop", "reject":
		default:
			add("rule %q has an unknown action %q", r.Name, r.Action)
		}
		if _, err := ParseService(r.Service); err != nil {
			add("rule %q: %v", r.Name, err)
		}
		for _, a := range []string{r.Src, r.Dst} {
			if _, _, err := parseAddrs(a, m.Groups, 0); err != nil {
				add("rule %q: %v", r.Name, err)
			}
		}
		if r.Limit < 0 || r.Limit > 1000000 {
			add("rule %q: rate limit is out of range", r.Name)
		}
	}
	if m.DNSTarget != "" {
		if ip := net.ParseIP(m.DNSTarget); ip == nil || ip.To4() == nil {
			add("the DNS server to enforce must be an IPv4 address, not %q", m.DNSTarget)
		} else if ip.IsLoopback() || ip.IsUnspecified() || ip.IsMulticast() {
			add("%s cannot be used as the DNS server", m.DNSTarget)
		}
	}
	for _, f := range m.Forwards {
		if !reName.MatchString(f.Name) {
			add("forward name %q is not allowed", f.Name)
		}
		if f.Proto != "tcp" && f.Proto != "udp" {
			add("forward %q must be tcp or udp", f.Name)
		}
		if _, _, err := parsePortRange(f.ExtPort); err != nil {
			add("forward %q: %v", f.Name, err)
		}
		if f.IntPort != "" {
			if _, _, err := parsePortRange(f.IntPort); err != nil {
				add("forward %q: %v", f.Name, err)
			} else if el, eh, e2 := parsePortRange(f.ExtPort); e2 == nil {
				il, ih, _ := parsePortRange(f.IntPort)
				if eh != el && (el != il || eh != ih) {
					add("forward %q: a range of ports must keep the same numbers inside; leave the inside port empty", f.Name)
				} else if eh == el && ih != il {
					add("forward %q: one outside port needs one inside port", f.Name)
				}
			}
		}
		if ip := net.ParseIP(f.IntAddr); ip == nil {
			add("forward %q: %q is not an address", f.Name, f.IntAddr)
		}
		if f.WAN != "" && ifaceZone[f.WAN] == "" {
			add("forward %q uses %q which is not an internet connection", f.Name, f.WAN)
		}
		// Never forward to the router's own console.
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

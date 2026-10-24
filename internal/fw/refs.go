package fw

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"sort"
	"strings"
)

// A rule's From/To device fields hold plain addresses and also references that Gatehouse keeps up
// to date by itself:
//
//	dev:aa:bb:cc:dd:ee:ff   a device, wherever it currently is
//	grp:Kitchen             a device group
//	dom:example.com         a domain (and *.example.com for its subdomains), from DNS answers
//	geo:DE                  a country, from the regional registries' address lists
//
// A reference is compiled to a named nftables set. The firewall script only names the set, and
// the contents are filled in separately. That is why a device getting a new address from DHCP
// changes a set, not the rules, and why nothing needs to be applied or confirmed for it.

// Reference kinds.
const (
	RefDevice = "dev"
	RefGroup  = "grp"
	RefDomain = "dom"
	RefGeo    = "geo"
)

var (
	reMAC    = regexp.MustCompile(`^(?i)[0-9a-f]{2}(:[0-9a-f]{2}){5}$`)
	reDomain = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)
	reCC     = regexp.MustCompile(`^[A-Z]{2}$`)
	reSetNm  = regexp.MustCompile(`^[a-z][a-z0-9_]{1,40}$`)
)

// Ref is one reference found in a rule's address field.
type Ref struct {
	Kind string
	Val  string // lower-case MAC, group name, domain, or upper-case country code
}

func (r Ref) String() string { return r.Kind + ":" + r.Val }

// SplitAddrField separates a rule's address field into plain addresses, typed MAC addresses and
// references. The plain part goes through the usual address checks.
func SplitAddrField(s string) (plain string, macs []string, refs []Ref, err error) {
	var keep []string
	for _, part := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' }) {
		switch {
		case reMAC.MatchString(part):
			macs = append(macs, strings.ToLower(part))
		case strings.HasPrefix(part, "dev:"):
			v := strings.ToLower(part[4:])
			if !reMAC.MatchString(v) {
				return "", nil, nil, fmt.Errorf("%q is not a device", part)
			}
			refs = append(refs, Ref{RefDevice, v})
		case strings.HasPrefix(part, "grp:"):
			refs = append(refs, Ref{RefGroup, part[4:]})
		case strings.HasPrefix(part, "dom:"):
			// A domain always covers its subdomains too, so a leading "*." is accepted and dropped.
			v := strings.TrimSuffix(strings.TrimPrefix(strings.ToLower(part[4:]), "*."), ".")
			if !reDomain.MatchString(v) {
				return "", nil, nil, fmt.Errorf("%q is not a domain name", part[4:])
			}
			refs = append(refs, Ref{RefDomain, v})
		case strings.HasPrefix(part, "geo:"):
			v := strings.ToUpper(part[4:])
			if !reCC.MatchString(v) {
				return "", nil, nil, fmt.Errorf("%q is not a country code", part[4:])
			}
			refs = append(refs, Ref{RefGeo, v})
		default:
			keep = append(keep, part)
		}
	}
	return strings.Join(keep, ","), macs, refs, nil
}

// DynSet is a named nftables set whose contents are managed outside the ruleset.
type DynSet struct {
	Name     string
	Type     string // ipv4_addr, ipv6_addr, ether_addr
	Interval bool
	Timeout  bool
	Ref      Ref
	Family   int // 4, 6, or 0 for MAC sets
}

func shortHash(s string) string {
	h := sha1.Sum([]byte(s))
	return hex.EncodeToString(h[:4])
}

// SetNameFor gives the set that holds a reference's addresses of one family (4 or 6), or its MAC
// addresses (family 0).
func SetNameFor(r Ref, fam int) string {
	switch {
	case fam == 0:
		return "mv_" + shortHash(r.String())
	case r.Kind == RefGeo:
		return fmt.Sprintf("gc_%s_%d", strings.ToLower(r.Val), fam)
	case r.Kind == RefDomain:
		return fmt.Sprintf("dm_%s_%d", shortHash(r.Val), fam)
	case r.Kind == RefGroup:
		return fmt.Sprintf("dg_%s_%d", shortHash(r.Val), fam)
	default:
		return fmt.Sprintf("dv_%s_%d", shortHash(r.Val), fam)
	}
}

// refSets lists the sets one reference needs. alsoMAC adds the MAC set for devices and groups.
func refSets(r Ref, alsoMAC bool) []DynSet {
	var out []DynSet
	add4 := DynSet{Name: SetNameFor(r, 4), Type: "ipv4_addr", Ref: r, Family: 4}
	add6 := DynSet{Name: SetNameFor(r, 6), Type: "ipv6_addr", Ref: r, Family: 6}
	switch r.Kind {
	case RefDevice, RefGroup:
		out = append(out, add4)
		if alsoMAC {
			out = append(out, DynSet{Name: SetNameFor(r, 0), Type: "ether_addr", Ref: r})
		}
	case RefDomain:
		add4.Timeout, add6.Timeout = true, true
		out = append(out, add4, add6)
	case RefGeo:
		add4.Interval, add6.Interval = true, true
		out = append(out, add4, add6)
	}
	return out
}

// The two sets that count each device's traffic. Elements are IPv4 addresses; the counters are
// bytes and packets since the address was first seen.
const (
	AcctUp   = "acct_up4"   // from a device to the internet
	AcctDown = "acct_down4" // from the internet to a device
)

// AcctKey is how a device's counter is named in what Counters reports.
const AcctPrefix = "acct:"

// MaxDynSets bounds how many sets one ruleset may declare.
const MaxDynSets = 96

// DynamicSets lists every set the enabled rules need, in a stable order.
func (m *Model) DynamicSets() []DynSet {
	seen := map[string]bool{}
	var out []DynSet
	for _, r := range m.Rules {
		if !r.Enabled {
			continue
		}
		for side, field := range []string{r.Src, r.Dst} {
			_, _, refs, err := SplitAddrField(field)
			if err != nil {
				continue
			}
			for _, ref := range refs {
				for _, s := range refSets(ref, side == 0 && r.AlsoMAC) {
					if !seen[s.Name] {
						seen[s.Name] = true
						out = append(out, s)
					}
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (s DynSet) declaration() string {
	var flags []string
	if s.Interval {
		flags = append(flags, "interval")
	}
	if s.Timeout {
		flags = append(flags, "timeout")
	}
	b := fmt.Sprintf("set %s { type %s;", s.Name, s.Type)
	if len(flags) > 0 {
		b += " flags " + strings.Join(flags, ", ") + ";"
	}
	if s.Interval {
		b += " auto-merge;"
	}
	return b + " }"
}

// SetFill is new content for a set. Replace empties it first. Elements are an address, a network
// (for interval sets), a MAC address, and for domain sets an optional " timeout Ns".
type SetFill struct {
	Elems   []string `json:"elems"`
	Replace bool     `json:"replace"`
}

var reTimeout = regexp.MustCompile(`^(\S+) timeout ([0-9]{1,7})s$`)

func validElem(e string) (string, error) {
	base, suffix := e, ""
	if m := reTimeout.FindStringSubmatch(e); m != nil {
		base, suffix = m[1], " timeout "+m[2]+"s"
	}
	if reMAC.MatchString(base) {
		return strings.ToLower(base) + suffix, nil
	}
	if p, err := netip.ParsePrefix(base); err == nil {
		if p.Bits() == p.Addr().BitLen() {
			return p.Addr().String() + suffix, nil
		}
		return p.Masked().String() + suffix, nil
	}
	if a, err := netip.ParseAddr(base); err == nil {
		return a.String() + suffix, nil
	}
	return "", fmt.Errorf("%q is not an address", e)
}

// BuildSetScript turns set contents into nft commands. Names and elements are checked, so a
// value from the DNS or the lease table can never add anything but an address to a set.
func BuildSetScript(fills map[string]SetFill, only map[string]bool) (string, error) {
	names := make([]string, 0, len(fills))
	for n := range fills {
		names = append(names, n)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, n := range names {
		if !reSetNm.MatchString(n) {
			return "", errors.New("not a set name: " + n)
		}
		if only != nil && !only[n] {
			continue
		}
		f := fills[n]
		if f.Replace {
			fmt.Fprintf(&b, "flush set inet %s %s\n", tableName, n)
		}
		var elems []string
		for _, e := range f.Elems {
			v, err := validElem(e)
			if err != nil {
				return "", err
			}
			elems = append(elems, v)
		}
		// Large sets go in several commands so a single line stays reasonable.
		for i := 0; i < len(elems); i += 2000 {
			j := i + 2000
			if j > len(elems) {
				j = len(elems)
			}
			fmt.Fprintf(&b, "add element inet %s %s { %s }\n", tableName, n, strings.Join(elems[i:j], ", "))
		}
	}
	return b.String(), nil
}

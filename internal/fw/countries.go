package fw

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// CountryPolicy is the one "Block or allow countries" setting. It is stored as a setting and turned
// into ordinary rules every time the firewall is built, so a network added later is covered too.
type CountryPolicy struct {
	Enabled   bool     `json:"enabled"`
	Mode      string   `json:"mode"` // "block" these countries, or "allow" only these
	Dir       string   `json:"dir"`  // "both", "out" (your devices to the world) or "in" (the world to you)
	Countries []string `json:"countries"`
}

// CountryPrefix starts the name of every rule the countries page makes.
const CountryPrefix = "Countries "

// IsCountryRule says whether a rule belongs to the countries page.
func IsCountryRule(name string) bool { return strings.HasPrefix(name, CountryPrefix) }

// MaxCountries keeps the number of firewall sets well inside MaxDynSets.
const MaxCountries = 40

// CountryPolicyFrom reads the saved policy; a missing or damaged one means "off".
func CountryPolicyFrom(db Queryer) CountryPolicy {
	var p CountryPolicy
	if err := json.Unmarshal([]byte(setting(db, "fw.countries", "")), &p); err != nil {
		return CountryPolicy{}
	}
	return p.Clean()
}

// Clean drops duplicates and anything that is not a two-letter code, and fills in defaults.
func (p CountryPolicy) Clean() CountryPolicy {
	if p.Mode != "allow" {
		p.Mode = "block"
	}
	if p.Dir != "out" && p.Dir != "in" {
		p.Dir = "both"
	}
	seen := map[string]bool{}
	var out []string
	for _, c := range p.Countries {
		c = strings.ToUpper(strings.TrimSpace(c))
		if reCC.MatchString(c) && !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	sort.Strings(out)
	p.Countries = out
	return p
}

// Active is true when the policy would add rules.
func (p CountryPolicy) Active() bool { return p.Enabled && len(p.Countries) > 0 }

// CountryRules builds the rules for a policy: one per network for what your devices send out, and
// one per network (and the router itself) for what comes in from the internet.
func CountryRules(zones []Zone, p CountryPolicy) []Rule {
	p = p.Clean()
	if !p.Active() {
		return nil
	}
	var refs []string
	for _, c := range p.Countries {
		refs = append(refs, "geo:"+c)
	}
	list := strings.Join(refs, ",")
	allow := p.Mode == "allow"
	what := "Blocked: "
	if allow {
		what = "Only these are allowed: "
	}
	var out []Rule
	var locals []string
	for _, z := range zones {
		if z.Kind != KindWan && len(z.Ifaces) > 0 {
			locals = append(locals, z.Name)
		}
	}
	if p.Dir == "both" || p.Dir == "out" {
		for _, n := range locals {
			out = append(out, Rule{Name: CountryPrefix + "out", From: n, To: "Internet", Dst: list, Service: "any", Action: "drop",
				Enabled: true, Not: allow, Descr: what + "what devices in " + n + " send to other countries."})
		}
	}
	if p.Dir == "both" || p.Dir == "in" {
		for _, n := range append(append([]string(nil), locals...), "self") {
			to := n
			where := "to " + n
			if n == "self" {
				where = "to Gatehouse itself"
			}
			out = append(out, Rule{Name: CountryPrefix + "in", From: "Internet", To: to, Src: list, Service: "any", Action: "drop",
				Enabled: true, Not: allow, Early: true, Descr: what + "what other countries send " + where + "."})
		}
	}
	return out
}

// notGeoTerms matches an address that is in none of the listed countries (and not private).
func (c *compiler) notGeoTerms(field string, src bool) []term {
	kw := "daddr"
	if src {
		kw = "saddr"
	}
	_, _, refs, _ := SplitAddrField(field)
	var e4, e6 []string
	for _, ref := range refs {
		if ref.Kind != RefGeo {
			continue
		}
		e4 = append(e4, fmt.Sprintf("ip %s != @%s", kw, SetNameFor(ref, 4)))
		e6 = append(e6, fmt.Sprintf("ip6 %s != @%s", kw, SetNameFor(ref, 6)))
	}
	if len(e4) == 0 {
		return []term{{0, ""}}
	}
	ex4 := append(append([]string(nil), privateV4...), c.exempt(4)...)
	ex6 := append(append([]string(nil), privateV6...), c.exempt(6)...)
	return []term{
		{4, strings.Join(e4, " ") + fmt.Sprintf(" ip %s != %s", kw, setLit(ex4))},
		{6, strings.Join(e6, " ") + fmt.Sprintf(" ip6 %s != %s", kw, setLit(ex6))},
	}
}

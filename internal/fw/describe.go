package fw

import (
	"fmt"
	"strings"
)

// Describe returns the model as plain sentences, one per network, for the console.
func Describe(m *Model) []string {
	var out []string
	for _, z := range m.Zones {
		switch z.Kind {
		case KindWan:
			out = append(out, fmt.Sprintf("%s: nothing from the internet can start a connection to your networks%s.", z.Name, forwardsNote(m)))
			continue
		}
		if len(z.Ifaces) == 0 {
			// A network nothing is plugged into: say so in one line instead of listing what it could reach.
			out = append(out, z.Name+": not attached to any port yet, so nothing is on it.")
			continue
		}
		var s strings.Builder
		s.WriteString(z.Name + ": ")
		var can []string
		if z.Internet && z.Gateway {
			can = append(can, "the internet")
		}
		if z.Kind == KindTrusted && z.Gateway {
			can = append(can, "every other network")
		}
		if z.IntraAllow {
			can = append(can, "each other")
		}
		if len(can) == 0 {
			s.WriteString("devices cannot reach anything")
		} else {
			s.WriteString("devices can reach " + join(can))
		}
		if z.Management {
			s.WriteString("; can manage this device")
		} else {
			s.WriteString("; cannot manage this device")
		}
		if !z.Gateway {
			s.WriteString("; another router is the gateway here")
		}
		s.WriteString(".")
		out = append(out, s.String())
		for _, r := range m.Rules {
			if r.Enabled && strings.EqualFold(r.From, z.Name) {
				out = append(out, describeRule(m, r))
			}
		}
	}
	return out
}

func forwardsNote(m *Model) string {
	n := 0
	for _, f := range m.Forwards {
		if f.Enabled {
			n++
		}
	}
	switch n {
	case 0:
		return ""
	case 1:
		return ", except 1 port you opened on purpose"
	}
	return fmt.Sprintf(", except %d ports you opened on purpose", n)
}

func verb(a string) string {
	switch a {
	case "accept":
		return "allow"
	case "reject":
		return "refuse"
	}
	return "block"
}

func join(s []string) string {
	switch len(s) {
	case 1:
		return s[0]
	case 2:
		return s[0] + " and " + s[1]
	}
	return strings.Join(s[:len(s)-1], ", ") + " and " + s[len(s)-1]
}

func describeRule(m *Model, r Rule) string {
	if IsMatrixRule(r.Name) {
		return strings.TrimSuffix(r.Descr, ".") + "."
	}
	what := "everything"
	if r.Service != "" && r.Service != "any" {
		var parts []string
		for _, p := range strings.Split(r.Service, ",") {
			p = strings.TrimSpace(p)
			if n, ok := strings.CutPrefix(p, "list:"); ok {
				p = "the " + n + " ports"
			}
			parts = append(parts, p)
		}
		what = join(parts)
	}
	to := r.To
	switch {
	case r.To == "self":
		to = "Gatehouse itself"
	case strings.EqualFold(r.To, "internet"):
		to = "the internet"
	}
	s := fmt.Sprintf("Exception %q: %s %s", r.Name, verb(r.Action), what)
	if who := describeSide(m, r.Src); who != "" {
		s += " from " + who
	}
	s += " to " + to
	if where := describeSide(m, r.Dst); where != "" {
		s += " (" + where + ")"
	}
	if sc, err := ParseSchedule(r.Sched); err == nil && sc != nil {
		s += ", " + strings.ToLower(sc.Describe()[:1]) + sc.Describe()[1:]
	}
	return s + "."
}

// describeSide names what an address field stands for; empty means anything.
func describeSide(m *Model, field string) string {
	plain, macs, refs, err := SplitAddrField(field)
	if err != nil {
		return field
	}
	var out []string
	if p := strings.TrimSpace(plain); p != "" && !strings.EqualFold(p, "any") {
		out = append(out, p)
	}
	for _, mac := range macs {
		out = append(out, "the device "+mac)
	}
	for _, r := range refs {
		switch r.Kind {
		case RefDevice:
			if n := m.Names[r.Val]; n != "" {
				out = append(out, n)
			} else {
				out = append(out, "the device "+r.Val)
			}
		case RefGroup:
			out = append(out, "the group "+r.Val)
		case RefDomain:
			out = append(out, r.Val+" and its subdomains")
		case RefGeo:
			out = append(out, "addresses in "+r.Val)
		}
	}
	if len(out) == 0 {
		return ""
	}
	return join(out)
}

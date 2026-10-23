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
				out = append(out, fmt.Sprintf("Exception %q: %s %s to %s (%s).", r.Name, verb(r.Action), r.Service, r.To, orAny(r.Dst)))
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

func orAny(s string) string {
	if s == "" || s == "any" {
		return "any address"
	}
	return s
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

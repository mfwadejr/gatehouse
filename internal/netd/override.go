package netd

import (
	"fmt"
	"net"
	"sort"
	"strings"
	"time"
)

// Virtual Network Override. Gatehouse puts one device's traffic on a chosen VLAN by its hardware
// address, at its own port: frames from that address are tagged on the way in, so they arrive on
// the VLAN network (DHCP, DNS, firewall and logs all see it there), and frames going back to it
// have the tag removed. Only that one address is matched; nothing else on the port is touched.

// OverrideRule moves one device onto one VLAN. Parent is the port or shared-port group the VLAN
// network sits on.
type OverrideRule struct {
	Parent string `json:"parent"`
	MAC    string `json:"mac"`
	VID    int    `json:"vid"`
	// Home is the port or network the device is really plugged into, when that is not Parent. Then
	// the device's frames are moved into the VLAN network Iface instead of being tagged.
	Home  string `json:"home,omitempty"`
	Iface string `json:"iface,omitempty"`
}

// OverridePlan is the whole set. Applying it replaces what was applied before.
type OverridePlan struct {
	Rules []OverrideRule `json:"rules"`
	// Parents are every port that may carry rules from earlier plans, so a removed device is cleaned up.
	Parents []string `json:"parents"`
}

const (
	overridePrio  = "4100"
	overrideBcast = "4101"
)

// OverrideCommands returns the tc commands for a plan (without running anything), in order. It is
// separate from OverrideApply so it can be tested everywhere.
func OverrideCommands(p OverridePlan) ([][]string, error) {
	parents := map[string]bool{}
	for _, d := range p.Parents {
		if !reHADev.MatchString(d) {
			return nil, fmt.Errorf("%q is not a usable port name", d)
		}
		parents[d] = true
	}
	var cmds [][]string
	type pv struct {
		p string
		v int
	}
	bc := map[pv]bool{}
	for _, r := range p.Rules {
		if !reHADev.MatchString(r.Parent) {
			return nil, fmt.Errorf("%q is not a usable port name", r.Parent)
		}
		hw, err := net.ParseMAC(r.MAC)
		if err != nil || len(hw) != 6 {
			return nil, fmt.Errorf("%q is not a device address", r.MAC)
		}
		if r.VID < 1 || r.VID > 4094 {
			return nil, fmt.Errorf("%d is not a VLAN number", r.VID)
		}
		parents[r.Parent] = true
		if r.Home != "" && r.Home != r.Parent {
			if !reHADev.MatchString(r.Home) || !reHADev.MatchString(r.Iface) {
				return nil, fmt.Errorf("%q or %q is not a usable port name", r.Home, r.Iface)
			}
			parents[r.Home], parents[r.Iface] = true, true
		}
	}
	names := make([]string, 0, len(parents))
	for d := range parents {
		names = append(names, d)
	}
	sort.Strings(names)
	for _, d := range names {
		cmds = append(cmds,
			[]string{"qdisc", "replace", "dev", d, "clsact"},
			[]string{"filter", "del", "dev", d, "ingress", "prio", overridePrio},
			[]string{"filter", "del", "dev", d, "egress", "prio", overridePrio},
			[]string{"filter", "del", "dev", d, "egress", "prio", overrideBcast})
	}
	for _, r := range p.Rules {
		hw, _ := net.ParseMAC(r.MAC)
		mac := hw.String()
		vid := fmt.Sprint(r.VID)
		if r.Home != "" && r.Home != r.Parent {
			// A device on another port: its frames are moved into the VLAN network as if they had
			// arrived there, and what the VLAN network sends to it goes back out of its own port.
			cmds = append(cmds,
				[]string{"filter", "add", "dev", r.Home, "ingress", "protocol", "all", "prio", overridePrio,
					"flower", "src_mac", mac, "action", "mirred", "ingress", "redirect", "dev", r.Iface},
				[]string{"filter", "add", "dev", r.Iface, "egress", "protocol", "all", "prio", overridePrio,
					"flower", "dst_mac", mac, "action", "mirred", "egress", "redirect", "dev", r.Home})
			k := pv{r.Iface + ">" + r.Home, 0}
			if !bc[k] {
				bc[k] = true
				cmds = append(cmds, []string{"filter", "add", "dev", r.Iface, "egress", "protocol", "all", "prio", overrideBcast,
					"flower", "dst_mac", "01:00:00:00:00:00/01:00:00:00:00:00", "action", "mirred", "egress", "mirror", "dev", r.Home})
			}
			continue
		}
		for _, proto := range []string{"ip", "ipv6", "arp"} {
			cmds = append(cmds, []string{"filter", "add", "dev", r.Parent, "ingress", "protocol", proto, "prio", overridePrio,
				"flower", "src_mac", mac, "action", "vlan", "push", "id", vid})
		}
		cmds = append(cmds, []string{"filter", "add", "dev", r.Parent, "egress", "protocol", "802.1q", "prio", overridePrio,
			"flower", "vlan_id", vid, "dst_mac", mac, "action", "vlan", "pop"})
		k := pv{r.Parent, r.VID}
		if !bc[k] {
			bc[k] = true
			// Broadcasts and multicasts (address offers, neighbour lookups) cannot be matched to one
			// device, so the tag is removed from them on this port while any override uses this VLAN.
			cmds = append(cmds, []string{"filter", "add", "dev", r.Parent, "egress", "protocol", "802.1q", "prio", overrideBcast,
				"flower", "vlan_id", vid, "dst_mac", "01:00:00:00:00:00/01:00:00:00:00:00", "action", "vlan", "pop"})
		}
	}
	return cmds, nil
}

// OverrideApply puts the plan in place. Failures to remove old rules are ignored (they may not
// exist); a failure to add a rule removes everything this feature added and reports why.
func (s *Service) OverrideApply(p OverridePlan) error {
	cmds, err := OverrideCommands(p)
	if err != nil {
		return err
	}
	for _, c := range cmds {
		out, err := wifiRun(15*time.Second, "tc", c...)
		if err == nil || c[0] == "filter" && c[1] == "del" {
			continue
		}
		clear := OverridePlan{Parents: append([]string{}, p.Parents...)}
		for _, r := range p.Rules {
			clear.Parents = append(clear.Parents, r.Parent)
		}
		if cc, e := OverrideCommands(clear); e == nil {
			for _, x := range cc {
				wifiRun(15*time.Second, "tc", x...)
			}
		}
		msg := strings.TrimSpace(out)
		if strings.Contains(msg, "Unknown qdisc") || strings.Contains(msg, "Error: Specified qdisc kind is unknown") || strings.Contains(msg, "unknown filter") {
			msg = "this system's kernel cannot tag traffic by device address"
		}
		return fmt.Errorf("could not move a device onto its VLAN: %s", msg)
	}
	return nil
}

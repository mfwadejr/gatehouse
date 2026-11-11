package fw

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Speed rules: Give priority (High, Normal, Low) and Limit speed (download and upload, in kilobits
// a second) for networks, profiles and devices, optionally only for some apps or websites.
//
// Priority works by marking traffic. Each packet that leaves for the internet gets a DSCP label
// (CS5 for High, CS1 for Low), and the same label is kept in the high bits of the connection's
// mark, where the smart queue (CAKE, on the Speed tab) reads it back for the replies on the way
// in. The queue only acts when the line is full, which is when priority matters. The low 24 bits
// of the connection mark belong to routing and are never touched here.
//
// A limit drops what is over the speed (a policer), so a transfer slows to fit. Limits and marks
// apply to IPv4 when they name devices, profiles or apps (the address lists are IPv4); a rule for
// whole networks and everything they send also covers IPv6.

const (
	// ConnMarkKeep is the part of the connection mark that routing uses.
	ConnMarkKeep = 0x00ffffff
	// ConnMarkDSCP is the part this feature uses: six bits of DSCP and a "valid" bit below them.
	ConnMarkDSCP  = 0xff000000
	connMarkValid = 0x01000000
)

// SpeedRule is one entry of the Speed control tab, already resolved for the firewall.
type SpeedRule struct {
	ID          int64
	Name        string
	Kind        string // priority or limit
	Prio        string // high, normal or low
	DownKbit    int
	UpKbit      int
	Each        bool     // a limit applies to each device on its own instead of all of them together
	Ifaces      []string // networks it covers, as their ports
	WhoSet      bool     // devices and profiles it covers are in the set SpeedWhoSet(ID)
	WhatSet     bool     // apps and websites it covers are in the set SpeedWhatSet(ID)
	Sched       string
	PausedUntil int64
}

// SpeedWhoSet and SpeedWhatSet name the address sets a rule owns.
func SpeedWhoSet(id int64) string  { return fmt.Sprintf("sp%d_who4", id) }
func SpeedWhatSet(id int64) string { return fmt.Sprintf("sp%d_what4", id) }

func speedMeter(dir string, id int64) string { return fmt.Sprintf("sp%s%d", dir, id) }

// Dscp is the label and the connection mark a priority stands for.
func prioMark(p string) (dscp string, mark uint32) {
	switch p {
	case "high":
		return "cs5", 40<<26 | connMarkValid
	case "low":
		return "cs1", 8<<26 | connMarkValid
	}
	return "cs0", connMarkValid
}

// SpeedSetNames lists the sets the speed rules own, for the program to fill.
func (m *Model) SpeedSetNames() []string {
	var out []string
	for _, r := range m.Speed {
		if r.WhoSet {
			out = append(out, SpeedWhoSet(r.ID))
		}
		if r.WhatSet {
			out = append(out, SpeedWhatSet(r.ID))
		}
	}
	return out
}

func (c *compiler) speedSets() {
	for _, r := range c.m.Speed {
		if r.WhoSet {
			c.line(1, "set %s { type ipv4_addr; size 8192; }", SpeedWhoSet(r.ID))
		}
		if r.WhatSet {
			c.line(1, "set %s { type ipv4_addr; flags timeout; size 65535; }", SpeedWhatSet(r.ID))
		}
		if r.Kind == "limit" && r.Each {
			for _, d := range []string{"u", "d"} {
				c.line(1, "set %s { type ipv4_addr; flags dynamic, timeout; timeout 10m; size 8192; }", speedMeter(d, r.ID))
			}
		}
	}
}

// who gives the ways a rule can name the people it covers in one direction, each ending with a
// space or empty. Up is traffic leaving a device (the device is the source), down is traffic
// coming to it. A rule that names nobody covers everyone.
func (r SpeedRule) who(up bool) []string {
	var out []string
	if len(r.Ifaces) > 0 {
		if up {
			out = append(out, "iifname "+ifSet(r.Ifaces)+" ")
		} else {
			out = append(out, "oifname "+ifSet(r.Ifaces)+" ")
		}
	}
	if r.WhoSet {
		f := "daddr"
		if up {
			f = "saddr"
		}
		out = append(out, fmt.Sprintf("ip %s @%s ", f, SpeedWhoSet(r.ID)))
	}
	if len(out) == 0 {
		return []string{""}
	}
	return out
}

func (r SpeedRule) what(up bool) string {
	if !r.WhatSet {
		return ""
	}
	f := "saddr"
	if up {
		f = "daddr"
	}
	return fmt.Sprintf("ip %s @%s ", f, SpeedWhatSet(r.ID))
}

// policeRate is the speed in bytes a second. The firewall's limit already allows a second's worth
// of bytes as a bucket, so no extra burst is added: a transfer is let through at full speed for a
// second and held to the limit after that.
func policeRate(kbit int) int { return kbit * 125 }

func (c *compiler) speed() {
	m := c.m
	wan := m.WanIfaces()
	if len(m.Speed) == 0 || len(wan) == 0 {
		return
	}
	wans := ifSet(wan)
	var prio, lim []string
	for _, r := range m.Speed {
		for _, tm := range c.timing(r.Sched, r.PausedUntil) {
			switch r.Kind {
			case "priority":
				dscp, mark := prioMark(r.Prio)
				for _, who := range r.who(true) {
					// IPv4 and, when nothing limits the rule to IPv4 addresses, IPv6 too.
					fams := []string{"ip"}
					if who == "" || strings.HasPrefix(who, "iifname") {
						if !r.WhatSet {
							fams = []string{"ip", "ip6"}
						}
					}
					for _, fam := range fams {
						prio = append(prio, fmt.Sprintf("oifname %s %s%s%smeta nfproto %s %s dscp set %s ct mark set ct mark & 0x%08x | 0x%08x return", wans, who, r.what(true), tm+" ", map[string]string{"ip": "ipv4", "ip6": "ipv6"}[fam], fam, dscp, ConnMarkKeep, mark))
					}
				}
			case "limit":
				for _, d := range []struct {
					up   bool
					kbit int
				}{{true, r.UpKbit}, {false, r.DownKbit}} {
					if d.kbit <= 0 {
						continue
					}
					rate := policeRate(d.kbit)
					dir, key, port := "d", "daddr", "iifname "
					if d.up {
						dir, key, port = "u", "saddr", "oifname "
					}
					for _, who := range r.who(d.up) {
						base := fmt.Sprintf("%s%s %s%s%s", port, wans, who, r.what(d.up), strings.TrimSpace(tm)+" ")
						if r.Each {
							lim = append(lim, fmt.Sprintf("%sadd @%s { ip %s limit rate over %d bytes/second } counter drop", base, speedMeter(dir, r.ID), key, rate))
						} else {
							lim = append(lim, fmt.Sprintf("%slimit rate over %d bytes/second counter drop", base, rate))
						}
					}
				}
			}
		}
	}
	if len(prio) > 0 {
		c.line(1, "chain speed_prio {")
		for _, l := range prio {
			c.line(2, "%s", strings.Join(strings.Fields(l), " "))
		}
		c.line(1, "}")
	}
	if len(lim) > 0 {
		c.line(1, "chain speed_limit {")
		for _, l := range lim {
			c.line(2, "%s", strings.Join(strings.Fields(l), " "))
		}
		c.line(1, "}")
	}
}

// speedJumps are the lines that run the speed chains from the forward chain.
func (c *compiler) speedJumps() {
	if len(c.m.Speed) == 0 || len(c.m.WanIfaces()) == 0 {
		return
	}
	// Whether a chain exists depends on the rules; only jump to ones that do.
	var hasPrio, hasLim bool
	for _, r := range c.m.Speed {
		hasPrio = hasPrio || r.Kind == "priority"
		hasLim = hasLim || r.Kind == "limit" && (r.UpKbit > 0 || r.DownKbit > 0)
	}
	if hasPrio {
		c.line(2, "jump speed_prio")
	}
	if hasLim {
		c.line(2, "jump speed_limit")
	}
}

// loadSpeedRules reads the entries that are on and resolves where each applies. An entry that
// names places that no longer exist applies nowhere, so it is left out rather than widened.
func loadSpeedRules(db Queryer, zones []Zone, on bool) []SpeedRule {
	if !on {
		return nil
	}
	rows, err := db.Query(`SELECT id,name,kind,prio,down_kbit,up_kbit,each,schedule,paused_until,items,targets FROM speed_rules WHERE enabled=1 ORDER BY id`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []SpeedRule
	for rows.Next() {
		var r SpeedRule
		var each int
		var items, targets string
		if rows.Scan(&r.ID, &r.Name, &r.Kind, &r.Prio, &r.DownKbit, &r.UpKbit, &each, &r.Sched, &r.PausedUntil, &items, &targets) != nil {
			continue
		}
		r.Each = each == 1
		var it []struct{ Kind, Value string }
		json.Unmarshal([]byte(items), &it)
		r.WhatSet = len(it) > 0
		var tg []struct{ Scope, Target string }
		json.Unmarshal([]byte(targets), &tg)
		everyone := len(tg) == 0
		resolved := false
		for _, t := range tg {
			switch t.Scope {
			case "everyone":
				everyone = true
			case "network":
				for _, z := range zones {
					if strings.EqualFold(z.Name, t.Target) && z.Kind != KindWan {
						r.Ifaces = append(r.Ifaces, z.Ifaces...)
						resolved = true
					}
				}
			case "profile", "device":
				r.WhoSet, resolved = true, true
			}
		}
		if everyone {
			r.Ifaces, r.WhoSet = nil, false
		} else if !resolved {
			continue
		}
		if r.Kind == "limit" && r.UpKbit <= 0 && r.DownKbit <= 0 {
			continue
		}
		if r.Kind != "limit" && r.Kind != "priority" {
			continue
		}
		out = append(out, r)
	}
	return out
}

package app

import (
	"sort"
	"strings"
)

// How an alert is shown. The device name is worked out when the page is shown, from the devices
// list (the person's own name first, then the name the device reported, then the address), so a
// name given later appears on old alerts too. Repeats are folded into one row with a count, and
// the wording says who started the connection when the engine told us.

// deviceLabeler returns a function that names a device: the name the person gave it, else the name
// it reported, else its address. It reads the devices list once, so build it once per page.
func (a *App) deviceLabeler() func(mac, ip string) string {
	byMAC := a.deviceNames() // name, else reported name, else the hardware address
	return func(mac, ip string) string {
		mac = strings.ToLower(mac)
		if t := byMAC[mac]; t != "" && t != mac {
			return t
		}
		if ip != "" && a.Devices != nil {
			if n := a.deviceName(ip); n != "" {
				return n
			}
		}
		if mac != "" {
			if n := a.fwdDeviceName(mac, ""); n != "" {
				return n
			}
		}
		return ip
	}
}

// Alert kinds, from the engine's category and the rule's name.
const (
	threatKindList    = "list"    // an address on a known-bad list
	threatKindMalware = "malware" // a device may be infected
	threatKindProbe   = "probe"   // someone is probing the network
	threatKindAttack  = "attack"  // an attempt to break into a device
	threatKindTrick   = "trick"   // a site that tries to trick people
	threatKindFlood   = "flood"
	threatKindPolicy  = "policy" // usually harmless
	threatKindOdd     = "odd"    // the engine noticed odd packets; not a threat
	threatKindOther   = "other"
)

func threatKind(category, sig string) string {
	s := strings.ToUpper(sig)
	switch {
	case strings.HasPrefix(s, "SURICATA "):
		return threatKindOdd
	case strings.HasPrefix(s, "ET DROP "), strings.HasPrefix(s, "ET CINS "), strings.HasPrefix(s, "ET COMPROMISED "), strings.Contains(s, "LISTED TRAFFIC"), strings.Contains(s, "KNOWN MALICIOUS"):
		return threatKindList
	}
	c := strings.ToLower(category)
	switch {
	case strings.Contains(c, "trojan"), strings.Contains(c, "malware"), strings.Contains(c, "command and control"), strings.Contains(c, "c2"):
		return threatKindMalware
	case strings.Contains(c, "scan"), strings.Contains(c, "reconnaissance"):
		return threatKindProbe
	case strings.Contains(c, "exploit"), strings.Contains(c, "attack"), strings.Contains(c, "shellcode"), strings.Contains(c, "web application"):
		return threatKindAttack
	case strings.Contains(c, "phish"), strings.Contains(c, "social"):
		return threatKindTrick
	case strings.Contains(c, "denial"), strings.Contains(c, "dos"):
		return threatKindFlood
	case strings.Contains(c, "policy"), strings.Contains(c, "p2p"), strings.Contains(c, "misc"), strings.Contains(c, "info"):
		return threatKindPolicy
	}
	return threatKindOther
}

// listName names the bad-address list a rule is about.
func listName(sig string) string {
	s := strings.ToUpper(sig)
	switch {
	case strings.Contains(s, "SPAMHAUS"):
		return "Spamhaus"
	case strings.HasPrefix(s, "ET CINS "):
		return "CINS Army"
	case strings.HasPrefix(s, "ET COMPROMISED "):
		return "compromised hosts"
	}
	return "known-bad addresses"
}

// threatTitle is the one line a person reads, in plain words, naming the device.
func threatTitle(kind, sig, dev, dir string) string {
	if dev == "" {
		dev = "a device"
	}
	switch kind {
	case threatKindList:
		l := listName(sig)
		switch dir {
		case "out":
			return dev + " contacted an address on a known-bad list (" + l + ")."
		case "in":
			return "An address on a known-bad list (" + l + ") contacted " + dev + "."
		}
		return dev + " exchanged traffic with an address on a known-bad list (" + l + ")."
	case threatKindMalware:
		return dev + " may be infected and talking to a known bad server."
	case threatKindProbe:
		return "Someone is probing " + dev + " for open doors."
	case threatKindAttack:
		return "Someone tried to break into " + dev + "."
	case threatKindTrick:
		return dev + " went to a site that tries to trick people."
	case threatKindFlood:
		return "Traffic that looks like an attempt to overwhelm " + dev + "."
	case threatKindPolicy:
		return "Unusual traffic from " + dev + " that is usually harmless."
	case threatKindOdd:
		return dev + " sent some odd network traffic."
	}
	return "Something that looks suspicious involving " + dev + "."
}

// threatExplain adds one plain line about what the kind of alert means.
func threatExplain(kind string) string {
	switch kind {
	case threatKindList:
		return "These lists hold addresses run by criminals or taken over by them. A match is a reason to look, not proof of a break-in: many are ordinary servers that share a range, and often your own device made the connection."
	case threatKindMalware:
		return "The device contacted a server known to give orders to infected machines. Scan the device, and if it keeps happening, disconnect it."
	case threatKindProbe:
		return "Someone, usually an automatic scanner on the internet, is trying doors to see which are open. Nothing is open to the internet unless you made a forward."
	case threatKindAttack:
		return "A known attack pattern was seen. It only matters if the device runs the software it targets and the attack got through."
	case threatKindTrick:
		return "The site matches ones used for fake sign-in pages and scams."
	case threatKindFlood:
		return "A burst of traffic that looks like a flood."
	case threatKindPolicy:
		return "Traffic that is unusual but is often an app doing something normal. Allow it if you know why it happens."
	case threatKindOdd:
		return "The engine noticed packets that are malformed or out of order. This is common with streaming boxes, Wi-Fi roaming and slow links, and is almost never a threat."
	}
	return "The engine matched a rule. Open the rule's name for details."
}

type threatGroup struct {
	Key       string `json:"key"`
	SID       int64  `json:"sid"`
	Kind      string `json:"kind"`
	Quiet     bool   `json:"quiet"`
	Title     string `json:"title"`
	Explain   string `json:"explain"`
	Signature string `json:"signature"`
	Device    string `json:"device"`
	MAC       string `json:"mac"`
	Remote    string `json:"remote"`
	Country   string `json:"country,omitempty"`
	Port      int    `json:"port,omitempty"`
	Proto     string `json:"proto,omitempty"`
	Started   string `json:"started,omitempty"` // device, remote or empty
	Count     int    `json:"count"`
	First     int64  `json:"first"`
	Last      int64  `json:"last"`
	Severity  int    `json:"severity"`
}

// threatGroups folds the recent alerts into one row per device, rule and remote address.
func (a *App) threatGroups(limit int) []threatGroup {
	label := a.deviceLabeler()
	rows, err := a.Logs.Query(`SELECT ts,sid,signature,category,severity,src,dst,dport,proto,mac,dir FROM threat_alerts ORDER BY id DESC LIMIT 1000`)
	if err != nil {
		return []threatGroup{}
	}
	defer rows.Close()
	idx := map[string]*threatGroup{}
	var order []*threatGroup
	for rows.Next() {
		var ts, sid int64
		var sig, cat, src, dst, proto, mac, dir string
		var sev, port int
		if rows.Scan(&ts, &sid, &sig, &cat, &sev, &src, &dst, &port, &proto, &mac, &dir) != nil {
			continue
		}
		local, remote := src, dst
		if !privateAddr(src) && privateAddr(dst) {
			local, remote = dst, src
		}
		key := strings.ToLower(mac) + "|" + sig + "|" + remote
		if mac == "" {
			key = local + "|" + sig + "|" + remote
		}
		g := idx[key]
		if g == nil {
			kind := threatKind(cat, sig)
			dev := label(mac, local)
			g = &threatGroup{Key: key, SID: sid, Kind: kind, Quiet: kind == threatKindOdd || kind == threatKindPolicy,
				Title: threatTitle(kind, sig, dev, dir), Explain: threatExplain(kind), Signature: sig, Device: dev, MAC: strings.ToLower(mac),
				Remote: remote, Port: port, Proto: proto, Last: ts, First: ts, Severity: sev}
			switch dir {
			case "out":
				g.Started = "device"
			case "in":
				g.Started = "remote"
			}
			g.Country = a.country(remote)
			idx[key] = g
			order = append(order, g)
		}
		g.Count++
		if ts < g.First {
			g.First = ts
		}
		if ts > g.Last {
			g.Last = ts
		}
	}
	sort.SliceStable(order, func(i, j int) bool { return order[i].Last > order[j].Last })
	out := make([]threatGroup, 0, len(order))
	for _, g := range order {
		if len(out) >= limit {
			break
		}
		out = append(out, *g)
	}
	return out
}

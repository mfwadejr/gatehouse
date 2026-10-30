package app

import (
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/miekg/dns"

	"gatehouse/internal/api"
	"gatehouse/internal/fw"
)

// "Why can't it reach that?" Pick a device and a place on the internet. Gatehouse goes through the
// things that can stop it, in the order they act, and says in plain words which one did. Each
// answer can offer a fix. It only reads settings; it never sends traffic from the device.

type whyFix struct {
	Label  string `json:"label"`
	Kind   string `json:"kind"`             // allow-domain, resume-device, resume-profile, goto
	Domain string `json:"domain,omitempty"` // for allow-domain
	ID     int64  `json:"id,omitempty"`     // for the resume fixes
	Page   string `json:"page,omitempty"`   // for goto
}

type whyFinding struct {
	Stage string  `json:"stage"`
	State string  `json:"state"` // blocks, ok, unknown
	Title string  `json:"title"`
	Text  string  `json:"text"`
	Fix   *whyFix `json:"fix,omitempty"`
}

type whyResult struct {
	Device   string       `json:"device"`
	Target   string       `json:"target"`
	Verdict  string       `json:"verdict"` // blocked, open, unclear
	Summary  string       `json:"summary"`
	Findings []whyFinding `json:"findings"`
}

// ruleAddrMatch says whether a rule's address field covers an address. sure is false when the field
// holds something that cannot be decided here (a domain or a country).
func ruleAddrMatch(field string, ip netip.Addr, mac string, groups map[string]fw.DevGroup) (match, sure bool) {
	f := strings.TrimSpace(field)
	if f == "" || strings.EqualFold(f, "any") {
		return true, true
	}
	plain, macs, refs, err := fw.SplitAddrField(f)
	if err != nil {
		return false, false
	}
	sure = true
	for _, m := range macs {
		if mac != "" && m == mac {
			return true, true
		}
	}
	for _, r := range refs {
		switch r.Kind {
		case fw.RefDevice:
			if mac != "" && r.Val == mac {
				return true, true
			}
		case fw.RefGroup:
			for _, m := range groups[r.Val].MACs {
				if mac != "" && m == mac {
					return true, true
				}
			}
		default:
			sure = false
		}
	}
	for _, p := range strings.Split(plain, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if ip.IsValid() {
			if pf, err := netip.ParsePrefix(p); err == nil && pf.Contains(ip) {
				return true, true
			}
			if ad, err := netip.ParseAddr(p); err == nil && ad == ip {
				return true, true
			}
			if lo, hi, ok := strings.Cut(p, "-"); ok {
				l, e1 := netip.ParseAddr(strings.TrimSpace(lo))
				h, e2 := netip.ParseAddr(strings.TrimSpace(hi))
				if e1 == nil && e2 == nil && ip.Compare(l) >= 0 && ip.Compare(h) <= 0 {
					return true, true
				}
			}
		} else {
			sure = false
		}
	}
	return false, sure
}

func ruleServiceMatch(service, proto string, port int, lists map[string][]string) (match, sure bool) {
	specs, err := fw.ParseServices(service, lists)
	if err != nil {
		return false, false
	}
	if specs == nil {
		return true, true
	}
	if port == 0 {
		return true, false // no port was given: the rule may or may not cover what the device does
	}
	for _, s := range specs {
		if s.Proto == proto && port >= s.Lo && port <= s.Hi {
			return true, true
		}
	}
	return false, true
}

func (a *App) deviceZone(ip string) string {
	if ip == "" {
		return ""
	}
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return ""
	}
	rows, err := a.Config.Query(`SELECT iface,subnet FROM dhcp_scopes`)
	if err != nil {
		return ""
	}
	type sc struct{ iface, sub string }
	var all []sc
	for rows.Next() {
		var s sc
		if rows.Scan(&s.iface, &s.sub) == nil {
			all = append(all, s)
		}
	}
	rows.Close()
	for _, s := range all {
		if pf, err := netip.ParsePrefix(s.sub); err == nil && pf.Contains(addr) {
			return a.zoneOf(s.iface)
		}
	}
	return ""
}

func (a *App) whyCannotReach(deviceID int64, target string, port int, proto string) (whyResult, error) {
	res := whyResult{Target: target, Findings: []whyFinding{}}
	var mac, name, lastIP string
	var gid int64
	var devPause int64
	if err := a.Config.QueryRow(`SELECT lower(mac),name,COALESCE(group_id,0),last_ip,paused_until FROM devices WHERE id=?`, deviceID).Scan(&mac, &name, &gid, &lastIP, &devPause); err != nil {
		return res, errBadID
	}
	if name == "" {
		name = mac
	}
	res.Device = name
	now := time.Now()
	add := func(f whyFinding) { res.Findings = append(res.Findings, f) }
	blocked := ""

	// 1. A pause, by hand or by a profile's hours.
	paused := false
	for _, m := range fw.PausedDevices(a.Config, now) {
		if m == mac {
			paused = true
		}
	}
	if paused {
		f := whyFinding{Stage: "pause", State: "blocks", Title: "The internet is paused for this device",
			Text: "Either someone pressed Pause, or the device's profile is inside its off-hours. Nothing on the internet is reachable until then."}
		if devPause > now.Unix() {
			f.Fix = &whyFix{Label: "Resume now", Kind: "resume-device", ID: deviceID}
		} else {
			f.Fix = &whyFix{Label: "Change the profile's hours", Kind: "goto", Page: "profiles"}
		}
		add(f)
		blocked = "pause"
	} else {
		add(whyFinding{Stage: "pause", State: "ok", Title: "Not paused", Text: "The internet is on for this device."})
	}

	// 2. DNS filtering, when the target is a name.
	var ip netip.Addr
	if ad, err := netip.ParseAddr(target); err == nil {
		ip = ad
	} else {
		ex := a.Lists.Explain(target, gid)
		switch {
		case ex.Blocked && ex.Reason == "custom-block":
			add(whyFinding{Stage: "dns", State: "blocks", Title: "You blocked this site",
				Text: "A custom DNS rule (" + ex.Rule + ") blocks " + target + " for everyone.",
				Fix:  &whyFix{Label: "Allow this site", Kind: "allow-domain", Domain: target}})
			blocked = orWord(blocked, "dns")
		case ex.Blocked:
			list := "a blocklist"
			if len(ex.Lists) > 0 {
				list = "the list " + ex.Lists[0]
			}
			add(whyFinding{Stage: "dns", State: "blocks", Title: "DNS filtering blocks this name",
				Text: "This device's profile uses " + list + ", and it contains " + target + ".",
				Fix:  &whyFix{Label: "Allow this site", Kind: "allow-domain", Domain: target}})
			blocked = orWord(blocked, "dns")
		case ex.Reason == "unfiltered":
			add(whyFinding{Stage: "dns", State: "ok", Title: "DNS filtering is off for this device", Text: "Its profile does not filter."})
		default:
			add(whyFinding{Stage: "dns", State: "ok", Title: "DNS filtering allows it", Text: "No blocklist or custom rule stops " + target + "."})
		}
		if resp, _, err := a.lookupName(target, dns.TypeA); err == nil {
			for _, rr := range resp.Answer {
				if ar, ok := rr.(*dns.A); ok {
					if ad, ok := netip.AddrFromSlice(ar.A.To4()); ok {
						ip = ad
						break
					}
				}
			}
		}
		if !ip.IsValid() && blocked == "" {
			add(whyFinding{Stage: "dns", State: "unknown", Title: "The name did not resolve", Text: "Gatehouse could not find an address for " + target + ", so it may not exist, or your DNS upstream is not answering. Try the Internet test."})
		}
	}

	// 3. The firewall, in the order the rules are checked.
	m, err := fw.Load(a.Config)
	if err != nil {
		return res, err
	}
	zone := a.deviceZone(lastIP)
	if zone == "" {
		add(whyFinding{Stage: "firewall", State: "unknown", Title: "Could not tell which network the device is on", Text: "It has not got an address from Gatehouse lately, so the firewall check was skipped."})
	} else {
		var zf *fw.Zone
		if z, ok := m.ZoneByName(zone); ok {
			zf = z
		}
		src, _ := netip.ParseAddr(lastIP)
		if proto == "" {
			proto = "tcp"
		}
		decided := false
		for _, r := range m.Rules {
			if !r.Enabled || r.From != zone || (r.To != "Internet" && !strings.EqualFold(r.To, "wan")) || r.Not || fw.IsPauseRule(r) {
				continue
			}
			sm, ssure := ruleAddrMatch(r.Src, src, mac, m.DevGroups)
			dm, dsure := ruleAddrMatch(r.Dst, ip, "", m.DevGroups)
			vm, vsure := ruleServiceMatch(r.Service, proto, port, m.PortLists)
			if !sm || !dm || !vm {
				continue
			}
			if !(ssure && dsure && vsure) {
				add(whyFinding{Stage: "firewall", State: "unknown", Title: "A firewall rule might apply: " + r.Name,
					Text: "It mentions something Gatehouse cannot check here (a country, a domain or a port you did not give)."})
				continue
			}
			if r.Action == "accept" {
				add(whyFinding{Stage: "firewall", State: "ok", Title: "A firewall rule allows it: " + r.Name, Text: "It is the first rule that matches."})
			} else {
				add(whyFinding{Stage: "firewall", State: "blocks", Title: "A firewall rule blocks it: " + r.Name,
					Text: "This rule (" + r.Action + ") is the first that matches. " + r.Descr,
					Fix:  &whyFix{Label: "Open the Rules tab", Kind: "goto", Page: "firewall"}})
				blocked = orWord(blocked, "firewall")
			}
			decided = true
			break
		}
		if !decided {
			inet := fw.Zone{Name: "Internet", Kind: fw.KindWan}
			if zf != nil && fw.PairOpen(*zf, inet) {
				add(whyFinding{Stage: "firewall", State: "ok", Title: "The network type allows it", Text: zone + " may reach the internet and no rule says otherwise."})
			} else {
				add(whyFinding{Stage: "firewall", State: "blocks", Title: "The network type does not allow internet access",
					Text: zone + " is not set to reach the internet, and no rule makes an exception for this device.",
					Fix:  &whyFix{Label: "Open Networks", Kind: "goto", Page: "networks"}})
				blocked = orWord(blocked, "firewall")
			}
		}
		if zf != nil && zf.DNSForce && port == 53 {
			add(whyFinding{Stage: "forced-dns", State: "unknown", Title: "Forced DNS is on", Text: "Anything this device sends to port 53 is redirected to Gatehouse, whatever server it asks."})
		}
	}

	switch {
	case blocked != "":
		res.Verdict = "blocked"
		for _, f := range res.Findings {
			if f.State == "blocks" {
				res.Summary = f.Title + "."
				break
			}
		}
	default:
		res.Verdict = "open"
		res.Summary = "Nothing in Gatehouse blocks this. If it still fails, the problem is outside: the site may be down, or the device's own settings may be at fault. Run the Internet test."
		for _, f := range res.Findings {
			if f.State == "unknown" {
				res.Verdict = "unclear"
				res.Summary = "Nothing clearly blocks this, but some checks could not be completed (see below)."
				break
			}
		}
	}
	return res, nil
}

func (a *App) registerWhyRoutes() {
	a.API.Handle("POST /api/diag/why", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			Device int64  `json:"device"`
			Target string `json:"target"`
			Port   string `json:"port"`
			Proto  string `json:"proto"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		in.Target = strings.ToLower(strings.TrimSpace(in.Target))
		in.Target = strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(in.Target, "https://"), "http://"), "/")
		if h, _, err := net.SplitHostPort(in.Target); err == nil {
			in.Target = h
		}
		if in.Target == "" || strings.ContainsAny(in.Target, " /\\") {
			fail(w, errWhyTarget)
			return
		}
		port, _ := strconv.Atoi(strings.TrimSpace(in.Port))
		if port < 0 || port > 65535 {
			fail(w, errWhyTarget)
			return
		}
		res, err := a.whyCannotReach(in.Device, in.Target, port, strings.ToLower(in.Proto))
		if err != nil {
			fail(w, err)
			return
		}
		api.JSON(w, 200, res)
	})
}

var errWhyTarget = errString("enter a website name or an address, such as example.com or 93.184.216.34, and a port number if you know it")

type errString string

func (e errString) Error() string { return string(e) }

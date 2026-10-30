package fw

import (
	"encoding/json"
	"net/netip"
	"strings"
	"time"
)

// Pausing the internet for a device, or for everyone in a profile, now or during set hours. The
// pause is an ordinary rule that drops traffic from a set of devices to the internet. The set is
// refilled by the dynamic-set machinery, so pausing and resuming take effect within seconds with
// nothing to apply.

// PausedGroup is the name of the device set the pause rules refer to.
const PausedGroup = "Paused"

// PausePrefix starts the name of every pause rule.
const PausePrefix = "Paused"

func IsPauseRule(r Rule) bool {
	return strings.HasPrefix(r.Name, PausePrefix) && r.Src == "grp:"+PausedGroup
}

// Hours is a weekly window when the internet is off. Days are 0 (Sunday) to 6.
type Hours struct {
	On   bool   `json:"on"`
	Days []int  `json:"days"`
	From string `json:"from"` // 21:00
	To   string `json:"to"`   // 07:00
}

func ParseHours(s string) Hours {
	var h Hours
	if s != "" {
		json.Unmarshal([]byte(s), &h)
	}
	return h
}

func clock(s string) (int, bool) {
	h, m, ok := strings.Cut(strings.TrimSpace(s), ":")
	if !ok || len(h) == 0 || len(m) != 2 {
		return 0, false
	}
	hh, mm := 0, 0
	for _, c := range h {
		if c < '0' || c > '9' {
			return 0, false
		}
		hh = hh*10 + int(c-'0')
	}
	for _, c := range m {
		if c < '0' || c > '9' {
			return 0, false
		}
		mm = mm*10 + int(c-'0')
	}
	if hh > 23 || mm > 59 {
		return 0, false
	}
	return hh*60 + mm, true
}

// Valid says whether the hours can be stored.
func (h Hours) Valid() bool {
	if !h.On {
		return true
	}
	f, ok1 := clock(h.From)
	t, ok2 := clock(h.To)
	return ok1 && ok2 && f != t && len(h.Days) > 0
}

// Active says whether the internet is off at a moment. A window that crosses midnight belongs to
// the day it starts on, so Friday 22:00 to 07:00 covers early Saturday.
func (h Hours) Active(now time.Time) bool {
	if !h.On || !h.Valid() {
		return false
	}
	f, _ := clock(h.From)
	t, _ := clock(h.To)
	day := func(d int) bool {
		for _, x := range h.Days {
			if x == d {
				return true
			}
		}
		return false
	}
	m := now.Hour()*60 + now.Minute()
	today, yesterday := int(now.Weekday()), (int(now.Weekday())+6)%7
	if f < t {
		return day(today) && m >= f && m < t
	}
	return (day(today) && m >= f) || (day(yesterday) && m < t)
}

// PausedDevices lists the devices whose internet is off right now: paused by hand, in a profile
// that is paused, or inside a profile's hours.
func PausedDevices(db Queryer, now time.Time) []string {
	type grp struct {
		paused int64
		hours  Hours
	}
	// The Default profile, and the profile of each network that has one.
	var def grp
	var dh string
	if r, err := db.Query(`SELECT COALESCE(paused_until,0),COALESCE(hours,'') FROM groups WHERE name='Default'`); err == nil {
		if r.Next() {
			r.Scan(&def.paused, &dh)
			def.hours = ParseHours(dh)
		}
		r.Close()
	}
	type netGrp struct {
		p netip.Prefix
		g grp
	}
	var nets []netGrp
	if r, err := db.Query(`SELECT i.ipv4_cidr,COALESCE(g.paused_until,0),COALESCE(g.hours,'') FROM network_profiles np
		JOIN zones z ON z.name=np.zone JOIN interfaces i ON i.zone_id=z.id JOIN groups g ON g.id=np.group_id WHERE i.ipv4_cidr<>''`); err == nil {
		for r.Next() {
			var cidr, h string
			var g grp
			if r.Scan(&cidr, &g.paused, &h) == nil {
				if p, err := netip.ParsePrefix(cidr); err == nil {
					g.hours = ParseHours(h)
					nets = append(nets, netGrp{p.Masked(), g})
				}
			}
		}
		r.Close()
	}
	rows, err := db.Query(`SELECT d.mac,d.last_ip,d.paused_until,COALESCE(d.group_id,0),COALESCE(g.paused_until,0),COALESCE(g.hours,'')
		FROM devices d LEFT JOIN groups g ON g.id=d.group_id`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	u := now.Unix()
	for rows.Next() {
		var mac, ip, hours string
		var dp, gid, gp int64
		if rows.Scan(&mac, &ip, &dp, &gid, &gp, &hours) != nil {
			continue
		}
		g := grp{gp, ParseHours(hours)}
		if gid == 0 {
			g = def
			if a, err := netip.ParseAddr(ip); err == nil {
				for _, n := range nets {
					if n.p.Contains(a) {
						g = n.g
						break
					}
				}
			}
		}
		if dp > u || g.paused > u || g.hours.Active(now) {
			out = append(out, strings.ToLower(mac))
		}
	}
	return out
}

// PauseRules makes one rule per network that drops the paused devices' traffic to the internet.
func PauseRules(zones []Zone) []Rule {
	var out []Rule
	for _, z := range zones {
		if z.Kind != KindWan && len(z.Ifaces) > 0 {
			out = append(out, Rule{Name: PausePrefix, From: z.Name, To: "Internet", Src: "grp:" + PausedGroup, Service: "any", Action: "drop",
				Enabled: true, Descr: "Devices that are paused, or inside their profile's off-hours, cannot reach the internet from " + z.Name + "."})
		}
	}
	return out
}

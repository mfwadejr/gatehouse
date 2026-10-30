package fw

import (
	"encoding/json"
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
	rows, err := db.Query(`SELECT d.mac,d.paused_until,COALESCE(g.paused_until,0),COALESCE(g.hours,'')
		FROM devices d LEFT JOIN groups g ON g.id=COALESCE(d.group_id,(SELECT id FROM groups WHERE name='Default'))`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	u := now.Unix()
	for rows.Next() {
		var mac, hours string
		var dp, gp int64
		if rows.Scan(&mac, &dp, &gp, &hours) != nil {
			continue
		}
		if dp > u || gp > u || ParseHours(hours).Active(now) {
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

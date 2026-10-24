package fw

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Schedule says when a rule is in force. The firewall does the time check itself, so a rule
// switches on and off with no help from the console.
type Schedule struct {
	Mode  string `json:"mode"`            // "" or "always", "daily", "weekly", "once"
	From  string `json:"from,omitempty"`  // HH:MM, daily and weekly
	To    string `json:"to,omitempty"`    // HH:MM; earlier than From means past midnight
	Days  []int  `json:"days,omitempty"`  // weekly: 0 Sunday .. 6 Saturday, the days a window starts
	Start string `json:"start,omitempty"` // once: 2006-01-02T15:04 in the machine's time zone
	End   string `json:"end,omitempty"`
}

// ParseSchedule reads the stored form. Empty means always.
func ParseSchedule(s string) (*Schedule, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	var sc Schedule
	if err := json.Unmarshal([]byte(s), &sc); err != nil {
		return nil, errors.New("the schedule could not be read")
	}
	if sc.Mode == "" || sc.Mode == "always" {
		return nil, nil
	}
	return &sc, nil
}

func parseHM(s string) (int, error) {
	t, err := time.Parse("15:04", s)
	if err != nil {
		return 0, fmt.Errorf("%q is not a time like 08:30", s)
	}
	return t.Hour()*60 + t.Minute(), nil
}

const localMinute = "2006-01-02T15:04"

// Check reports a problem with the schedule.
func (sc *Schedule) Check(loc *time.Location) error {
	if sc == nil {
		return nil
	}
	switch sc.Mode {
	case "daily", "weekly":
		a, err := parseHM(sc.From)
		if err != nil {
			return err
		}
		b, err := parseHM(sc.To)
		if err != nil {
			return err
		}
		if a == b {
			return errors.New("the start and end time are the same")
		}
		if sc.Mode == "weekly" {
			if len(sc.Days) == 0 {
				return errors.New("choose at least one day")
			}
			for _, d := range sc.Days {
				if d < 0 || d > 6 {
					return errors.New("a day of the week is not valid")
				}
			}
		}
	case "once":
		a, err1 := time.ParseInLocation(localMinute, sc.Start, loc)
		b, err2 := time.ParseInLocation(localMinute, sc.End, loc)
		if err1 != nil || err2 != nil {
			return errors.New("enter the start and end as a date and time")
		}
		if !b.After(a) {
			return errors.New("the end must be after the start")
		}
	default:
		return fmt.Errorf("unknown schedule %q", sc.Mode)
	}
	return nil
}

var dayNames = []string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}

const weekMin = 7 * 24 * 60

// Clauses renders the schedule as alternatives, each a piece of an nft rule. A rule without a
// schedule has one empty alternative. nft is run with the clock in UTC, so the local times are
// converted here, using the offset in force when the ruleset is built. The console builds the
// ruleset again when that offset changes (summer time), so the windows stay where people put them.
func (sc *Schedule) Clauses(loc *time.Location, now time.Time) []string {
	if sc == nil {
		return []string{""}
	}
	if sc.Mode == "once" {
		a, _ := time.ParseInLocation(localMinute, sc.Start, loc)
		b, _ := time.ParseInLocation(localMinute, sc.End, loc)
		return []string{fmt.Sprintf("meta time >= %d meta time < %d", a.Unix(), b.Unix())}
	}
	_, off := now.In(loc).Zone()
	offMin := off / 60
	from, _ := parseHM(sc.From)
	to, _ := parseHM(sc.To)
	length := to - from
	if length <= 0 {
		length += 24 * 60
	}
	var starts []int // local minute of the week at which a window starts
	if sc.Mode == "daily" {
		for d := 0; d < 7; d++ {
			starts = append(starts, d*1440+from)
		}
	} else {
		for _, d := range sc.Days {
			starts = append(starts, d*1440+from)
		}
	}
	type seg struct{ day, a, b int } // UTC day, first and last minute (b exclusive)
	var segs []seg
	for _, st := range starts {
		u := ((st-offMin)%weekMin + weekMin) % weekMin
		left := length
		for left > 0 {
			day, m := u/1440, u%1440
			take := 1440 - m
			if take > left {
				take = left
			}
			segs = append(segs, seg{day, m, m + take})
			left -= take
			u = (u + take) % weekMin
		}
	}
	// Whole days collapse into one clause per day; the rest are time ranges within a day.
	sort.Slice(segs, func(i, j int) bool {
		if segs[i].day != segs[j].day {
			return segs[i].day < segs[j].day
		}
		return segs[i].a < segs[j].a
	})
	var out []string
	for _, s := range segs {
		clause := fmt.Sprintf("meta day %q", dayNames[s.day])
		if !(s.a == 0 && s.b == 1440) {
			end := s.b*60 - 1
			clause += fmt.Sprintf(" meta hour \"%02d:%02d:%02d\"-\"%02d:%02d:%02d\"", s.a/60, s.a%60, 0, end/3600, (end/60)%60, end%60)
		}
		out = append(out, clause)
	}
	return out
}

// Describe puts the schedule in words.
func (sc *Schedule) Describe() string {
	if sc == nil {
		return "Always"
	}
	switch sc.Mode {
	case "daily":
		return fmt.Sprintf("Every day, %s to %s", sc.From, sc.To)
	case "weekly":
		var d []string
		for _, x := range sc.Days {
			if x >= 0 && x <= 6 {
				d = append(d, dayNames[x][:3])
			}
		}
		return fmt.Sprintf("%s, %s to %s", strings.Join(d, ", "), sc.From, sc.To)
	case "once":
		return "Once, " + strings.Replace(sc.Start, "T", " ", 1) + " to " + strings.Replace(sc.End, "T", " ", 1)
	}
	return "Always"
}

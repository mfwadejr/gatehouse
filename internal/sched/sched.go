// Package sched is the one place that says when a recurring job is next due. A schedule is a short
// text such as "every:12", "daily:03:30", "weekly:1:03:30" (Monday) or "off". It is a pure
// calculation, so it is easy to test and the same for every job.
package sched

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

const MinEveryHours = 1

// Spec is a parsed schedule.
type Spec struct {
	Kind    string // every, daily, weekly, off
	Hours   int    // every
	Weekday int    // weekly, 0 = Sunday
	Hour    int
	Minute  int
}

// Parse reads a schedule text. It reports a plain-English problem when the text is not usable.
func Parse(s string) (Spec, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	p := strings.Split(s, ":")
	switch p[0] {
	case "off":
		return Spec{Kind: "off"}, nil
	case "every":
		if len(p) != 2 {
			return Spec{}, fmt.Errorf("Choose how many hours.")
		}
		h, err := strconv.Atoi(p[1])
		if err != nil || h < MinEveryHours || h > 24*30 {
			return Spec{}, fmt.Errorf("Choose between 1 hour and 30 days (720 hours).")
		}
		return Spec{Kind: "every", Hours: h}, nil
	case "daily":
		if len(p) != 3 {
			return Spec{}, fmt.Errorf("Choose a time of day.")
		}
		h, m, err := hm(p[1], p[2])
		if err != nil {
			return Spec{}, err
		}
		return Spec{Kind: "daily", Hour: h, Minute: m}, nil
	case "weekly":
		if len(p) != 4 {
			return Spec{}, fmt.Errorf("Choose a day and a time.")
		}
		d, err := strconv.Atoi(p[1])
		if err != nil || d < 0 || d > 6 {
			return Spec{}, fmt.Errorf("Choose a day of the week.")
		}
		h, m, err := hm(p[2], p[3])
		if err != nil {
			return Spec{}, err
		}
		return Spec{Kind: "weekly", Weekday: d, Hour: h, Minute: m}, nil
	}
	return Spec{}, fmt.Errorf("That schedule isn't one this job understands.")
}

func hm(a, b string) (int, int, error) {
	h, e1 := strconv.Atoi(a)
	m, e2 := strconv.Atoi(b)
	if e1 != nil || e2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, 0, fmt.Errorf("Choose a time between 00:00 and 23:59.")
	}
	return h, m, nil
}

// String is the text form of the schedule.
func (s Spec) String() string {
	switch s.Kind {
	case "every":
		return fmt.Sprintf("every:%d", s.Hours)
	case "daily":
		return fmt.Sprintf("daily:%02d:%02d", s.Hour, s.Minute)
	case "weekly":
		return fmt.Sprintf("weekly:%d:%02d:%02d", s.Weekday, s.Hour, s.Minute)
	}
	return "off"
}

// Plain describes the schedule in words.
func (s Spec) Plain() string {
	days := []string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}
	switch s.Kind {
	case "every":
		if s.Hours == 1 {
			return "Every hour"
		}
		if s.Hours%24 == 0 {
			if s.Hours == 24 {
				return "Every day"
			}
			return fmt.Sprintf("Every %d days", s.Hours/24)
		}
		return fmt.Sprintf("Every %d hours", s.Hours)
	case "daily":
		return fmt.Sprintf("Every day at %02d:%02d", s.Hour, s.Minute)
	case "weekly":
		return fmt.Sprintf("Every %s at %02d:%02d", days[s.Weekday], s.Hour, s.Minute)
	}
	return "Off"
}

// Next is when the job is next due. last is when it last finished well (zero if never). A job that has
// never run, or that missed its slot while the machine was off, is due straight away (catch-up).
// Off schedules are never due (the zero time).
func (s Spec) Next(last, now time.Time) time.Time {
	loc := now.Location()
	switch s.Kind {
	case "every":
		if last.IsZero() {
			return now
		}
		return last.Add(time.Duration(s.Hours) * time.Hour)
	case "daily", "weekly":
		if last.IsZero() {
			return now
		}
		// the first slot after the last good run
		t := time.Date(last.Year(), last.Month(), last.Day(), s.Hour, s.Minute, 0, 0, loc)
		for i := 0; i < 400; i++ {
			ok := t.After(last)
			if ok && s.Kind == "weekly" && int(t.Weekday()) != s.Weekday {
				ok = false
			}
			if ok {
				return t
			}
			t = time.Date(t.Year(), t.Month(), t.Day()+1, s.Hour, s.Minute, 0, 0, loc)
		}
	}
	return time.Time{}
}

// Due says whether the job should run now.
func (s Spec) Due(last, now time.Time) bool {
	if s.Kind == "off" {
		return false
	}
	n := s.Next(last, now)
	return !n.IsZero() && !n.After(now)
}

// RetryDelay is how long to wait after the n-th failure in a row: 5 minutes, 15, 30, then an hour.
func RetryDelay(n int) time.Duration {
	steps := []time.Duration{5 * time.Minute, 15 * time.Minute, 30 * time.Minute, time.Hour}
	if n < 0 {
		n = 0
	}
	if n >= len(steps) {
		n = len(steps) - 1
	}
	return steps[n]
}

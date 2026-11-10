package app

import (
	"fmt"
	"net/http"
	"sort"
	"time"

	"nopewarden/internal/api"
)

// The Threats Overview tab: what the watching program is doing, in numbers a person can read at a
// glance. Everything here is read from what NopeWarden already keeps (the program's status, the
// alerts, the Logs and the history), so showing it costs the program nothing.

type threatKindCount struct {
	Kind  string `json:"kind"`
	Label string `json:"label"`
	Count int    `json:"count"`
	Quiet bool   `json:"quiet"` // usually harmless
}

type threatDeviceCount struct {
	Device string `json:"device"`
	Count  int    `json:"count"`
}

type threatFeedItem struct {
	TS       int64  `json:"ts"`
	Severity string `json:"severity"`
	Title    string `json:"title"`
	Detail   string `json:"detail,omitempty"`
}

func threatKindLabel(kind string) string {
	switch kind {
	case threatKindList:
		return "Known-bad addresses"
	case threatKindMalware:
		return "Possible infection"
	case threatKindProbe:
		return "Probing for open doors"
	case threatKindAttack:
		return "Break-in attempts"
	case threatKindTrick:
		return "Tricking sites"
	case threatKindFlood:
		return "Floods of traffic"
	case threatKindPolicy:
		return "Unusual but usually harmless"
	case threatKindOdd:
		return "Odd network traffic"
	}
	return "Other"
}

// threatCounts adds up the saved alerts: how many in the last hour, day and week, which kinds are
// the most common over the week, and which devices are involved most.
func (a *App) threatCounts(now time.Time) (hour, day, week int, kinds []threatKindCount, devs []threatDeviceCount) {
	kinds, devs = []threatKindCount{}, []threatDeviceCount{}
	if a.Logs == nil {
		return
	}
	rows, err := a.Logs.Query(`SELECT ts,signature,category,mac,device,src,dst FROM threat_alerts WHERE ts>=?`, now.Add(-7*24*time.Hour).Unix())
	if err != nil {
		return
	}
	defer rows.Close()
	byKind, byDev := map[string]int{}, map[string]int{}
	label := a.deviceLabeler()
	for rows.Next() {
		var ts int64
		var sig, cat, mac, dev, src, dst string
		if rows.Scan(&ts, &sig, &cat, &mac, &dev, &src, &dst) != nil {
			continue
		}
		week++
		age := now.Unix() - ts
		if age <= 24*3600 {
			day++
		}
		if age <= 3600 {
			hour++
		}
		k := threatKind(cat, sig)
		byKind[k]++
		if k == threatKindOdd || k == threatKindPolicy {
			continue // odd and usually harmless traffic does not make a device look bad
		}
		local := src
		if !privateAddr(local) {
			local = dst
		}
		d := label(mac, local)
		if d == "" {
			d = dev
		}
		if d != "" {
			byDev[d]++
		}
	}
	for k, n := range byKind {
		kinds = append(kinds, threatKindCount{Kind: k, Label: threatKindLabel(k), Count: n, Quiet: k == threatKindOdd || k == threatKindPolicy})
	}
	sort.Slice(kinds, func(i, j int) bool {
		if kinds[i].Quiet != kinds[j].Quiet {
			return !kinds[i].Quiet // the ones that matter come first
		}
		if kinds[i].Count != kinds[j].Count {
			return kinds[i].Count > kinds[j].Count
		}
		return kinds[i].Kind < kinds[j].Kind
	})
	for d, n := range byDev {
		devs = append(devs, threatDeviceCount{d, n})
	}
	sort.Slice(devs, func(i, j int) bool {
		if devs[i].Count != devs[j].Count {
			return devs[i].Count > devs[j].Count
		}
		return devs[i].Device < devs[j].Device
	})
	if len(devs) > 5 {
		devs = devs[:5]
	}
	return
}

// threatFeed is the short plain-language list of what the program and NopeWarden did about it:
// rule updates, switch-offs, installs and changes. Alerts themselves are on the Alerts tab.
func (a *App) threatFeed(limit int) []threatFeedItem {
	out := []threatFeedItem{}
	if a.Logs == nil {
		return out
	}
	rows, err := a.Logs.Query(`SELECT ts,severity,title,detail FROM events
		WHERE (area='threat' AND actor='system')
		   OR title LIKE 'Threat %' OR title LIKE '%threat-watching%' OR title LIKE '%threat checks%' OR title LIKE '%threat settings%'
		ORDER BY ts DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var it threatFeedItem
		if rows.Scan(&it.TS, &it.Severity, &it.Title, &it.Detail) == nil {
			out = append(out, it)
		}
	}
	return out
}

// threatRulesNextDue says when the schedule will refresh the rules next, or zero when it will not.
func (a *App) threatRulesNextDue(now time.Time) int64 {
	if a.threatMode() != "watch" {
		return 0
	}
	j, ok := schedFind("threat")
	if !ok {
		return 0
	}
	n := a.schedSpec("threat").Next(j.Last(a), now)
	if n.IsZero() {
		return 0
	}
	return n.Unix()
}

func (a *App) threatOverview(now time.Time) map[string]any {
	info := a.threatInfo()
	hour, day, week, kinds, devs := a.threatCounts(now)
	w := &a.threatW
	w.mu.Lock()
	since, cpu, mem := w.runningSince, w.cpuPct, w.memMB
	w.mu.Unlock()
	var sinceUnix int64
	if !since.IsZero() {
		sinceUnix = since.Unix()
	}
	return map[string]any{
		"mode": a.threatMode(), "installed": info.Installed, "running": info.Running, "version": info.Version,
		"since": sinceUnix, "ports": a.ndpiIfaces(),
		"cpu_now": cpu, "cpu_limit": a.threatInt(threatCPUSetting, 50, 10, 400),
		"mem_now_mb": mem, "mem_limit_mb": a.threatInt(threatMemSetting, 2048, 256, 16384),
		"rules_count": info.RulesCount, "rules_updated": info.RulesUpdated, "rules_next": a.threatRulesNextDue(now),
		"alerts": map[string]int{"hour": hour, "day": day, "week": week},
		"kinds":  kinds, "devices": devs, "feed": a.threatFeed(12),
		"why": info.Why, "failing": info.Failing,
	}
}

// threatHistPoints are the numbers drawn on the Overview charts: how much traffic the watched ports
// carry, and how much of the machine the program uses. They are saved only while it is running.
func (a *App) threatHistPoints() []histPoint {
	if a.threatMode() != "watch" {
		return nil
	}
	w := &a.threatW
	w.mu.Lock()
	running, cpu, mem := !w.runningSince.IsZero(), w.cpuPct, w.memMB
	w.mu.Unlock()
	if !running {
		return nil
	}
	var bits float64
	for _, v := range a.netTraffic.latest() {
		bits += v[0] + v[1]
	}
	return []histPoint{{"threat.watched", bits}, {"threat.cpu", cpu}, {"threat.mem", float64(mem)}}
}

func (a *App) registerThreatOverviewRoutes() {
	a.API.Handle("GET /api/threat/overview", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		api.JSON(w, 200, a.threatOverview(time.Now()))
	})
}

// threatRulesUpdatedNote writes the "rules updated" line the activity feed shows.
func (a *App) threatRulesUpdatedNote(now time.Time) {
	if a.Logs == nil || a.Net == nil {
		return
	}
	n := a.Net.ThreatStatus().RulesCount
	detail := ""
	if n > 0 {
		detail = fmt.Sprintf("%d rules are loaded.", n)
	}
	a.Logs.Exec(`INSERT INTO events(ts,area,severity,actor,title,detail) VALUES(?,?,?,?,?,?)`, now.Unix(), "threat", "info", "system", "Threat rules updated", detail)
}

package app

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"nopewarden/internal/api"
)

// The edge numbers answer "who is knocking on my door": blocked attempts that came in from the
// internet, taken from the firewall log, so they work with or without the threat-watching program.

type edgeProber struct {
	Addr     string   `json:"addr"`
	Country  string   `json:"country,omitempty"`
	Attempts int      `json:"attempts"`
	Ports    int      `json:"ports"`
	Top      []string `json:"top"`
	Reason   string   `json:"reason"`
	Last     int64    `json:"last"`
}

type edgePort struct {
	Label    string `json:"label"`
	Proto    string `json:"proto"`
	Port     int    `json:"port"`
	Attempts int    `json:"attempts"`
	Sources  int    `json:"sources"`
}

type edgeNet struct {
	Network  string `json:"network"`
	Attempts int    `json:"attempts"`
}

type edgeDay struct {
	Day   int64 `json:"day"`
	Total int   `json:"total"`
	Worst int   `json:"worst"`    // most blocked in one hour of that day
	At    int64 `json:"worst_at"` // the start of that hour
}

func edgePortLabel(proto string, port int) string {
	if n := serviceName(proto, port); n != "" {
		return n + " (" + strings.ToLower(proto) + "/" + strconv.Itoa(port) + ")"
	}
	if port == 0 {
		return strings.ToUpper(proto)
	}
	return strings.ToLower(proto) + "/" + strconv.Itoa(port)
}

func edgeReason(ports int, topLabel string, pingOnly bool) string {
	switch {
	case pingOnly:
		return "Checking whether anything answers"
	case ports >= 15:
		return "Trying many doors to see which are open"
	case ports >= 4:
		return "Trying a handful of doors"
	default:
		return "Knocking on " + topLabel
	}
}

func (a *App) threatEdge(now time.Time, hours int) map[string]any {
	if hours < 1 {
		hours = 24
	}
	if hours > 24*30 {
		hours = 24 * 30
	}
	names := []any{}
	for n := range a.wanZoneNames() {
		names = append(names, n)
	}
	ph := strings.TrimSuffix(strings.Repeat("?,", len(names)), ",")
	since := now.Add(-time.Duration(hours) * time.Hour).Unix()
	base := `FROM fw_events WHERE action='drop' AND ts>=? AND from_zone IN (` + ph + `)`
	args := append([]any{since}, names...)

	out := map[string]any{"hours": hours, "probers": []edgeProber{}, "ports": []edgePort{}, "networks": []edgeNet{}, "days": []edgeDay{}}
	var total, sources int
	a.Logs.QueryRow(`SELECT COUNT(*), COUNT(DISTINCT from_addr) `+base, args...).Scan(&total, &sources)
	out["total"], out["sources"] = total, sources

	// Who: the busiest addresses, with how many different doors each tried.
	probers := []edgeProber{}
	if rows, err := a.Logs.Query(`SELECT from_addr, COUNT(*) n, COUNT(DISTINCT proto||'/'||dport), MAX(ts) `+base+` GROUP BY from_addr ORDER BY n DESC LIMIT 10`, args...); err == nil {
		for rows.Next() {
			var p edgeProber
			rows.Scan(&p.Addr, &p.Attempts, &p.Ports, &p.Last)
			p.Country = a.country(p.Addr)
			probers = append(probers, p)
		}
		rows.Close()
	}
	for i := range probers {
		p := &probers[i]
		rows, err := a.Logs.Query(`SELECT proto, dport, COUNT(*) n `+base+` AND from_addr=? GROUP BY proto, dport ORDER BY n DESC LIMIT 3`, append(append([]any{}, args...), p.Addr)...)
		pingOnly := p.Ports == 1
		if err == nil {
			for rows.Next() {
				var proto string
				var port, n int
				rows.Scan(&proto, &port, &n)
				p.Top = append(p.Top, edgePortLabel(proto, port))
				if port != 0 {
					pingOnly = false
				}
			}
			rows.Close()
		}
		top := ""
		if len(p.Top) > 0 {
			top = p.Top[0]
		}
		p.Reason = edgeReason(p.Ports, strings.Join(p.Top, ", "), pingOnly && top != "" && strings.HasPrefix(strings.ToLower(top), "icmp"))
	}
	out["probers"] = probers

	// Which doors are being tried most, and by how many different addresses.
	ports := []edgePort{}
	if rows, err := a.Logs.Query(`SELECT proto, dport, COUNT(*) n, COUNT(DISTINCT from_addr) `+base+` GROUP BY proto, dport ORDER BY n DESC LIMIT 10`, args...); err == nil {
		for rows.Next() {
			var p edgePort
			rows.Scan(&p.Proto, &p.Port, &p.Attempts, &p.Sources)
			p.Label = edgePortLabel(p.Proto, p.Port)
			ports = append(ports, p)
		}
		rows.Close()
	}
	out["ports"] = ports

	// Which of your networks the blocked attempts were aimed at.
	nets := []edgeNet{}
	if rows, err := a.Logs.Query(`SELECT to_zone, COUNT(*) n `+base+` GROUP BY to_zone ORDER BY n DESC LIMIT 8`, args...); err == nil {
		for rows.Next() {
			var z string
			var n int
			rows.Scan(&z, &n)
			if z == "" {
				z = "This firewall"
			}
			nets = append(nets, edgeNet{Network: z, Attempts: n})
		}
		rows.Close()
	}
	out["networks"] = nets

	// The last 30 days, one cell a day, with each day's worst hour.
	from := dayStart(now.AddDate(0, 0, -29))
	byDay := map[int64]*edgeDay{}
	dargs := append([]any{from}, names...)
	if rows, err := a.Logs.Query(`SELECT ts/3600, COUNT(*) FROM fw_events WHERE action='drop' AND ts>=? AND from_zone IN (`+ph+`) GROUP BY ts/3600`, dargs...); err == nil {
		for rows.Next() {
			var h int64
			var n int
			rows.Scan(&h, &n)
			d := dayStart(time.Unix(h*3600, 0))
			e := byDay[d]
			if e == nil {
				e = &edgeDay{Day: d}
				byDay[d] = e
			}
			e.Total += n
			if n > e.Worst {
				e.Worst, e.At = n, h*3600
			}
		}
		rows.Close()
	}
	days := []edgeDay{}
	for i := 29; i >= 0; i-- {
		d := dayStart(now.AddDate(0, 0, -i))
		if e := byDay[d]; e != nil {
			days = append(days, *e)
		} else {
			days = append(days, edgeDay{Day: d})
		}
	}
	sort.Slice(days, func(i, j int) bool { return days[i].Day < days[j].Day })
	out["days"] = days
	return out
}

func (a *App) registerThreatEdgeRoutes() {
	a.API.Handle("GET /api/threat/edge", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		h, _ := strconv.Atoi(r.URL.Query().Get("hours"))
		api.JSON(w, 200, a.threatEdge(time.Now(), h))
	})
}

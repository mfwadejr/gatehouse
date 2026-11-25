package app

import (
	"net/http"
	"strings"
	"time"

	"nopewarden/internal/api"
)

// The numbers behind the morning summary: what happened yesterday (the whole local day before today),
// taken from what the box already keeps. The words are put together in the console from these and from
// the same list the left menu's dots come from, by fixed rules, with no AI involved.

type yesterdayNumbers struct {
	From        int64    `json:"from"`
	To          int64    `json:"to"`
	DNSTotal    int      `json:"dns_total"`
	DNSBlocked  int      `json:"dns_blocked"`
	BlockedIn   int      `json:"blocked_in"`
	ProbeAddrs  int      `json:"probe_addrs"`
	Outages     int      `json:"outages"`
	DownSeconds int64    `json:"down_seconds"`
	DevicesPeak int      `json:"devices_peak"`
	Alerts      int      `json:"alerts"`
	Drives      []string `json:"drives"` // one plain line per drive
	Power       []string `json:"power"`  // the power line, when a UPS is connected
	Since       int64    `json:"since"`  // when the oldest record the box has begins; later than From means a partial day
}

func (a *App) yesterday(now time.Time) yesterdayNumbers {
	to := dayStart(now)
	from := dayStart(time.Unix(to-3600, 0))
	y := yesterdayNumbers{From: from, To: to}
	if a.Logs == nil {
		return y
	}
	y.Drives = a.driveSummaryLines()
	y.Power = a.powerSummaryLines(from, to)
	a.Logs.QueryRow(`SELECT COUNT(*), COALESCE(SUM(CASE WHEN result='blocked' THEN 1 ELSE 0 END),0) FROM dns_queries WHERE ts>=? AND ts<?`, from, to).Scan(&y.DNSTotal, &y.DNSBlocked)
	var names []any
	for n := range a.wanZoneNames() {
		names = append(names, n)
	}
	ph := strings.TrimSuffix(strings.Repeat("?,", len(names)), ",")
	args := append([]any{from, to}, names...)
	a.Logs.QueryRow(`SELECT COUNT(*), COUNT(DISTINCT from_addr) FROM fw_events WHERE action='drop' AND ts>=? AND ts<? AND from_zone IN (`+ph+`)`, args...).Scan(&y.BlockedIn, &y.ProbeAddrs)
	if rows, err := a.Logs.Query(`SELECT start, end FROM outages WHERE start<? AND (end>? OR end=0)`, to, from); err == nil {
		for rows.Next() {
			var s, e int64
			rows.Scan(&s, &e)
			if e == 0 || e > to {
				e = to
			}
			if s < from {
				s = from
			}
			if e > s {
				y.Outages++
				y.DownSeconds += e - s
			}
		}
		rows.Close()
	}
	var peak float64
	a.Logs.QueryRow(`SELECT COALESCE(MAX(value),0) FROM metrics WHERE key='x|devices.online' AND ts=?`, from).Scan(&peak)
	if peak == 0 {
		a.Logs.QueryRow(`SELECT COALESCE(MAX(value),0) FROM metrics WHERE key='b|devices.online' AND ts>=? AND ts<?`, from, to).Scan(&peak)
	}
	y.DevicesPeak = int(peak)
	a.Logs.QueryRow(`SELECT COUNT(*) FROM threat_alerts WHERE ts>=? AND ts<?`, from, to).Scan(&y.Alerts)
	var m1, m2 int64
	a.Logs.QueryRow(`SELECT COALESCE(MIN(ts),0) FROM metrics`).Scan(&m1)
	a.Logs.QueryRow(`SELECT COALESCE(MIN(ts),0) FROM dns_queries`).Scan(&m2)
	y.Since = m1
	if m2 > 0 && (y.Since == 0 || m2 < y.Since) {
		y.Since = m2
	}
	return y
}

func (a *App) registerMorningRoutes() {
	a.API.Handle("GET /api/overview/yesterday", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		api.JSON(w, 200, a.cached("yesterday", time.Minute, func() any { return a.yesterday(time.Now()) }))
	})
}

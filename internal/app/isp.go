package app

import (
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"sort"
	"strings"
	"time"

	"gatehouse/internal/api"
)

// The provider report card and data use. Outages are recorded as they happen; speeds come from the
// speed tests; data use comes from the traffic counters. Everything states how many measurements it
// is built on, so it can be shown to a provider without overclaiming.

// ---- outages ----

type outageState struct{ open int64 } // id of the outage in progress, 0 when none

var outages outageState

// outageTick keeps the outage table: a row opens when the internet looks down and closes when it
// is back. It runs whether or not alerts are on.
func (a *App) outageTick(now time.Time) {
	if a.Logs == nil {
		return
	}
	down := a.internetLooksDown()
	switch {
	case down && outages.open == 0:
		if res, err := a.Logs.Exec(`INSERT INTO outages(start) VALUES(?)`, now.Unix()); err == nil {
			outages.open, _ = res.LastInsertId()
		}
	case !down && outages.open != 0:
		a.Logs.Exec(`UPDATE outages SET end=? WHERE id=?`, now.Unix(), outages.open)
		outages.open = 0
	}
}

type ispOutage struct {
	Start int64 `json:"start"`
	End   int64 `json:"end"` // 0: still down
	Secs  int64 `json:"secs"`
}

type ispLink struct {
	Link        string  `json:"link"`
	Tests       int     `json:"tests"`
	AvgDownMbps float64 `json:"avg_down_mbps"`
	AvgUpMbps   float64 `json:"avg_up_mbps"`
	WorstDown   float64 `json:"worst_down_mbps"`
	PlanDown    float64 `json:"plan_down_mbps"`
	PlanUp      float64 `json:"plan_up_mbps"`
	BelowPlan   int     `json:"below_plan"` // tests under 80% of the plan's download speed
	AvgDelayMs  float64 `json:"avg_delay_ms"`
	AvgLoadedMs float64 `json:"avg_loaded_ms"`
}

type ispReport struct {
	Month      string      `json:"month"`
	From       int64       `json:"from"`
	To         int64       `json:"to"`
	Outages    []ispOutage `json:"outages"`
	DownSecs   int64       `json:"down_secs"`
	UpPct      float64     `json:"up_pct"`
	Links      []ispLink   `json:"links"`
	SpeedTests int         `json:"speed_tests"`
	Since      int64       `json:"since"` // when Gatehouse started recording outages
}

// ispMonth parses YYYY-MM (the current month when empty) in the router's time zone.
func (a *App) ispMonth(m string) (time.Time, time.Time, string, error) {
	loc := a.localNow().Location()
	var t time.Time
	if m == "" {
		n := a.localNow()
		t = time.Date(n.Year(), n.Month(), 1, 0, 0, 0, 0, loc)
	} else {
		p, err := time.ParseInLocation("2006-01", m, loc)
		if err != nil {
			return t, t, "", fmt.Errorf("choose a month like 2026-10")
		}
		t = p
	}
	return t, t.AddDate(0, 1, 0), t.Format("2006-01"), nil
}

func (a *App) ispReport(month string) (ispReport, error) {
	from, to, label, err := a.ispMonth(month)
	if err != nil {
		return ispReport{}, err
	}
	r := ispReport{Month: label, From: from.Unix(), To: to.Unix(), Outages: []ispOutage{}, Links: []ispLink{}}
	now := time.Now().Unix()
	end := to.Unix()
	if now < end {
		end = now
	}
	var first int64
	a.Logs.QueryRow(`SELECT COALESCE(MIN(start),0) FROM outages`).Scan(&first)
	r.Since = first
	rows, err := a.Logs.Query(`SELECT start,end FROM outages WHERE start<? AND (end=0 OR end>?) ORDER BY start`, to.Unix(), from.Unix())
	if err != nil {
		return r, err
	}
	var raw [][2]int64
	for rows.Next() {
		var s, e int64
		if rows.Scan(&s, &e) == nil {
			raw = append(raw, [2]int64{s, e})
		}
	}
	rows.Close()
	for _, o := range raw {
		s, e := o[0], o[1]
		if e == 0 || e > end {
			e = end
		}
		if s < from.Unix() {
			s = from.Unix()
		}
		if e-s < 60 { // a blip shorter than a minute is not an outage worth reporting
			continue
		}
		oo := ispOutage{Start: s, End: o[1], Secs: e - s}
		r.Outages = append(r.Outages, oo)
		r.DownSecs += oo.Secs
	}
	span := end - from.Unix()
	if first > from.Unix() {
		span = end - first
	}
	if span > 0 {
		r.UpPct = 100 * (1 - float64(r.DownSecs)/float64(span))
	}
	cfg := a.speedConfig()
	trows, err := a.Logs.Query(`SELECT link,down_bps,up_bps,delay_ms,loaded_ms FROM speedtests WHERE ts>=? AND ts<? AND error='' AND down_bps>0`, from.Unix(), to.Unix())
	if err != nil {
		return r, err
	}
	by := map[string]*ispLink{}
	for trows.Next() {
		var link string
		var d, u, dl, ld float64
		if trows.Scan(&link, &d, &u, &dl, &ld) != nil {
			continue
		}
		l := by[link]
		if l == nil {
			p := cfg.Links[link]
			l = &ispLink{Link: link, PlanDown: p.DownMbps, PlanUp: p.UpMbps, WorstDown: d / 1e6}
			by[link] = l
		}
		l.Tests++
		l.AvgDownMbps += d / 1e6
		l.AvgUpMbps += u / 1e6
		l.AvgDelayMs += dl
		l.AvgLoadedMs += ld
		if d/1e6 < l.WorstDown {
			l.WorstDown = d / 1e6
		}
		if l.PlanDown > 0 && d/1e6 < 0.8*l.PlanDown {
			l.BelowPlan++
		}
	}
	trows.Close()
	for _, l := range by {
		n := float64(l.Tests)
		l.AvgDownMbps, l.AvgUpMbps, l.AvgDelayMs, l.AvgLoadedMs = l.AvgDownMbps/n, l.AvgUpMbps/n, l.AvgDelayMs/n, l.AvgLoadedMs/n
		r.SpeedTests += l.Tests
		r.Links = append(r.Links, *l)
	}
	sort.Slice(r.Links, func(i, j int) bool { return r.Links[i].Link < r.Links[j].Link })
	return r, nil
}

func dur(secs int64) string {
	d := time.Duration(secs) * time.Second
	if d < time.Hour {
		return fmt.Sprintf("%d min", int(d.Minutes()))
	}
	return fmt.Sprintf("%d h %d min", int(d.Hours()), int(d.Minutes())%60)
}

func renderISP(r ispReport, loc *time.Location) string {
	e := html.EscapeString
	var b strings.Builder
	b.WriteString(`<!doctype html><html lang=en><meta charset=utf-8><meta name=viewport content="width=device-width,initial-scale=1"><title>Internet report</title>
<style>body{font:15px/1.45 system-ui,sans-serif;margin:0;padding:2rem;max-width:50rem;margin-inline:auto;color:#111}h1{margin:0}h2{margin:1.5rem 0 .4rem;font-size:1.05rem;border-bottom:1px solid #999}table{border-collapse:collapse;width:100%}th,td{text-align:left;padding:.25rem .5rem;border-bottom:1px solid #ddd}th{color:#555}.note{color:#555;font-size:.85rem}.big{font-size:1.4rem;font-weight:600}@media print{.tip{display:none}body{padding:0}}</style>`)
	fmt.Fprintf(&b, "<h1>Internet report, %s</h1><p class=note>Measured by the router itself. It counts an outage only when the internet could not be reached for at least a minute.</p><p class=note tip>Print or save as PDF with Ctrl+P or Cmd+P.</p>", e(r.Month))
	fmt.Fprintf(&b, "<h2>Outages</h2><p><span class=big>%.2f%%</span> up, down for %s in %d outage(s).</p>", r.UpPct, e(dur(r.DownSecs)), len(r.Outages))
	if r.Since > r.From {
		fmt.Fprintf(&b, "<p class=note>Recording started %s, so earlier time is not counted.</p>", e(time.Unix(r.Since, 0).In(loc).Format("2 Jan 2006, 15:04")))
	}
	if len(r.Outages) > 0 {
		b.WriteString("<table><tr><th>From</th><th>Length</th></tr>")
		for _, o := range r.Outages {
			fmt.Fprintf(&b, "<tr><td>%s</td><td>%s</td></tr>", e(time.Unix(o.Start, 0).In(loc).Format("Mon 2 Jan, 15:04")), e(dur(o.Secs)))
		}
		b.WriteString("</table>")
	}
	b.WriteString("<h2>Speed</h2>")
	if len(r.Links) == 0 {
		b.WriteString("<p class=note>No speed tests were run this month. Turn on the daily test (Internet, Speed test) to build this part.</p>")
	}
	for _, l := range r.Links {
		fmt.Fprintf(&b, "<p><b>%s</b>, %d test(s)</p><table><tr><th></th><th>Average</th><th>Worst</th><th>Plan</th></tr><tr><td>Download</td><td>%.0f Mb/s</td><td>%.0f Mb/s</td><td>%s</td></tr><tr><td>Upload</td><td>%.0f Mb/s</td><td></td><td>%s</td></tr><tr><td>Delay (idle / under load)</td><td>%.0f / %.0f ms</td><td></td><td></td></tr></table>",
			e(l.Link), l.Tests, l.AvgDownMbps, l.WorstDown, planText(l.PlanDown), l.AvgUpMbps, planText(l.PlanUp), l.AvgDelayMs, l.AvgLoadedMs)
		if l.PlanDown > 0 {
			fmt.Fprintf(&b, "<p class=note>%d of %d tests were below 80%% of the plan's download speed.</p>", l.BelowPlan, l.Tests)
		}
	}
	b.WriteString("<p class=note>Packet loss is not measured. Speed tests go to one test server, so a slow result can also be the server's.</p></html>")
	return b.String()
}

func planText(v float64) string {
	if v <= 0 {
		return "not entered"
	}
	return fmt.Sprintf("%.0f Mb/s", v)
}

// ---- data use ----

type usageConfig struct {
	On     bool  `json:"on"`
	CapGB  int   `json:"cap_gb"`
	Start  int   `json:"start_day"` // day of the month a billing period begins, 1 to 28
	Levels []int `json:"levels"`    // percentages to warn at
}

func (a *App) usageConfig() usageConfig {
	c := usageConfig{Start: 1, Levels: []int{80, 100}}
	if raw := a.setting("usage.config", ""); raw != "" {
		json.Unmarshal([]byte(raw), &c)
	}
	if c.Start < 1 || c.Start > 28 {
		c.Start = 1
	}
	return c
}

// usagePeriod gives the billing period that contains now.
func usagePeriod(now time.Time, start int) (time.Time, time.Time) {
	y, m, _ := now.Date()
	from := time.Date(y, m, start, 0, 0, 0, 0, now.Location())
	if now.Before(from) {
		from = from.AddDate(0, -1, 0)
	}
	return from, from.AddDate(0, 1, 0)
}

type usageDev struct {
	MAC  string `json:"mac"`
	Name string `json:"name"`
	Up   int64  `json:"up"`
	Down int64  `json:"down"`
}

type usageView struct {
	usageConfig
	From int64      `json:"from"`
	To   int64      `json:"to"`
	Up   int64      `json:"up"`
	Down int64      `json:"down"`
	Pct  float64    `json:"pct"`
	Top  []usageDev `json:"top"`
}

func (a *App) usageNow() usageView {
	c := a.usageConfig()
	from, to := usagePeriod(a.localNow(), c.Start)
	v := usageView{usageConfig: c, From: from.Unix(), To: to.Unix(), Top: []usageDev{}}
	a.Logs.QueryRow(`SELECT COALESCE(SUM(up),0),COALESCE(SUM(down),0) FROM traffic WHERE mac='' AND ts>=? AND ts<?`, from.Unix(), to.Unix()).Scan(&v.Up, &v.Down)
	if c.On && c.CapGB > 0 {
		v.Pct = 100 * float64(v.Up+v.Down) / (float64(c.CapGB) * 1e9)
	}
	rows, err := a.Logs.Query(`SELECT mac,SUM(up),SUM(down) FROM traffic WHERE mac<>'' AND ts>=? AND ts<? GROUP BY mac ORDER BY SUM(up)+SUM(down) DESC LIMIT 8`, from.Unix(), to.Unix())
	if err == nil {
		for rows.Next() {
			var d usageDev
			if rows.Scan(&d.MAC, &d.Up, &d.Down) == nil {
				v.Top = append(v.Top, d)
			}
		}
		rows.Close()
	}
	names := a.deviceNames()
	for i := range v.Top {
		v.Top[i].Name = orWord(names[v.Top[i].MAC], v.Top[i].MAC)
	}
	return v
}

// usageTick sends each warning level once per billing period.
func (a *App) usageTick(now time.Time) {
	c := a.usageConfig()
	if !c.On || c.CapGB <= 0 || a.Logs == nil {
		return
	}
	v := a.usageNow()
	told := a.setting("usage.told", "")
	period := fmt.Sprint(v.From)
	best := 0
	for _, l := range c.Levels {
		if v.Pct >= float64(l) && l > best {
			best = l
		}
	}
	var prev int
	if p, lv, ok := strings.Cut(told, ":"); ok && p == period {
		fmt.Sscan(lv, &prev)
	}
	if best > prev {
		a.Config.SetSetting("usage.told", period+":"+fmt.Sprint(best))
		a.alert("usage", fmt.Sprintf("Data use is at %d%% of your limit", best),
			fmt.Sprintf("%.1f of %d GB used this billing period.", float64(v.Up+v.Down)/1e9, c.CapGB))
	}
}

func (a *App) registerISPRoutes() {
	s := a.API
	s.HandleAdmin("GET /api/isp/report", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		rep, err := a.ispReport(r.URL.Query().Get("month"))
		if err != nil {
			fail(w, err)
			return
		}
		api.JSON(w, 200, rep)
	})
	s.HandleAdmin("GET /api/isp/report/print", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		rep, err := a.ispReport(r.URL.Query().Get("month"))
		if err != nil {
			fail(w, err)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
		w.Header().Set("Cache-Control", "no-store")
		fmt.Fprint(w, renderISP(rep, a.localNow().Location()))
	})
	s.HandleAdmin("GET /api/usage", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		api.JSON(w, 200, a.usageNow())
	})
	s.HandleAdmin("PUT /api/usage", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in usageConfig
		if !api.Decode(w, r, &in) {
			return
		}
		if in.On && (in.CapGB < 1 || in.CapGB > 100000) {
			fail(w, fmt.Errorf("enter the monthly limit in gigabytes, for example 1000"))
			return
		}
		if in.Start < 1 || in.Start > 28 {
			fail(w, fmt.Errorf("the billing period starts on a day from 1 to 28"))
			return
		}
		var lv []int
		for _, l := range in.Levels {
			if l >= 10 && l <= 200 {
				lv = append(lv, l)
			}
		}
		if len(lv) == 0 {
			lv = []int{80, 100}
		}
		sort.Ints(lv)
		in.Levels = lv
		b, _ := json.Marshal(in)
		a.Config.SetSetting("usage.config", string(b))
		a.Config.SetSetting("usage.told", "")
		api.JSON(w, 200, a.usageNow())
	})
}

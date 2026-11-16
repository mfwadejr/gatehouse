package app

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Reports: pick a subject, a period, filters and a grouping, and get a summary, charts and rows.
// A report only covers what the box keeps, so every report says how far back its data goes.

type reportReq struct {
	Subject string            `json:"subject"`
	From    int64             `json:"from"`
	To      int64             `json:"to"`
	Group   string            `json:"group"` // hour, day, week, month, or an entity such as device, name, port
	Filters map[string]string `json:"filters"`
}

type reportStat struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type reportBar struct {
	Label string  `json:"label"`
	Value float64 `json:"value"`
}

type reportChart struct {
	ID     string                 `json:"id"`
	Title  string                 `json:"title"`
	Unit   string                 `json:"unit"`
	Series []reportSeries         `json:"series,omitempty"`
	Bars   []reportBar            `json:"bars,omitempty"`
	Extra  map[string]interface{} `json:"-"`
}

type reportSeries struct {
	Name string       `json:"name"`
	Pts  [][2]float64 `json:"pts"`
}

type reportResp struct {
	Subject   string        `json:"subject"`
	Title     string        `json:"title"`
	From      int64         `json:"from"`
	To        int64         `json:"to"`
	Group     string        `json:"group"`
	Made      int64         `json:"made"`
	Filters   []string      `json:"filters"` // the filters used, in words
	Summary   []reportStat  `json:"summary"`
	Charts    []reportChart `json:"charts"`
	Columns   []string      `json:"columns"`
	Rows      [][]string    `json:"rows"`
	Truncated bool          `json:"truncated"`
	Notes     []string      `json:"notes"`
	Available int64         `json:"available"` // the oldest data this subject has, 0 when there is none
	Keeps     string        `json:"keeps"`     // which Storage setting limits how far back it goes
}

const reportMaxRows = 5000

var timeGroups = map[string]bool{"hour": true, "day": true, "week": true, "month": true}

// bucketStart is the start of the local hour, day, week (Monday) or month that ts falls in.
func bucketStart(ts int64, group string) int64 {
	t := time.Unix(ts, 0).In(time.Local)
	switch group {
	case "hour":
		return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, t.Location()).Unix()
	case "week":
		d := (int(t.Weekday()) + 6) % 7
		return time.Date(t.Year(), t.Month(), t.Day()-d, 0, 0, 0, 0, t.Location()).Unix()
	case "month":
		return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location()).Unix()
	}
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location()).Unix()
}

func bucketLabel(ts int64, group string) string {
	t := time.Unix(ts, 0).In(time.Local)
	switch group {
	case "hour":
		return t.Format("2006-01-02 15:00")
	case "month":
		return t.Format("2006-01")
	case "week":
		return "Week of " + t.Format("2006-01-02")
	}
	return t.Format("2006-01-02")
}

func (r *reportReq) filter(k string) string { return strings.TrimSpace(r.Filters[k]) }

func fnum(v float64, dec int) string {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return "–"
	}
	return strconv.FormatFloat(v, 'f', dec, 64)
}

func fint(n int) string { return strconv.Itoa(n) }

func pctOf(a, b int) string {
	if b == 0 {
		return "0"
	}
	return fnum(100*float64(a)/float64(b), 1)
}

func humanDur(sec int64) string {
	switch {
	case sec < 90:
		return fmt.Sprintf("%d s", sec)
	case sec < 5400:
		return fmt.Sprintf("%.0f min", float64(sec)/60)
	case sec < 86400*2:
		return fmt.Sprintf("%.1f h", float64(sec)/3600)
	}
	return fmt.Sprintf("%.1f days", float64(sec)/86400)
}

// run makes one report. A bad request comes back as an error the console shows as it is.
func (a *App) runReport(req reportReq, now time.Time) (*reportResp, error) {
	if req.To == 0 || req.To > now.Unix() {
		req.To = now.Unix()
	}
	if req.From == 0 {
		req.From = req.To - 7*86400
	}
	if req.From >= req.To {
		return nil, fmt.Errorf("the start must be before the end")
	}
	if req.To-req.From > 11*365*86400 {
		return nil, fmt.Errorf("pick a period of 10 years or less")
	}
	if req.Filters == nil {
		req.Filters = map[string]string{}
	}
	out := &reportResp{Subject: req.Subject, From: req.From, To: req.To, Group: req.Group, Made: now.Unix(),
		Charts: []reportChart{}, Summary: []reportStat{}, Rows: [][]string{}, Notes: []string{}, Filters: []string{}}
	var err error
	switch req.Subject {
	case "links":
		err = a.reportLinks(req, out)
	case "dns":
		err = a.reportDNS(req, out)
	case "threats":
		err = a.reportThreats(req, out)
	case "firewall":
		err = a.reportFirewall(req, out)
	default:
		return nil, fmt.Errorf("pick what the report is about")
	}
	if err != nil {
		return nil, err
	}
	if out.Available > 0 && out.Available > req.From+86400 {
		out.Notes = append(out.Notes, fmt.Sprintf("NopeWarden has data for this from %s, so anything earlier in the period is empty. %s", time.Unix(out.Available, 0).Format("Jan 2, 2006"), out.Keeps))
	} else if out.Available == 0 {
		out.Notes = append(out.Notes, "NopeWarden has no data for this yet.")
	}
	if out.Truncated {
		out.Notes = append(out.Notes, fmt.Sprintf("Only the first %d rows are shown. Narrow the period or the filters, or group the results.", reportMaxRows))
	}
	return out, nil
}

// ---- Internet links ----

// linkTier chooses the finest saved step that still reaches back to the start of the period.
func (a *App) linkTier(from, to int64) (tier string, step int64) {
	span := to - from
	age := time.Now().Unix() - from
	switch {
	case age <= 2*3600:
		return "a", 30
	case age <= 25*3600 && span <= 25*3600:
		return "b", 300
	case age <= int64(a.hourlyDays())*86400:
		return "c", 3600
	}
	return "d", 86400
}

func (a *App) reportLinks(req reportReq, out *reportResp) error {
	out.Title = "Internet links"
	out.Keeps = "How far back it goes is set by Keep chart history for in Settings, Storage."
	group := req.Group
	if !timeGroups[group] {
		group = "day"
		if req.To-req.From <= 2*86400 {
			group = "hour"
		}
	}
	out.Group = group
	tier, step := a.linkTier(req.From, req.To)
	want := req.filter("link")
	minRTT, _ := strconv.ParseFloat(req.filter("min_delay"), 64)
	minLoss, _ := strconv.ParseFloat(req.filter("min_loss"), 64)
	minDrop, _ := strconv.ParseInt(req.filter("min_drop"), 10, 64)
	if want != "" {
		out.Filters = append(out.Filters, "Link: "+want)
	}
	if minRTT > 0 {
		out.Filters = append(out.Filters, "Average delay at least "+fnum(minRTT, 0)+" ms")
	}
	if minLoss > 0 {
		out.Filters = append(out.Filters, "Average loss at least "+fnum(minLoss, 1)+" %")
	}
	if minDrop > 0 {
		out.Filters = append(out.Filters, "Drops of at least "+humanDur(minDrop))
	}

	// What the box has: the oldest reading of any link, in any step.
	a.Logs.QueryRow(`SELECT COALESCE(MIN(ts),0) FROM metrics WHERE key LIKE '_|wan.%'`).Scan(&out.Available)

	type key struct {
		link string
		b    int64
	}
	type agg struct {
		n, nRTT, nLoss, nUp int
		rtt, loss, up       float64
		worst, worstLoss    float64
		down                float64 // seconds
	}
	groups := map[key]*agg{}
	links := map[string]bool{}
	q := `SELECT key, ts, value FROM metrics WHERE key LIKE ? AND ts>=? AND ts<?`
	rows, err := a.Logs.Query(q, tier+"|wan.%", req.From, req.To)
	if err != nil {
		return err
	}
	for rows.Next() {
		var k string
		var ts int64
		var v float64
		rows.Scan(&k, &ts, &v)
		k = k[2+len("wan."):]
		var link, field string
		for _, suf := range []string{".rtt", ".loss", ".up"} {
			if strings.HasSuffix(k, suf) {
				link, field = strings.TrimSuffix(k, suf), suf
			}
		}
		if link == "" || (want != "" && link != want) {
			continue
		}
		links[link] = true
		g := groups[key{link, bucketStart(ts, group)}]
		if g == nil {
			g = &agg{}
			groups[key{link, bucketStart(ts, group)}] = g
		}
		switch field {
		case ".rtt":
			g.rtt += v
			g.nRTT++
			g.worst = math.Max(g.worst, v)
		case ".loss":
			g.loss += v
			g.nLoss++
			g.worstLoss = math.Max(g.worstLoss, v)
		case ".up":
			g.up += v
			g.nUp++
			g.down += (1 - v) * float64(step)
		}
	}
	rows.Close()
	// The daily step also keeps each day's highest reading, which is the worst day's delay.
	if tier == "d" {
		if hr, err := a.Logs.Query(`SELECT key, ts, value FROM metrics WHERE key LIKE 'x|wan.%.rtt' AND ts>=? AND ts<?`, req.From, req.To); err == nil {
			for hr.Next() {
				var k string
				var ts int64
				var v float64
				hr.Scan(&k, &ts, &v)
				link := strings.TrimSuffix(k[2+len("wan."):], ".rtt")
				if g := groups[key{link, bucketStart(ts, group)}]; g != nil {
					g.worst = math.Max(g.worst, v)
				}
			}
			hr.Close()
		}
	}
	names := make([]string, 0, len(links))
	for l := range links {
		names = append(names, l)
	}
	sort.Strings(names)

	keys := make([]key, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].b != keys[j].b {
			return keys[i].b < keys[j].b
		}
		return keys[i].link < keys[j].link
	})
	out.Columns = []string{"Period", "Link", "Uptime %", "Down", "Average delay (ms)", "Worst delay (ms)", "Average loss %"}
	delay := map[string][][2]float64{}
	loss := map[string][][2]float64{}
	upt := map[string][][2]float64{}
	totUp, totUpN := map[string]float64{}, map[string]int{}
	totDown := map[string]float64{}
	totRTT, totRTTN, worst := map[string]float64{}, map[string]int{}, map[string]float64{}
	for _, k := range keys {
		g := groups[k]
		avgRTT, avgLoss, avgUp := math.NaN(), math.NaN(), math.NaN()
		if g.nRTT > 0 {
			avgRTT = g.rtt / float64(g.nRTT)
		}
		if g.nLoss > 0 {
			avgLoss = g.loss / float64(g.nLoss)
		}
		if g.nUp > 0 {
			avgUp = 100 * g.up / float64(g.nUp)
		}
		if minRTT > 0 && !(avgRTT >= minRTT) {
			continue
		}
		if minLoss > 0 && !(avgLoss >= minLoss) {
			continue
		}
		if len(out.Rows) >= reportMaxRows {
			out.Truncated = true
			break
		}
		out.Rows = append(out.Rows, []string{bucketLabel(k.b, group), k.link, fnum(avgUp, 2), humanDur(int64(g.down)), fnum(avgRTT, 1), fnum(g.worst, 1), fnum(avgLoss, 2)})
		x := float64(k.b)
		if !math.IsNaN(avgRTT) {
			delay[k.link] = append(delay[k.link], [2]float64{x, avgRTT})
			totRTT[k.link] += g.rtt
			totRTTN[k.link] += g.nRTT
		}
		if !math.IsNaN(avgLoss) {
			loss[k.link] = append(loss[k.link], [2]float64{x, avgLoss})
		}
		if !math.IsNaN(avgUp) {
			upt[k.link] = append(upt[k.link], [2]float64{x, avgUp})
			totUp[k.link] += g.up
			totUpN[k.link] += g.nUp
		}
		totDown[k.link] += g.down
		worst[k.link] = math.Max(worst[k.link], g.worst)
	}
	mk := func(m map[string][][2]float64) []reportSeries {
		var s []reportSeries
		for _, n := range names {
			if len(m[n]) > 0 {
				s = append(s, reportSeries{Name: n, Pts: m[n]})
			}
		}
		return s
	}
	out.Charts = append(out.Charts,
		reportChart{ID: "delay", Title: "Average delay", Unit: "ms", Series: mk(delay)},
		reportChart{ID: "uptime", Title: "Uptime", Unit: "%", Series: mk(upt)},
		reportChart{ID: "loss", Title: "Average loss", Unit: "%", Series: mk(loss)})

	// Summary: one line of numbers per link, then the outages and speed tests.
	for _, n := range names {
		if totUpN[n] > 0 {
			out.Summary = append(out.Summary, reportStat{n + ": uptime", fnum(100*totUp[n]/float64(totUpN[n]), 2) + " %"})
			out.Summary = append(out.Summary, reportStat{n + ": time down", humanDur(int64(totDown[n]))})
		}
		if totRTTN[n] > 0 {
			out.Summary = append(out.Summary, reportStat{n + ": delay", fnum(totRTT[n]/float64(totRTTN[n]), 1) + " ms average, " + fnum(worst[n], 1) + " ms worst"})
		}
	}
	// Outages are when every link was down at once; a filter on length keeps only the long ones.
	var oN int
	var oTot, oLong int64
	if or, err := a.Logs.Query(`SELECT start, end FROM outages WHERE start<? AND (end>? OR end=0)`, req.To, req.From); err == nil {
		for or.Next() {
			var s, e int64
			or.Scan(&s, &e)
			if e == 0 || e > req.To {
				e = req.To
			}
			if s < req.From {
				s = req.From
			}
			if d := e - s; d > 0 && d >= minDrop {
				oN++
				oTot += d
				if d > oLong {
					oLong = d
				}
			}
		}
		or.Close()
	}
	out.Summary = append(out.Summary, reportStat{"Outages (every link down)", fint(oN)})
	if oN > 0 {
		out.Summary = append(out.Summary, reportStat{"Time with no internet", humanDur(oTot)}, reportStat{"Longest outage", humanDur(oLong)})
	}
	var tN int
	var dn, up float64
	sq := `SELECT COUNT(*), COALESCE(AVG(down_bps),0), COALESCE(AVG(up_bps),0) FROM speedtests WHERE ts>=? AND ts<? AND error=''`
	sargs := []any{req.From, req.To}
	if want != "" {
		sq += ` AND link=?`
		sargs = append(sargs, want)
	}
	a.Logs.QueryRow(sq, sargs...).Scan(&tN, &dn, &up)
	if tN > 0 {
		out.Summary = append(out.Summary, reportStat{"Speed tests", fint(tN) + ": " + fnum(dn/1e6, 1) + " Mbps down, " + fnum(up/1e6, 1) + " Mbps up on average"})
	}
	out.Notes = append(out.Notes, "Uptime and delay come from the readings NopeWarden takes of each link every 30 seconds, kept in coarser steps as they age. Time when NopeWarden itself was off is not counted as down.")
	return nil
}

func timeAt(ts int64) string {
	if ts <= 0 {
		return ""
	}
	return time.Unix(ts, 0).In(time.Local).Format("2006-01-02 15:04")
}

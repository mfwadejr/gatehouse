package app

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// The subjects that come from the logs: DNS lookups, threat alerts and firewall connections. Each one
// groups by time (hour, day, week, month) or by what the rows are about (device, name, port and so on).

type entitySpec struct {
	title string // the column heading
	expr  string // SQL that gives the label
	where string // extra condition, or empty
}

type logReport struct {
	table, ts string
	where     []string
	args      []any
	// counts are the SQL sums shown per period: label, expression.
	counts   [][2]string
	entities map[string]entitySpec
	// entityCols are extra per-entity columns beyond the first count: label, expression.
	entityCols [][2]string
	label      func(entity, v string) string
}

func likeArg(s string) string {
	return "%" + strings.ReplaceAll(strings.ReplaceAll(s, "%", ""), "_", "") + "%"
}

func (a *App) runLogReport(req reportReq, out *reportResp, lr logReport) error {
	conds := append([]string{lr.ts + ">=?", lr.ts + "<?"}, lr.where...)
	args := append([]any{req.From, req.To}, lr.args...)
	where := strings.Join(conds, " AND ")
	a.Logs.QueryRow(`SELECT COALESCE(MIN(` + lr.ts + `),0) FROM ` + lr.table).Scan(&out.Available)

	if e, ok := lr.entities[req.Group]; ok {
		out.Group = req.Group
		w := where
		if e.where != "" {
			w += " AND " + e.where
		}
		sel := `SELECT ` + e.expr + ` AS lbl, COUNT(*)`
		for _, c := range lr.entityCols {
			sel += `, ` + c[1]
		}
		sel += `, MAX(` + lr.ts + `) FROM ` + lr.table + ` WHERE ` + w + ` GROUP BY lbl ORDER BY COUNT(*) DESC, lbl LIMIT ` + strconv.Itoa(reportMaxRows+1)
		rows, err := a.Logs.Query(sel, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		out.Columns = []string{e.title, lr.counts[0][0]}
		for _, c := range lr.entityCols {
			out.Columns = append(out.Columns, c[0])
		}
		out.Columns = append(out.Columns, "Last seen")
		var bars []reportBar
		for rows.Next() {
			var lbl string
			var n int
			extra := make([]float64, len(lr.entityCols))
			var last int64
			dest := []any{&lbl, &n}
			for i := range extra {
				dest = append(dest, &extra[i])
			}
			dest = append(dest, &last)
			if err := rows.Scan(dest...); err != nil {
				return err
			}
			if len(out.Rows) >= reportMaxRows {
				out.Truncated = true
				break
			}
			shown := lbl
			if lr.label != nil {
				shown = lr.label(req.Group, lbl)
			}
			row := []string{shown, fint(n)}
			for _, x := range extra {
				row = append(row, fnum(x, 0))
			}
			row = append(row, timeAt(last))
			out.Rows = append(out.Rows, row)
			if len(bars) < 10 {
				bars = append(bars, reportBar{Label: shown, Value: float64(n)})
			}
		}
		out.Charts = append(out.Charts, reportChart{ID: "top", Title: "Most, by " + strings.ToLower(e.title), Unit: "", Bars: bars})
		return nil
	}

	group := req.Group
	if !timeGroups[group] {
		group = "day"
		if req.To-req.From <= 2*86400 {
			group = "hour"
		}
	}
	out.Group = group
	sel := `SELECT ` + lr.ts + `/3600`
	for _, c := range lr.counts {
		sel += `, ` + c[1]
	}
	sel += ` FROM ` + lr.table + ` WHERE ` + where + ` GROUP BY ` + lr.ts + `/3600`
	rows, err := a.Logs.Query(sel, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	sums := map[int64][]float64{}
	for rows.Next() {
		var h int64
		v := make([]float64, len(lr.counts))
		dest := []any{&h}
		for i := range v {
			dest = append(dest, &v[i])
		}
		if err := rows.Scan(dest...); err != nil {
			return err
		}
		b := bucketStart(h*3600, group)
		if sums[b] == nil {
			sums[b] = make([]float64, len(lr.counts))
		}
		for i := range v {
			sums[b][i] += v[i]
		}
	}
	buckets := make([]int64, 0, len(sums))
	for b := range sums {
		buckets = append(buckets, b)
	}
	sort.Slice(buckets, func(i, j int) bool { return buckets[i] < buckets[j] })
	out.Columns = []string{"Period"}
	for _, c := range lr.counts {
		out.Columns = append(out.Columns, c[0])
	}
	series := make([]reportSeries, len(lr.counts))
	for i, c := range lr.counts {
		series[i].Name = c[0]
	}
	for _, b := range buckets {
		if len(out.Rows) >= reportMaxRows {
			out.Truncated = true
			break
		}
		row := []string{bucketLabel(b, group)}
		for i, v := range sums[b] {
			row = append(row, fnum(v, 0))
			series[i].Pts = append(series[i].Pts, [2]float64{float64(b), v})
		}
		out.Rows = append(out.Rows, row)
	}
	out.Charts = append(out.Charts, reportChart{ID: "time", Title: lr.counts[0][0] + " over time", Series: series[:1]})
	if len(series) > 1 {
		out.Charts = append(out.Charts, reportChart{ID: "time2", Title: lr.counts[1][0] + " over time", Series: series[1:2]})
	}
	return nil
}

func (a *App) wanPlaceholders() (string, []any) {
	var names []any
	for n := range a.wanZoneNames() {
		names = append(names, n)
	}
	return strings.TrimSuffix(strings.Repeat("?,", len(names)), ","), names
}

// ---- DNS ----

func (a *App) reportDNS(req reportReq, out *reportResp) error {
	out.Title = "DNS lookups"
	out.Keeps = "How far back it goes is set by Keep traffic and events for (days) in Settings, Storage."
	lr := logReport{table: "dns_queries", ts: "ts"}
	switch v := req.filter("result"); v {
	case "blocked":
		lr.where = append(lr.where, "result='blocked'")
		out.Filters = append(out.Filters, "Blocked lookups only")
	case "allowed":
		lr.where = append(lr.where, "result<>'blocked'")
		out.Filters = append(out.Filters, "Allowed lookups only")
	case "failed":
		lr.where = append(lr.where, "result='failed'")
		out.Filters = append(out.Filters, "Failed lookups only")
	}
	if v := req.filter("device"); v != "" {
		lr.where = append(lr.where, "(client LIKE ? OR client_name LIKE ?)")
		lr.args = append(lr.args, likeArg(v), likeArg(v))
		out.Filters = append(out.Filters, "Device: "+v)
	}
	if v := req.filter("name"); v != "" {
		lr.where = append(lr.where, "qname LIKE ?")
		lr.args = append(lr.args, likeArg(v))
		out.Filters = append(out.Filters, "Name contains: "+v)
	}
	if v := req.filter("list"); v != "" {
		lr.where = append(lr.where, "rule LIKE ?")
		lr.args = append(lr.args, likeArg(v))
		out.Filters = append(out.Filters, "Blocked by list or rule: "+v)
	}
	if v, _ := strconv.Atoi(req.filter("slow_ms")); v > 0 {
		lr.where = append(lr.where, "latency_us>=?")
		lr.args = append(lr.args, v*1000)
		out.Filters = append(out.Filters, fmt.Sprintf("Answers took at least %d ms", v))
	}
	lr.counts = [][2]string{
		{"Lookups", "COUNT(*)"},
		{"Blocked", "COALESCE(SUM(result='blocked'),0)"},
		{"Failed", "COALESCE(SUM(result='failed'),0)"},
	}
	lr.entities = map[string]entitySpec{
		"device": {"Device", "CASE WHEN client_name<>'' THEN client_name ELSE client END", ""},
		"name":   {"Name", "qname", ""},
		"result": {"How it was answered", "result", ""},
		"list":   {"Blocked by", "rule", "rule<>''"},
	}
	lr.entityCols = [][2]string{{"Blocked", "COALESCE(SUM(result='blocked'),0)"}, {"Average answer (ms)", "COALESCE(AVG(CASE WHEN latency_us>0 THEN latency_us/1000.0 END),0)"}}
	if err := a.runLogReport(req, out, lr); err != nil {
		return err
	}
	var total, blocked, failed, devices int
	var avg float64
	cond := []string{"ts>=?", "ts<?"}
	args := []any{req.From, req.To}
	cond = append(cond, lr.where...)
	args = append(args, lr.args...)
	a.Logs.QueryRow(`SELECT COUNT(*), COALESCE(SUM(result='blocked'),0), COALESCE(SUM(result='failed'),0), COUNT(DISTINCT client), COALESCE(AVG(CASE WHEN latency_us>0 THEN latency_us/1000.0 END),0) FROM dns_queries WHERE `+strings.Join(cond, " AND "), args...).Scan(&total, &blocked, &failed, &devices, &avg)
	out.Summary = []reportStat{{"Lookups", fint(total)}, {"Blocked", fint(blocked) + " (" + pctOf(blocked, total) + " %)"}, {"Failed", fint(failed)}, {"Devices asking", fint(devices)}, {"Average answer", fnum(avg, 1) + " ms"}}
	return nil
}

// ---- Threats ----

func (a *App) reportThreats(req reportReq, out *reportResp) error {
	out.Title = "Threat alerts"
	out.Keeps = "How far back it goes is set by Keep alerts and outages for (days) in Settings, Storage."
	lr := logReport{table: "threat_alerts", ts: "ts"}
	if v, _ := strconv.Atoi(req.filter("severity")); v >= 1 && v <= 3 {
		lr.where = append(lr.where, "severity<=?")
		lr.args = append(lr.args, v)
		out.Filters = append(out.Filters, map[int]string{1: "High severity only", 2: "Medium severity and higher", 3: "All severities"}[v])
	}
	if v := req.filter("category"); v != "" {
		lr.where = append(lr.where, "category LIKE ?")
		lr.args = append(lr.args, likeArg(v))
		out.Filters = append(out.Filters, "Kind: "+v)
	}
	if v := req.filter("device"); v != "" {
		lr.where = append(lr.where, "(device LIKE ? OR src LIKE ? OR dst LIKE ?)")
		lr.args = append(lr.args, likeArg(v), likeArg(v), likeArg(v))
		out.Filters = append(out.Filters, "Device: "+v)
	}
	if v := req.filter("rule"); v != "" {
		lr.where = append(lr.where, "(plain LIKE ? OR signature LIKE ?)")
		lr.args = append(lr.args, likeArg(v), likeArg(v))
		out.Filters = append(out.Filters, "Check contains: "+v)
	}
	lr.counts = [][2]string{{"Alerts", "COUNT(*)"}, {"High severity", "COALESCE(SUM(severity=1),0)"}}
	lr.entities = map[string]entitySpec{
		"category": {"Kind", "CASE WHEN category<>'' THEN category ELSE 'Other' END", ""},
		"device":   {"Device", "CASE WHEN device<>'' THEN device ELSE src END", ""},
		"rule":     {"What it looked for", "plain", ""},
		"severity": {"Severity", "severity", ""},
	}
	lr.entityCols = [][2]string{{"Devices", "COUNT(DISTINCT src)"}, {"High severity", "COALESCE(SUM(severity=1),0)"}}
	lr.label = func(entity, v string) string {
		if entity == "severity" {
			return map[string]string{"1": "High", "2": "Medium", "3": "Low"}[v]
		}
		return v
	}
	if err := a.runLogReport(req, out, lr); err != nil {
		return err
	}
	var total, high, devices int
	cond := append([]string{"ts>=?", "ts<?"}, lr.where...)
	args := append([]any{req.From, req.To}, lr.args...)
	a.Logs.QueryRow(`SELECT COUNT(*), COALESCE(SUM(severity=1),0), COUNT(DISTINCT CASE WHEN device<>'' THEN device ELSE src END) FROM threat_alerts WHERE `+strings.Join(cond, " AND "), args...).Scan(&total, &high, &devices)
	out.Summary = []reportStat{{"Alerts", fint(total)}, {"High severity", fint(high)}, {"Devices involved", fint(devices)}}
	var top string
	var topN int
	a.Logs.QueryRow(`SELECT CASE WHEN category<>'' THEN category ELSE 'Other' END c, COUNT(*) n FROM threat_alerts WHERE `+strings.Join(cond, " AND ")+` GROUP BY c ORDER BY n DESC LIMIT 1`, args...).Scan(&top, &topN)
	if topN > 0 {
		out.Summary = append(out.Summary, reportStat{"Most common kind", top + " (" + fint(topN) + ")"})
	}
	return nil
}

// ---- Firewall connections ----

func (a *App) reportFirewall(req reportReq, out *reportResp) error {
	out.Title = "Firewall connections"
	out.Keeps = "How far back it goes is set by Keep traffic and events for (days) in Settings, Storage."
	ph, wan := a.wanPlaceholders()
	lr := logReport{table: "fw_events", ts: "ts"}
	switch req.filter("action") {
	case "blocked":
		lr.where = append(lr.where, "action='drop'")
		out.Filters = append(out.Filters, "Blocked only")
	case "allowed":
		lr.where = append(lr.where, "action<>'drop'")
		out.Filters = append(out.Filters, "Allowed only")
	}
	switch req.filter("direction") {
	case "in":
		lr.where = append(lr.where, "from_zone IN ("+ph+")")
		lr.args = append(lr.args, wan...)
		out.Filters = append(out.Filters, "Coming in from the internet")
	case "out":
		lr.where = append(lr.where, "to_zone IN ("+ph+") AND from_zone NOT IN ("+ph+")")
		lr.args = append(lr.args, wan...)
		lr.args = append(lr.args, wan...)
		out.Filters = append(out.Filters, "Going out to the internet")
	}
	if v, _ := strconv.Atoi(req.filter("port")); v > 0 {
		lr.where = append(lr.where, "dport=?")
		lr.args = append(lr.args, v)
		out.Filters = append(out.Filters, "Port "+strconv.Itoa(v))
	}
	if v := req.filter("network"); v != "" {
		lr.where = append(lr.where, "(from_zone=? OR to_zone=?)")
		lr.args = append(lr.args, v, v)
		out.Filters = append(out.Filters, "Network: "+v)
	}
	if v := req.filter("address"); v != "" {
		lr.where = append(lr.where, "(from_addr LIKE ? OR to_addr LIKE ? OR src_name LIKE ? OR dst_name LIKE ?)")
		lr.args = append(lr.args, likeArg(v), likeArg(v), likeArg(v), likeArg(v))
		out.Filters = append(out.Filters, "Address or device: "+v)
	}
	if v := req.filter("rule"); v != "" {
		lr.where = append(lr.where, "rule LIKE ?")
		lr.args = append(lr.args, likeArg(v))
		out.Filters = append(out.Filters, "Rule contains: "+v)
	}
	lr.counts = [][2]string{{"Connections", "COUNT(*)"}, {"Blocked", "COALESCE(SUM(action='drop'),0)"}}
	lr.entities = map[string]entitySpec{
		"port":        {"Port", "proto||'/'||dport", ""},
		"source":      {"From", "CASE WHEN src_name<>'' THEN src_name ELSE from_addr END", ""},
		"destination": {"To", "CASE WHEN dst_name<>'' THEN dst_name ELSE to_addr END", ""},
		"network":     {"Network", "to_zone", "to_zone<>''"},
		"rule":        {"Rule", "rule", "rule<>''"},
		"action":      {"What happened", "action", ""},
	}
	lr.entityCols = [][2]string{{"Blocked", "COALESCE(SUM(action='drop'),0)"}, {"Different sources", "COUNT(DISTINCT from_addr)"}}
	lr.label = func(entity, v string) string {
		if entity == "port" {
			p, port, _ := strings.Cut(v, "/")
			n, _ := strconv.Atoi(port)
			return edgePortLabel(p, n)
		}
		if entity == "action" {
			if v == "drop" {
				return "Blocked"
			}
			return "Allowed"
		}
		return v
	}
	if err := a.runLogReport(req, out, lr); err != nil {
		return err
	}
	var total, blocked, sources int
	cond := append([]string{"ts>=?", "ts<?"}, lr.where...)
	args := append([]any{req.From, req.To}, lr.args...)
	a.Logs.QueryRow(`SELECT COUNT(*), COALESCE(SUM(action='drop'),0), COUNT(DISTINCT from_addr) FROM fw_events WHERE `+strings.Join(cond, " AND "), args...).Scan(&total, &blocked, &sources)
	out.Summary = []reportStat{{"Connections", fint(total)}, {"Blocked", fint(blocked) + " (" + pctOf(blocked, total) + " %)"}, {"Different sources", fint(sources)}}
	var port string
	var pn int
	a.Logs.QueryRow(`SELECT proto||'/'||dport p, COUNT(*) n FROM fw_events WHERE `+strings.Join(cond, " AND ")+` AND dport>0 GROUP BY p ORDER BY n DESC LIMIT 1`, args...).Scan(&port, &pn)
	if pn > 0 {
		p, pt, _ := strings.Cut(port, "/")
		n, _ := strconv.Atoi(pt)
		out.Summary = append(out.Summary, reportStat{"Busiest port", edgePortLabel(p, n) + " (" + fint(pn) + ")"})
	}
	return nil
}

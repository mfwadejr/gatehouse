package app

import (
	"encoding/csv"
	"fmt"
	"gatehouse/internal/appid"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"gatehouse/internal/api"
)

// Filters, summaries, export and retention for the connection log.

type fwFilter struct {
	Text      string
	Verdict   string
	Since     int64
	Until     int64 // newest time to include, 0 for now
	Before    int64 // paging: entries at or before this time
	Limit     int
	Device    string // an address or name
	Network   string // a network the connection came from or went to
	Port      int
	Service   string
	Proto     string
	Direction string // outbound, inbound, between or router
	Rule      string
	Dest      string // an address or name
	Country   string
	App       string // an application name from the catalogue
}

// serviceNames are the usual names of well-known ports.
var serviceNames = map[string]string{
	"tcp/20": "FTP", "tcp/21": "FTP", "tcp/22": "SSH", "tcp/23": "Telnet", "tcp/25": "Email (SMTP)", "tcp/53": "DNS", "udp/53": "DNS",
	"udp/67": "DHCP", "udp/68": "DHCP", "tcp/80": "Web (HTTP)", "udp/123": "Time (NTP)", "tcp/110": "Email (POP3)", "tcp/143": "Email (IMAP)",
	"tcp/443": "Web (HTTPS)", "udp/443": "Web (QUIC)", "tcp/445": "File sharing (SMB)", "tcp/465": "Email (SMTPS)", "tcp/587": "Email (submission)",
	"tcp/853": "DNS over TLS", "tcp/993": "Email (IMAPS)", "tcp/995": "Email (POP3S)", "tcp/1883": "MQTT", "tcp/8883": "MQTT (TLS)",
	"tcp/3389": "Remote Desktop", "udp/3478": "Video calls (STUN)", "udp/5353": "mDNS", "udp/1900": "UPnP", "tcp/8080": "Web (alternate)",
	"tcp/8443": "Web (alternate TLS)", "udp/51820": "WireGuard", "udp/1194": "OpenVPN", "tcp/5060": "Phone (SIP)", "udp/5060": "Phone (SIP)",
	"tcp/554": "Camera video (RTSP)", "tcp/631": "Printing", "tcp/9100": "Printing", "tcp/5228": "Google services",
}

func serviceName(proto string, port int) string {
	if port == 0 {
		if proto == "icmp" || proto == "icmpv6" {
			return "Ping"
		}
		return ""
	}
	return serviceNames[strings.ToLower(proto)+"/"+strconv.Itoa(port)]
}

// portsOfService lists the proto/port pairs that carry a service name.
func portsOfService(name string) [][2]string {
	var out [][2]string
	for k, v := range serviceNames {
		if strings.EqualFold(v, name) {
			p, port, _ := strings.Cut(k, "/")
			out = append(out, [2]string{p, port})
		}
	}
	return out
}

func (a *App) wanZoneNames() map[string]bool {
	out := map[string]bool{"Internet": true}
	if rows, err := a.Config.Query(`SELECT name FROM wan_links`); err == nil {
		for rows.Next() {
			var n string
			rows.Scan(&n)
			out[n] = true
		}
		rows.Close()
	}
	return out
}

func (f fwFilter) where(a *App) (string, []any) {
	where := []string{"1=1"}
	var args []any
	add := func(c string, v ...any) { where, args = append(where, c), append(args, v...) }
	if f.Since > 0 {
		add("ts>=?", f.Since)
	}
	if f.Until > 0 {
		add("ts<=?", f.Until)
	}
	if f.Before > 0 {
		add("ts<=?", f.Before)
	}
	switch f.Verdict {
	case "blocked":
		add("action='drop'")
	case "allowed":
		add("action='allow'")
	}
	if strings.TrimSpace(f.Text) != "" {
		l := like(f.Text)
		add(`(from_addr LIKE ? OR to_addr LIKE ? OR src_name LIKE ? OR dst_name LIKE ? OR rule LIKE ? OR reason LIKE ? OR proto LIKE ? OR CAST(dport AS TEXT) LIKE ? OR from_zone LIKE ? OR to_zone LIKE ?)`,
			l, l, l, l, l, l, l, l, l, l)
	}
	if f.Device != "" {
		l := like(f.Device)
		add("(from_addr LIKE ? OR src_name LIKE ?)", l, l)
	}
	if f.Dest != "" {
		l := like(f.Dest)
		add("(to_addr LIKE ? OR dst_name LIKE ?)", l, l)
	}
	if f.Network != "" {
		add("(from_zone=? OR to_zone=?)", f.Network, f.Network)
	}
	if f.Port > 0 {
		add("dport=?", f.Port)
	}
	if f.Proto != "" {
		add("proto=?", strings.ToLower(f.Proto))
	}
	if f.Service != "" {
		var ors []string
		var as []any
		for _, pp := range portsOfService(f.Service) {
			n, _ := strconv.Atoi(pp[1])
			ors, as = append(ors, "(proto=? AND dport=?)"), append(as, pp[0], n)
		}
		if strings.EqualFold(f.Service, "Ping") {
			ors = append(ors, "proto IN ('icmp','icmpv6')")
		}
		if len(ors) == 0 {
			add("0=1")
		} else {
			add("("+strings.Join(ors, " OR ")+")", as...)
		}
	}
	if f.Rule != "" {
		add("rule=?", f.Rule)
	}
	if f.App != "" {
		var ors []string
		var as []any
		for _, d := range appid.Domains(f.App) {
			ors, as = append(ors, "(dst_name=? OR dst_name LIKE ?)"), append(as, d, "%."+d)
		}
		if len(ors) == 0 {
			add("0=1")
		} else {
			add("("+strings.Join(ors, " OR ")+")", as...)
		}
	}
	if f.Direction != "" {
		wans := a.wanZoneNames()
		var in []string
		for n := range wans {
			in = append(in, n)
		}
		sort.Strings(in)
		ph := strings.TrimSuffix(strings.Repeat("?,", len(in)), ",")
		anys := make([]any, 0, len(in))
		for _, n := range in {
			anys = append(anys, n)
		}
		switch f.Direction {
		case "outbound":
			add("to_zone IN ("+ph+") AND from_zone NOT IN ("+ph+")", append(append([]any{}, anys...), anys...)...)
		case "inbound":
			add("from_zone IN ("+ph+")", anys...)
		case "router":
			add("to_zone='Router'")
		case "between":
			add("to_zone NOT IN ("+ph+") AND from_zone NOT IN ("+ph+") AND to_zone<>'Router'", append(append([]any{}, anys...), anys...)...)
		}
	}
	return strings.Join(where, " AND "), args
}

func (a *App) directionOf(from, to string, wans map[string]bool) string {
	switch {
	case wans[from]:
		return "inbound"
	case to == "Router":
		return "router"
	case wans[to]:
		return "outbound"
	}
	return "between"
}

func (a *App) searchFWf(f fwFilter) []LogItem {
	where, args := f.where(a)
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	fetch := limit
	if f.Country != "" {
		fetch = 5000 // country is worked out per row, so look at more rows and keep the matches
	}
	args = append(args, fetch)
	rows, err := a.Logs.Query(`SELECT id,ts,action,from_zone,from_addr,to_zone,to_addr,proto,dport,sport,src_name,dst_name,reason,rule FROM fw_events WHERE `+where+` ORDER BY ts DESC,id DESC LIMIT ?`, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	wans := a.wanZoneNames()
	on := a.appsOn()
	var out []LogItem
	for rows.Next() {
		var id, ts int64
		var action, fz, fa, tz, ta, proto, sn, dn, reason, rule string
		var dport, sport int
		if rows.Scan(&id, &ts, &action, &fz, &fa, &tz, &ta, &proto, &dport, &sport, &sn, &dn, &reason, &rule) != nil {
			continue
		}
		cc := a.country(ta)
		if cc == "" {
			cc = a.country(fa)
		}
		if f.Country != "" && !strings.EqualFold(cc, f.Country) {
			continue
		}
		it := LogItem{Key: "f" + strconv.FormatInt(id, 10), TS: ts, Kind: "firewall", Verdict: "allowed", Who: sn, Addr: fa, Why: reason,
			Src: fa, Dst: ta, DstName: dn, Port: dport, Proto: proto, Service: serviceName(proto, dport), Country: cc, FromZone: fz, ToZone: tz,
			Direction: a.directionOf(fz, tz, wans), Rule: rule}
		if it.Who == "" {
			it.Who = fa
		}
		if action == "drop" {
			it.Verdict = "blocked"
		}
		if on {
			if ap, found := appid.Lookup(dn); found {
				it.App, it.Category = ap.Name, ap.Category
			}
		}
		dest := ta
		if dn != "" {
			dest = dn + " (" + ta + ")"
		}
		it.What = strings.ToUpper(proto)
		if dport > 0 {
			dest += ":" + strconv.Itoa(dport)
		}
		it.What += " to " + dest
		if fz != "" || tz != "" {
			it.Detail = fz + " to " + tz
		}
		out = append(out, it)
		if len(out) >= limit {
			break
		}
	}
	return out
}

// logParams reads the filters from a request. A range given as from and to (unix seconds) wins over hours.
func logParams(r *http.Request) fwFilter {
	q := r.URL.Query()
	f := fwFilter{Text: strings.TrimSpace(q.Get("q")), Verdict: q.Get("verdict"), Device: q.Get("device"), Network: q.Get("network"),
		Service: q.Get("service"), Proto: q.Get("proto"), Direction: q.Get("direction"), Rule: q.Get("rule"), Dest: q.Get("dest"), Country: strings.ToUpper(q.Get("country")), App: q.Get("app")}
	f.Port, _ = strconv.Atoi(q.Get("port"))
	if h, err := strconv.Atoi(q.Get("hours")); err == nil && h > 0 {
		f.Since = time.Now().Add(-time.Duration(h) * time.Hour).Unix()
	}
	if v, err := strconv.ParseInt(q.Get("from"), 10, 64); err == nil && v > 0 {
		f.Since = v
	}
	if v, err := strconv.ParseInt(q.Get("to"), 10, 64); err == nil && v > 0 {
		f.Until = v
	}
	f.Before, _ = strconv.ParseInt(q.Get("before"), 10, 64)
	f.Limit, _ = strconv.Atoi(q.Get("limit"))
	return f
}

// firewallOnly says that a filter only makes sense for connections.
func (f fwFilter) firewallOnly() bool {
	return f.Network != "" || f.Port > 0 || f.Service != "" || f.Proto != "" || f.Direction != "" || f.Rule != "" || f.Dest != "" || f.Country != "" || f.App != ""
}

type topItem struct {
	Key     string `json:"key"`
	Label   string `json:"label"`
	N       int    `json:"n"`
	Country string `json:"country,omitempty"`
}

// logSummary counts what the filters match: totals, and the busiest destinations, devices,
// services and countries.
func (a *App) logSummary(f fwFilter) map[string]any {
	where, args := f.where(a)
	out := map[string]any{}
	var total, blocked int
	a.Logs.QueryRow(`SELECT COUNT(*), COALESCE(SUM(action='drop'),0) FROM fw_events WHERE `+where, args...).Scan(&total, &blocked)
	out["total"], out["blocked"], out["allowed"] = total, blocked, total-blocked

	top := func(sql string, label func(k, n1 string) string, limit int) []topItem {
		rows, err := a.Logs.Query(sql, args...)
		if err != nil {
			return []topItem{}
		}
		defer rows.Close()
		res := []topItem{}
		for rows.Next() {
			var k, n1 string
			var n int
			rows.Scan(&k, &n1, &n)
			if k == "" {
				continue
			}
			res = append(res, topItem{Key: k, Label: label(k, n1), N: n})
			if len(res) >= limit {
				break
			}
		}
		return res
	}
	dst := top(`SELECT to_addr, MAX(dst_name), COUNT(*) c FROM fw_events WHERE `+where+` AND to_zone<>'Router' GROUP BY to_addr ORDER BY c DESC LIMIT 200`,
		func(k, n string) string {
			if n != "" {
				return n
			}
			return k
		}, 200)
	// Several addresses can carry the same website; group by the name when there is one.
	merged := map[string]*topItem{}
	var order []string
	for _, d := range dst {
		key := d.Label
		if m, ok := merged[key]; ok {
			m.N += d.N
			continue
		}
		c := d
		c.Country = a.country(d.Key)
		merged[key] = &c
		order = append(order, key)
	}
	var dests []topItem
	for _, k := range order {
		dests = append(dests, *merged[k])
	}
	sort.SliceStable(dests, func(i, j int) bool { return dests[i].N > dests[j].N })
	if a.appsOn() {
		by := map[string]int{}
		for _, d := range dests {
			if ap, ok := appid.Lookup(d.Label); ok {
				by[ap.Name] += d.N
			}
		}
		apps := []topItem{}
		for k, n := range by {
			apps = append(apps, topItem{Key: k, Label: k, N: n})
		}
		sort.Slice(apps, func(i, j int) bool {
			return apps[i].N > apps[j].N || apps[i].N == apps[j].N && apps[i].Label < apps[j].Label
		})
		if len(apps) > 8 {
			apps = apps[:8]
		}
		out["apps"] = apps
	}
	if len(dests) > 8 {
		dests = dests[:8]
	}
	out["destinations"] = dests
	out["devices"] = top(`SELECT from_addr, MAX(src_name), COUNT(*) c FROM fw_events WHERE `+where+` AND from_zone NOT IN ('Internet') GROUP BY from_addr ORDER BY c DESC LIMIT 8`,
		func(k, n string) string {
			if n != "" {
				return n
			}
			return k
		}, 8)

	svc := map[string]int{}
	if rows, err := a.Logs.Query(`SELECT proto, dport, COUNT(*) FROM fw_events WHERE `+where+` GROUP BY proto, dport`, args...); err == nil {
		for rows.Next() {
			var proto string
			var port, n int
			rows.Scan(&proto, &port, &n)
			name := serviceName(proto, port)
			if name == "" {
				if port == 0 {
					continue
				}
				name = strings.ToUpper(proto) + " " + strconv.Itoa(port)
			}
			svc[name] += n
		}
		rows.Close()
	}
	services := []topItem{}
	for k, n := range svc {
		services = append(services, topItem{Key: k, Label: k, N: n})
	}
	sort.Slice(services, func(i, j int) bool {
		return services[i].N > services[j].N || services[i].N == services[j].N && services[i].Label < services[j].Label
	})
	if len(services) > 8 {
		services = services[:8]
	}
	out["services"] = services
	out["countries"] = a.countryCounts(f, where, args)
	return out
}

// countryCounts adds up connections by the country of the address on the far side.
func (a *App) countryCounts(f fwFilter, where string, args []any) []topItem {
	rows, err := a.Logs.Query(`SELECT to_addr, COUNT(*) c FROM fw_events WHERE `+where+` AND to_zone<>'Router' GROUP BY to_addr ORDER BY c DESC LIMIT 3000`, args...)
	if err != nil {
		return []topItem{}
	}
	defer rows.Close()
	by := map[string]int{}
	for rows.Next() {
		var ip string
		var n int
		rows.Scan(&ip, &n)
		if cc := a.country(ip); cc != "" {
			by[cc] += n
		}
	}
	res := []topItem{}
	for cc, n := range by {
		res = append(res, topItem{Key: cc, Label: cc, N: n, Country: cc})
	}
	sort.Slice(res, func(i, j int) bool { return res[i].N > res[j].N || res[i].N == res[j].N && res[i].Key < res[j].Key })
	return res
}

// csvSafe stops a spreadsheet from running a log value as a formula.
func csvSafe(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

// logUsage says how much room the logs take, counted as the data in use (deleted rows leave free
// pages that are reused).
func (a *App) logUsage() map[string]any {
	var pages, free, size int64
	a.Logs.QueryRow(`PRAGMA page_count`).Scan(&pages)
	a.Logs.QueryRow(`PRAGMA freelist_count`).Scan(&free)
	a.Logs.QueryRow(`PRAGMA page_size`).Scan(&size)
	var fwN, dnsN, evN int64
	a.Logs.QueryRow(`SELECT COUNT(*) FROM fw_events`).Scan(&fwN)
	a.Logs.QueryRow(`SELECT COUNT(*) FROM dns_queries`).Scan(&dnsN)
	a.Logs.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&evN)
	var oldest int64
	a.Logs.QueryRow(`SELECT COALESCE(MIN(ts),0) FROM fw_events`).Scan(&oldest)
	return map[string]any{"used_bytes": (pages - free) * size, "file_bytes": pages * size, "connections": fwN, "lookups": dnsN, "events": evN, "oldest": oldest,
		"last_prune": int64(a.settingFloat("logs.last_prune", 0)), "last_removed": int64(a.settingFloat("logs.last_removed", 0)),
		"days": int(a.settingFloat("logs.retention_days", 30)), "max_mb": int(a.settingFloat("logs.max_mb", 200))}
}

// enforceLogCap trims the oldest entries when the logs have grown past their size limit.
func (a *App) enforceLogCap() {
	capMB := int64(a.settingFloat("logs.max_mb", 200))
	if capMB <= 0 {
		return
	}
	for i := 0; i < 20; i++ {
		var pages, free, size int64
		a.Logs.QueryRow(`PRAGMA page_count`).Scan(&pages)
		a.Logs.QueryRow(`PRAGMA freelist_count`).Scan(&free)
		a.Logs.QueryRow(`PRAGMA page_size`).Scan(&size)
		if (pages-free)*size <= capMB<<20 {
			return
		}
		// Remove the oldest tenth of the connection and lookup history, then look again.
		for _, t := range []string{"fw_events", "dns_queries"} {
			a.Logs.Exec(`DELETE FROM ` + t + ` WHERE id IN (SELECT id FROM ` + t + ` ORDER BY id LIMIT MAX(1000,(SELECT COUNT(*)/10 FROM ` + t + `)))`)
		}
	}
}

func (a *App) registerLogExtraRoutes() {
	s := a.API
	s.Handle("GET /api/logs/summary", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		api.JSON(w, 200, a.logSummary(logParams(r)))
	})
	s.Handle("GET /api/logs/export", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		f := logParams(r)
		f.Limit = 50000
		items := a.searchFWf(f)
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="gatehouse-connections-`+time.Now().Format("2006-01-02")+`.csv"`)
		cw := csv.NewWriter(w)
		cw.Write([]string{"time", "result", "device", "device address", "from network", "to network", "destination", "destination address", "country", "protocol", "port", "service", "reason"})
		for _, it := range items {
			cw.Write([]string{time.Unix(it.TS, 0).Format("2006-01-02 15:04:05"), it.Verdict, csvSafe(it.Who), it.Src, csvSafe(it.FromZone), csvSafe(it.ToZone),
				csvSafe(it.DstName), it.Dst, it.Country, it.Proto, strconv.Itoa(it.Port), it.Service, csvSafe(it.Why)})
		}
		cw.Flush()
	})
	s.Handle("GET /api/logs/usage", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		api.JSON(w, 200, a.logUsage())
	})
	// Clean up now: apply the limits straight away and say how much was removed.
	s.HandleAdmin("POST /api/logs/prune", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		count := func() int64 {
			var n, m, e int64
			a.Logs.QueryRow(`SELECT COUNT(*) FROM fw_events`).Scan(&n)
			a.Logs.QueryRow(`SELECT COUNT(*) FROM dns_queries`).Scan(&m)
			a.Logs.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&e)
			return n + m + e
		}
		before := count()
		days := int(a.settingFloat("logs.retention_days", 30))
		a.QLog.Prune(days)
		a.pruneFWLog(days)
		a.pruneTraffic(days)
		a.enforceLogCap()
		removed := before - count()
		if removed < 0 {
			removed = 0
		}
		a.Config.SetSetting("logs.last_prune", strconv.FormatInt(time.Now().Unix(), 10))
		a.Config.SetSetting("logs.last_removed", strconv.FormatInt(removed, 10))
		api.JSON(w, 200, a.logUsage())
	})
	s.HandleAdmin("PUT /api/logs/retention", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			Days  int `json:"days"`
			MaxMB int `json:"max_mb"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		if in.Days < 1 || in.Days > 365 {
			fail(w, fmt.Errorf("keep the logs between 1 and 365 days"))
			return
		}
		if in.MaxMB < 10 || in.MaxMB > 20000 {
			fail(w, fmt.Errorf("the size limit must be between 10 MB and 20000 MB"))
			return
		}
		a.Config.SetSetting("logs.retention_days", strconv.Itoa(in.Days))
		a.Config.SetSetting("dns.log_retention_days", strconv.Itoa(in.Days))
		a.Config.SetSetting("logs.max_mb", strconv.Itoa(in.MaxMB))
		a.pruneFWLog(in.Days)
		a.pruneTraffic(in.Days)
		a.enforceLogCap()
		api.JSON(w, 200, a.logUsage())
	})
	// Where connections go, by country, for the map.
	s.Handle("GET /api/logs/countries", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		f := logParams(r)
		where, args := f.where(a)
		a.geoEnsure()
		_, have := a.geoAge()
		api.JSON(w, 200, map[string]any{"ready": have, "countries": a.countryCounts(f, where, args)})
	})
	s.Handle("GET /api/traffic/domains", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		api.JSON(w, 200, a.topDomains(r.URL.Query().Get("range")))
	})
}

var _ = os.Getpid

// baseDomain keeps the part of a name people recognise: www.cdn.example.com -> example.com.
func baseDomain(name string) string {
	name = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(name)), ".")
	p := strings.Split(name, ".")
	if len(p) <= 2 {
		return name
	}
	second := map[string]bool{"co": true, "com": true, "org": true, "net": true, "gov": true, "ac": true, "edu": true}
	n := 2
	if second[p[len(p)-2]] && len(p[len(p)-1]) == 2 {
		n = 3
	}
	if len(p) < n {
		n = len(p)
	}
	return strings.Join(p[len(p)-n:], ".")
}

type domainRow struct {
	Domain string `json:"domain"`
	Bytes  int64  `json:"bytes"`
}

// topDomains estimates which sites used the most data. Data is counted per device, not per site,
// so each device's use in a five-minute window is shared evenly between the sites it looked up
// around then. It is an estimate, and the console says so.
func (a *App) topDomains(rng string) map[string]any {
	out := a.topDomainsAll(rng)
	if len(out) > 25 {
		out = out[:25]
	}
	return map[string]any{"approximate": true, "domains": out}
}

// topDomainsAll is the expensive one (it walks the lookups of the whole range), so the answer is
// kept for a minute and shared by everything that asks, the sites list and the apps list included.
func (a *App) topDomainsAll(rng string) []domainRow {
	if _, ok := trafficRanges[rng]; !ok {
		rng = "1d"
	}
	return a.cached("domains:"+rng, time.Minute, func() any { return a.computeDomains(rng) }).([]domainRow)
}

// maxLookupsScanned keeps a month-long range from reading millions of rows: the newest ones win.
const maxLookupsScanned = 400000

func (a *App) computeDomains(rng string) []domainRow {
	r, ok := trafficRanges[rng]
	if !ok {
		r = trafficRanges["1d"]
	}
	const win = 300
	since := time.Now().Unix() - r.secs
	type key struct {
		b   int64
		mac string
	}
	use := map[key]int64{}
	rows, err := a.Logs.Query(`SELECT (ts/300)*300,mac,SUM(up+down) FROM traffic WHERE ts>=? AND mac<>'' GROUP BY 1,mac`, since)
	if err == nil {
		for rows.Next() {
			var k key
			var n int64
			rows.Scan(&k.b, &k.mac, &n)
			use[k] = n
		}
		rows.Close()
	}
	seen := map[key]map[string]bool{}
	q, err := a.Logs.Query(`SELECT ts,client,qname FROM (SELECT ts,client,qname FROM dns_queries WHERE ts>=? AND result<>'blocked' ORDER BY ts DESC LIMIT ?) ORDER BY ts`, since-win, maxLookupsScanned)
	if err == nil {
		arp := arpTable()
		macs := map[string]string{}
		for q.Next() {
			var ts int64
			var ip, name string
			q.Scan(&ts, &ip, &name)
			mac, ok := macs[ip]
			if !ok {
				mac = a.macForIP(ip, arp)
				macs[ip] = mac
			}
			if mac == "" {
				continue
			}
			d := baseDomain(name)
			if d == "" || !strings.Contains(d, ".") || strings.HasSuffix(d, ".arpa") {
				continue
			}
			// a lookup counts for its own window and the one before it, since a page loads after the lookup
			for _, b := range []int64{ts / win * win, ts/win*win - win} {
				k := key{b, mac}
				if seen[k] == nil {
					seen[k] = map[string]bool{}
				}
				seen[k][d] = true
			}
		}
		q.Close()
	}
	tot := map[string]float64{}
	for k, n := range use {
		ds := seen[k]
		if len(ds) == 0 {
			continue
		}
		share := float64(n) / float64(len(ds))
		for d := range ds {
			tot[d] += share
		}
	}
	out := []domainRow{}
	for d, n := range tot {
		out = append(out, domainRow{d, int64(n)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Bytes > out[j].Bytes })
	return out
}

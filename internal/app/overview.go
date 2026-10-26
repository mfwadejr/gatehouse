package app

import (
	"context"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"gatehouse/internal/api"
)

// Everything the Overview page needs that is not already served by another page: a short history
// of the numbers it draws, how the machine itself is doing, the world map, and the busiest devices.

// ---- machine health ----

type sysHealth struct {
	CPU      float64 `json:"cpu"`  // percent busy, over the last few seconds
	Mem      float64 `json:"mem"`  // percent of memory in use
	Disk     float64 `json:"disk"` // percent of the system disk in use
	Temp     float64 `json:"temp"` // degrees Celsius; 0 when the machine has no sensor
	Uptime   int64   `json:"uptime"`
	MemTotal int64   `json:"mem_total"`
	DiskFree int64   `json:"disk_free"`
}

type healthSampler struct {
	mu          sync.Mutex
	idle, total uint64
	at          time.Time
	last        sysHealth
}

func readCPU() (idle, total uint64, ok bool) {
	b, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0, 0, false
	}
	line := strings.SplitN(string(b), "\n", 2)[0]
	f := strings.Fields(line)
	if len(f) < 5 || f[0] != "cpu" {
		return 0, 0, false
	}
	for i, s := range f[1:] {
		n, _ := strconv.ParseUint(s, 10, 64)
		if i >= 8 { // guest time is already counted in user time
			break
		}
		total += n
		if i == 3 || i == 4 { // idle and iowait
			idle += n
		}
	}
	return idle, total, true
}

func readMem() (pct float64, total int64) {
	b, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	var tot, avail int64
	for _, l := range strings.Split(string(b), "\n") {
		f := strings.Fields(l)
		if len(f) < 2 {
			continue
		}
		n, _ := strconv.ParseInt(f[1], 10, 64)
		switch f[0] {
		case "MemTotal:":
			tot = n
		case "MemAvailable:":
			avail = n
		}
	}
	if tot <= 0 {
		return 0, 0
	}
	return float64(tot-avail) * 100 / float64(tot), tot * 1024
}

func readTemp() float64 {
	best := 0.0
	zones, _ := os.ReadDir("/sys/class/thermal")
	for _, z := range zones {
		if !strings.HasPrefix(z.Name(), "thermal_zone") {
			continue
		}
		b, err := os.ReadFile("/sys/class/thermal/" + z.Name() + "/temp")
		if err != nil {
			continue
		}
		n, err := strconv.ParseFloat(strings.TrimSpace(string(b)), 64)
		if err == nil && n/1000 > best && n/1000 < 150 {
			best = n / 1000
		}
	}
	return best
}

func readUptime() int64 {
	b, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0
	}
	f := strings.Fields(string(b))
	if len(f) == 0 {
		return 0
	}
	v, _ := strconv.ParseFloat(f[0], 64)
	return int64(v)
}

// Health reads the machine's own numbers. The processor figure is the change since the last
// reading, so readings less than a second apart return the previous one.
func (h *healthSampler) Health(path string) sysHealth {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := time.Now()
	if !h.at.IsZero() && now.Sub(h.at) < time.Second {
		return h.last
	}
	out := sysHealth{Uptime: readUptime(), Temp: readTemp()}
	out.Mem, out.MemTotal = readMem()
	if idle, total, ok := readCPU(); ok {
		if h.total > 0 && total > h.total {
			out.CPU = 100 * (1 - float64(idle-h.idle)/float64(total-h.total))
			if out.CPU < 0 {
				out.CPU = 0
			}
		} else {
			out.CPU = h.last.CPU
		}
		h.idle, h.total = idle, total
	}
	var st syscall.Statfs_t
	if syscall.Statfs(path, &st) == nil && st.Blocks > 0 {
		out.Disk = float64(st.Blocks-st.Bavail) * 100 / float64(st.Blocks)
		out.DiskFree = int64(st.Bavail) * int64(st.Bsize)
	}
	h.at, h.last = now, out
	return out
}

// ---- cache ----

type ocacheEntry struct {
	mu sync.Mutex // held while this one answer is being built, so a slow one never blocks the others
	at time.Time
	v  any
}

// cached keeps an expensive answer for a while, so a page open on several screens costs one query.
// Two callers asking for the same thing at once share one build.
func (a *App) cached(key string, ttl time.Duration, build func() any) any {
	a.ocache.mu.Lock()
	if a.ocache.m == nil || len(a.ocache.m) > 128 {
		a.ocache.m = map[string]*ocacheEntry{}
	}
	e := a.ocache.m[key]
	if e == nil {
		e = &ocacheEntry{}
		a.ocache.m[key] = e
	}
	a.ocache.mu.Unlock()
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.at.IsZero() && time.Since(e.at) < ttl {
		return e.v
	}
	e.v, e.at = build(), time.Now()
	return e.v
}

type ocache struct {
	mu sync.Mutex
	m  map[string]*ocacheEntry
}

// ---- history ----

// The history keeps three tiers in the metrics table. The key starts with the tier letter:
// a| every 30 seconds for 2 hours, b| every 5 minutes for 25 hours, c| every hour for 31 days.
const (
	histEvery = 30 * time.Second
	tierB     = 300
	tierC     = 3600
)

type histPoint struct {
	Key string
	Val float64
}

type zoneCounters struct {
	rx, tx uint64
	ok     bool
}

type histState struct {
	mu       sync.Mutex
	lastAt   time.Time
	lastFW   int64
	zones    map[string]zoneCounters
	lastRoll [2]int64
}

func avgTail(s []float64, n int) float64 {
	if len(s) == 0 {
		return 0
	}
	if n > len(s) {
		n = len(s)
	}
	var sum float64
	for _, v := range s[len(s)-n:] {
		sum += v
	}
	return sum / float64(n)
}

// histPoints gathers one reading of everything the history keeps.
func (a *App) histPoints(now time.Time) []histPoint {
	var pts []histPoint
	add := func(k string, v float64) { pts = append(pts, histPoint{k, v}) }
	n := int(histEvery / trafficEvery)
	for name, s := range a.traffic.snapshot() {
		add("wan."+name+".rx", avgTail(s.Rx, n))
		add("wan."+name+".tx", avgTail(s.Tx, n))
	}
	if a.Net != nil {
		if st, err := a.Net.WanStatus(); err == nil {
			for name, s := range st {
				if strings.HasPrefix(name, "group:") || !s.Checked {
					continue
				}
				if s.Up {
					add("wan."+name+".rtt", float64(s.RTT)/float64(time.Millisecond))
				}
				add("wan."+name+".loss", float64(s.Loss))
				up := 0.0
				if s.Up {
					up = 1
				}
				add("wan."+name+".up", up)
			}
		}
	}
	if rows, err := a.deviceRows(); err == nil {
		on := 0
		for _, d := range rows {
			if d.Online {
				on++
			}
		}
		add("devices.online", float64(on))
	}
	add("dns.qps", a.DNS.Stats().Snapshot().QPS)
	// Blocked connections per second since the last reading.
	var fwN int64
	a.Logs.QueryRow(`SELECT COUNT(*) FROM fw_events WHERE action='drop' AND ts>=?`, now.Add(-histEvery).Unix()).Scan(&fwN)
	add("fw.blocked", float64(fwN)/histEvery.Seconds())
	h := a.health.Health("/")
	add("sys.cpu", h.CPU)
	add("sys.mem", h.Mem)
	add("sys.disk", h.Disk)
	if h.Temp > 0 {
		add("sys.temp", h.Temp)
	}
	// Each network's own traffic, from its ports' counters.
	a.hist.mu.Lock()
	if a.hist.zones == nil {
		a.hist.zones = map[string]zoneCounters{}
	}
	dt := now.Sub(a.hist.lastAt).Seconds()
	cur := map[string]zoneCounters{}
	for iface, zone := range a.zoneByIface() {
		rx, ok1 := readCounter(iface, "rx_bytes")
		tx, ok2 := readCounter(iface, "tx_bytes")
		if !ok1 || !ok2 {
			continue
		}
		z := cur[zone]
		z.rx, z.tx, z.ok = z.rx+rx, z.tx+tx, true
		cur[zone] = z
	}
	for zone, c := range cur {
		if p, ok := a.hist.zones[zone]; ok && p.ok && dt > 0 && c.rx >= p.rx && c.tx >= p.tx {
			// A port's received bytes are what the devices on it sent, so the names swap here:
			// "down" is what the devices received.
			add("net."+zone+".down", float64(c.tx-p.tx)*8/dt)
			add("net."+zone+".up", float64(c.rx-p.rx)*8/dt)
		}
	}
	a.hist.zones = cur
	a.hist.lastAt = now
	a.hist.mu.Unlock()
	return pts
}

// recordHistory writes one reading and rolls finished five-minute and one-hour windows up into the
// slower tiers. Old points are removed.
func (a *App) recordHistory(now time.Time) {
	ts := now.Unix()
	pts := a.histPoints(now)
	tx, err := a.Logs.Begin()
	if err != nil {
		return
	}
	for _, p := range pts {
		tx.Exec(`INSERT OR REPLACE INTO metrics(ts,key,value) VALUES(?,?,?)`, ts, "a|"+p.Key, p.Val)
	}
	tx.Commit()
	roll := func(from, to string, size int64, idx int) {
		cur := ts / size
		a.hist.mu.Lock()
		prev := a.hist.lastRoll[idx]
		a.hist.lastRoll[idx] = cur
		a.hist.mu.Unlock()
		if prev == 0 || cur == prev {
			return
		}
		start, end := (cur-1)*size, cur*size
		a.Logs.Exec(`INSERT OR REPLACE INTO metrics(ts,key,value)
			SELECT ?, ?||substr(key,3), AVG(value) FROM metrics WHERE key LIKE ? AND ts>=? AND ts<? GROUP BY key`,
			start, to, from+"|%", start, end)
	}
	roll("a", "b|", tierB, 0)
	roll("b", "c|", tierC, 1)
	a.Logs.Exec(`DELETE FROM metrics WHERE key LIKE 'a|%' AND ts<?`, ts-2*3600)
	a.Logs.Exec(`DELETE FROM metrics WHERE key LIKE 'b|%' AND ts<?`, ts-25*3600)
	a.Logs.Exec(`DELETE FROM metrics WHERE key LIKE 'c|%' AND ts<?`, ts-31*24*3600)
}

func (a *App) collectHistory(ctx context.Context) {
	t := time.NewTicker(histEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			a.recordHistory(now)
		}
	}
}

var histRanges = map[string]struct {
	tier string
	secs int64
	step int64
}{
	"1h":  {"a", 3600, int64(histEvery / time.Second)},
	"24h": {"b", 24 * 3600, tierB},
	"7d":  {"c", 7 * 24 * 3600, tierC},
	"30d": {"c", 30 * 24 * 3600, tierC},
}

// history returns the saved points of a range: series name -> [[time, value], ...], oldest first.
func (a *App) history(rng string) map[string]any {
	r, ok := histRanges[rng]
	if !ok {
		rng = "1h"
		r = histRanges[rng]
	}
	to := time.Now().Unix()
	from := to - r.secs
	series := map[string][][2]float64{}
	rows, err := a.Logs.Query(`SELECT key,ts,value FROM metrics WHERE key LIKE ? AND ts>=? ORDER BY ts`, r.tier+"|%", from)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var k string
			var ts int64
			var v float64
			rows.Scan(&k, &ts, &v)
			k = k[2:]
			series[k] = append(series[k], [2]float64{float64(ts), v})
		}
	}
	return map[string]any{"range": rng, "step": r.step, "from": from, "to": to, "series": series}
}

// ---- world map ----

type mapCountry struct {
	Country string `json:"country"`
	N       int    `json:"n"`
	Blocked int    `json:"blocked,omitempty"`
	Allowed int    `json:"allowed,omitempty"`
	Up      int64  `json:"up,omitempty"`   // bytes sent to the country
	Down    int64  `json:"down,omitempty"` // bytes received from it
}

// worldMap adds up connections by the country on the far side. Going out is the connections the
// devices made; coming in is what the internet tried to reach, split into blocked and allowed.
func (a *App) worldMap(dir string, hours int) map[string]any {
	wans := a.wanZoneNames()
	var names []any
	for n := range wans {
		names = append(names, n)
	}
	ph := strings.TrimSuffix(strings.Repeat("?,", len(names)), ",")
	since := time.Now().Add(-time.Duration(hours) * time.Hour).Unix()
	var q string
	args := []any{since}
	incoming := dir == "in"
	if incoming {
		q = `SELECT from_addr, action, COUNT(*) FROM fw_events WHERE ts>=? AND from_zone IN (` + ph + `) GROUP BY from_addr, action ORDER BY 3 DESC LIMIT 4000`
		args = append(args, names...)
	} else {
		q = `SELECT to_addr, action, COUNT(*) FROM fw_events WHERE ts>=? AND to_zone IN (` + ph + `) AND from_zone NOT IN (` + ph + `) GROUP BY to_addr, action ORDER BY 3 DESC LIMIT 4000`
		args = append(args, names...)
		args = append(args, names...)
	}
	by := map[string]*mapCountry{}
	rows, err := a.Logs.Query(q, args...)
	if err == nil {
		for rows.Next() {
			var ip, action string
			var n int
			rows.Scan(&ip, &action, &n)
			cc := a.country(ip)
			if cc == "" {
				continue
			}
			m := by[cc]
			if m == nil {
				m = &mapCountry{Country: cc}
				by[cc] = m
			}
			m.N += n
			if action == "drop" {
				m.Blocked += n
			} else {
				m.Allowed += n
			}
		}
		rows.Close()
	}
	// Data volume per country, counted by the firewall.
	if rows, err := a.Logs.Query(`SELECT cc, SUM(up), SUM(down) FROM country_traffic WHERE ts>=? GROUP BY cc`, since); err == nil {
		for rows.Next() {
			var cc string
			var up, down int64
			rows.Scan(&cc, &up, &down)
			m := by[cc]
			if m == nil && incoming {
				continue
			}
			if m == nil {
				m = &mapCountry{Country: cc}
				by[cc] = m
			}
			m.Up, m.Down = up, down
		}
		rows.Close()
	}
	list := make([]mapCountry, 0, len(by))
	top := 0
	for _, m := range by {
		if !incoming {
			m.Blocked, m.Allowed = 0, 0
		}
		list = append(list, *m)
		if m.N > top {
			top = m.N
		}
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].N > list[j].N || list[i].N == list[j].N && list[i].Country < list[j].Country
	})
	return map[string]any{"dir": dir, "hours": hours, "top": top, "countries": list}
}

// ---- devices ----

func (a *App) deviceCounts() map[string]any {
	rows, err := a.deviceRows()
	if err != nil {
		return map[string]any{"total": 0}
	}
	var online, wired, wifi, vpn, unnamed, fresh, away int
	byNet := map[string]int{}
	day := time.Now().Add(-24 * time.Hour).Unix()
	first := map[string]int64{}
	if r, err := a.Config.Query(`SELECT lower(mac),first_seen FROM devices`); err == nil {
		for r.Next() {
			var m string
			var f int64
			r.Scan(&m, &f)
			first[m] = f
		}
		r.Close()
	}
	for _, d := range rows {
		if d.Source == "device" && d.Name == "" {
			unnamed++
		}
		if d.Source == "device" && first[strings.ToLower(d.MAC)] >= day {
			fresh++
		}
		if !d.Online {
			away++
			continue
		}
		online++
		if d.Network != "" {
			byNet[d.Network]++
		}
		switch d.Conn {
		case "wifi":
			wifi++
		case "vpn":
			vpn++
		default:
			wired++
		}
	}
	return map[string]any{"online": online, "wired": wired, "wifi": wifi, "vpn": vpn, "offline": away, "unnamed": unnamed, "new": fresh, "total": len(rows), "by_network": byNet}
}

func (a *App) registerOverviewRoutes() {
	s := a.API
	s.Handle("GET /api/overview/history", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		rng := r.URL.Query().Get("range")
		api.JSON(w, 200, a.cached("hist:"+rng, 5*time.Second, func() any { return a.history(rng) }))
	})
	s.Handle("GET /api/system/health", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		api.JSON(w, 200, a.health.Health("/"))
	})
	s.Handle("GET /api/overview/devices", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		api.JSON(w, 200, a.cached("devs", 3*time.Second, func() any { return a.deviceCounts() }))
	})
	s.Handle("GET /api/overview/busiest", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		api.JSON(w, 200, a.cached("busy", 30*time.Second, func() any {
			rows, _ := a.usageByDevice("1d")
			if rows == nil {
				rows = []usageRow{}
			}
			if len(rows) > 5 {
				rows = rows[:5]
			}
			return rows
		}))
	})
	s.Handle("GET /api/overview/map", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		dir := "out"
		if r.URL.Query().Get("dir") == "in" {
			dir = "in"
		}
		hours := map[string]int{"1h": 1, "24h": 24, "7d": 168, "30d": 720}[r.URL.Query().Get("range")]
		if hours == 0 {
			hours = 24
		}
		a.geoEnsure()
		key := "map:" + dir + ":" + strconv.Itoa(hours)
		api.JSON(w, 200, a.cached(key, 60*time.Second, func() any { return a.worldMap(dir, hours) }))
	})
	s.Handle("GET /api/overview/security", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		api.JSON(w, 200, a.cached("sec", 10*time.Second, func() any {
			var blocked, forwards int
			a.Logs.QueryRow(`SELECT COUNT(*) FROM fw_events WHERE action='drop' AND ts>=?`, time.Now().Add(-24*time.Hour).Unix()).Scan(&blocked)
			a.Config.QueryRow(`SELECT COUNT(*) FROM nat_forwards WHERE enabled=1`).Scan(&forwards)
			return map[string]any{"blocked_24h": blocked, "open_ports": forwards}
		}))
	})
	s.Handle("GET /api/overview/events", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		type ev struct {
			TS       int64  `json:"ts"`
			Area     string `json:"area"`
			Severity string `json:"severity"`
			Title    string `json:"title"`
		}
		out := []ev{}
		if rows, err := a.Logs.Query(`SELECT ts,area,severity,title FROM events ORDER BY ts DESC,id DESC LIMIT 8`); err == nil {
			for rows.Next() {
				var e ev
				rows.Scan(&e.TS, &e.Area, &e.Severity, &e.Title)
				out = append(out, e)
			}
			rows.Close()
		}
		api.JSON(w, 200, out)
	})
}

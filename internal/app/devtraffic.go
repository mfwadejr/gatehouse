package app

import (
	"bufio"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gatehouse/internal/api"
	"gatehouse/internal/fw"
)

// How much each device sends and receives. The firewall counts it (see fw.AcctUp); this reads the
// counters every so often, turns them into speeds, and keeps the totals in five-minute buckets.

type devRate struct{ Up, Down float64 } // bytes per second

type devTraffic struct {
	mu     sync.Mutex
	last   map[string]uint64 // "up:ip" or "down:ip" -> last counter
	lastAt time.Time
	rate   map[string]devRate // by address
	wan    [2]uint64          // internet connection bytes received and sent, last seen
	wanOK  bool
}

const (
	deviceBucket = 300 // seconds
	wanBucket    = 60
)

func (a *App) devRates() map[string]devRate {
	a.devt.mu.Lock()
	defer a.devt.mu.Unlock()
	out := make(map[string]devRate, len(a.devt.rate))
	for k, v := range a.devt.rate {
		out[k] = v
	}
	return out
}

// arpEntry is a neighbour the kernel has seen on a port.
type arpEntry struct{ MAC, Dev string }

// arpTable reads the kernel's list of neighbours: address, hardware address and the port it was
// seen on. It also finds devices that were given a fixed address and never used DHCP.
func arpTable() map[string]arpEntry {
	out := map[string]arpEntry{}
	f, err := os.Open("/proc/net/arp")
	if err != nil {
		return out
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Scan() // header
	for sc.Scan() {
		p := strings.Fields(sc.Text())
		if len(p) < 6 || p[2] == "0x0" || p[3] == "00:00:00:00:00:00" {
			continue
		}
		out[p[0]] = arpEntry{strings.ToLower(p[3]), p[5]}
	}
	return out
}

func (a *App) macForIP(ip string, arp map[string]arpEntry) string {
	if m := a.DHCP.Lookups().MACFor(ip); m != "" {
		return strings.ToLower(m)
	}
	return arp[ip].MAC
}

// sampleDevices takes one reading of the per-device counters and of the internet connection.
func (a *App) sampleDevices(cur map[string]fw.Counter, now time.Time) {
	t := &a.devt
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.last == nil {
		t.last = map[string]uint64{}
	}
	first := t.lastAt.IsZero()
	dt := now.Sub(t.lastAt).Seconds()
	arp := arpTable()
	rates := map[string]devRate{}
	deltas := map[string][2]int64{} // by MAC: up, down
	seen := map[string]bool{}
	for key, c := range cur {
		if !strings.HasPrefix(key, fw.AcctPrefix) {
			continue
		}
		rest := key[len(fw.AcctPrefix):]
		dir, ip, ok := strings.Cut(rest, ":")
		if !ok {
			continue
		}
		seen[rest] = true
		prev, had := t.last[rest]
		d := c.Bytes - prev
		if !had || c.Bytes < prev { // new address, or the ruleset was loaded again and counting restarted
			d = c.Bytes
			if !had && first {
				d = 0 // no baseline yet: do not count what happened before the first look
			}
		}
		t.last[rest] = c.Bytes
		if d == 0 {
			continue
		}
		r := rates[ip]
		x := deltas[a.macForIP(ip, arp)]
		if dir == "up" {
			r.Up += float64(d) / maxF(dt, 1)
			x[0] += int64(d)
		} else {
			r.Down += float64(d) / maxF(dt, 1)
			x[1] += int64(d)
		}
		rates[ip] = r
		if m := a.macForIP(ip, arp); m != "" {
			deltas[m] = x
		}
	}
	for k := range t.last {
		if !seen[k] {
			delete(t.last, k)
		}
	}
	t.rate, t.lastAt = rates, now

	ts := now.Unix() / deviceBucket * deviceBucket
	for mac, d := range deltas {
		if d[0] == 0 && d[1] == 0 {
			continue
		}
		a.Logs.Exec(`INSERT INTO traffic(ts,mac,up,down) VALUES(?,?,?,?) ON CONFLICT(ts,mac) DO UPDATE SET up=up+excluded.up, down=down+excluded.down`, ts, mac, d[0], d[1])
	}

	// The whole internet connection, from the interface counters.
	var rx, tx uint64
	any := false
	for _, dev := range a.wanDevices() {
		r, ok1 := readCounter(dev, "rx_bytes")
		x, ok2 := readCounter(dev, "tx_bytes")
		if ok1 && ok2 {
			rx, tx, any = rx+r, tx+x, true
		}
	}
	if any && t.wanOK && rx >= t.wan[0] && tx >= t.wan[1] {
		if dr, dx := rx-t.wan[0], tx-t.wan[1]; dr > 0 || dx > 0 {
			a.Logs.Exec(`INSERT INTO traffic(ts,mac,up,down) VALUES(?,?,?,?) ON CONFLICT(ts,mac) DO UPDATE SET up=up+excluded.up, down=down+excluded.down`,
				now.Unix()/wanBucket*wanBucket, "", int64(dx), int64(dr))
		}
	}
	t.wan, t.wanOK = [2]uint64{rx, tx}, any
}

func maxF(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// Time ranges the console offers.
var trafficRanges = map[string]struct{ secs, step int64 }{
	"30m": {30 * 60, 60},
	"1h":  {3600, 60},
	"1d":  {86400, 900},
	"1w":  {7 * 86400, 3600},
	"1m":  {30 * 86400, 14400},
}

type trafficPoint struct {
	T    int64   `json:"t"`
	Up   float64 `json:"up"`   // bits per second, averaged over the step
	Down float64 `json:"down"` // bits per second
}

// trafficSeries returns the speed over time for the whole connection (no macs) or for the sum of
// the chosen devices.
func (a *App) trafficSeries(rng string, macs []string) (step int64, from, to int64, pts []trafficPoint, err error) {
	r, ok := trafficRanges[rng]
	if !ok {
		r = trafficRanges["1h"]
	}
	to = time.Now().Unix()
	from = (to - r.secs) / r.step * r.step
	step = r.step
	var where string
	args := []any{from}
	if len(macs) == 0 {
		where = "mac=''"
	} else {
		ph := make([]string, len(macs))
		for i, m := range macs {
			ph[i] = "?"
			args = append(args, strings.ToLower(m))
		}
		where = "mac IN (" + strings.Join(ph, ",") + ")"
	}
	rows, err := a.Logs.Query(`SELECT (ts/`+strconv.FormatInt(step, 10)+`)*`+strconv.FormatInt(step, 10)+` AS b, SUM(up), SUM(down) FROM traffic WHERE ts>=? AND `+where+` GROUP BY b ORDER BY b`, args...)
	if err != nil {
		return
	}
	defer rows.Close()
	got := map[int64]trafficPoint{}
	for rows.Next() {
		var b, up, down int64
		rows.Scan(&b, &up, &down)
		got[b] = trafficPoint{b, float64(up) * 8 / float64(step), float64(down) * 8 / float64(step)}
	}
	for t := from; t <= to; t += step {
		p, ok := got[t]
		if !ok {
			p = trafficPoint{T: t}
		}
		pts = append(pts, p)
	}
	return
}

type usageRow struct {
	MAC   string `json:"mac"`
	Name  string `json:"name"`
	Up    int64  `json:"up"`
	Down  int64  `json:"down"`
	Total int64  `json:"total"`
}

// usageByDevice adds up the data each device used in the range, most first.
func (a *App) usageByDevice(rng string) ([]usageRow, error) {
	r, ok := trafficRanges[rng]
	if !ok {
		r = trafficRanges["1d"]
	}
	rows, err := a.Logs.Query(`SELECT mac,SUM(up),SUM(down) FROM traffic WHERE ts>=? AND mac<>'' GROUP BY mac`, time.Now().Unix()-r.secs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	names := a.deviceNames()
	out := []usageRow{}
	for rows.Next() {
		var u usageRow
		rows.Scan(&u.MAC, &u.Up, &u.Down)
		u.Total = u.Up + u.Down
		u.Name = names[u.MAC]
		out = append(out, u)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Total > out[j].Total })
	return out, nil
}

// todayBytes is what each device has used since midnight in the machine's time zone.
func (a *App) todayBytes() map[string][2]int64 {
	now := time.Now()
	y, m, d := now.Date()
	mid := time.Date(y, m, d, 0, 0, 0, 0, now.Location()).Unix()
	out := map[string][2]int64{}
	rows, err := a.Logs.Query(`SELECT mac,SUM(up),SUM(down) FROM traffic WHERE ts>=? AND mac<>'' GROUP BY mac`, mid)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var mac string
		var up, down int64
		rows.Scan(&mac, &up, &down)
		out[mac] = [2]int64{up, down}
	}
	return out
}

// pruneTraffic removes buckets older than the retention.
func (a *App) pruneTraffic(days int) {
	if days < 1 {
		days = 30
	}
	a.Logs.Exec(`DELETE FROM traffic WHERE ts<?`, time.Now().Add(-time.Duration(days)*24*time.Hour).Unix())
}

func (a *App) registerDevTrafficRoutes() {
	s := a.API
	s.Handle("GET /api/traffic", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var macs []string
		if v := strings.TrimSpace(r.URL.Query().Get("macs")); v != "" {
			macs = strings.Split(v, ",")
		}
		step, from, to, pts, err := a.trafficSeries(r.URL.Query().Get("range"), macs)
		if err != nil {
			fail(w, err)
			return
		}
		api.JSON(w, 200, map[string]any{"step": step, "from": from, "to": to, "points": pts})
	})
	s.Handle("GET /api/traffic/usage", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		u, err := a.usageByDevice(r.URL.Query().Get("range"))
		if err != nil {
			fail(w, err)
			return
		}
		api.JSON(w, 200, u)
	})
}

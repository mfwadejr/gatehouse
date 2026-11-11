package app

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"nopewarden/internal/fw"
)

// Data use per internet connection. Each connection's own byte counters give its totals by the
// minute; each device is counted per connection by the firewall's own sets, keyed on the port a
// packet really left by, so routes and failover are counted where the traffic went. The older
// "traffic" table keeps counting everything together and is what the All connections view reads.

// sampleLinks runs inside sampleDevices, with the device lock held.
func (a *App) sampleLinks(t *devTraffic, cur map[string]fw.Counter, deltas map[string][2]int64, arp map[string]arpEntry, ts5 int64, now time.Time, firstLook bool) {
	devs := a.wanDevices()
	if t.link == nil {
		t.link = map[string][2]uint64{}
	}
	if t.lastW == nil {
		t.lastW = map[string]uint64{}
	}
	ts1 := now.Unix() / wanBucket * wanBucket
	// Each connection's own totals.
	for name, dev := range devs {
		rx, ok1 := readCounter(dev, "rx_bytes")
		tx, ok2 := readCounter(dev, "tx_bytes")
		if !ok1 || !ok2 {
			delete(t.link, name)
			continue
		}
		if prev, had := t.link[name]; had && rx >= prev[0] && tx >= prev[1] && (rx > prev[0] || tx > prev[1]) {
			a.Logs.Exec(`INSERT INTO link_traffic(ts,link,up,down) VALUES(?,?,?,?) ON CONFLICT(ts,link) DO UPDATE SET up=up+excluded.up, down=down+excluded.down`, ts1, name, int64(tx-prev[1]), int64(rx-prev[0]))
		}
		t.link[name] = [2]uint64{rx, tx}
	}
	for name := range t.link {
		if _, ok := devs[name]; !ok {
			delete(t.link, name)
		}
	}
	put := func(link, mac string, up, down int64) {
		if mac == "" || (up == 0 && down == 0) {
			return
		}
		a.Logs.Exec(`INSERT INTO link_dev_traffic(ts,link,mac,up,down) VALUES(?,?,?,?,?) ON CONFLICT(ts,link,mac) DO UPDATE SET up=up+excluded.up, down=down+excluded.down`, ts5, link, mac, up, down)
	}
	switch {
	case len(devs) == 1:
		// Every device reaches the internet through the one connection.
		for name := range devs {
			for mac, d := range deltas {
				put(name, mac, d[0], d[1])
			}
		}
	case len(devs) > 1:
		byKey := map[string]string{}
		for name, dev := range devs {
			byKey[fw.WanKey(dev)] = name
		}
		per := map[[2]string][2]int64{}
		seen := map[string]bool{}
		first := firstLook
		for key, c := range cur {
			if !strings.HasPrefix(key, fw.AcctWanPrefix) {
				continue
			}
			rest := key[len(fw.AcctWanPrefix):]
			parts := strings.SplitN(rest, ":", 3)
			if len(parts) != 3 {
				continue
			}
			link, ok := byKey[parts[0]]
			if !ok {
				continue
			}
			seen[rest] = true
			prev, had := t.lastW[rest]
			d := c.Bytes - prev
			if !had || c.Bytes < prev {
				d = c.Bytes
				if !had && first {
					d = 0
				}
			}
			t.lastW[rest] = c.Bytes
			mac := a.macForIP(parts[2], arp)
			if d == 0 || mac == "" {
				continue
			}
			x := per[[2]string{link, mac}]
			if parts[1] == "up" {
				x[0] += int64(d)
			} else {
				x[1] += int64(d)
			}
			per[[2]string{link, mac}] = x
		}
		for k, d := range per {
			put(k[0], k[1], d[0], d[1])
		}
		for k := range t.lastW {
			if !seen[k] {
				delete(t.lastW, k)
			}
		}
	}
}

// ---- reading it back ----

type usageLink struct {
	Name string `json:"name"`
}

// usageRange turns a named period into times. "billing" needs a connection (billing days differ).
func (a *App) usageRange(rng, fromS, toS string, start int) (time.Time, time.Time, string, error) {
	now := a.localNow()
	loc := now.Location()
	y, m, d := now.Date()
	today := time.Date(y, m, d, 0, 0, 0, 0, loc)
	switch rng {
	case "billing":
		f, t := usagePeriod(now, start)
		return f, t, "billing", nil
	case "last_month":
		f := time.Date(y, m-1, 1, 0, 0, 0, 0, loc)
		return f, time.Date(y, m, 1, 0, 0, 0, 0, loc), rng, nil
	case "last_30":
		return today.AddDate(0, 0, -29), today.AddDate(0, 0, 1), rng, nil
	case "last_7":
		return today.AddDate(0, 0, -6), today.AddDate(0, 0, 1), rng, nil
	case "custom":
		f, e1 := time.ParseInLocation("2006-01-02", fromS, loc)
		t, e2 := time.ParseInLocation("2006-01-02", toS, loc)
		if e1 != nil || e2 != nil {
			return time.Time{}, time.Time{}, rng, fmt.Errorf("choose the first and the last day")
		}
		if t.Before(f) {
			f, t = t, f
		}
		t = t.AddDate(0, 0, 1)
		if t.Sub(f) > 366*24*time.Hour {
			return time.Time{}, time.Time{}, rng, fmt.Errorf("choose a range of a year or less")
		}
		return f, t, rng, nil
	default:
		return time.Date(y, m, 1, 0, 0, 0, 0, loc), time.Date(y, m+1, 1, 0, 0, 0, 0, loc), "this_month", nil
	}
}

func (a *App) usageLinks() []string {
	var out []string
	if wans, err := a.listWans(a.Config); err == nil {
		for _, w := range wans {
			if w.Enabled {
				out = append(out, w.Name)
			}
		}
	}
	return out
}

// usageLinkConfig is the limit for one connection: its own, or the one saved before connections
// were counted apart, which then applies to each of them.
func (a *App) usageLinkConfig(link string) usageConfig {
	c := a.usageConfig()
	var all map[string]usageConfig
	if raw := a.setting("usage.links", ""); raw != "" {
		json.Unmarshal([]byte(raw), &all)
	}
	if lc, ok := all[link]; ok {
		if lc.Start < 1 || lc.Start > 28 {
			lc.Start = 1
		}
		if len(lc.Levels) == 0 {
			lc.Levels = []int{80, 100}
		}
		return lc
	}
	return c
}

// usageSum reads one connection's bytes in a period. With a single connection, history from before
// connections were counted apart is the same traffic, so it is filled in from the older total.
func (a *App) usageSum(link string, from, to int64) (up, down int64, partialSince int64) {
	if link == "" {
		a.Logs.QueryRow(`SELECT COALESCE(SUM(up),0),COALESCE(SUM(down),0) FROM traffic WHERE mac='' AND ts>=? AND ts<?`, from, to).Scan(&up, &down)
		return
	}
	a.Logs.QueryRow(`SELECT COALESCE(SUM(up),0),COALESCE(SUM(down),0) FROM link_traffic WHERE link=? AND ts>=? AND ts<?`, link, from, to).Scan(&up, &down)
	var first int64
	a.Logs.QueryRow(`SELECT COALESCE(MIN(ts),0) FROM link_traffic`).Scan(&first)
	if first == 0 {
		first = time.Now().Unix()
	}
	if from < first {
		if len(a.usageLinks()) <= 1 {
			var u2, d2 int64
			a.Logs.QueryRow(`SELECT COALESCE(SUM(up),0),COALESCE(SUM(down),0) FROM traffic WHERE mac='' AND ts>=? AND ts<?`, from, minI64(to, first)).Scan(&u2, &d2)
			up, down = up+u2, down+d2
		} else {
			partialSince = first
		}
	}
	return
}

func minI64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func (a *App) usageTop(link string, from, to int64) []usageDev {
	sum := map[string]*usageDev{}
	add := func(q string, args ...any) {
		rows, err := a.Logs.Query(q, args...)
		if err != nil {
			return
		}
		defer rows.Close()
		for rows.Next() {
			var mac string
			var up, down int64
			if rows.Scan(&mac, &up, &down) == nil {
				d := sum[mac]
				if d == nil {
					d = &usageDev{MAC: mac}
					sum[mac] = d
				}
				d.Up += up
				d.Down += down
			}
		}
	}
	if link == "" {
		add(`SELECT mac,SUM(up),SUM(down) FROM traffic WHERE mac<>'' AND ts>=? AND ts<? GROUP BY mac`, from, to)
	} else {
		add(`SELECT mac,SUM(up),SUM(down) FROM link_dev_traffic WHERE link=? AND ts>=? AND ts<? GROUP BY mac`, link, from, to)
		var first int64
		a.Logs.QueryRow(`SELECT COALESCE(MIN(ts),0) FROM link_dev_traffic`).Scan(&first)
		if first == 0 {
			first = time.Now().Unix()
		}
		if from < first && len(a.usageLinks()) <= 1 {
			add(`SELECT mac,SUM(up),SUM(down) FROM traffic WHERE mac<>'' AND ts>=? AND ts<? GROUP BY mac`, from, minI64(to, first))
		}
	}
	top := []usageDev{}
	for _, d := range sum {
		top = append(top, *d)
	}
	sort.Slice(top, func(i, j int) bool { return top[i].Up+top[i].Down > top[j].Up+top[j].Down })
	if len(top) > 8 {
		top = top[:8]
	}
	names := a.deviceNames()
	for i := range top {
		top[i].Name = orWord(names[top[i].MAC], top[i].MAC)
	}
	return top
}

// usageFor builds the Data use view for a connection ("" is all of them) and a period.
func (a *App) usageFor(link, rng, fromS, toS string) (usageView, error) {
	var cfg usageConfig
	if link != "" {
		cfg = a.usageLinkConfig(link)
	} else {
		cfg = usageConfig{Start: 1, Levels: []int{80, 100}}
		if rng == "billing" {
			rng = "this_month"
		}
	}
	from, to, rng, err := a.usageRange(rng, fromS, toS, cfg.Start)
	if err != nil {
		return usageView{}, err
	}
	v := usageView{usageConfig: cfg, From: from.Unix(), To: to.Unix(), Link: link, Range: rng, Links: []usageLink{}}
	for _, n := range a.usageLinks() {
		v.Links = append(v.Links, usageLink{n})
	}
	var since int64
	v.Up, v.Down, since = a.usageSum(link, from.Unix(), to.Unix())
	if since > 0 {
		v.Note = "Counting by connection began on " + time.Unix(since, 0).In(from.Location()).Format("Jan 2") + ". Earlier use was counted for all connections together, so it is not shown here. Choose All connections to see it."
	}
	if link != "" && cfg.On && cfg.CapGB > 0 && rng == "billing" {
		v.Pct = 100 * float64(v.Up+v.Down) / (float64(cfg.CapGB) * 1e9)
	}
	v.Top = a.usageTop(link, from.Unix(), to.Unix())
	return v, nil
}

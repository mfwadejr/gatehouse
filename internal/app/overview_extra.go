package app

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"nopewarden/internal/api"
)

// homeMapSetting is where the person put themselves on the Overview map, as a point on the map's
// own drawing (0 to 1000 across, 0 to 450 down). It stays on this machine; support bundles hide it.
const homeMapSetting = "home.map"

func (a *App) homeMap() (x, y int, ok bool) {
	parts := strings.Split(a.Config.Setting(homeMapSetting, ""), ",")
	if len(parts) != 2 {
		return 0, 0, false
	}
	x, e1 := strconv.Atoi(parts[0])
	y, e2 := strconv.Atoi(parts[1])
	if e1 != nil || e2 != nil || x < 0 || x > 1000 || y < 0 || y > 450 {
		return 0, 0, false
	}
	return x, y, true
}

// threatToday counts the alerts of the last day that deserve a look (not the quiet group), the
// quiet ones, and how many devices the first kind involve.
func (a *App) threatToday() (loud, odd, devices int) {
	cut := time.Now().Add(-24 * time.Hour).Unix()
	seen := map[string]bool{}
	for _, g := range a.threatGroups(200) {
		if g.Last < cut {
			continue
		}
		if g.Quiet {
			odd += g.Count
			continue
		}
		loud += g.Count
		seen[g.MAC+"|"+g.Device] = true
	}
	return loud, odd, len(seen)
}

type happenedItem struct {
	TS       int64  `json:"ts"`
	Severity string `json:"severity"` // info, warning
	Title    string `json:"title"`
	Count    int    `json:"count"`
	Tag      string `json:"tag,omitempty"` // new, info
	To       string `json:"to,omitempty"`
	Detail   string `json:"detail,omitempty"`
}

func (a *App) registerHomeAndHappened() {
	a.registerGeocode()
	s := a.API
	s.Handle("GET /api/overview/home", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		x, y, ok := a.homeMap()
		api.JSON(w, 200, map[string]any{"set": ok, "x": x, "y": y})
	})
	s.HandleAdmin("PUT /api/overview/home", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			X, Y  int
			Clear bool
		}
		if !api.Decode(w, r, &in) {
			return
		}
		if in.Clear {
			a.Config.SetSetting(homeMapSetting, "")
			ok(w)
			return
		}
		if in.X < 0 || in.X > 1000 || in.Y < 0 || in.Y > 450 {
			fail(w, fmt.Errorf("choose a place on the map"))
			return
		}
		a.Config.SetSetting(homeMapSetting, fmt.Sprintf("%d,%d", in.X, in.Y))
		ok(w)
	})
	// What happened: ordinary events with repeats folded together, and threat alerts as the grouped
	// plain-words rows, newest first. Threat events are not taken from the events table (their text
	// there is the same line for every device).
	s.Handle("GET /api/overview/happened", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var items []happenedItem
		if rows, err := a.Logs.Query(`SELECT ts,severity,title FROM events WHERE area<>'threat' ORDER BY ts DESC,id DESC LIMIT 60`); err == nil {
			for rows.Next() {
				var it happenedItem
				rows.Scan(&it.TS, &it.Severity, &it.Title)
				it.Count = 1
				if it.Severity != "warning" {
					it.Severity = "info"
				}
				if n := len(items); n > 0 && items[n-1].Title == it.Title {
					items[n-1].Count++
					continue
				}
				items = append(items, it)
			}
			rows.Close()
		}
		// One line per kind of alert per device, however many addresses were involved.
		type fold struct {
			it     happenedItem
			places map[string]bool
		}
		folds := map[string]*fold{}
		var order []string
		for _, g := range a.threatGroups(200) {
			k := fmt.Sprintf("%s|%v", g.Title, g.Quiet)
			f := folds[k]
			if f == nil {
				it := happenedItem{TS: g.Last, Severity: "warning", Title: g.Title, To: "protection"}
				if g.Quiet {
					it.Severity, it.Tag = "info", "info"
				}
				f = &fold{it: it, places: map[string]bool{}}
				folds[k] = f
				order = append(order, k)
			}
			f.it.Count += g.Count
			if g.Last > f.it.TS {
				f.it.TS = g.Last
			}
			f.places[g.Remote] = true
		}
		for _, k := range order {
			f := folds[k]
			if n := len(f.places); n > 1 {
				f.it.Detail = fmt.Sprintf("%d different addresses", n)
			}
			items = append(items, f.it)
		}
		sort.SliceStable(items, func(i, j int) bool { return items[i].TS > items[j].TS })
		if len(items) > 6 {
			items = items[:6]
		}
		if items == nil {
			items = []happenedItem{}
		}
		api.JSON(w, 200, items)
	})
}

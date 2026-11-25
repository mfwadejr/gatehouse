package app

import (
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"nopewarden/internal/api"
	"nopewarden/internal/wanmon"
)

// Pause for internet connections. A person can pause one connection, or all of them, for a time:
// nothing leaves through a paused connection, it is shown as Paused (not as broken, and no outage
// alert is raised), and it starts again by itself when the time is up. Traffic moves to another
// connection if one is working. Networks inside the building keep working: only the way out stops,
// so the console, DHCP and local DNS are not affected.
//
// The paths are not under /api/net/ on purpose: a pause acts at once and is not a saved network change.

type pauseIn struct {
	IDs     []int64 `json:"ids"`
	All     bool    `json:"all"`
	Minutes int     `json:"minutes"`
}

// pickWans returns the enabled connections named by ids (or all of them).
func (a *App) pickWans(ids []int64, all bool) ([]WanRow, []WanRow, error) {
	v, err := a.listWans(a.Config)
	if err != nil {
		return nil, nil, err
	}
	var on, picked []WanRow
	for _, r := range v {
		if !r.Enabled {
			continue
		}
		on = append(on, r)
		hit := all
		for _, id := range ids {
			hit = hit || id == r.ID
		}
		if hit {
			picked = append(picked, r)
		}
	}
	if len(picked) == 0 {
		return on, nil, fmt.Errorf("choose an internet connection that is turned on")
	}
	return on, picked, nil
}

// youAreOnTheInternet says whether the person managing this device is probably connected from outside
// (through the internet or a VPN) rather than from a network inside, so pausing would cut them off.
func youAreOnTheInternet(ip string) (bool, string) {
	p := net.ParseIP(ip)
	if p == nil || p.IsLoopback() {
		return false, ""
	}
	ifc := ifaceOf(ip)
	for _, pre := range []string{"wg", "tun", "tap", "ppp"} {
		if strings.HasPrefix(ifc, pre) {
			return true, "You are connected through a VPN, and a VPN runs over an internet connection."
		}
	}
	if ifc == "" && !p.IsPrivate() {
		return true, "You are connected from the internet."
	}
	return false, ""
}

func (a *App) registerInternetPauseRoutes() {
	s := a.API
	s.Handle("GET /api/internet/pause-check", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var ids []int64
		for _, f := range strings.Split(r.URL.Query().Get("ids"), ",") {
			if n, err := strconv.ParseInt(strings.TrimSpace(f), 10, 64); err == nil {
				ids = append(ids, n)
			}
		}
		on, picked, err := a.pickWans(ids, r.URL.Query().Get("all") == "1")
		if err != nil {
			fail(w, err)
			return
		}
		st := map[string]wanmon.State{}
		if a.Net != nil {
			st, _ = a.Net.WanStatus()
		}
		working := func(name string) bool { s, ok := st[name]; return ok && s.Up && !s.Paused }
		pickedSet := map[string]bool{}
		for _, p := range picked {
			pickedSet[p.Name] = true
		}
		now, left := 0, 0
		for _, o := range on {
			if working(o.Name) {
				now++
				if !pickedSet[o.Name] {
					left++
				}
			}
		}
		inet, why := youAreOnTheInternet(c.IP)
		names := []string{}
		for _, p := range picked {
			names = append(names, p.Name)
		}
		api.JSON(w, 200, map[string]any{
			"names": names, "working": now, "left": left,
			"cuts_all": now > 0 && left == 0, // nothing would be working afterwards
			"you":      inet, "you_why": why,
		})
	})
	s.HandleAdmin("POST /api/internet/pause", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in pauseIn
		if !api.Decode(w, r, &in) || !a.needHelper(w) {
			return
		}
		if in.Minutes == 0 {
			fail(w, fmt.Errorf("choose how long to pause for"))
			return
		}
		until, err := pausePick(in.Minutes)
		if err != nil {
			fail(w, err)
			return
		}
		_, picked, err := a.pickWans(in.IDs, in.All)
		if err != nil {
			fail(w, err)
			return
		}
		var names []string
		for _, p := range picked {
			if err := a.Net.PauseWAN(p.Name, until); err != nil {
				fail(w, err)
				return
			}
			names = append(names, p.Name)
		}
		how := "until resumed"
		if in.Minutes > 0 {
			how = fmt.Sprintf("for %d minutes", in.Minutes)
		}
		what := strings.Join(names, ", ")
		if in.All {
			what = "all internet connections (" + what + ")"
		}
		a.auditEvent(c.User.Username, "info", "Internet paused", "Paused "+what+" "+how+".")
		a.publishNet()
		api.JSON(w, 200, map[string]any{"ok": true, "names": names, "until": until})
	})
	s.HandleAdmin("POST /api/internet/resume", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in pauseIn
		if !api.Decode(w, r, &in) || !a.needHelper(w) {
			return
		}
		v, err := a.listWans(a.Config)
		if err != nil {
			fail(w, err)
			return
		}
		var names []string
		for _, row := range v {
			hit := in.All
			for _, id := range in.IDs {
				hit = hit || id == row.ID
			}
			if hit {
				if err := a.Net.PauseWAN(row.Name, 0); err != nil {
					fail(w, err)
					return
				}
				names = append(names, row.Name)
			}
		}
		if len(names) > 0 {
			a.auditEvent(c.User.Username, "info", "Internet resumed", "Resumed "+strings.Join(names, ", ")+" after a pause.")
		}
		a.publishNet()
		api.JSON(w, 200, map[string]any{"ok": true, "names": names, "at": time.Now().Unix()})
	})
}

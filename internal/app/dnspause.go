package app

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"nopewarden/internal/api"
)

// Pause blocking. For a set time the DNS filter lets every name through (lists, your own blocks and
// the app and category blocks), then blocking starts again by itself. It acts at once and is not
// held for Review, because it is a pause and not a change to how DNS is set up. It survives a
// restart: the end time is kept in a setting.

const settingDNSPause = "dns.pause_until"

// loadDNSPause puts a saved pause back after a restart. A pause that has already run out is dropped.
func (a *App) loadDNSPause() {
	v := a.setting(settingDNSPause, "")
	if v == "" {
		return
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n == 0 {
		return
	}
	if n < 0 {
		a.DNS.PauseBlocking(time.Time{})
		return
	}
	if time.Now().Unix() >= n {
		a.Config.SetSetting(settingDNSPause, "")
		return
	}
	a.DNS.PauseBlocking(time.Unix(n, 0))
}

// dnsPauseView is what the console shows about the pause.
func (a *App) dnsPauseView() map[string]any {
	on, until := a.DNS.BlockingPaused(time.Now())
	v := map[string]any{"paused": on}
	if on && !until.IsZero() {
		v["until"] = until.Unix()
	}
	return v
}

func (a *App) registerDNSPauseRoutes() {
	s := a.API
	s.HandleAdmin("POST /api/dns/pause", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			Minutes int `json:"minutes"` // 0 = until resumed
		}
		if !api.Decode(w, r, &in) {
			return
		}
		if in.Minutes < 0 || in.Minutes > 24*60 {
			fail(w, errors.New("choose a time between a minute and a day, or until you resume"))
			return
		}
		how := "until you resume it"
		if in.Minutes == 0 {
			a.DNS.PauseBlocking(time.Time{})
			a.Config.SetSetting(settingDNSPause, "-1")
		} else {
			until := time.Now().Add(time.Duration(in.Minutes) * time.Minute)
			a.DNS.PauseBlocking(until)
			a.Config.SetSetting(settingDNSPause, strconv.FormatInt(until.Unix(), 10))
			how = "for " + strconv.Itoa(in.Minutes) + " minutes"
		}
		a.auditEvent(c.User.Username, "warn", "Blocking paused", "DNS blocking was paused "+how+". Every name is let through until then.")
		api.JSON(w, 200, a.dnsPauseView())
	})
	s.HandleAdmin("POST /api/dns/resume", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		if on, _ := a.DNS.BlockingPaused(time.Now()); on {
			a.auditEvent(c.User.Username, "info", "Blocking resumed", "DNS blocking was switched back on.")
		}
		a.DNS.ResumeBlocking()
		a.Config.SetSetting(settingDNSPause, "")
		api.JSON(w, 200, a.dnsPauseView())
	})
}

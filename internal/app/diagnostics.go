package app

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"gatehouse/internal/api"
)

// Diagnostics: see what each service is doing and its log, restart one, save a support bundle, and
// (only when the owner switches it on) open a shell. All of it is for administrators. None of it can
// touch traffic, so there is nothing here that fails open or closed.

const termEnabledSetting = "term.enabled"

type termOwner struct {
	user    string
	started time.Time
}

// termConfirmed remembers, per signed-in session, when the password or code was last checked for
// the terminal, so opening it again soon after does not ask a second time.
const termConfirmFor = 15 * time.Minute

var termConfirmed = struct {
	sync.Mutex
	m map[string]time.Time
}{m: map[string]time.Time{}}

func termRecentlyConfirmed(c *api.Ctx) bool {
	if c.Session == nil {
		return false
	}
	termConfirmed.Lock()
	defer termConfirmed.Unlock()
	t, ok := termConfirmed.m[c.Session.ID]
	if ok && time.Since(t) > termConfirmFor {
		delete(termConfirmed.m, c.Session.ID)
		return false
	}
	return ok
}

func termConfirm(c *api.Ctx) {
	if c.Session == nil {
		return
	}
	termConfirmed.Lock()
	termConfirmed.m[c.Session.ID] = time.Now()
	termConfirmed.Unlock()
}

var termOwners = struct {
	sync.Mutex
	m map[string]termOwner
}{m: map[string]termOwner{}}

var reSecretKey = regexp.MustCompile(`(?i)(password|secret|token|psk|key|hash|code|salt|totp|cookie|session)`)

// settingsForBundle lists the saved settings with anything secret-looking hidden.
func (a *App) settingsForBundle() string {
	var b strings.Builder
	rows, err := a.Config.Query(`SELECT key,value FROM settings ORDER BY key`)
	if err != nil {
		return ""
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		rows.Scan(&k, &v)
		if reSecretKey.MatchString(k) || k == homeMapSetting {
			v = "[hidden]"
		}
		if len(v) > 300 {
			v = v[:300] + "…"
		}
		fmt.Fprintf(&b, "%s = %s\n", k, v)
	}
	return b.String()
}

func (a *App) termFinished(id, how string, sum string) {
	termOwners.Lock()
	o, ok := termOwners.m[id]
	delete(termOwners.m, id)
	termOwners.Unlock()
	if ok {
		a.event(o.user, "warning", "Closed a terminal", how+" "+sum)
	}
}

func (a *App) registerDiagnosticsRoutes() {
	s := a.API
	s.HandleAdmin("GET /api/diag/services", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		if a.Net == nil {
			fail(w, errNoHelper)
			return
		}
		api.JSON(w, 200, map[string]any{"services": a.Net.DiagServices(), "version": a.Cfg.Version})
	})
	s.HandleAdmin("GET /api/diag/log", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		if a.Net == nil {
			fail(w, errNoHelper)
			return
		}
		n, _ := strconv.Atoi(r.URL.Query().Get("lines"))
		out, err := a.Net.DiagLog(r.URL.Query().Get("unit"), n)
		if err != nil {
			fail(w, err)
			return
		}
		api.JSON(w, 200, map[string]any{"log": out})
	})
	s.HandleAdmin("POST /api/diag/restart", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			Unit string `json:"unit"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		if a.Net == nil {
			fail(w, errNoHelper)
			return
		}
		if err := a.Net.DiagRestart(in.Unit); err != nil {
			fail(w, err)
			return
		}
		a.event(c.User.Username, "warning", "Restarted a service", in.Unit)
		ok(w)
	})
	s.HandleAdmin("GET /api/diag/bundle", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		if a.Net == nil {
			fail(w, errNoHelper)
			return
		}
		out, err := a.Net.DiagBundle()
		if err != nil {
			fail(w, err)
			return
		}
		out = "Gatehouse " + a.Cfg.Version + "\n" + out + "\n===== Settings (secrets hidden) =====\n" + a.settingsForBundle()
		a.event(c.User.Username, "info", "Saved a support bundle", "")
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="gatehouse-support-bundle.txt"`)
		w.Write([]byte(out))
	})

	// The terminal.
	s.HandleAdmin("GET /api/term", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		api.JSON(w, 200, map[string]any{
			"enabled": a.Config.Setting(termEnabledSetting, "0") == "1",
			"warning": "A terminal is a full command line on the firewall, with the highest rights. Anyone who can sign in as an administrator and pass the check can do anything on this machine. It is off until you switch it on, asks for your password or code when it is switched on and again when it is opened if you have not confirmed in the last 15 minutes, closes by itself when nobody types for 10 minutes, and each time it is opened and closed is recorded in the Logs (what is typed is not).",
			"totp":    c.User.TOTPEnabled,
			// true when the check was done in the last 15 minutes, so opening needs no new password
			"confirmed": termRecentlyConfirmed(c),
		})
	})
	s.HandleAdmin("PUT /api/term", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			Enabled  bool   `json:"enabled"`
			Code     string `json:"code"`
			Password string `json:"password"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		if in.Enabled {
			if err := a.serious(c, in.Code, in.Password); err != nil {
				api.Error(w, http.StatusForbidden, err.Error())
				return
			}
			termConfirm(c)
		}
		v := "0"
		if in.Enabled {
			v = "1"
		}
		a.Config.SetSetting(termEnabledSetting, v)
		if in.Enabled {
			a.event(c.User.Username, "warning", "Switched the terminal on", "Administrators can now open a command line on the firewall.")
		} else {
			a.event(c.User.Username, "info", "Switched the terminal off", "")
		}
		ok(w)
	})
	s.HandleAdmin("POST /api/term/open", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			Rows, Cols int
			Code       string `json:"code"`
			Password   string `json:"password"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		if a.Config.Setting(termEnabledSetting, "0") != "1" {
			api.Error(w, http.StatusForbidden, "the terminal is switched off")
			return
		}
		if a.Net == nil {
			fail(w, errNoHelper)
			return
		}
		if !termRecentlyConfirmed(c) {
			if err := a.serious(c, in.Code, in.Password); err != nil {
				api.Error(w, http.StatusForbidden, err.Error())
				return
			}
			termConfirm(c)
		}
		id, err := a.Net.TermOpen(in.Rows, in.Cols)
		if err != nil {
			fail(w, err)
			return
		}
		termOwners.Lock()
		termOwners.m[id] = termOwner{c.User.Username, time.Now()}
		termOwners.Unlock()
		a.event(c.User.Username, "warning", "Opened a terminal", "From "+c.IP+".")
		api.JSON(w, 200, map[string]any{"id": id})
	})
	mine := func(w http.ResponseWriter, c *api.Ctx, id string) bool {
		termOwners.Lock()
		o, found := termOwners.m[id]
		termOwners.Unlock()
		if !found || o.user != c.User.Username {
			api.Error(w, http.StatusNotFound, "that terminal is not open")
			return false
		}
		if a.Config.Setting(termEnabledSetting, "0") != "1" {
			api.Error(w, http.StatusForbidden, "the terminal is switched off")
			return false
		}
		return true
	}
	s.HandleAdmin("GET /api/term/{id}/out", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		id := r.PathValue("id")
		if !mine(w, c, id) || a.Net == nil {
			return
		}
		since, _ := strconv.Atoi(r.URL.Query().Get("since"))
		d, next, closed, err := a.Net.TermRead(id, since, 4*time.Second)
		if err != nil {
			a.termFinished(id, "It ended.", "")
			api.JSON(w, 200, map[string]any{"data": "", "next": since, "closed": true})
			return
		}
		if closed {
			a.termFinished(id, "It ended by itself (closed, or nobody typed for 10 minutes).", "")
		}
		api.JSON(w, 200, map[string]any{"data": base64.StdEncoding.EncodeToString(d), "next": next, "closed": closed})
	})
	s.HandleAdmin("POST /api/term/{id}/in", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		id := r.PathValue("id")
		var in struct {
			Data string `json:"data"`
		}
		if !mine(w, c, id) || a.Net == nil || !api.Decode(w, r, &in) {
			return
		}
		b, err := base64.StdEncoding.DecodeString(in.Data)
		if err != nil {
			fail(w, errors.New("that input could not be read"))
			return
		}
		if err := a.Net.TermWrite(id, b); err != nil {
			fail(w, err)
			return
		}
		ok(w)
	})
	s.HandleAdmin("POST /api/term/{id}/size", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		id := r.PathValue("id")
		var in struct{ Rows, Cols int }
		if !mine(w, c, id) || a.Net == nil || !api.Decode(w, r, &in) {
			return
		}
		a.Net.TermResize(id, in.Rows, in.Cols)
		ok(w)
	})
	s.HandleAdmin("DELETE /api/term/{id}", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		id := r.PathValue("id")
		if !mine(w, c, id) || a.Net == nil {
			return
		}
		sum, _ := a.Net.TermClose(id)
		a.termFinished(id, "Closed by the person using it.", fmt.Sprintf("Open for %d seconds.", sum.Seconds))
		ok(w)
	})
}

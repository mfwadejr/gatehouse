package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gatehouse/internal/api"
	"gatehouse/internal/netd"
	"gatehouse/internal/update"
)

// Updates: Gatehouse can install a newer version from a signed package published with a release,
// so an installed machine never needs a new installer image. Only tagged releases are offered,
// and nothing is installed until an administrator presses the button.

const (
	updateRepoDefault = "mfwadejr/gatehouse"
	updateRepoSetting = "update.repo"
	osSecuritySetting = "update.os_security"
)

type updates struct {
	mu        sync.Mutex
	latest    *update.Release
	checkedAt time.Time
	checkErr  string
	working   bool
}

func (a *App) updateState() string {
	if a.Cfg.UpdateState != "" {
		return a.Cfg.UpdateState
	}
	return netd.UpdateState
}

func (a *App) currentVersion() string {
	if v := strings.TrimSpace(a.Cfg.Version); v != "" && v != "dev" {
		return v
	}
	if b, err := os.ReadFile("/etc/gatehouse-release"); err == nil {
		return strings.TrimSpace(string(b))
	}
	return "dev"
}

func (a *App) updateSource() update.Source {
	api := a.Cfg.UpdateAPI
	if api == "" {
		repo := strings.TrimSpace(a.Config.Setting(updateRepoSetting, updateRepoDefault))
		api = "https://api.github.com/repos/" + repo + "/releases/latest"
	}
	return update.Source{API: api, AllowHTTP: a.Cfg.UpdateAllowHTTP}
}

func (a *App) updateKey() (pk []byte, err error) {
	if len(a.Cfg.UpdateKey) > 0 {
		return a.Cfg.UpdateKey, nil
	}
	return update.EmbeddedKey()
}

// checkUpdate asks the release page what the newest version is.
func (a *App) checkUpdate(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	r, err := a.updateSource().Latest(ctx)
	a.upd.mu.Lock()
	defer a.upd.mu.Unlock()
	a.upd.checkedAt = time.Now()
	if err != nil {
		a.upd.checkErr = err.Error()
		return
	}
	a.upd.checkErr, a.upd.latest = "", r
}

// updateLoop looks for a new version a little after start-up and then twice a day, and reports
// what the last update did.
func (a *App) updateLoop(ctx context.Context) {
	a.reportUpdateResult()
	t := time.NewTimer(45 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if _, err := a.updateKey(); err == nil {
			a.checkUpdate(ctx)
		}
		t.Reset(12 * time.Hour)
	}
}

// reportUpdateResult writes the outcome of the last update to the Gatehouse log, once.
func (a *App) reportUpdateResult() {
	r, ok := update.ReadResult(a.updateState())
	if !ok || fmt.Sprint(r.At) == a.Config.Setting("update.reported", "") {
		return
	}
	a.Config.SetSetting("update.reported", fmt.Sprint(r.At))
	switch {
	case r.OK:
		a.event("system", "info", "Gatehouse was updated", fmt.Sprintf("From %s to %s.", r.From, r.To))
	default:
		a.event("system", "warning", "An update did not work and was undone", fmt.Sprintf("Version %s: %s", r.To, r.Error))
	}
}

type updateView struct {
	Current     string          `json:"current"`
	Latest      *update.Release `json:"latest"`
	Available   bool            `json:"available"`
	CheckedAt   int64           `json:"checked_at"`
	Error       string          `json:"error,omitempty"`
	State       string          `json:"state"`
	Result      *update.Result  `json:"result,omitempty"`
	KeySet      bool            `json:"key_set"`
	Repo        string          `json:"repo"`
	Helper      bool            `json:"helper"`
	OSSecurity  bool            `json:"os_security"`
	NeedsReboot bool            `json:"needs_reboot"`
}

func (a *App) updateStatus() updateView {
	a.upd.mu.Lock()
	latest, checked, errText := a.upd.latest, a.upd.checkedAt, a.upd.checkErr
	a.upd.mu.Unlock()
	v := updateView{Current: a.currentVersion(), Latest: latest, Error: errText, State: update.ReadState(a.updateState()),
		Repo: a.Config.Setting(updateRepoSetting, updateRepoDefault), Helper: a.Net != nil,
		OSSecurity: a.Config.Setting(osSecuritySetting, "0") == "1"}
	if !checked.IsZero() {
		v.CheckedAt = checked.Unix()
	}
	_, err := a.updateKey()
	v.KeySet = err == nil
	if latest != nil {
		v.Available = update.Compare(latest.Version, v.Current) > 0
	}
	if r, ok := update.ReadResult(a.updateState()); ok {
		v.Result = &r
	}
	if _, err := os.Stat("/var/run/reboot-required"); err == nil {
		v.NeedsReboot = true
	}
	return v
}

func (a *App) registerUpdateRoutes() {
	s := a.API
	s.Handle("GET /api/system/update", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		api.JSON(w, 200, a.updateStatus())
	})
	s.HandleAdmin("POST /api/system/update/check", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		a.checkUpdate(r.Context())
		api.JSON(w, 200, a.updateStatus())
	})
	s.HandleAdmin("PUT /api/system/update/settings", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			Repo string `json:"repo"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		in.Repo = strings.TrimSpace(in.Repo)
		if in.Repo == "" {
			in.Repo = updateRepoDefault
		}
		if parts := strings.Split(in.Repo, "/"); len(parts) != 2 || !reRepoPart.MatchString(parts[0]) || !reRepoPart.MatchString(parts[1]) {
			fail(w, errors.New("write the release page as owner/name, like mfwadejr/gatehouse"))
			return
		}
		a.Config.SetSetting(updateRepoSetting, in.Repo)
		a.upd.mu.Lock()
		a.upd.latest, a.upd.checkErr = nil, ""
		a.upd.mu.Unlock()
		ok(w)
	})
	s.HandleAdmin("PUT /api/system/update/os-security", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			Enabled bool `json:"enabled"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		if a.Net == nil {
			fail(w, errNoHelper)
			return
		}
		if err := a.Net.OSSecurity(in.Enabled); err != nil {
			fail(w, err)
			return
		}
		a.Config.SetSetting(osSecuritySetting, map[bool]string{true: "1", false: "0"}[in.Enabled])
		a.event(c.User.Username, "info", "Operating system security updates "+map[bool]string{true: "switched on", false: "switched off"}[in.Enabled], "")
		ok(w)
	})
	s.HandleAdmin("POST /api/system/update/install", a.updateInstall)
}

var reRepoPart = regexpMust(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)

func (a *App) updateInstall(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
	var in struct {
		Code string `json:"code"`
	}
	if !api.Decode(w, r, &in) {
		return
	}
	if a.Net == nil {
		fail(w, errNoHelper)
		return
	}
	if a.Config.Setting("fw.stepup", "1") == "1" && c.User.TOTPEnabled {
		if err := a.Auth.VerifyStepUp(c.User.ID, in.Code); err != nil {
			api.Error(w, http.StatusForbidden, "enter a current code from your authenticator to install an update")
			return
		}
	}
	pubRaw, err := a.updateKey()
	if err != nil {
		fail(w, err)
		return
	}
	pub := pubRaw
	a.upd.mu.Lock()
	if a.upd.working {
		a.upd.mu.Unlock()
		api.Error(w, http.StatusConflict, update.ErrBusy.Error())
		return
	}
	a.upd.working = true
	rel := a.upd.latest
	a.upd.mu.Unlock()
	defer func() { a.upd.mu.Lock(); a.upd.working = false; a.upd.mu.Unlock() }()
	if st := update.ReadState(a.updateState()); st != "idle" {
		api.Error(w, http.StatusConflict, "an update is already running ("+st+")")
		return
	}
	if rel == nil || update.Compare(rel.Version, a.currentVersion()) <= 0 {
		fail(w, errors.New("there is no newer version to install; check for updates first"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	data, sig, err := a.updateSource().Fetch(ctx, rel)
	if err != nil {
		fail(w, err)
		return
	}
	p, err := update.Open(data, sig, pub)
	if err != nil {
		fail(w, err)
		return
	}
	if p.Manifest.Version != rel.Version {
		fail(w, errors.New("the downloaded package is a different version than the release says; it was not installed"))
		return
	}
	inbox := filepath.Join(a.Cfg.DataDir, "update")
	os.RemoveAll(inbox)
	if err := os.MkdirAll(inbox, 0o755); err != nil {
		fail(w, err)
		return
	}
	file := filepath.Join(inbox, "gatehouse-update-"+rel.Version+".tar.gz")
	if err := os.WriteFile(file, data, 0o644); err != nil {
		fail(w, err)
		return
	}
	if err := os.WriteFile(file+".sig", []byte(sig), 0o644); err != nil {
		fail(w, err)
		return
	}
	// A copy of the settings from before the update, in case it is ever needed.
	bak := filepath.Join(inbox, "config.db.before-update")
	os.Remove(bak)
	a.Config.Exec(`VACUUM INTO ?`, bak)
	if err := a.Net.UpdateStart(file); err != nil {
		fail(w, err)
		return
	}
	a.event(c.User.Username, "info", "Update started", fmt.Sprintf("Installing %s over %s. The console restarts.", rel.Version, a.currentVersion()))
	api.JSON(w, 200, map[string]any{"started": true, "to": rel.Version})
}

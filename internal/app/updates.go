package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"gatehouse/internal/api"
	"gatehouse/internal/netd"
	"gatehouse/internal/update"
)

// Updates: one update covers Gatehouse and the operating system underneath it. Gatehouse comes
// from a signed package published with a release; the package also names the Debian packages
// the release needs, and the Debian updates for the release the machine is on are installed
// with it, system first. Only tagged releases are offered. Nothing is installed until an
// administrator presses the button, unless the machine is set to update itself in a quiet hour.

const (
	updateRepoDefault   = "mfwadejr/gatehouse"
	updateRepoSetting   = "update.repo"
	updateModeSetting   = "update.mode"    // off, notify or auto
	updateWindowSetting = "update.window"  // the hour (0 to 23) automatic updates may start
	updateAutoAtSetting = "update.auto_at" // when the last automatic attempt began
)

type updates struct {
	mu         sync.Mutex
	latest     *update.Release
	checkedAt  time.Time
	checkErr   string
	sys        *netd.SystemUpdates
	sysErr     string
	sysChecked time.Time
	checking   bool // the operating system check is running
	working    bool
	step       string // what the console itself is doing before it hands over to the helper
	plan       update.Plan
	runErr     string
}

// rebootNeeded is replaceable so tests do not depend on the machine they run on.
var rebootNeeded = update.RebootNeeded

func (a *App) updateMode() string {
	switch m := a.Config.Setting(updateModeSetting, "notify"); m {
	case "off", "auto":
		return m
	}
	return "notify"
}

func (a *App) updateWindow() int {
	h, err := strconv.Atoi(a.Config.Setting(updateWindowSetting, "3"))
	if err != nil || h < 0 || h > 23 {
		return 3
	}
	return h
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

// checkUpdate asks the release page what the newest version is, and starts looking at what the
// operating system has waiting. Asking Debian can take a minute, so that part runs on its own and
// the page picks the answer up when it is ready.
func (a *App) checkUpdate(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	r, err := a.updateSource().Latest(ctx)
	a.upd.mu.Lock()
	a.upd.checkedAt = time.Now()
	if err != nil {
		a.upd.checkErr = err.Error()
	} else {
		a.upd.checkErr, a.upd.latest = "", r
	}
	a.upd.mu.Unlock()
	a.checkSystem()
}

// checkSystem looks at the operating system's updates in the background.
func (a *App) checkSystem() {
	if a.Net == nil {
		return
	}
	a.upd.mu.Lock()
	if a.upd.checking {
		a.upd.mu.Unlock()
		return
	}
	a.upd.checking = true
	a.upd.mu.Unlock()
	go func() {
		su, err := a.Net.SystemCheck()
		a.upd.mu.Lock()
		defer a.upd.mu.Unlock()
		a.upd.checking = false
		a.upd.sysChecked = time.Now()
		if err != nil {
			a.upd.sysErr = err.Error()
			return
		}
		a.upd.sysErr, a.upd.sys = "", &su
	}()
}

// updateLoop looks for updates a little after start-up and then twice a day (unless updates are
// off), reports what the last update did, and in "automatic" mode installs in the chosen hour.
func (a *App) updateLoop(ctx context.Context) {
	a.reportUpdateResult()
	go a.pairResumeLoop(ctx)
	first := time.NewTimer(45 * time.Second)
	defer first.Stop()
	tick := time.NewTicker(10 * time.Minute)
	defer tick.Stop()
	isFirst := true
	for {
		select {
		case <-ctx.Done():
			return
		case <-first.C:
		case <-tick.C:
		}
		a.updateTick(ctx, isFirst)
		isFirst = false
	}
}

func (a *App) updateTick(ctx context.Context, force bool) {
	mode := a.updateMode()
	if mode == "off" {
		return
	}
	if _, err := a.updateKey(); err != nil {
		return
	}
	a.upd.mu.Lock()
	stale := time.Since(a.upd.checkedAt) > 12*time.Hour
	a.upd.mu.Unlock()
	if force || stale {
		a.checkUpdate(ctx)
		return
	}
	if mode == "auto" {
		a.autoInstall(time.Now())
	}
}

// autoInstall starts an update on its own, once a day at most, in the hour the owner chose.
func (a *App) autoInstall(now time.Time) {
	if now.Hour() != a.updateWindow() {
		return
	}
	if last, _ := strconv.ParseInt(a.Config.Setting(updateAutoAtSetting, "0"), 10, 64); now.Unix()-last < 20*3600 {
		return
	}
	v := a.updateStatus()
	if !v.Available || v.State != "idle" || v.Working {
		return
	}
	a.Config.SetSetting(updateAutoAtSetting, strconv.FormatInt(now.Unix(), 10))
	if _, err := a.startAnyUpdate("automatic update", true); err != nil {
		a.event("system", "warning", "The automatic update could not start", err.Error())
	}
}

// reportUpdateResult writes the outcome of the last update to the Gatehouse log, once.
func (a *App) reportUpdateResult() {
	r, ok := update.ReadResult(a.updateState())
	if !ok || fmt.Sprint(r.At) == a.Config.Setting("update.reported", "") {
		return
	}
	a.Config.SetSetting("update.reported", fmt.Sprint(r.At))
	sys := ""
	if r.System != nil {
		sys = fmt.Sprintf(" System packages changed: %d.", r.System.Packages)
		if r.System.Skipped != "" {
			sys += " System updates were skipped: " + r.System.Skipped + "."
		}
		if r.System.RebootNeeded {
			sys += " A restart is needed to finish."
		}
	}
	switch {
	case r.OK && r.From == r.To:
		a.event("system", "info", "System updates were installed", strings.TrimSpace(sys))
	case r.OK:
		a.event("system", "info", "Gatehouse was updated", fmt.Sprintf("From %s to %s.%s", r.From, r.To, sys))
	case r.RolledBack:
		a.event("system", "warning", "An update did not work and was undone", fmt.Sprintf("Version %s: %s", r.To, r.Error))
	default:
		a.event("system", "warning", "An update did not work", fmt.Sprintf("%s%s", r.Error, sys))
	}
}

// updateStep is one line of the progress list.
type updateStep struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	State string `json:"state"` // done, active or pending
}

type updateView struct {
	Current       string          `json:"current"`
	Latest        *update.Release `json:"latest"`
	GatehouseNew  bool            `json:"gatehouse_new"` // a newer Gatehouse is published
	SystemCount   int             `json:"system_count"`  // operating system updates waiting
	SystemNames   []string        `json:"system_names,omitempty"`
	SystemError   string          `json:"system_error,omitempty"`
	Checking      bool            `json:"checking"`  // still asking Debian
	Available     bool            `json:"available"` // there is something to install
	CheckedAt     int64           `json:"checked_at"`
	Error         string          `json:"error,omitempty"`
	State         string          `json:"state"`
	Working       bool            `json:"working"` // the console is preparing the update
	RunError      string          `json:"run_error,omitempty"`
	Steps         []updateStep    `json:"steps,omitempty"`
	RollingBack   bool            `json:"rolling_back,omitempty"`
	Result        *update.Result  `json:"result,omitempty"`
	KeySet        bool            `json:"key_set"`
	Repo          string          `json:"repo"`
	Helper        bool            `json:"helper"`
	Mode          string          `json:"mode"`   // off, notify or auto
	Window        int             `json:"window"` // hour automatic updates may start
	NeedsReboot   bool            `json:"needs_reboot"`
	RebootComment string          `json:"-"`
	Pair          *pairView       `json:"pair,omitempty"`
}

// pairView is the state of an update of both units of a pair.
type pairView struct {
	Phase  string `json:"phase"`
	Target string `json:"target"`
	Error  string `json:"error,omitempty"`
}

// updateSteps builds the progress list for the run that is going on.
func updateSteps(plan update.Plan, active string) []updateStep {
	all := []struct {
		id, label string
		gh, sys   bool
	}{
		{"download", "Downloading the update", true, false},
		{"verify", "Checking its signature", true, false},
		{"backup", "Saving a copy of your settings", true, false},
		{"system", "Installing system updates", false, true},
		{"install", "Installing Gatehouse", true, false},
		{"restart", "Restarting Gatehouse", true, false},
		{"health", "Checking that it runs properly", true, false},
	}
	var out []updateStep
	for _, st := range all {
		if (st.gh && plan.Gatehouse) || (st.sys && plan.System) {
			out = append(out, updateStep{ID: st.id, Label: st.label})
		}
	}
	seen := false
	for i := range out {
		switch {
		case out[i].ID == active:
			out[i].State, seen = "active", true
		case seen:
			out[i].State = "pending"
		default:
			out[i].State = "done"
		}
	}
	if !seen { // unknown step: show everything as waiting rather than done
		for i := range out {
			out[i].State = "pending"
		}
	}
	return out
}

func (a *App) updateStatus() updateView {
	a.upd.mu.Lock()
	latest, checked, errText := a.upd.latest, a.upd.checkedAt, a.upd.checkErr
	sys, sysErr, checking := a.upd.sys, a.upd.sysErr, a.upd.checking
	working, step, plan, runErr := a.upd.working, a.upd.step, a.upd.plan, a.upd.runErr
	a.upd.mu.Unlock()
	v := updateView{Current: a.currentVersion(), Latest: latest, Error: errText, State: update.ReadState(a.updateState()),
		Repo: a.Config.Setting(updateRepoSetting, updateRepoDefault), Helper: a.Net != nil,
		Mode: a.updateMode(), Window: a.updateWindow(), SystemError: sysErr, Checking: checking, Working: working, RunError: runErr}
	if !checked.IsZero() {
		v.CheckedAt = checked.Unix()
	}
	_, err := a.updateKey()
	v.KeySet = err == nil
	if latest != nil {
		v.GatehouseNew = update.Compare(latest.Version, v.Current) > 0
	}
	if sys != nil {
		v.SystemCount, v.SystemNames = sys.Count, sys.Names
	}
	v.Available = v.GatehouseNew || v.SystemCount > 0
	if r, ok := update.ReadResult(a.updateState()); ok {
		v.Result = &r
	}
	v.NeedsReboot = rebootNeeded()
	if ps, p := a.pairSteps(); p != nil {
		v.Pair = &pairView{Phase: p.Phase, Target: p.Target, Error: p.Error}
		if len(ps) > 0 {
			v.Steps = ps
			v.Working = p.Phase != "failed"
			return v
		}
	}
	if working && step != "" {
		v.Steps = updateSteps(plan, step)
	} else if v.State != "idle" {
		p, ok := update.ReadPlan(a.updateState())
		if !ok {
			p = update.Plan{Gatehouse: true, System: true}
		}
		act := map[string]string{"starting": "system", "system": "system", "installing": "install", "restarting": "restart", "checking": "health", "rolling-back": "health"}[v.State]
		if v.State == "starting" && !p.System {
			act = "install"
		}
		v.Steps, v.RollingBack = updateSteps(p, act), v.State == "rolling-back"
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
			Repo   *string `json:"repo"`
			Mode   *string `json:"mode"`
			Window *int    `json:"window"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		if in.Mode != nil && *in.Mode != "off" && *in.Mode != "notify" && *in.Mode != "auto" {
			fail(w, errors.New("choose Off, Notify or Automatic"))
			return
		}
		if in.Window != nil && (*in.Window < 0 || *in.Window > 23) {
			fail(w, errors.New("the hour must be from 0 to 23"))
			return
		}
		repo := ""
		if in.Repo != nil {
			repo = strings.TrimSpace(*in.Repo)
			if repo == "" {
				repo = updateRepoDefault
			}
			if parts := strings.Split(repo, "/"); len(parts) != 2 || !reRepoPart.MatchString(parts[0]) || !reRepoPart.MatchString(parts[1]) {
				fail(w, errors.New("write the release page as owner/name, like mfwadejr/gatehouse"))
				return
			}
		}
		if in.Repo != nil {
			a.Config.SetSetting(updateRepoSetting, repo)
			a.upd.mu.Lock()
			a.upd.latest, a.upd.checkErr = nil, ""
			a.upd.mu.Unlock()
		}
		if in.Mode != nil {
			a.Config.SetSetting(updateModeSetting, *in.Mode)
			a.event(c.User.Username, "info", "Automatic updates set to "+map[string]string{"off": "Off", "notify": "Notify", "auto": "Automatic"}[*in.Mode], "")
		}
		if in.Window != nil {
			a.Config.SetSetting(updateWindowSetting, strconv.Itoa(*in.Window))
		}
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
	if a.Config.Setting("fw.stepup", "0") == "1" && c.User.TOTPEnabled {
		if err := a.Auth.VerifyStepUp(c.User.ID, in.Code); err != nil {
			api.Error(w, http.StatusForbidden, "enter a current code from your authenticator to install an update")
			return
		}
	}
	to, err := a.startAnyUpdate(c.User.Username, false)
	if err != nil {
		if errors.Is(err, update.ErrBusy) {
			api.Error(w, http.StatusConflict, err.Error())
			return
		}
		fail(w, err)
		return
	}
	api.JSON(w, 200, map[string]any{"started": true, "to": to})
}

// startAnyUpdate updates a running pair the safe way (spare first) when there is a newer Gatehouse,
// and a single unit, or a system-only update, the ordinary way.
func (a *App) startAnyUpdate(user string, auto bool) (string, error) {
	if st := a.haLoad(); st != nil && st.Activated && st.PeerFP != "" {
		a.upd.mu.Lock()
		rel := a.upd.latest
		a.upd.mu.Unlock()
		if rel != nil && update.Compare(rel.Version, a.currentVersion()) > 0 {
			if _, err := a.startPairUpdate(user); err != nil {
				return "", err
			}
			return rel.Version, nil
		}
	}
	return a.startUpdate(user, auto)
}

// startUpdate checks that there is something to install and starts the run in the background:
// the console downloads and checks the package, then hands it to the helper, which installs the
// system updates first and Gatehouse second. It returns the version being installed (the current
// one for a run that only updates the operating system).
func (a *App) startUpdate(user string, auto bool) (string, error) {
	pub, err := a.updateKey()
	if err != nil {
		return "", err
	}
	if a.Net == nil {
		return "", errNoHelper
	}
	if st := update.ReadState(a.updateState()); st != "idle" {
		return "", fmt.Errorf("%w (%s)", update.ErrBusy, st)
	}
	a.upd.mu.Lock()
	if a.upd.working {
		a.upd.mu.Unlock()
		return "", update.ErrBusy
	}
	rel, sys := a.upd.latest, a.upd.sys
	gh := rel != nil && update.Compare(rel.Version, a.currentVersion()) > 0
	if !gh && (sys == nil || sys.Count == 0) {
		a.upd.mu.Unlock()
		return "", errors.New("there is nothing new to install; check for updates first")
	}
	plan := update.Plan{Gatehouse: gh, System: true}
	a.upd.working, a.upd.plan, a.upd.runErr = true, plan, ""
	a.upd.step = "system"
	if gh {
		a.upd.step = "download"
	}
	a.upd.mu.Unlock()
	to := a.currentVersion()
	if gh {
		to = rel.Version
	}
	go func() {
		err := a.prepareUpdate(pub, rel, gh)
		a.upd.mu.Lock()
		a.upd.working, a.upd.step = false, ""
		if err != nil {
			a.upd.runErr = err.Error()
		}
		a.upd.mu.Unlock()
		if err != nil {
			a.event(user, "warning", "The update could not start", err.Error())
			return
		}
		what := "System updates are being installed."
		if gh {
			what = fmt.Sprintf("Installing %s over %s, with the system updates. The console restarts.", to, a.currentVersion())
		}
		a.event(user, "info", "Update started", what)
	}()
	return to, nil
}

func (a *App) setStep(id string) {
	a.upd.mu.Lock()
	a.upd.step = id
	a.upd.mu.Unlock()
}

// prepareUpdate does the console's part: download, check the signature, keep a copy of the
// settings, and hand over to the helper.
func (a *App) prepareUpdate(pub []byte, rel *update.Release, gh bool) error {
	if !gh {
		a.setStep("system")
		return a.Net.SystemUpdateStart()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	a.setStep("download")
	data, sig, err := a.updateSource().Fetch(ctx, rel)
	if err != nil {
		return err
	}
	a.setStep("verify")
	p, err := update.Open(data, sig, pub)
	if err != nil {
		return err
	}
	if p.Manifest.Version != rel.Version {
		return errors.New("the downloaded package is a different version than the release says; it was not installed")
	}
	a.setStep("backup")
	inbox := filepath.Join(a.Cfg.DataDir, "update")
	os.RemoveAll(inbox)
	if err := os.MkdirAll(inbox, 0o755); err != nil {
		return err
	}
	file := filepath.Join(inbox, "gatehouse-update-"+rel.Version+".tar.gz")
	if err := os.WriteFile(file, data, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(file+".sig", []byte(sig), 0o644); err != nil {
		return err
	}
	// A copy of the settings from before the update, in case it is ever needed.
	bak := filepath.Join(inbox, "config.db.before-update")
	os.Remove(bak)
	a.Config.Exec(`VACUUM INTO ?`, bak)
	return a.Net.UpdateStart(file)
}

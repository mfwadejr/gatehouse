package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gatehouse/internal/ha"
	"gatehouse/internal/update"
)

// Updating a pair. The active unit is never touched until the spare has been updated and has shown
// that it works:
//
//  1. check that both units are ready and their settings agree;
//  2. download and check the package once, on the active unit;
//  3. send it to the spare, which installs it (the spare has no internet of its own);
//  4. wait for the spare to come back on the new version, healthy;
//  5. hand the active role to the spare;
//  6. update the unit that was active, now idle;
//  7. check that both agree.
//
// If anything fails before step 5 nothing about the active unit has changed. The roles end up
// swapped; hand back from the other unit whenever you like. The run is remembered in a file, so it
// carries on across this unit's own restart in step 6.

const pairUpdateFile = "pairupdate.json"

var pairStepList = []struct{ id, label string }{
	{"ready", "Checking that both units are ready"},
	{"download", "Downloading the update"},
	{"spare", "Updating the spare unit"},
	{"check", "Checking that the spare works"},
	{"handover", "Handing over to the spare"},
	{"self", "Updating this unit"},
	{"final", "Checking both units"},
}

type pairUpd struct {
	Phase  string `json:"phase"` // a step id, "done" or "failed"
	Target string `json:"target"`
	From   string `json:"from"`
	Error  string `json:"error,omitempty"`
	At     int64  `json:"at"`
}

type pairRun struct {
	mu      sync.Mutex
	running bool
}

// Test hooks: the real ones read the release and the partner's status; tests replace them.
type pairHooks struct {
	pollEvery time.Duration
	spareWait time.Duration
	takeWait  time.Duration
	// pkg returns the verified package to install.
	pkg func(ctx context.Context) (data []byte, sig, version string, err error)
}

func (a *App) pairPath() string { return filepath.Join(a.Cfg.DataDir, pairUpdateFile) }

func (a *App) pairLoad() *pairUpd {
	b, err := os.ReadFile(a.pairPath())
	if err != nil {
		return nil
	}
	var p pairUpd
	if json.Unmarshal(b, &p) != nil {
		return nil
	}
	return &p
}

func (a *App) pairSave(p pairUpd) {
	p.At = time.Now().Unix()
	b, _ := json.Marshal(p)
	os.WriteFile(a.pairPath(), b, 0o600)
}

func (a *App) pairSet(phase string) {
	p := a.pairLoad()
	if p == nil {
		p = &pairUpd{}
	}
	p.Phase = phase
	a.pairSave(*p)
}

func (a *App) pairFail(user, msg string) {
	p := a.pairLoad()
	if p == nil {
		p = &pairUpd{}
	}
	p.Phase, p.Error = "failed", msg
	a.pairSave(*p)
	a.event(user, "warning", "The update of both units stopped", msg)
}

// pairSteps is the progress list while a pair update is going on or has just ended.
func (a *App) pairSteps() ([]updateStep, *pairUpd) {
	p := a.pairLoad()
	if p == nil || p.Phase == "" {
		return nil, nil
	}
	if p.Phase == "done" || (p.Phase == "failed" && time.Since(time.Unix(p.At, 0)) > 24*time.Hour) {
		return nil, p
	}
	var out []updateStep
	seen := false
	for _, s := range pairStepList {
		st := updateStep{ID: s.id, Label: s.label}
		switch {
		case p.Phase == "failed":
			st.State = "pending"
		case s.id == p.Phase:
			st.State, seen = "active", true
		case seen:
			st.State = "pending"
		default:
			st.State = "done"
		}
		out = append(out, st)
	}
	return out, p
}

func (a *App) pairHook() pairHooks {
	h := a.pairHooks
	if h.pollEvery == 0 {
		h.pollEvery = 5 * time.Second
	}
	if h.spareWait == 0 {
		h.spareWait = 25 * time.Minute
	}
	if h.takeWait == 0 {
		h.takeWait = 90 * time.Second
	}
	if h.pkg == nil {
		h.pkg = a.pairRealPackage
	}
	return h
}

func (a *App) pairRealPackage(ctx context.Context) ([]byte, string, string, error) {
	pub, err := a.updateKey()
	if err != nil {
		return nil, "", "", err
	}
	a.upd.mu.Lock()
	rel := a.upd.latest
	a.upd.mu.Unlock()
	if rel == nil || update.Compare(rel.Version, a.currentVersion()) <= 0 {
		return nil, "", "", errors.New("there is no newer Gatehouse to install; check for updates first")
	}
	data, sig, err := a.updateSource().Fetch(ctx, rel)
	if err != nil {
		return nil, "", "", err
	}
	p, err := update.Open(data, sig, pub)
	if err != nil {
		return nil, "", "", err
	}
	if p.Manifest.Version != rel.Version {
		return nil, "", "", errors.New("the downloaded package is a different version than the release says; it was not installed")
	}
	return data, sig, rel.Version, nil
}

// pairActiveUnit says whether this is a running pair and this unit is the one in charge.
func (a *App) pairReady() (*haLocal, error) {
	st := a.haLoad()
	if st == nil || !st.Activated || st.PeerFP == "" {
		return nil, errors.New("this unit is not part of a running pair")
	}
	if !a.haActive(st) {
		return nil, fmt.Errorf("start the update from the active unit (%s); it updates the spare first", st.peerAddr())
	}
	return st, nil
}

func (a *App) peerStatus(st *haLocal, timeout time.Duration) (ha.Status, error) {
	var s ha.Status
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	b, err := ha.Call(ctx, st.peerHost(), st.Self, st.PeerFP, "GET", "/ha/status", nil, nil, "")
	if err != nil {
		return s, err
	}
	err = json.Unmarshal(b, &s)
	return s, err
}

// startPairUpdate checks the pair and starts the run in the background. It returns the version
// being installed.
func (a *App) startPairUpdate(user string) (string, error) {
	st, err := a.pairReady()
	if err != nil {
		return "", err
	}
	if a.Net == nil {
		return "", errNoHelper
	}
	if s := update.ReadState(a.updateState()); s != "idle" {
		return "", fmt.Errorf("%w (%s)", update.ErrBusy, s)
	}
	a.pairRun.mu.Lock()
	if a.pairRun.running {
		a.pairRun.mu.Unlock()
		return "", update.ErrBusy
	}
	a.pairRun.running = true
	a.pairRun.mu.Unlock()
	done := func() { a.pairRun.mu.Lock(); a.pairRun.running = false; a.pairRun.mu.Unlock() }

	a.pairSave(pairUpd{Phase: "ready", From: a.currentVersion()})
	ps, err := a.peerStatus(st, 6*time.Second)
	if err != nil {
		done()
		a.pairSet("")
		return "", errors.New("the spare unit cannot be reached, so nothing was changed: " + err.Error())
	}
	switch {
	case ps.Active:
		done()
		a.pairSet("")
		return "", errors.New("both units say they are active; sort that out first (Settings, Advanced, pair)")
	case ps.Update != "" && ps.Update != "idle":
		done()
		a.pairSet("")
		return "", errors.New("the spare unit is already busy updating (" + ps.Update + "); wait for it to finish")
	case a.fwStatus()["dirty"] == true || a.netDirty():
		done()
		a.pairSet("")
		return "", errors.New("apply your pending changes first, so both units agree before updating")
	}
	go func() {
		defer done()
		a.pairUpdateRun(user, st)
	}()
	return "", nil
}

func (a *App) pairUpdateRun(user string, st *haLocal) {
	h := a.pairHook()
	from := a.currentVersion()
	a.event(user, "info", "Updating both units", "The spare unit is updated first; this unit is not touched until the spare works.")
	// Settings agree before anything else.
	if err := a.haPush(true); err != nil {
		a.pairFail(user, "the settings could not be copied to the spare first: "+err.Error())
		return
	}
	a.pairSet("download")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	data, sig, ver, err := h.pkg(ctx)
	cancel()
	if err != nil {
		a.pairFail(user, err.Error())
		return
	}
	a.pairSave(pairUpd{Phase: "spare", Target: ver, From: from})
	// The spare installs it. It may have no internet, so the package comes from here.
	ctx, cancel = context.WithTimeout(context.Background(), 3*time.Minute)
	_, err = ha.Call(ctx, st.peerHost(), st.Self, st.PeerFP, "POST", "/ha/update", data, map[string]string{"X-GH-Sig": sig, "X-GH-Version": ver}, "")
	cancel()
	if err != nil {
		a.pairFail(user, "the spare unit did not accept the update, so nothing was changed on this unit: "+err.Error())
		return
	}
	// Wait for the spare to finish and come back on the new version.
	deadline := time.Now().Add(h.spareWait)
	var ps ha.Status
	for {
		time.Sleep(h.pollEvery)
		ps, err = a.peerStatus(st, 5*time.Second)
		if err == nil && ps.Version == ver && (ps.Update == "" || ps.Update == "idle") {
			break
		}
		if err == nil && (ps.Update == "" || ps.Update == "idle") && ps.Result == "failed" && ps.ResultTo == ver {
			a.pairFail(user, "the spare unit could not install the update and went back to how it was. This unit was not changed.")
			return
		}
		if time.Now().After(deadline) {
			a.pairFail(user, "the spare unit did not come back on the new version in time. This unit was not changed; look at the spare unit.")
			return
		}
	}
	a.pairSet("check")
	// The spare must be healthy and not already in charge.
	if ps.Active {
		a.pairFail(user, "the spare unit became active on its own while updating; stopping before anything else changes")
		return
	}
	if ps.Result == "failed" {
		a.pairFail(user, "the spare unit reports that its update failed")
		return
	}
	a.pairSet("handover")
	if err := a.haInstall(st); err != nil { // stepping aside, as for a manual handover
		a.pairFail(user, "this unit could not hand over: "+err.Error())
		return
	}
	deadline = time.Now().Add(h.takeWait)
	for {
		time.Sleep(h.pollEvery)
		if ps, err = a.peerStatus(st, 4*time.Second); err == nil && ps.Active {
			break
		}
		if time.Now().After(deadline) {
			a.pairFail(user, "the spare unit did not take over. This unit has not been updated and will take the role back by itself.")
			return
		}
	}
	a.event(user, "info", "The updated unit is now active", "The unit that was active is next.")
	// Now this unit, which is idle. The run carries on after its restart (see pairResume).
	a.pairSave(pairUpd{Phase: "self", Target: ver, From: from})
	inbox := filepath.Join(a.Cfg.DataDir, "update")
	os.RemoveAll(inbox)
	if err := os.MkdirAll(inbox, 0o755); err != nil {
		a.pairFail(user, err.Error())
		return
	}
	file := filepath.Join(inbox, "gatehouse-update-"+ver+".tar.gz")
	if err := os.WriteFile(file, data, 0o644); err != nil {
		a.pairFail(user, err.Error())
		return
	}
	os.WriteFile(file+".sig", []byte(sig), 0o644)
	bak := filepath.Join(inbox, "config.db.before-update")
	a.Config.Exec(`VACUUM INTO ?`, bak)
	if err := a.Net.UpdateStart(file); err != nil {
		a.pairFail(user, "this unit could not start its update: "+err.Error()+". The other unit is already updated and in charge.")
		return
	}
	// The console restarts while this installs; pairResume finishes the run.
}

// pairResume picks up a run after this unit's own update: success when it is on the target version
// and the partner is healthy, a plain report when the update was undone.
func (a *App) pairResume() {
	p := a.pairLoad()
	if p == nil || p.Phase != "self" {
		return
	}
	state := update.ReadState(a.updateState())
	if state != "idle" {
		return // still installing; looked at again later
	}
	if a.currentVersion() == p.Target {
		a.pairSet("final")
		if st := a.haLoad(); st != nil && st.PeerFP != "" {
			if ps, err := a.peerStatus(st, 6*time.Second); err != nil || ps.Version != p.Target {
				a.pairFail("system", "this unit is updated, but the other unit could not be confirmed on the same version. Check both units.")
				return
			}
		}
		pp := a.pairLoad()
		pp.Phase = "done"
		a.pairSave(*pp)
		a.event("system", "info", "Both units are updated", fmt.Sprintf("Version %s. The unit that was spare is now active; hand the role back whenever you like.", p.Target))
		return
	}
	msg := "this unit's update did not work and was undone; the other unit is updated and in charge"
	if r, ok := update.ReadResult(a.updateState()); ok && r.Error != "" {
		msg += " (" + strings.TrimSpace(r.Error) + ")"
	}
	a.pairFail("system", msg)
}

// haOnUpdate is what the spare does with a package from the active unit.
func (a *App) haOnUpdate(pkg []byte, sig, version string) error {
	st := a.haLoad()
	if st == nil || !st.Activated {
		return errors.New("this unit is not part of a running pair")
	}
	if a.haActive(st) {
		return errors.New("this unit is the active one; updates are sent to the spare only")
	}
	if a.Net == nil {
		return errNoHelper
	}
	if s := update.ReadState(a.updateState()); s != "idle" {
		return fmt.Errorf("%w (%s)", update.ErrBusy, s)
	}
	pub, err := a.updateKey()
	if err != nil {
		return err
	}
	p, err := update.Open(pkg, sig, pub)
	if err != nil {
		return err
	}
	if p.Manifest.Version != version {
		return errors.New("the package is not the version it was sent as")
	}
	if update.Compare(version, a.currentVersion()) <= 0 {
		return errors.New("this unit already has that version or a newer one")
	}
	inbox := filepath.Join(a.Cfg.DataDir, "update")
	os.RemoveAll(inbox)
	if err := os.MkdirAll(inbox, 0o755); err != nil {
		return err
	}
	file := filepath.Join(inbox, "gatehouse-update-"+version+".tar.gz")
	if err := os.WriteFile(file, pkg, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(file+".sig", []byte(sig), 0o644); err != nil {
		return err
	}
	a.Config.Exec(`VACUUM INTO ?`, filepath.Join(inbox, "config.db.before-update"))
	a.event("partner unit", "info", "An update from the active unit is being installed", "Version "+version)
	return a.Net.UpdateStart(file)
}

// pairResumeLoop finishes a run that was waiting for this unit's own update.
func (a *App) pairResumeLoop(ctx context.Context) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(30 * time.Second):
	}
	for {
		if p := a.pairLoad(); p != nil && p.Phase == "self" {
			a.pairResume()
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(30 * time.Second):
		}
	}
}

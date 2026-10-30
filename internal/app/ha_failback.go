package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"gatehouse/internal/api"
	"gatehouse/internal/ha"
	"gatehouse/internal/netd"
)

// Failback. When the unit that normally runs the network (the Primary) comes back after the other
// unit took over, nothing moves by itself. The console says the Primary is back and healthy and
// offers two choices: give the role back to the Primary, or keep the unit that is running the
// network and make it the Primary. Until a choice is made the network keeps running as it is.
//
// The Primary is a choice kept on both units (haLocal.Prefer, never part of the copied settings).
// It decides the priority each unit starts with, so if both start together the Primary wins. It
// never makes a unit take the role back on its own: that stays "nopreempt" in the keepalived settings.

// failback timings; tests shorten them.
var (
	failbackWait = 25 * time.Second
	failbackPoll = 500 * time.Millisecond
	// failbackSettle is how long to give the Primary to begin loading the settings it was sent
	// before asking whether it has finished.
	failbackSettle = 400 * time.Millisecond
)

// haFailbackState says whether the prompt should show, and if it should, whether going back is
// possible right now and, if not, why in plain words. Only the unit that is running the network
// and is not the Primary has anything to offer.
func (a *App) haFailbackState(s *haLocal) map[string]any {
	if s == nil || !s.Activated || s.PeerFP == "" || !a.haActive(s) || s.isPreferred() {
		return nil
	}
	out := map[string]any{"peer_addr": s.peerAddr(), "own_addr": s.ownAddr()}
	ps, err := a.peerStatus(s, 4*time.Second)
	switch {
	case err != nil:
		// The Primary is not back, so there is nothing to ask yet.
		return nil
	case ps.Active:
		out["ready"], out["why"] = false, "Both units say they are running the network. Check the cable between them; nothing is changed until that is sorted out."
	case ps.Version != a.Cfg.Version:
		out["ready"], out["why"] = false, "The other unit is on version "+ps.Version+" and this one is on "+a.Cfg.Version+". Update them to the same version first."
	case ps.Update != "" && ps.Update != "idle":
		out["ready"], out["why"] = false, "The other unit is busy updating. Wait for that to finish."
	case ps.Prefer != "" && ps.Prefer != string(s.preferRole()):
		out["ready"], out["why"] = false, "The two units disagree about which one is the Primary. Choose \"Make this unit the Primary\" to settle it."
	case ps.Seq != s.Seq:
		out["ready"], out["why"] = false, "The other unit is still copying the latest settings. This takes a minute."
	default:
		out["ready"] = true
	}
	return out
}

// haFailback hands the role to the Primary. It first sends the latest settings, then steps aside.
// The stepping aside happens in the background, because it ends the connection the person is using.
// If the Primary does not take over, this unit carries on by itself.
func (a *App) haFailback(user string) error {
	s := a.haLoad()
	if s == nil || !s.Activated {
		return errors.New("high availability is not running yet")
	}
	if !a.haActive(s) {
		return errors.New("this unit is not running the network, so there is nothing to give back")
	}
	if s.isPreferred() {
		return errors.New("this unit is the Primary already")
	}
	st := a.haFailbackState(s)
	if st == nil {
		return errors.New("the Primary unit cannot be reached right now")
	}
	if st["ready"] != true {
		return errors.New(fmt.Sprint(st["why"]))
	}
	// Send the newest settings first, so the Primary takes over with everything changed here.
	if err := a.haPush(true); err != nil {
		return errors.New("the latest settings could not be copied to the Primary first, so nothing was changed: " + err.Error())
	}
	s = a.haLoad()
	// Let the Primary finish loading what it was sent, so it is not busy when the role arrives.
	time.Sleep(failbackSettle)
	end := time.Now().Add(30 * time.Second)
	for {
		ps, err := a.peerStatus(s, 4*time.Second)
		if err != nil || ps.Seq != s.Seq {
			return errors.New("the Primary did not confirm the latest settings, so nothing was changed")
		}
		if !ps.Applying {
			break
		}
		if time.Now().After(end) {
			return errors.New("the Primary is still loading the latest settings, so nothing was changed; try again in a minute")
		}
		time.Sleep(failbackPoll)
	}
	go func() {
		a.event(user, "warning", "Giving the network back to the Primary unit", "This unit steps aside; there is a short pause.")
		if err := a.haInstall(s); err != nil {
			a.event(user, "error", "Failback did not start", err.Error())
			return
		}
		end := time.Now().Add(failbackWait)
		for time.Now().Before(end) {
			time.Sleep(failbackPoll)
			if ps, err := a.peerStatus(s, 4*time.Second); err == nil && ps.Active {
				a.event(user, "info", "The Primary unit is running the network again", "")
				return
			}
		}
		a.event(user, "error", "The Primary unit did not take over", "This unit is still running the network. Nothing was lost.")
	}()
	return nil
}

// haSetPriority writes the priority for the current choice of Primary and has the running program
// re-read it. It never restarts anything, so the shared addresses stay where they are.
func (a *App) haSetPriority(s *haLocal) error {
	if !s.Activated {
		return nil
	}
	if a.Net == nil {
		return errNoHelper
	}
	ka, err := ha.KeepalivedConfPrefer(s.local(), s.Role, s.ownAddr(), s.isPreferred())
	if err != nil {
		return err
	}
	return a.Net.HAApply(haPlanConfOnly(ka))
}

// haOnPrefer is the partner telling this unit which unit is the Primary.
func (a *App) haOnPrefer(role string) error {
	r := ha.Role(role)
	if r != ha.Primary && r != ha.Standby {
		return errors.New("unknown role")
	}
	s := a.haLoad()
	if s == nil {
		return errors.New("this unit is not paired")
	}
	n := *s
	n.Prefer = r
	if err := a.haSetPriority(&n); err != nil {
		return err
	}
	return a.haSave(&n)
}

// haPromote makes this unit the Primary on both units. The partner is told first; if it cannot be
// reached or refuses, nothing changes anywhere.
func (a *App) haPromote(user string) error {
	s := a.haLoad()
	if s == nil || s.PeerFP == "" {
		return errors.New("this unit is not paired")
	}
	if s.isPreferred() {
		return errors.New("this unit is the Primary already")
	}
	old := s.preferRole()
	call := func(role ha.Role) error {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_, err := ha.Call(ctx, s.peerHost(), s.Self, s.PeerFP, "POST", "/ha/prefer", nil, map[string]string{"X-GH-Prefer": string(role)}, "")
		return err
	}
	if err := call(s.Role); err != nil {
		return errors.New("the other unit could not be told, so nothing was changed: " + err.Error())
	}
	n := *s
	n.Prefer = s.Role
	if err := a.haSetPriority(&n); err != nil {
		call(old)
		return err
	}
	if err := a.haSave(&n); err != nil {
		call(old)
		return err
	}
	a.event(user, "warning", "This unit is now the Primary", "The other unit is the standby. Nothing moved.")
	return nil
}

func (a *App) registerFailbackRoutes() {
	s := a.API
	check := func(w http.ResponseWriter, c *api.Ctx, code string) bool {
		if a.Config.Setting("fw.stepup", "0") == "1" && c.User.TOTPEnabled {
			if err := a.Auth.VerifyStepUp(c.User.ID, code); err != nil {
				api.Error(w, http.StatusForbidden, "enter a current code from your authenticator")
				return false
			}
		}
		return true
	}
	s.HandleAdmin("POST /api/ha/failback", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			Code string `json:"code"`
		}
		if !api.Decode(w, r, &in) || !check(w, c, in.Code) {
			return
		}
		if err := a.haFailback(c.User.Username); err != nil {
			fail(w, err)
			return
		}
		ok(w)
	})
	s.HandleAdmin("POST /api/ha/promote", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			Code string `json:"code"`
		}
		if !api.Decode(w, r, &in) || !check(w, c, in.Code) {
			return
		}
		if err := a.haPromote(c.User.Username); err != nil {
			fail(w, err)
			return
		}
		ok(w)
	})
}

func haPlanConfOnly(ka string) netd.HAPlan {
	return netd.HAPlan{Enabled: true, Keepalived: ka, ConfOnly: true}
}

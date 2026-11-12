package app

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"nopewarden/internal/api"
)

// The steps of putting a change live, kept in words so the console can show them as a ticked list.
// The box reports the steps it does itself (checking, saving a way back, loading, the safety timer,
// and keeping or undoing); the browser reports the one only it can do (checking that it can still
// reach NopeWarden) and adds that to the list on screen.

// Step keys, in the order they are shown.
const (
	stepSafe  = "safe"
	stepBack  = "back"
	stepLoad  = "load"
	stepLists = "lists"
	stepTimer = "timer"
	stepReach = "reach"
	stepKeep  = "keep"
)

type applyStep struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Detail string `json:"detail"`
	State  string `json:"state"` // pending, active, done, failed
	Reason string `json:"reason,omitempty"`
}

// applyRun is one change going live.
type applyRun struct {
	ID       int64       `json:"id"`
	Kind     string      `json:"kind"` // fw or net
	Started  int64       `json:"started"`
	Ended    int64       `json:"ended,omitempty"`
	Deadline int64       `json:"deadline,omitempty"` // when the safety timer undoes it
	Outcome  string      `json:"outcome"`            // "" while going, then kept, undone or failed
	Failed   string      `json:"failed,omitempty"`   // key of the step that failed
	Steps    []applyStep `json:"steps"`
}

type applyProgress struct {
	mu  sync.Mutex
	seq int64
	run *applyRun
}

func applyStepList(kind string) []applyStep {
	reach := applyStep{Key: stepReach, Label: "Checking you can still reach NopeWarden", Detail: "This browser opens a fresh connection to make sure the change did not cut you off."}
	keep := applyStep{Key: stepKeep, Label: "Keeping the change, or undoing it", Detail: "Kept when the check passes. If it fails, or time runs out, the old settings come back."}
	timer := applyStep{Key: stepTimer, Label: "Starting the 60-second safety timer", Detail: "If nothing confirms the change in time, the old settings return on their own."}
	var first []applyStep
	if kind == "net" {
		first = []applyStep{
			{Key: stepSafe, Label: "Checking the change is safe", Detail: "The new network plan is checked for clashes, and for whether your own connection can still manage this device."},
			{Key: stepBack, Label: "Saving a way back", Detail: "A copy of the network settings running now is kept, so they can come back."},
			{Key: stepLoad, Label: "Writing the new network settings", Detail: "The new addresses, routes and DHCP settings are written out."},
			{Key: stepLists, Label: "Switching the network ports over", Detail: "The ports start using the new settings."},
		}
	} else {
		first = []applyStep{
			{Key: stepSafe, Label: "Checking the change is safe", Detail: "The new rules are tested, and NopeWarden makes sure your own connection can still manage this device."},
			{Key: stepBack, Label: "Saving a way back", Detail: "A copy of what is running now is kept, so the old settings can come back."},
			{Key: stepLoad, Label: "Loading the new rules", Detail: "The new rules are handed to the firewall in one go."},
			{Key: stepLists, Label: "Filling in the device and country lists", Detail: "The devices, groups and countries the rules refer to are loaded with them."},
		}
	}
	steps := append(first, timer, reach, keep)
	for i := range steps {
		steps[i].State = "pending"
	}
	return steps
}

// begin starts a new run, replacing the last one, and makes the first step active.
func (p *applyProgress) begin(kind string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.seq++
	p.run = &applyRun{ID: p.seq, Kind: kind, Started: time.Now().Unix(), Steps: applyStepList(kind)}
	p.run.Steps[0].State = "active"
}

// set changes one step. A step that is done or failed is left alone.
func (p *applyProgress) set(key, state, reason string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.run == nil {
		return
	}
	for i := range p.run.Steps {
		s := &p.run.Steps[i]
		if s.Key != key {
			continue
		}
		if s.State == "done" || s.State == "failed" {
			return
		}
		s.State, s.Reason = state, reason
		if state == "failed" {
			p.run.Failed = key
		}
	}
}

// doneTo marks every step up to and including key as done.
func (p *applyProgress) doneTo(key string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.run == nil {
		return
	}
	for i := range p.run.Steps {
		s := &p.run.Steps[i]
		if s.State != "failed" {
			s.State, s.Reason = "done", ""
		}
		if s.Key == key {
			return
		}
	}
}

// activate makes the named steps active (those not yet finished).
func (p *applyProgress) activate(keys ...string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.run == nil {
		return
	}
	for i := range p.run.Steps {
		for _, k := range keys {
			if p.run.Steps[i].Key == k && p.run.Steps[i].State == "pending" {
				p.run.Steps[i].State = "active"
			}
		}
	}
}

// fail marks the step as failed, with the reason in words, and ends the run.
func (p *applyProgress) fail(key, reason string) {
	p.set(key, "failed", reason)
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.run != nil && p.run.Outcome == "" {
		p.run.Outcome, p.run.Ended = "failed", time.Now().Unix()
		for i := range p.run.Steps {
			if p.run.Steps[i].State == "active" {
				p.run.Steps[i].State = "pending"
			}
		}
	}
}

// armed records the safety timer: the change is now live and on trial until deadline.
func (p *applyProgress) armed(deadline time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.run == nil {
		return
	}
	p.run.Deadline = deadline.Unix()
	for i := range p.run.Steps {
		if p.run.Steps[i].State != "failed" && p.run.Steps[i].Key != stepReach && p.run.Steps[i].Key != stepKeep {
			p.run.Steps[i].State, p.run.Steps[i].Reason = "done", ""
		}
	}
	p.run.Steps[len(p.run.Steps)-2].State = "active" // reach: the browser reports it
}

// finish ends a run that was going: kept or undone. The "reach" step is settled as well.
func (p *applyProgress) finish(outcome string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.finishLocked(outcome)
}

func (p *applyProgress) finishLocked(outcome string) {
	r := p.run
	if r == nil || r.Outcome != "" {
		return
	}
	r.Outcome, r.Ended = outcome, time.Now().Unix()
	for i := range r.Steps {
		s := &r.Steps[i]
		switch {
		case outcome == "kept" && s.State != "failed":
			s.State, s.Reason = "done", ""
		case outcome == "undone" && s.Key == stepReach && s.State != "done":
			s.State, s.Reason = "failed", "This browser could not confirm in time."
		case outcome == "undone" && s.Key == stepKeep:
			s.State, s.Reason = "done", "The change was undone and the previous settings are back."
		}
	}
}

// snapshot returns a copy, settling a run whose safety timer ran out without anyone confirming.
func (p *applyProgress) snapshot() *applyRun {
	p.mu.Lock()
	defer p.mu.Unlock()
	r := p.run
	if r == nil {
		return nil
	}
	if r.Outcome == "" && r.Deadline > 0 && time.Now().Unix() > r.Deadline+3 {
		p.finishLocked("undone")
	}
	cp := *r
	cp.Steps = append([]applyStep(nil), r.Steps...)
	return &cp
}

// failedStep says which step an error from the helper belongs to.
func failedStepFor(err error) string {
	if err != nil && strings.Contains(err.Error(), "safety timer") {
		return stepTimer
	}
	return stepLoad
}

func (a *App) registerApplyProgress() {
	a.API.Handle("GET /api/apply/progress", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		api.JSON(w, 200, map[string]any{"run": a.prog.snapshot()})
	})
}

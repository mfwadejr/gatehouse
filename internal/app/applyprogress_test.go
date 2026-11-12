package app

import (
	"errors"
	"testing"
	"time"
)

func stepState(r *applyRun, key string) string {
	for _, s := range r.Steps {
		if s.Key == key {
			return s.State
		}
	}
	return "?"
}

// A change that goes through: the box's steps tick, the browser's check is waited for, and keeping
// it finishes the list.
func TestApplyStepsGoThrough(t *testing.T) {
	var p applyProgress
	p.begin("fw")
	r := p.snapshot()
	if len(r.Steps) != 7 || stepState(r, stepSafe) != "active" || stepState(r, stepKeep) != "pending" {
		t.Fatalf("start: %+v", r.Steps)
	}
	p.set(stepSafe, "done", "")
	p.set(stepBack, "done", "")
	p.activate(stepLoad, stepLists, stepTimer)
	if r = p.snapshot(); stepState(r, stepLoad) != "active" || stepState(r, stepTimer) != "active" {
		t.Fatalf("loading: %+v", r.Steps)
	}
	p.armed(time.Now().Add(confirmWindow))
	r = p.snapshot()
	for _, k := range []string{stepSafe, stepBack, stepLoad, stepLists, stepTimer} {
		if stepState(r, k) != "done" {
			t.Fatalf("%s should be done: %+v", k, r.Steps)
		}
	}
	if stepState(r, stepReach) != "active" || r.Outcome != "" {
		t.Fatalf("waiting for the browser: %+v", r)
	}
	p.finish("kept")
	r = p.snapshot()
	if r.Outcome != "kept" || stepState(r, stepReach) != "done" || stepState(r, stepKeep) != "done" {
		t.Fatalf("kept: %+v", r)
	}
}

// A failure names the step and says why; later steps stay waiting and nothing is marked done after it.
func TestApplyStepFailureNamesTheStep(t *testing.T) {
	var p applyProgress
	p.begin("fw")
	p.set(stepSafe, "done", "")
	p.set(stepBack, "done", "")
	p.activate(stepLoad, stepLists, stepTimer)
	err := errors.New("could not start the safety timer: no such unit")
	p.fail(failedStepFor(err), err.Error())
	r := p.snapshot()
	if r.Outcome != "failed" || r.Failed != stepTimer || stepState(r, stepTimer) != "failed" {
		t.Fatalf("%+v", r)
	}
	if stepState(r, stepLoad) == "active" || stepState(r, stepReach) != "pending" {
		t.Fatalf("steps after a failure should wait: %+v", r.Steps)
	}
	if failedStepFor(errors.New("nft: syntax error")) != stepLoad {
		t.Fatal("a load error belongs to loading the rules")
	}
}

// When nobody keeps the change before the timer runs out, the list says it was undone.
func TestApplyStepsUndoneWhenTimeRunsOut(t *testing.T) {
	var p applyProgress
	p.begin("net")
	p.armed(time.Now().Add(-10 * time.Second))
	r := p.snapshot()
	if r.Outcome != "undone" || stepState(r, stepReach) != "failed" {
		t.Fatalf("%+v", r)
	}
	if r.Steps[2].Label != "Writing the new network settings" {
		t.Fatalf("network changes have their own steps: %+v", r.Steps)
	}
}

package update

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Env is what Apply needs from the machine, so it can be tested without changing one.
type Env struct {
	StateDir string // where backups and results are kept
	Root     string // prefix for every destination, empty on a real machine
	Run      func(name string, args ...string) error
	Health   func() bool // true when the console answers
	Sleep    func(time.Duration)
	Now      func() time.Time
	// Services restarted, in order, after files are replaced.
	Services []string
	// System installs the Debian packages a release needs and the operating system's own updates.
	// It runs first; if it fails, nothing of Gatehouse has been touched. Nil skips the step.
	System func(required []string) (SystemResult, error)
	// How long the new version gets to start answering, and the old one after a rollback.
	Wait, RollbackWait time.Duration
}

// SystemResult is what the operating system step did.
type SystemResult struct {
	Packages     int    `json:"packages"`                // installed or upgraded
	Skipped      string `json:"skipped,omitempty"`       // why the system updates were left out
	RebootNeeded bool   `json:"reboot_needed,omitempty"` // a restart is needed to finish
}

// Plan says which parts an update run covers, so the Updates page can show the right steps even
// after the console has restarted.
type Plan struct {
	Gatehouse bool `json:"gatehouse"`
	System    bool `json:"system"`
}

// Result is what happened, kept for the Updates page and the log.
type Result struct {
	OK         bool   `json:"ok"`
	From       string `json:"from"`
	To         string `json:"to"`
	At         int64  `json:"at"`
	RolledBack bool   `json:"rolled_back"`
	Error      string `json:"error,omitempty"`
	// System is set when the run covered the operating system too.
	System *SystemResult `json:"system,omitempty"`
}

func (e Env) dest(d string) string { return filepath.Join(e.Root, d) }

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".new"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	if err := os.Chmod(tmp, mode); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

func (e Env) state(st string) {
	os.MkdirAll(e.StateDir, 0o755)
	os.WriteFile(filepath.Join(e.StateDir, "state"), []byte(st+"\n"), 0o644)
}

func (e Env) finish(r Result) Result {
	r.At = e.Now().Unix()
	b, _ := json.Marshal(r)
	os.MkdirAll(e.StateDir, 0o755)
	os.WriteFile(filepath.Join(e.StateDir, "result.json"), b, 0o644)
	e.state("idle")
	return r
}

func (e Env) restart() error {
	if err := e.Run("systemctl", "daemon-reload"); err != nil {
		return err
	}
	var first error
	for _, s := range e.Services {
		if err := e.Run("systemctl", "restart", s); err != nil && first == nil {
			first = fmt.Errorf("%s did not restart: %v", s, err)
		}
	}
	return first
}

func (e Env) waitHealthy(limit time.Duration) bool {
	deadline := e.Now().Add(limit)
	for {
		if e.Health() {
			return true
		}
		if !e.Now().Before(deadline) {
			return false
		}
		e.Sleep(3 * time.Second)
	}
}

// Apply installs a checked package. If the console does not answer afterwards, it puts the
// previous version back. It returns what happened; it never leaves the machine half updated.
//
// The operating system goes first: the packages the release names, then the Debian updates. A
// failure there stops the run with Gatehouse untouched. p may be nil for a run that only updates
// the operating system.
func Apply(e Env, p *Package, from string) Result {
	res := Result{From: from, To: from}
	if p != nil {
		res.To = p.Manifest.Version
	}
	if e.System != nil {
		e.state("system")
		var req []string
		if p != nil {
			req = p.Manifest.Packages
		}
		sr, err := e.System(req)
		res.System = &sr
		if err != nil {
			res.Error = "the system updates did not install, so nothing was changed: " + err.Error()
			return e.finish(res)
		}
	}
	if p == nil {
		res.OK = true
		return e.finish(res)
	}
	e.state("installing")
	backup := filepath.Join(e.StateDir, "backup")
	os.RemoveAll(backup)
	if err := os.MkdirAll(backup, 0o755); err != nil {
		res.Error = "could not make a backup: " + err.Error()
		return e.finish(res)
	}
	type saved struct {
		dest    string
		existed bool
	}
	var done []saved
	undo := func() {
		for _, s := range done {
			if s.existed {
				if b, err := os.ReadFile(filepath.Join(backup, strings.ReplaceAll(s.dest, "/", "_"))); err == nil {
					mode := os.FileMode(0o644)
					if fi, err := os.Stat(e.dest(s.dest)); err == nil {
						mode = fi.Mode().Perm()
					}
					if s.dest == BinaryDest {
						mode = 0o755
					}
					writeAtomic(e.dest(s.dest), b, mode)
				}
			} else {
				os.Remove(e.dest(s.dest))
			}
		}
	}
	for _, f := range p.Manifest.Files {
		cur, err := os.ReadFile(e.dest(f.Dest))
		existed := err == nil
		if existed {
			if err := os.WriteFile(filepath.Join(backup, strings.ReplaceAll(f.Dest, "/", "_")), cur, 0o600); err != nil {
				res.Error = "could not make a backup: " + err.Error()
				return e.finish(res)
			}
		}
		done = append(done, saved{f.Dest, existed})
		if err := writeAtomic(e.dest(f.Dest), p.Files[filepath.ToSlash(filepath.Clean(f.Path))], os.FileMode(f.Mode)); err != nil {
			undo()
			res.Error = "could not install " + f.Dest + ": " + err.Error()
			res.RolledBack = true
			return e.finish(res)
		}
	}
	oldRel, _ := os.ReadFile(e.dest("/etc/gatehouse-release"))
	writeAtomic(e.dest("/etc/gatehouse-release"), []byte(p.Manifest.Version+"\n"), 0o644)

	e.state("restarting")
	err := e.restart()
	e.state("checking")
	if err == nil && e.waitHealthy(e.Wait) {
		res.OK = true
		return e.finish(res)
	}
	// The new version did not come up: go back.
	e.state("rolling-back")
	undo()
	if len(oldRel) > 0 {
		writeAtomic(e.dest("/etc/gatehouse-release"), oldRel, 0o644)
	}
	e.restart()
	res.RolledBack = true
	res.Error = "the new version did not start answering, so the previous version was put back"
	if err != nil {
		res.Error = "the new version could not be started (" + err.Error() + "), so the previous version was put back"
	}
	if res.System != nil && res.System.Packages > 0 {
		res.Error += ". The system updates stay installed"
	}
	if !e.waitHealthy(e.RollbackWait) {
		res.Error += ". The console is not answering yet; press Alt+F2 on the machine and choose 4 to see why"
	}
	return e.finish(res)
}

// WritePlan records what the current run covers.
func WritePlan(stateDir string, p Plan) {
	os.MkdirAll(stateDir, 0o755)
	b, _ := json.Marshal(p)
	os.WriteFile(filepath.Join(stateDir, "plan.json"), b, 0o644)
}

// ReadPlan returns what the current or last run covered.
func ReadPlan(stateDir string) (Plan, bool) {
	var p Plan
	b, err := os.ReadFile(filepath.Join(stateDir, "plan.json"))
	if err != nil || json.Unmarshal(b, &p) != nil {
		return p, false
	}
	return p, true
}

// ReadResult returns the last result, if there is one.
func ReadResult(stateDir string) (Result, bool) {
	var r Result
	b, err := os.ReadFile(filepath.Join(stateDir, "result.json"))
	if err != nil || json.Unmarshal(b, &r) != nil {
		return r, false
	}
	return r, true
}

// ReadState returns what an update in progress is doing, or "idle".
func ReadState(stateDir string) string {
	b, err := os.ReadFile(filepath.Join(stateDir, "state"))
	if err != nil {
		return "idle"
	}
	return strings.TrimSpace(string(b))
}

var ErrBusy = errors.New("an update is already running")

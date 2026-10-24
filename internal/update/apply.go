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
	// How long the new version gets to start answering, and the old one after a rollback.
	Wait, RollbackWait time.Duration
}

// Result is what happened, kept for the Updates page and the log.
type Result struct {
	OK         bool   `json:"ok"`
	From       string `json:"from"`
	To         string `json:"to"`
	At         int64  `json:"at"`
	RolledBack bool   `json:"rolled_back"`
	Error      string `json:"error,omitempty"`
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
func Apply(e Env, p *Package, from string) Result {
	res := Result{From: from, To: p.Manifest.Version}
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
	if !e.waitHealthy(e.RollbackWait) {
		res.Error += ". The console is not answering yet; press Alt+F2 on the machine and choose 4 to see why"
	}
	return e.finish(res)
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

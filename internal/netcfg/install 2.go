package netcfg

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Installer writes generated configuration into the system and can put the previous
// configuration back. Like the firewall, every change starts a countdown; unless it is
// confirmed in time an independent process restores the previous files.
type Installer struct {
	// Directories; production values are the defaults in NewInstaller.
	NetworkdDir string // /etc/systemd/network
	PPPDir      string // /etc/ppp/peers
	PPPSecrets  string // /etc/ppp/chap-secrets (pap-secrets is written beside it)
	SysctlFile  string // /etc/sysctl.d/90-gatehouse.conf
	StateDir    string // /var/lib/gatehouse/net

	// Hooks for the parts that talk to systemd; replaced in tests.
	Reload   func() error
	Services func(start, stop []string) error
	Sysctl   func(file string) error
	Watchdog func(dir string, deadline time.Time, token string) error
	Now      func() time.Time

	mu sync.Mutex
}

const filePrefix = "gatehouse-"

func NewInstaller() *Installer {
	return &Installer{
		NetworkdDir: "/etc/systemd/network", PPPDir: "/etc/ppp/peers", PPPSecrets: "/etc/ppp/chap-secrets",
		SysctlFile: "/etc/sysctl.d/90-gatehouse.conf", StateDir: "/var/lib/gatehouse/net",
		Reload: func() error {
			if out, err := exec.Command("networkctl", "reload").CombinedOutput(); err != nil {
				return fmt.Errorf("networkctl reload: %s", strings.TrimSpace(string(out)))
			}
			exec.Command("networkctl", "reconfigure").Run() // nudge every link
			return nil
		},
		Services: func(start, stop []string) error {
			for _, u := range stop {
				exec.Command("systemctl", "disable", "--now", "gatehouse-ppp@"+u+".service").Run()
			}
			for _, u := range start {
				if out, err := exec.Command("systemctl", "enable", "--now", "gatehouse-ppp@"+u+".service").CombinedOutput(); err != nil {
					return fmt.Errorf("start %s: %s", u, strings.TrimSpace(string(out)))
				}
			}
			return nil
		},
		Sysctl: func(file string) error {
			return exec.Command("sysctl", "-p", file).Run()
		},
		Now: time.Now, Watchdog: StartWatchdogProcess,
	}
}

type pendingState struct {
	Token    string `json:"token"`
	Deadline int64  `json:"deadline"`
}

// ErrPending means a change is already waiting for confirmation.
var ErrPending = errors.New("a previous network change is still waiting to be confirmed")

func (in *Installer) path(n string) string { return filepath.Join(in.StateDir, n) }

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".gh-tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// current reads every file Gatehouse owns, keyed by absolute path.
func (in *Installer) current() (map[string]string, error) {
	out := map[string]string{}
	add := func(dir, pattern string) error {
		m, err := filepath.Glob(filepath.Join(dir, pattern))
		if err != nil {
			return err
		}
		for _, f := range m {
			b, err := os.ReadFile(f)
			if err != nil {
				return err
			}
			out[f] = string(b)
		}
		return nil
	}
	if err := add(in.NetworkdDir, filePrefix+"*"); err != nil {
		return nil, err
	}
	if err := add(in.PPPDir, "gh-*"); err != nil {
		return nil, err
	}
	for _, f := range []string{in.SysctlFile, in.PPPSecrets, in.papSecrets(), in.path("ppp-units")} {
		if b, err := os.ReadFile(f); err == nil {
			out[f] = string(b)
		}
	}
	return out, nil
}

func (in *Installer) papSecrets() string {
	return filepath.Join(filepath.Dir(in.PPPSecrets), "pap-secrets")
}

const secretsMarker = "# gatehouse-managed"

// desired maps absolute path to the new content for an Output.
func (in *Installer) desired(o *Output) map[string]string {
	want := map[string]string{}
	for n, b := range o.Networkd {
		want[filepath.Join(in.NetworkdDir, n)] = b
	}
	for n, b := range o.PPPPeers {
		want[filepath.Join(in.PPPDir, n)] = b
	}
	want[in.SysctlFile] = o.Sysctl
	want[in.path("ppp-units")] = strings.Join(o.PPPUnits, "\n")
	return want
}

// mergeSecrets keeps any lines the administrator added outside our marked block.
func mergeSecrets(existing, ours string) string {
	var keep []string
	skip := false
	for _, l := range strings.Split(existing, "\n") {
		switch {
		case l == secretsMarker+" begin":
			skip = true
		case l == secretsMarker+" end":
			skip = false
		case !skip:
			keep = append(keep, l)
		}
	}
	base := strings.TrimRight(strings.Join(keep, "\n"), "\n")
	if ours == "" {
		if base == "" {
			return ""
		}
		return base + "\n"
	}
	if base != "" {
		base += "\n"
	}
	return base + secretsMarker + " begin\n" + ours + secretsMarker + " end\n"
}

func (in *Installer) readPending() (*pendingState, error) {
	b, err := os.ReadFile(in.path("pending.json"))
	if err != nil {
		return nil, err
	}
	var p pendingState
	return &p, json.Unmarshal(b, &p)
}

// Pending reports a change waiting for confirmation.
func (in *Installer) Pending() (string, time.Time, bool) {
	in.mu.Lock()
	defer in.mu.Unlock()
	p, err := in.readPending()
	if err != nil {
		return "", time.Time{}, false
	}
	return p.Token, time.Unix(p.Deadline, 0), true
}

// Apply installs the output and starts the confirmation window.
func (in *Installer) Apply(o *Output, window time.Duration) (string, time.Time, error) {
	in.mu.Lock()
	defer in.mu.Unlock()
	if p, err := in.readPending(); err == nil && in.Now().Unix() < p.Deadline {
		return "", time.Time{}, ErrPending
	}
	if err := os.MkdirAll(in.StateDir, 0o700); err != nil {
		return "", time.Time{}, err
	}
	// Remember exactly what was there, so rollback is a byte-for-byte restore.
	prev, err := in.current()
	if err != nil {
		return "", time.Time{}, err
	}
	pj, _ := json.Marshal(prev)
	if err := writeAtomic(in.path("prev.json"), pj, 0o600); err != nil {
		return "", time.Time{}, err
	}
	b := make([]byte, 16)
	rand.Read(b)
	token := hex.EncodeToString(b)
	deadline := in.Now().Add(window)
	st, _ := json.Marshal(pendingState{Token: token, Deadline: deadline.Unix()})
	if err := writeAtomic(in.path("pending.json"), st, 0o600); err != nil {
		return "", time.Time{}, err
	}
	if in.Watchdog != nil {
		if err := in.Watchdog(in.StateDir, deadline, token); err != nil {
			os.Remove(in.path("pending.json"))
			return "", time.Time{}, fmt.Errorf("could not start the safety timer: %w", err)
		}
	}
	if err := in.install(o); err != nil {
		in.restoreLocked()
		return "", time.Time{}, err
	}
	return token, deadline, nil
}

func (in *Installer) install(o *Output) error {
	want := in.desired(o)
	have, err := in.current()
	if err != nil {
		return err
	}
	for path := range have {
		if _, keep := want[path]; keep {
			continue
		}
		if path == in.PPPSecrets || path == in.papSecrets() {
			continue
		}
		os.Remove(path)
	}
	for path, body := range want {
		mode := os.FileMode(0o644)
		if err := writeAtomic(path, []byte(body), mode); err != nil {
			return err
		}
	}
	for _, f := range []string{in.PPPSecrets, in.papSecrets()} {
		old, _ := os.ReadFile(f)
		if err := writeAtomic(f, []byte(mergeSecrets(string(old), o.Secrets)), 0o600); err != nil {
			return err
		}
	}
	return in.activate(o.PPPUnits)
}

func (in *Installer) activate(units []string) error {
	var prevUnits []string
	if b, err := os.ReadFile(in.path("ppp-units-prev")); err == nil {
		prevUnits = strings.Fields(string(b))
	}
	var stop []string
	for _, p := range prevUnits {
		found := false
		for _, u := range units {
			found = found || u == p
		}
		if !found {
			stop = append(stop, p)
		}
	}
	sort.Strings(units)
	os.WriteFile(in.path("ppp-units-prev"), []byte(strings.Join(units, "\n")), 0o600)
	if in.Sysctl != nil {
		in.Sysctl(in.SysctlFile)
	}
	if in.Reload != nil {
		if err := in.Reload(); err != nil {
			return err
		}
	}
	if in.Services != nil {
		return in.Services(units, stop)
	}
	return nil
}

// Confirm keeps the change.
func (in *Installer) Confirm(token string) error {
	in.mu.Lock()
	defer in.mu.Unlock()
	p, err := in.readPending()
	if err != nil {
		return errors.New("there is nothing waiting to be confirmed")
	}
	if p.Token != token {
		return errors.New("that confirmation does not match the pending change")
	}
	if in.Now().Unix() > p.Deadline {
		return errors.New("the confirmation window has ended and the change was undone")
	}
	os.Remove(in.path("pending.json"))
	os.Remove(in.path("prev.json"))
	return nil
}

// Rollback restores the previous files and reloads.
func (in *Installer) Rollback() error {
	in.mu.Lock()
	defer in.mu.Unlock()
	return in.restoreLocked()
}

func (in *Installer) restoreLocked() error {
	b, err := os.ReadFile(in.path("prev.json"))
	if err != nil {
		os.Remove(in.path("pending.json"))
		return nil // nothing to restore
	}
	var prev map[string]string
	if err := json.Unmarshal(b, &prev); err != nil {
		return err
	}
	have, _ := in.current()
	for path := range have {
		if _, ok := prev[path]; !ok && path != in.PPPSecrets && path != in.papSecrets() {
			os.Remove(path)
		}
	}
	var units []string
	for path, body := range prev {
		mode := os.FileMode(0o644)
		if path == in.PPPSecrets || path == in.papSecrets() {
			mode = 0o600
		}
		if path == in.path("ppp-units") {
			units = strings.Fields(body)
			continue
		}
		if err := writeAtomic(path, []byte(body), mode); err != nil {
			return err
		}
	}
	err = in.activate(units)
	os.Remove(in.path("pending.json"))
	os.Remove(in.path("prev.json"))
	return err
}

// RunWatchdog waits for the deadline and rolls back if the change was never confirmed.
func RunWatchdog(in *Installer, deadline time.Time, token string) error {
	if d := time.Until(deadline); d > 0 {
		time.Sleep(d + 500*time.Millisecond)
	}
	in.mu.Lock()
	p, err := in.readPending()
	pending := err == nil && p.Token == token
	in.mu.Unlock()
	if !pending {
		return nil
	}
	return in.Rollback()
}

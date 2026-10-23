package fw

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Applier loads rulesets with a safety net: every change starts a countdown, and unless it
// is confirmed in time an independent process puts the previous ruleset back.
//
// State lives in Dir:
//
//	lastgood.nft  the last ruleset that was confirmed (or the baseline on a fresh system)
//	pending.nft   the ruleset waiting for confirmation
//	pending.json  token and deadline of the change waiting for confirmation
type Applier struct {
	Dir string
	NFT string // path of the nft binary
	// Netns runs nft inside a named network namespace; used by tests only.
	Netns string
	// Baseline is loaded when there is no last known good ruleset. It must keep management reachable.
	Baseline string
	// Watchdog starts the independent rollback process. Production uses StartWatchdogProcess.
	Watchdog func(dir string, deadline time.Time, token string) error
	Now      func() time.Time

	mu sync.Mutex
}

type pendingState struct {
	Token    string `json:"token"`
	Deadline int64  `json:"deadline"`
	Author   string `json:"author"`
}

// ErrPending means a change is already waiting for confirmation.
var ErrPending = errors.New("a previous change is still waiting to be confirmed")

func NewApplier(dir string) *Applier {
	return &Applier{Dir: dir, NFT: "nft", Now: time.Now, Watchdog: StartWatchdogProcess}
}

func (a *Applier) path(n string) string { return filepath.Join(a.Dir, n) }

func (a *Applier) nftCmd(args ...string) *exec.Cmd {
	if a.Netns != "" {
		return exec.Command("ip", append([]string{"netns", "exec", a.Netns, a.NFT}, args...)...)
	}
	return exec.Command(a.NFT, args...)
}

func (a *Applier) run(script string, check bool) error {
	args := []string{"-f", "-"}
	if check {
		args = []string{"-c", "-f", "-"}
	}
	c := a.nftCmd(args...)
	c.Stdin = strings.NewReader(script)
	var out bytes.Buffer
	c.Stdout, c.Stderr = &out, &out
	if err := c.Run(); err != nil {
		return fmt.Errorf("nft: %s", strings.TrimSpace(out.String()))
	}
	return nil
}

// Check asks nft to parse the script without loading it.
func (a *Applier) Check(script string) error { return a.run(script, true) }

func writeFileAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (a *Applier) readPending() (*pendingState, error) {
	b, err := os.ReadFile(a.path("pending.json"))
	if err != nil {
		return nil, err
	}
	var p pendingState
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// Pending reports the change waiting for confirmation, if any.
func (a *Applier) Pending() (token string, deadline time.Time, ok bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	p, err := a.readPending()
	if err != nil {
		return "", time.Time{}, false
	}
	return p.Token, time.Unix(p.Deadline, 0), true
}

// Apply loads script and starts the confirmation window. The caller must call Confirm
// with the returned token, on a fresh connection, before the window ends.
func (a *Applier) Apply(script string, window time.Duration, author string) (token string, deadline time.Time, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := os.MkdirAll(a.Dir, 0o700); err != nil {
		return "", time.Time{}, err
	}
	if p, err := a.readPending(); err == nil && a.Now().Unix() < p.Deadline {
		return "", time.Time{}, ErrPending
	}
	if err := a.run(script, true); err != nil {
		return "", time.Time{}, err
	}
	// Make sure there is something to go back to before anything changes.
	if _, err := os.Stat(a.path("lastgood.nft")); err != nil {
		base := a.Baseline
		if base == "" {
			base = emptyBaseline
		}
		if err := writeFileAtomic(a.path("lastgood.nft"), []byte(base)); err != nil {
			return "", time.Time{}, err
		}
	}
	b := make([]byte, 16)
	rand.Read(b)
	token = hex.EncodeToString(b)
	deadline = a.Now().Add(window)
	st, _ := json.Marshal(pendingState{Token: token, Deadline: deadline.Unix(), Author: author})
	if err := writeFileAtomic(a.path("pending.nft"), []byte(script)); err != nil {
		return "", time.Time{}, err
	}
	if err := writeFileAtomic(a.path("pending.json"), st); err != nil {
		return "", time.Time{}, err
	}
	// Start the watchdog before loading, so a crash right after the load still rolls back.
	if a.Watchdog != nil {
		if err := a.Watchdog(a.Dir, deadline, token); err != nil {
			os.Remove(a.path("pending.json"))
			return "", time.Time{}, fmt.Errorf("could not start the safety timer: %w", err)
		}
	}
	if err := a.run(script, false); err != nil {
		os.Remove(a.path("pending.json"))
		return "", time.Time{}, err
	}
	return token, deadline, nil
}

// Confirm makes the pending change permanent.
func (a *Applier) Confirm(token string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	p, err := a.readPending()
	if err != nil {
		return errors.New("there is nothing waiting to be confirmed")
	}
	if p.Token != token {
		return errors.New("that confirmation does not match the pending change")
	}
	if a.Now().Unix() > p.Deadline {
		return errors.New("the confirmation window has ended and the change was undone")
	}
	script, err := os.ReadFile(a.path("pending.nft"))
	if err != nil {
		return err
	}
	if err := writeFileAtomic(a.path("lastgood.nft"), script); err != nil {
		return err
	}
	os.Remove(a.path("pending.json"))
	os.Remove(a.path("pending.nft"))
	return nil
}

// Rollback restores the last known good ruleset and clears any pending change.
func (a *Applier) Rollback() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.rollbackLocked()
}

func (a *Applier) rollbackLocked() error {
	script, err := os.ReadFile(a.path("lastgood.nft"))
	if err != nil {
		script = []byte(a.baseline())
	}
	if err := a.run(string(script), false); err != nil {
		return err
	}
	os.Remove(a.path("pending.json"))
	os.Remove(a.path("pending.nft"))
	return nil
}

func (a *Applier) baseline() string {
	if a.Baseline != "" {
		return a.Baseline
	}
	return emptyBaseline
}

// Boot is called before the console starts listening: it loads the last known good ruleset,
// discarding any change that was never confirmed.
func (a *Applier) Boot() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := os.MkdirAll(a.Dir, 0o700); err != nil {
		return err
	}
	return a.rollbackLocked()
}

// emptyBaseline only keeps the machine itself reachable on every port, used when nothing else is known.
const emptyBaseline = `add table inet gatehouse
delete table inet gatehouse
table inet gatehouse {
	chain input {
		type filter hook input priority filter; policy accept;
	}
}
`

// FirstBootBaseline protects a freshly installed machine until the first ruleset is confirmed:
// nothing is forwarded, and the machine itself answers only replies, ping, DHCP client traffic
// and the console, so setup can proceed from the network it was plugged into.
const FirstBootBaseline = `add table inet gatehouse
delete table inet gatehouse
table inet gatehouse {
	chain input {
		type filter hook input priority filter; policy drop;
		iif "lo" accept
		ct state established,related accept
		ct state invalid drop
		ip protocol icmp icmp type { echo-request, destination-unreachable, time-exceeded } limit rate 10/second accept
		meta l4proto ipv6-icmp accept
		udp sport 67 udp dport 68 accept
		udp sport 547 udp dport 546 accept
		tcp dport 443 ct state new limit rate 30/second accept
	}
	chain forward {
		type filter hook forward priority filter; policy drop;
	}
}
`

// RunWatchdog is the independent rollback process: wait for the deadline and, if the same
// change is still pending, undo it. It shares nothing with the daemon except the state directory.
func RunWatchdog(dir string, deadline time.Time, token string, a *Applier) error {
	if d := time.Until(deadline); d > 0 {
		time.Sleep(d + 500*time.Millisecond)
	}
	if a == nil {
		a = NewApplier(dir)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	p, err := a.readPending()
	if err != nil || p.Token != token {
		return nil // confirmed or superseded
	}
	return a.rollbackLocked()
}

// StartWatchdogProcess launches "gatehouse rollback" detached from the caller, so it survives
// the daemon and the helper being stopped or hanging.
func StartWatchdogProcess(dir string, deadline time.Time, token string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if _, err := exec.LookPath("systemd-run"); err == nil && os.Getenv("GATEHOUSE_NO_SYSTEMD") == "" {
		unit := "gatehouse-rollback"
		exec.Command("systemctl", "stop", unit+".service").Run()
		out, err := exec.Command("systemd-run", "--unit="+unit, "--collect",
			fmt.Sprintf("--on-active=%ds", int(time.Until(deadline).Seconds())+1),
			self, "rollback", "-dir", dir, "-token", token, "-deadline", fmt.Sprint(deadline.Unix())).CombinedOutput()
		if err == nil {
			return nil
		}
		_ = out // fall through to a plain detached process
	}
	c := exec.Command(self, "rollback", "-dir", dir, "-token", token, "-deadline", fmt.Sprint(deadline.Unix()))
	c.SysProcAttr = detachAttr()
	return c.Start()
}

// Controller is what the daemon needs from the privileged side. Applier implements it in
// process; the netd client implements it over a socket.
type Controller interface {
	Check(script string) error
	Apply(script string, window time.Duration, author string) (token string, deadline time.Time, err error)
	Confirm(token string) error
	Rollback() error
	Pending() (token string, deadline time.Time, ok bool)
}

var _ Controller = (*Applier)(nil)

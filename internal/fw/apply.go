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
	"strconv"
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
	var c *exec.Cmd
	if a.Netns != "" {
		c = exec.Command("ip", append([]string{"netns", "exec", a.Netns, a.NFT}, args...)...)
	} else {
		c = exec.Command(a.NFT, args...)
	}
	// The kernel checks schedules against UTC. Running nft in UTC too means the times written into
	// the ruleset are taken exactly as given and not converted a second time.
	c.Env = append(os.Environ(), "TZ=UTC")
	return c
}

// LoadSets fills the dynamic sets (devices, domains, countries) that exist in the loaded ruleset.
// Sets that are not there are skipped, so a change not yet applied cannot make this fail.
func (a *Applier) LoadSets(fills map[string]SetFill) error {
	if len(fills) == 0 {
		return nil
	}
	have, err := a.setNames()
	if err != nil {
		return err
	}
	script, err := BuildSetScript(fills, have)
	if err != nil || strings.TrimSpace(script) == "" {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.run(script, false)
}

// setNames lists the sets in the loaded gatehouse table.
func (a *Applier) setNames() (map[string]bool, error) {
	c := a.nftCmd("-j", "list", "sets", "table", "inet", tableName)
	out, err := c.Output()
	if err != nil {
		return map[string]bool{}, nil // no table loaded yet
	}
	var doc struct {
		Nftables []struct {
			Set *struct {
				Name string `json:"name"`
			} `json:"set"`
		} `json:"nftables"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		return nil, err
	}
	names := map[string]bool{}
	for _, it := range doc.Nftables {
		if it.Set != nil {
			names[it.Set.Name] = true
		}
	}
	return names, nil
}

// Counter is how often a rule has matched since the ruleset was loaded.
type Counter struct {
	Packets uint64 `json:"packets"`
	Bytes   uint64 `json:"bytes"`
}

// Counters sums the match counters of every rule by its comment (the rule's tag).
func (a *Applier) Counters() (map[string]Counter, error) {
	out, err := a.nftCmd("-j", "list", "table", "inet", tableName).Output()
	if err != nil {
		return map[string]Counter{}, nil
	}
	var doc struct {
		Nftables []struct {
			Rule *struct {
				Comment string                       `json:"comment"`
				Expr    []map[string]json.RawMessage `json:"expr"`
			} `json:"rule"`
			Set *struct {
				Name string          `json:"name"`
				Elem json.RawMessage `json:"elem"`
			} `json:"set"`
		} `json:"nftables"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		return nil, err
	}
	res := map[string]Counter{}
	for _, it := range doc.Nftables {
		if it.Set != nil && (it.Set.Name == AcctUp || it.Set.Name == AcctDown) {
			dir := "up:"
			if it.Set.Name == AcctDown {
				dir = "down:"
			}
			for ip, c := range acctElems(it.Set.Elem) {
				res[AcctPrefix+dir+ip] = c
			}
			continue
		}
		if it.Rule == nil || !strings.HasPrefix(it.Rule.Comment, "rule:") {
			continue
		}
		var c Counter
		for _, e := range it.Rule.Expr {
			if raw, ok := e["counter"]; ok {
				json.Unmarshal(raw, &c)
			}
		}
		t := res[it.Rule.Comment]
		t.Packets += c.Packets
		t.Bytes += c.Bytes
		res[it.Rule.Comment] = t
	}
	return res, nil
}

// acctElems reads the elements of a counting set. nft writes each one as {"elem": {...}}.
func acctElems(raw json.RawMessage) map[string]Counter {
	out := map[string]Counter{}
	var items []struct {
		Elem struct {
			Val     string  `json:"val"`
			Counter Counter `json:"counter"`
		} `json:"elem"`
	}
	if json.Unmarshal(raw, &items) != nil {
		return out
	}
	for _, it := range items {
		if it.Elem.Val != "" {
			out[it.Elem.Val] = it.Elem.Counter
		}
	}
	return out
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
		base := a.baseline()
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

// ConsolePortFile holds the console's saved port, kept up to date by the console so the
// starting rules and the machine's screens follow it.
var ConsolePortFile = "/var/lib/gatehouse/console.port"

// SavedConsolePort reads the saved console port, or 443 when none is saved.
func SavedConsolePort() int {
	if b, err := os.ReadFile(ConsolePortFile); err == nil {
		if n, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil && n >= 1 && n <= 65535 {
			return n
		}
	}
	return 443
}

func (a *Applier) baseline() string {
	if a.Baseline == FirstBootBaseline {
		return FirstBootBaselineFor(SavedConsolePort())
	}
	if a.Baseline != "" {
		return a.Baseline
	}
	return emptyBaseline
}

// FirstBootBaselineFor is the starting rules with the console on the given port.
func FirstBootBaselineFor(port int) string {
	return strings.Replace(FirstBootBaseline, "tcp dport 443 ct state new", fmt.Sprintf("tcp dport %d ct state new", port), 1)
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
	// LoadSets fills the dynamic sets of the loaded ruleset.
	LoadSets(fills map[string]SetFill) error
	// Counters reports how often each rule has matched, by the rule's tag.
	Counters() (map[string]Counter, error)
}

var _ Controller = (*Applier)(nil)

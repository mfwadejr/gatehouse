package netd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// A front panel display (the small text screen on many firewall appliances) is driven by LCDproc: its
// LCDd program talks to the display, and NopeWarden draws on it through LCDd's local port. This file
// installs LCDproc when asked, writes LCDd's settings from the plan, and starts or stops it.

const (
	lcdConfFile = "/etc/LCDd.conf"
	lcdUnit     = "LCDd.service"
	lcdClient   = "lcdproc.service" // the stock screen-feeding program; NopeWarden feeds LCDd itself
)

// LCDInfo is what the console shows about the display software.
type LCDInfo struct {
	Installed bool         `json:"installed"`
	Running   bool         `json:"running"`
	State     string       `json:"state"` // idle, installing, removing, failed
	Message   string       `json:"message"`
	Version   string       `json:"version"`
	Drivers   []string     `json:"drivers"` // drivers this install of LCDd has
	Steps     []ThreatStep `json:"steps"`
}

// LCDPlan is what the panel should be doing. Settings are the keys of the driver's own section in
// LCDd.conf (Size, Device, ConnectionType and so on).
type LCDPlan struct {
	Enabled  bool              `json:"enabled"`
	Driver   string            `json:"driver"`
	Settings map[string]string `json:"settings"`
}

var (
	reLCDName  = regexp.MustCompile(`^[A-Za-z0-9_-]{1,40}$`)
	reLCDKey   = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,40}$`)
	reLCDValue = regexp.MustCompile(`^[^\x00-\x1f"#\[\]]{0,120}$`)
)

// Check says what is wrong with a plan.
func (p LCDPlan) Check() error {
	if !p.Enabled {
		return nil
	}
	if !reLCDName.MatchString(p.Driver) {
		return errors.New("choose the kind of display")
	}
	for k, v := range p.Settings {
		if !reLCDKey.MatchString(k) {
			return fmt.Errorf("%q is not a setting name", k)
		}
		if !reLCDValue.MatchString(v) {
			return fmt.Errorf("the value for %s has a character that is not allowed", k)
		}
	}
	return nil
}

var lcdState = struct {
	sync.Mutex
	state, msg string
	steps      []ThreatStep
}{state: "idle"}

func setLCD(state, msg string) {
	lcdState.Lock()
	lcdState.state, lcdState.msg = state, msg
	lcdState.Unlock()
}

func lcdStep(labels ...string) {
	lcdState.Lock()
	lcdState.steps = nil
	for i, l := range labels {
		st := "todo"
		if i == 0 {
			st = "doing"
		}
		lcdState.steps = append(lcdState.steps, ThreatStep{Label: l, State: st})
	}
	lcdState.Unlock()
}

func lcdStepTo(i int) {
	lcdState.Lock()
	for k := range lcdState.steps {
		switch {
		case k < i:
			lcdState.steps[k].State = "done"
		case k == i:
			lcdState.steps[k].State = "doing"
		}
	}
	lcdState.Unlock()
}

func lcdStepsDone() {
	lcdState.Lock()
	for k := range lcdState.steps {
		lcdState.steps[k].State = "done"
	}
	lcdState.Unlock()
}

func lcdStepFail() {
	lcdState.Lock()
	for k := range lcdState.steps {
		if lcdState.steps[k].State == "doing" {
			lcdState.steps[k].State = "failed"
		}
	}
	lcdState.Unlock()
}

func lcdBusy() bool {
	lcdState.Lock()
	defer lcdState.Unlock()
	return lcdState.state == "installing" || lcdState.state == "removing"
}

func (s *Service) lcdInstalled() bool {
	if _, err := os.Stat(s.rootPath(lcdConfFile)); err != nil {
		return false
	}
	_, err := lookPath("LCDd")
	return err == nil
}

// lcdDrivers lists the drivers LCDd can load, from the driver files the package put on disk.
func (s *Service) lcdDrivers() []string {
	var out []string
	seen := map[string]bool{}
	for _, dir := range []string{"/usr/lib/lcdproc", "/usr/lib/x86_64-linux-gnu/lcdproc", "/usr/lib/aarch64-linux-gnu/lcdproc", "/usr/lib64/lcdproc"} {
		ents, _ := os.ReadDir(s.rootPath(dir))
		for _, e := range ents {
			n := e.Name()
			if strings.HasSuffix(n, ".so") && !seen[strings.TrimSuffix(n, ".so")] {
				seen[strings.TrimSuffix(n, ".so")] = true
				out = append(out, strings.TrimSuffix(n, ".so"))
			}
		}
	}
	sort.Strings(out)
	return out
}

// LCDStatus says whether the display software is installed and running.
func (s *Service) LCDStatus() LCDInfo {
	lcdState.Lock()
	info := LCDInfo{State: lcdState.state, Message: lcdState.msg, Steps: append([]ThreatStep(nil), lcdState.steps...)}
	lcdState.Unlock()
	info.Installed = s.lcdInstalled()
	if info.Installed {
		info.Drivers = s.lcdDrivers()
		if out, err := sysRun(10*time.Second, "systemctl", "is-active", lcdUnit); err == nil && strings.TrimSpace(out) == "active" {
			info.Running = true
		}
		if out, err := sysRun(10*time.Second, "dpkg-query", "-W", "-f=${Version}", "lcdproc"); err == nil {
			info.Version = strings.TrimSpace(out)
		}
	}
	return info
}

// LCDInstallStart installs LCDproc in the background. It leaves both of its services off: NopeWarden
// starts LCDd itself, with the settings the person chose.
func (s *Service) LCDInstallStart() error {
	if err := onDebian13(); err != nil {
		return err
	}
	if s.lcdInstalled() {
		return errors.New("it is already installed")
	}
	if lcdBusy() || threatBusy() {
		return errors.New("something is already being installed or removed")
	}
	setLCD("installing", "Downloading and installing. This takes a minute.")
	lcdStep("Checking Debian's package list", "Downloading the display software", "Installing it", "Making sure it stays off until you turn it on", "Checking it is in place")
	go func() {
		fail := func(msg string) { lcdStepFail(); setLCD("failed", msg) }
		noAutoUpgrade()
		if b, err := aptRun(5*time.Minute, "apt-get", aptArgs("update")...); err != nil {
			fail("Debian's package servers could not be reached: " + lastLine(b, err))
			return
		}
		lcdStepTo(1)
		if b, err := aptRun(20*time.Minute, "apt-get", aptArgs("install", "--download-only", "--no-install-recommends", "lcdproc")...); err != nil {
			fail("The download did not work: " + lastLine(b, err))
			return
		}
		lcdStepTo(2)
		if b, err := aptRun(20*time.Minute, "apt-get", aptArgs("install", "--no-install-recommends", "lcdproc")...); err != nil {
			fail("The install did not work: " + lastLine(b, err))
			return
		}
		lcdStepTo(3)
		sysRun(time.Minute, "systemctl", "disable", "--now", lcdClient)
		sysRun(time.Minute, "systemctl", "disable", "--now", lcdUnit)
		lcdStepTo(4)
		if !s.lcdInstalled() {
			fail("The install finished, but the program is not where it should be.")
			return
		}
		lcdStepsDone()
		time.Sleep(2 * time.Second)
		setLCD("idle", "")
	}()
	return nil
}

// LCDRemove stops the display and takes the software out.
func (s *Service) LCDRemove() error {
	if lcdBusy() {
		return errors.New("something is already being installed or removed")
	}
	setLCD("removing", "Removing.")
	lcdStep("Switching the display off", "Putting the settings back", "Removing the program")
	defer func() {
		lcdState.Lock()
		if lcdState.state == "removing" {
			lcdState.state, lcdState.msg = "idle", ""
		}
		lcdState.Unlock()
	}()
	sysRun(30*time.Second, "systemctl", "disable", "--now", lcdUnit)
	lcdStepTo(1)
	if b, err := os.ReadFile(s.rootPath(lcdConfFile + ".nopewarden-orig")); err == nil {
		os.WriteFile(s.rootPath(lcdConfFile), b, 0o644)
		os.Remove(s.rootPath(lcdConfFile + ".nopewarden-orig"))
	}
	lcdStepTo(2)
	if s.lcdInstalled() {
		if b, err := aptRun(10*time.Minute, "apt-get", aptArgs("remove", "lcdproc")...); err != nil {
			lcdStepFail()
			setLCD("failed", "The removal did not work: "+lastLine(b, err))
			return errors.New(lcdState.msg)
		}
	}
	return nil
}

// LCDApply makes LCDd match the plan: settings written and the display running, or stopped.
func (s *Service) LCDApply(p LCDPlan) error {
	if err := p.Check(); err != nil {
		return err
	}
	if !s.lcdInstalled() {
		return errors.New("the display software is not installed yet")
	}
	if !p.Enabled {
		sysRun(30*time.Second, "systemctl", "disable", "--now", lcdUnit)
		return nil
	}
	path := s.rootPath(lcdConfFile)
	orig := path + ".nopewarden-orig"
	if _, err := os.Stat(orig); err != nil {
		if b, e := os.ReadFile(path); e == nil {
			os.WriteFile(orig, b, 0o644)
		}
	}
	base, err := os.ReadFile(orig)
	if err != nil {
		base, err = os.ReadFile(path)
		if err != nil {
			return err
		}
	}
	conf := LCDConf(string(base), p)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(conf), 0o644); err != nil {
		return err
	}
	sysRun(30*time.Second, "systemctl", "disable", "--now", lcdClient)
	if out, err := sysRun(30*time.Second, "systemctl", "enable", lcdUnit); err != nil {
		return fmt.Errorf("could not switch the display on: %s", strings.TrimSpace(out))
	}
	if out, err := sysRun(60*time.Second, "systemctl", "restart", lcdUnit); err != nil {
		return fmt.Errorf("the display program would not start: %s", lastLine([]byte(out), err))
	}
	return nil
}

// LCDConf returns LCDd's settings with the plan applied: the server section bound to this machine
// only and using the chosen driver, and the driver's own section holding the plan's settings. Every
// other line of the packaged file is kept as it was.
func LCDConf(base string, p LCDPlan) string {
	conf := lcdSet(base, "server", map[string]string{
		"Driver": p.Driver, "Bind": "127.0.0.1", "Port": "13666", "ReportToSyslog": "yes", "ServerScreen": "no", "Foreground": "no",
	})
	set := map[string]string{}
	for k, v := range p.Settings {
		set[k] = v
	}
	return lcdSet(conf, p.Driver, set)
}

// lcdSet sets keys inside one [section] of an ini text, replacing an existing or commented-out line
// for the key when there is one, and adding the section and keys when there are none.
func lcdSet(text, section string, kv map[string]string) string {
	lines := strings.Split(text, "\n")
	var keys []string
	for k := range kv {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	head := "[" + section + "]"
	start, end := -1, len(lines)
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if strings.EqualFold(t, head) {
			start = i
			continue
		}
		if start >= 0 && strings.HasPrefix(t, "[") && strings.HasSuffix(t, "]") {
			end = i
			break
		}
	}
	if start < 0 {
		out := strings.TrimRight(text, "\n") + "\n\n" + head + "\n"
		for _, k := range keys {
			out += k + "=" + kv[k] + "\n"
		}
		return out
	}
	done := map[string]bool{}
	for i := start + 1; i < end; i++ {
		t := strings.TrimSpace(lines[i])
		t = strings.TrimSpace(strings.TrimPrefix(t, "#"))
		for _, k := range keys {
			if done[k] {
				continue
			}
			if strings.HasPrefix(t, k+"=") || strings.HasPrefix(t, k+" =") {
				lines[i] = k + "=" + kv[k]
				done[k] = true
				break
			}
		}
	}
	// The packaged file may name more than one driver; only the chosen one should load.
	if _, ok := kv["Driver"]; ok && done["Driver"] {
		seen := false
		for i := start + 1; i < end; i++ {
			t := strings.TrimSpace(lines[i])
			if strings.HasPrefix(t, "Driver=") || strings.HasPrefix(t, "Driver =") {
				if seen {
					lines[i] = "#" + t
				}
				seen = true
			}
		}
	}
	var add []string
	for _, k := range keys {
		if !done[k] {
			add = append(add, k+"="+kv[k])
		}
	}
	out := append([]string{}, lines[:start+1]...)
	out = append(out, add...)
	out = append(out, lines[start+1:]...)
	return strings.Join(out, "\n")
}

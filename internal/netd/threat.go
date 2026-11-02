package netd

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"gatehouse/internal/threatcfg"
)

// Threat watching: an optional detection engine (Suricata) that Gatehouse installs only when the
// owner asks. It runs in watch-only mode on copies of packets, so it can never hold up traffic.
// The package is deliberately not in the update package list: a bad package can never stop an
// update, and nothing is installed unless a person pressed Install.

const (
	threatBinary = "usr/bin/suricata"
	threatRules  = "var/lib/suricata/rules/suricata.rules"
	// the engine's own service, which the package turns on; Gatehouse turns it off and runs its own
	threatStockUnit = "suricata.service"
)

// ThreatInfo is what the console shows about the engine.
type ThreatInfo struct {
	Installed    bool   `json:"installed"`
	Version      string `json:"version"`
	Running      bool   `json:"running"`
	State        string `json:"state"`   // idle, installing, removing, updating or failed
	Message      string `json:"message"` // what is happening, or why it failed
	DiskFreeMB   int64  `json:"disk_free_mb"`
	MemoryMB     int64  `json:"memory_mb"`
	CPUs         int    `json:"cpus"`
	RulesUpdated int64  `json:"rules_updated"`
	RulesCount   int    `json:"rules_count"`
	Why          string `json:"why"`     // plain reason it is not running, when it should be
	Failing      bool   `json:"failing"` // crashing and being restarted, not just starting
	HasPrevious  bool   `json:"has_previous"`
	CPUNSec      uint64 `json:"cpu_nsec"` // processor time used so far
	MemoryBytes  uint64 `json:"memory_bytes"`
}

// ThreatPreview is what installing would do, shown before anything happens.
type ThreatPreview struct {
	Packages   []string `json:"packages"`
	DownloadMB float64  `json:"download_mb"`
	DiskMB     float64  `json:"disk_mb"`
	Note       string   `json:"note"`
}

var threatState = struct {
	sync.Mutex
	state, msg string
	rulesAt    time.Time
	rulesN     int
}{state: "idle"}

func setThreat(state, msg string) {
	threatState.Lock()
	threatState.state, threatState.msg = state, msg
	threatState.Unlock()
}

func threatBusy() bool {
	threatState.Lock()
	defer threatState.Unlock()
	return threatState.state != "idle" && threatState.state != "failed"
}

func (s *Service) threatInstalled() bool {
	st, err := os.Stat(s.rootPath(threatBinary))
	return err == nil && st.Mode().IsRegular()
}

var reDF = regexp.MustCompile(`(?m)^\S+\s+\d+\s+\d+\s+(\d+)\s`)
var reMemTotal = regexp.MustCompile(`MemTotal:\s+(\d+) kB`)

// ThreatStatus describes the engine and the machine it would run on.
func (s *Service) ThreatStatus() ThreatInfo {
	threatState.Lock()
	info := ThreatInfo{State: threatState.state, Message: threatState.msg}
	threatState.Unlock()
	info.CPUs = runtime.NumCPU()
	if out, err := wifiRun(10*time.Second, "df", "-Pm", "/var"); err == nil {
		if m := reDF.FindStringSubmatch(out); m != nil {
			info.DiskFreeMB, _ = strconv.ParseInt(m[1], 10, 64)
		}
	}
	if b, err := os.ReadFile("/proc/meminfo"); err == nil {
		if m := reMemTotal.FindSubmatch(b); m != nil {
			kb, _ := strconv.ParseInt(string(m[1]), 10, 64)
			info.MemoryMB = kb / 1024
		}
	}
	info.Installed = s.threatInstalled()
	if !info.Installed {
		return info
	}
	if out, err := wifiRun(10*time.Second, s.rootPath(threatBinary), "-V"); err == nil {
		info.Version = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(out), "This is Suricata version"))
	}
	if out, _ := wifiRun(10*time.Second, "systemctl", "is-active", threatcfg.UnitName); strings.TrimSpace(out) == "active" {
		info.Running = true
		if out, err := wifiRun(10*time.Second, "systemctl", "show", "-p", "CPUUsageNSec", "-p", "MemoryCurrent", threatcfg.UnitName); err == nil {
			for _, l := range strings.Split(out, "\n") {
				k, v, _ := strings.Cut(strings.TrimSpace(l), "=")
				n, err := strconv.ParseUint(v, 10, 64)
				if err != nil {
					continue
				}
				if k == "CPUUsageNSec" {
					info.CPUNSec = n
				} else if k == "MemoryCurrent" {
					info.MemoryBytes = n
				}
			}
		}
	}
	if !info.Running {
		info.Why, info.Failing = s.threatWhy()
	}
	if st, err := os.Stat(s.rootPath(threatRules)); err == nil {
		info.RulesUpdated = st.ModTime().Unix()
		threatState.Lock()
		if !threatState.rulesAt.Equal(st.ModTime()) {
			threatState.rulesAt, threatState.rulesN = st.ModTime(), countRules(s.rootPath(threatRules))
		}
		info.RulesCount = threatState.rulesN
		threatState.Unlock()
	}
	_, err := os.Stat(s.rootPath(threatRules) + ".prev")
	info.HasPrevious = err == nil
	return info
}

func countRules(path string) int {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	n := 0
	for _, l := range bytes.Split(b, []byte("\n")) {
		if len(l) > 0 && l[0] != '#' {
			n++
		}
	}
	return n
}

var reAfter = regexp.MustCompile(`After this operation, ([\d.,]+) ([kMG])B of additional disk space`)
var reNeed = regexp.MustCompile(`Need to get ([\d.,]+) ([kMG])B`)

// sumURISizes adds up the sizes in "apt-get --print-uris" lines: 'url' file size hash.
func sumURISizes(out []byte) float64 {
	var total float64
	for _, l := range strings.Split(string(out), "\n") {
		l = strings.TrimSpace(l)
		if !strings.HasPrefix(l, "'") {
			continue
		}
		f := strings.Fields(l)
		if len(f) >= 3 {
			if n, err := strconv.ParseFloat(f[2], 64); err == nil {
				total += n
			}
		}
	}
	return total / (1024 * 1024)
}

// sumInstalledSize adds up the "Installed-Size" (in kB) of the packages "apt-cache show" describes.
func sumInstalledSize(out []byte) float64 {
	var total float64
	for _, l := range strings.Split(string(out), "\n") {
		if v, ok := strings.CutPrefix(l, "Installed-Size:"); ok {
			if n, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
				total += n
			}
		}
	}
	return total / 1024
}

func toMB(num, unit string) float64 {
	v, _ := strconv.ParseFloat(strings.ReplaceAll(num, ",", ""), 64)
	switch unit {
	case "k":
		return v / 1024
	case "G":
		return v * 1024
	}
	return v
}

// ThreatInstallPreview says what would be installed and how big it is, without installing.
func (s *Service) ThreatInstallPreview() (ThreatPreview, error) {
	var p ThreatPreview
	if err := onDebian13(); err != nil {
		return p, err
	}
	if s.threatInstalled() {
		return p, errors.New("it is already installed")
	}
	p.Packages = []string{"suricata", "suricata-update"}
	b, err := aptRun(2*time.Minute, "apt-get", aptArgs(append([]string{"-s", "install", "--no-install-recommends"}, p.Packages...)...)...)
	if err != nil {
		return p, fmt.Errorf("Debian's package list could not be read: %s", lastLine(b, err))
	}
	if m := reAfter.FindSubmatch(b); m != nil {
		p.DiskMB = toMB(string(m[1]), string(m[2]))
	}
	if m := reNeed.FindSubmatch(b); m != nil {
		p.DownloadMB = toMB(string(m[1]), string(m[2]))
	}
	// Debian's simulation does not always print the totals, so work them out from the package list.
	if p.DownloadMB == 0 || p.DiskMB == 0 {
		names := append(parseInst(b), p.Packages...)
		if p.DownloadMB == 0 {
			if u, err := aptRun(2*time.Minute, "apt-get", aptArgs("install", "--no-install-recommends", "--print-uris", "-qq", strings.Join(p.Packages, " "))...); err == nil || len(u) > 0 {
				p.DownloadMB = sumURISizes(u)
			}
		}
		if p.DiskMB == 0 {
			if o, err := aptRun(time.Minute, "apt-cache", append([]string{"show", "--no-all-versions"}, names...)...); err == nil {
				p.DiskMB = sumInstalledSize(o)
			}
		}
	}
	p.Note = "Installed from Debian's own package source. It needs the internet for a minute or two."
	return p, nil
}

// ThreatInstallStart installs the engine in the background. It never starts the engine's own
// service: Gatehouse runs it in its own limited, watch-only service when the owner turns it on.
func (s *Service) ThreatInstallStart() error {
	if err := onDebian13(); err != nil {
		return err
	}
	if s.threatInstalled() {
		return errors.New("it is already installed")
	}
	if threatBusy() {
		return errors.New("something is already being installed or removed")
	}
	setThreat("installing", "Downloading and installing. This takes a minute or two.")
	go func() {
		noAutoUpgrade()
		if b, err := aptRun(5*time.Minute, "apt-get", aptArgs("update")...); err != nil {
			setThreat("failed", "Debian's package servers could not be reached: "+lastLine(b, err))
			return
		}
		b, err := aptRun(30*time.Minute, "apt-get", aptArgs("install", "--no-install-recommends", "suricata", "suricata-update")...)
		if err != nil {
			setThreat("failed", "The install did not work: "+lastLine(b, err))
			return
		}
		// The package turns its own service on; ours is the only one that should run.
		aptRun(time.Minute, "systemctl", "disable", "--now", threatStockUnit)
		if !s.threatInstalled() {
			setThreat("failed", "The install finished, but the program is not where it should be.")
			return
		}
		setThreat("idle", "")
	}()
	return nil
}

// ThreatRemove stops the engine and takes the packages out.
func (s *Service) ThreatRemove() error {
	if threatBusy() {
		return errors.New("something is already being installed or removed")
	}
	setThreat("removing", "Removing.")
	defer func() {
		if threatState.state == "removing" {
			setThreat("idle", "")
		}
	}()
	wifiRun(30*time.Second, "systemctl", "disable", "--now", threatcfg.UnitName)
	os.Remove(s.rootPath(threatcfg.UnitFile))
	os.Remove(s.rootPath(threatcfg.IncludeFile))
	os.Remove(s.rootPath(threatcfg.DisableConf))
	wifiRun(30*time.Second, "systemctl", "daemon-reload")
	if s.threatInstalled() {
		if b, err := aptRun(10*time.Minute, "apt-get", aptArgs("remove", "suricata", "suricata-update")...); err != nil {
			setThreat("failed", "The removal did not work: "+lastLine(b, err))
			return errors.New(threatState.msg)
		}
	}
	return nil
}

func readOrEmpty(p string) string {
	b, _ := os.ReadFile(p)
	return string(b)
}

// ThreatApply makes the engine match the plan: switched off, or running watch-only on the given
// ports with the chosen rule groups, within its processor and memory limits.
func (s *Service) ThreatApply(p threatcfg.Plan) error {
	if err := p.Check(); err != nil {
		return err
	}
	if !p.Enabled {
		wifiRun(30*time.Second, "systemctl", "disable", "--now", threatcfg.UnitName)
		return nil
	}
	if !s.threatInstalled() {
		return errors.New("the threat-watching program is not installed yet")
	}
	for _, f := range []string{threatcfg.IncludeFile, threatcfg.UnitFile, threatcfg.DisableConf} {
		if err := os.MkdirAll(filepath.Dir(s.rootPath(f)), 0o755); err != nil {
			return err
		}
	}
	oldUnit := readOrEmpty(s.rootPath(threatcfg.UnitFile))
	oldDisable := readOrEmpty(s.rootPath(threatcfg.DisableConf))
	oldInc := readOrEmpty(s.rootPath(threatcfg.IncludeFile))
	unit, dis, inc := threatcfg.Unit(p), threatcfg.DisableList(p), threatcfg.Include()
	for path, body := range map[string]string{threatcfg.IncludeFile: inc, threatcfg.UnitFile: unit, threatcfg.DisableConf: dis} {
		if err := writeFileAtomic(s.rootPath(path), []byte(body), 0o644); err != nil {
			return err
		}
	}
	changed := unit != oldUnit || inc != oldInc
	if changed {
		wifiRun(30*time.Second, "systemctl", "daemon-reload")
	}
	if dis != oldDisable {
		// Which groups are on changed, so the rules are rebuilt from the groups the person chose.
		if err := s.ThreatUpdateRules(); err != nil {
			// Without new rules the engine still runs on the ones it has, if it has any.
			if _, e := os.Stat(s.rootPath(threatRules)); e != nil {
				return err
			}
		}
	}
	wifiRun(30*time.Second, "systemctl", "enable", threatcfg.UnitName)
	verb := "start"
	if changed {
		verb = "restart"
	}
	if out, err := wifiRun(90*time.Second, "systemctl", verb, threatcfg.UnitName); err != nil {
		return fmt.Errorf("the threat-watching program did not start: %s", strings.TrimSpace(out))
	}
	return nil
}

// threatWhy says in plain words why the engine's service is not running: it ran out of memory, it
// keeps failing and being restarted, it was never started, or the last thing it complained about.
// failing is true when the service is crashing rather than just starting or stopped.
func (s *Service) threatWhy() (why string, failing bool) {
	st := unitState(threatcfg.UnitName)
	switch {
	case st.Load == "not-found" || st.Load == "":
		return "It has not been started yet.", false
	case st.Result == "oom-kill":
		return "It ran out of memory and was stopped. Raise its memory limit under Settings, or choose Light.", true
	case st.Sub == "auto-restart" || st.Active == "failed" || (st.Active == "inactive" && st.Result != "success" && st.Result != ""):
		if e := lastUnitError(threatcfg.UnitName); e != "" {
			return "It keeps stopping and Gatehouse keeps restarting it. The last thing it said: " + e, true
		}
		return "It keeps stopping and Gatehouse keeps restarting it, and it did not say why. See Advanced, Program log.", true
	case st.Active == "activating":
		return "It is starting. Loading the rules takes a minute or two.", false
	}
	if e := lastUnitError(threatcfg.UnitName); e != "" {
		return "It stopped. The last thing it said: " + e, true
	}
	return "It is switched off.", false
}

var threatUpdateMu sync.Mutex

// ThreatUpdateRules fetches the newest rules and rebuilds the rule file from the chosen groups. The
// rules in use are kept as the previous set, so a bad update can be rolled back. If the new rules
// do not pass the engine's own test, the old ones stay.
func (s *Service) ThreatUpdateRules() error {
	if !s.threatInstalled() {
		return errors.New("the threat-watching program is not installed yet")
	}
	if !threatUpdateMu.TryLock() {
		return errors.New("rules are already being updated")
	}
	defer threatUpdateMu.Unlock()
	rules := s.rootPath(threatRules)
	if b, err := os.ReadFile(rules); err == nil {
		os.WriteFile(rules+".prev", b, 0o644)
	}
	out, err := wifiRun(10*time.Minute, "suricata-update", "--no-reload", "--disable-conf", s.rootPath(threatcfg.DisableConf))
	if err != nil {
		// put the old rules back
		if b, e := os.ReadFile(rules + ".prev"); e == nil {
			os.WriteFile(rules, b, 0o644)
		}
		return fmt.Errorf("the rules could not be updated: %s", lastLine([]byte(out), err))
	}
	if out, _ := wifiRun(10*time.Second, "systemctl", "is-active", threatcfg.UnitName); strings.TrimSpace(out) == "active" {
		wifiRun(time.Minute, "systemctl", "restart", threatcfg.UnitName)
	}
	return nil
}

// ThreatRollback puts the previous rules back.
func (s *Service) ThreatRollback() error {
	rules := s.rootPath(threatRules)
	b, err := os.ReadFile(rules + ".prev")
	if err != nil {
		return errors.New("there are no earlier rules to go back to")
	}
	cur, _ := os.ReadFile(rules)
	if err := os.WriteFile(rules, b, 0o644); err != nil {
		return err
	}
	os.WriteFile(rules+".prev", cur, 0o644) // so going back can itself be undone
	if out, _ := wifiRun(10*time.Second, "systemctl", "is-active", threatcfg.UnitName); strings.TrimSpace(out) == "active" {
		wifiRun(time.Minute, "systemctl", "restart", threatcfg.UnitName)
	}
	return nil
}

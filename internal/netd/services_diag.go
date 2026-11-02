package netd

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Diagnostics: what each Gatehouse service is doing, its recent log, and a restart. Only a short,
// fixed list of services can be looked at or restarted, so this cannot be pointed at anything else.

// DiagUnit is one service Gatehouse can show.
type DiagUnit struct {
	Unit    string
	Title   string
	Restart string // "", "unit" (restart it directly) or "gatehouse" (the delayed restart of the console programs)
}

var diagUnits = []DiagUnit{
	{"gatehouse", "Gatehouse console", "gatehouse"},
	{"gatehouse-netd", "Gatehouse helper (changes the network)", "gatehouse"},
	{"gatehouse-screen", "Gatehouse screen", "gatehouse"},
	{"gatehouse-suricata", "Threat watching", "unit"},
	{"ssh", "Remote login (SSH)", "unit"},
	{"keepalived", "High availability", "unit"},
	{"chrony", "Clock", "unit"},
}

// DiagService is the state of one service, in words the console shows as they are.
type DiagService struct {
	Unit      string `json:"unit"`
	Title     string `json:"title"`
	State     string `json:"state"` // running, stopped, failing, starting, missing
	Reason    string `json:"reason"`
	Restarts  int    `json:"restarts"`
	CanReboot bool   `json:"can_restart"`
}

type unitInfo struct {
	Load, Active, Sub, Result string
	Restarts                  int
}

func unitState(unit string) unitInfo {
	out, _ := sysRun(10*time.Second, "systemctl", "show", "-p", "LoadState", "-p", "ActiveState", "-p", "SubState", "-p", "Result", "-p", "NRestarts", unit)
	var u unitInfo
	for _, l := range strings.Split(out, "\n") {
		k, v, _ := strings.Cut(strings.TrimSpace(l), "=")
		switch k {
		case "LoadState":
			u.Load = v
		case "ActiveState":
			u.Active = v
		case "SubState":
			u.Sub = v
		case "Result":
			u.Result = v
		case "NRestarts":
			u.Restarts, _ = strconv.Atoi(v)
		}
	}
	return u
}

var reErrLine = regexp.MustCompile(`(?i)<error>|<critical>|error|failed|fatal|cannot|can't|unable`)

// lastUnitError is the most recent line in a service's log that reads like a complaint.
func lastUnitError(unit string) string {
	j, _ := sysRun(10*time.Second, "journalctl", "-u", unit, "-n", "60", "--no-pager", "-o", "cat")
	last := ""
	for _, l := range strings.Split(j, "\n") {
		l = strings.TrimSpace(l)
		if l != "" && reErrLine.MatchString(l) {
			last = l
		}
	}
	if len(last) > 240 {
		last = last[:240]
	}
	return last
}

func diagUnit(unit string) (DiagUnit, bool) {
	for _, d := range diagUnits {
		if d.Unit == unit {
			return d, true
		}
	}
	return DiagUnit{}, false
}

// DiagServices lists every service with its state and, when it is not fine, why.
func (s *Service) DiagServices() []DiagService {
	var out []DiagService
	for _, d := range diagUnits {
		st := unitState(d.Unit)
		x := DiagService{Unit: d.Unit, Title: d.Title, Restarts: st.Restarts, CanReboot: d.Restart != ""}
		switch {
		case st.Load == "" || st.Load == "not-found":
			x.State, x.Reason = "missing", "Not installed on this machine."
		case st.Sub == "auto-restart" || st.Active == "failed":
			x.State = "failing"
			x.Reason = "It keeps stopping and being restarted."
			if e := lastUnitError(d.Unit); e != "" {
				x.Reason += " The last thing it said: " + e
			}
		case st.Active == "activating":
			x.State, x.Reason = "starting", "It is starting."
		case st.Active == "active":
			x.State = "running"
		default:
			x.State, x.Reason = "stopped", "It is switched off."
		}
		out = append(out, x)
	}
	return out
}

var reSecret = regexp.MustCompile(`(?i)(password|passwd|secret|token|psk|private[ _-]?key|authtok|api[_-]?key)(\s*[=:]\s*|\s+)\S+`)

// redact hides anything that looks like a password or key in a line of text.
func redact(s string) string {
	return reSecret.ReplaceAllString(s, "$1 [hidden]")
}

// DiagLog returns the last lines of one service's log ("system" for warnings from everything,
// "system-all" for every level).
func (s *Service) DiagLog(unit string, lines int) (string, error) {
	if lines < 10 {
		lines = 10
	}
	if lines > 5000 {
		lines = 5000
	}
	args := []string{"-n", strconv.Itoa(lines), "--no-pager", "-o", "short-iso"}
	if unit == "system" {
		args = append(args, "-p", "warning")
	} else if unit == "system-all" {
		// everything, every level
	} else {
		if _, ok := diagUnit(unit); !ok {
			return "", errors.New("that service cannot be shown")
		}
		args = append(args, "-u", unit)
	}
	out, err := sysRun(20*time.Second, "journalctl", args...)
	if err != nil && strings.TrimSpace(out) == "" {
		return "", fmt.Errorf("the log could not be read: %v", err)
	}
	return redact(out), nil
}

// DiagRestart restarts one of the listed services.
func (s *Service) DiagRestart(unit string) error {
	d, ok := diagUnit(unit)
	if !ok || d.Restart == "" {
		return errors.New("that service cannot be restarted from here")
	}
	if d.Restart == "gatehouse" {
		return s.Power("restart-gatehouse")
	}
	if out, err := sysRun(60*time.Second, "systemctl", "restart", unit); err != nil {
		return fmt.Errorf("it did not restart: %s", strings.TrimSpace(out))
	}
	return nil
}

// DiagBundle gathers what is needed to understand a problem: the machine, the network, every
// service's state and the recent log of each. Anything that looks like a password or key is hidden.
func (s *Service) DiagBundle() (string, error) {
	var b strings.Builder
	section := func(title, name string, args ...string) {
		out, _ := sysRun(30*time.Second, name, args...)
		fmt.Fprintf(&b, "\n===== %s =====\n%s\n", title, redact(strings.TrimSpace(out)))
	}
	fmt.Fprintf(&b, "Gatehouse support bundle, made %s\n", time.Now().Format(time.RFC3339))
	section("Machine", "uname", "-a")
	section("Release", "cat", "/etc/os-release")
	section("Uptime", "uptime")
	section("Memory", "free", "-m")
	section("Disk", "df", "-Pm")
	section("Addresses", "ip", "-br", "addr")
	section("Routes", "ip", "route")
	section("Firewall rule counts", "nft", "list", "tables")
	fmt.Fprintf(&b, "\n===== Services =====\n")
	for _, x := range s.DiagServices() {
		fmt.Fprintf(&b, "%-18s %-9s %s\n", x.Unit, x.State, x.Reason)
	}
	for _, d := range diagUnits {
		out, _ := s.DiagLog(d.Unit, 120)
		fmt.Fprintf(&b, "\n===== Log: %s =====\n%s\n", d.Title, strings.TrimSpace(out))
	}
	out, _ := s.DiagLog("system", 200)
	fmt.Fprintf(&b, "\n===== Log: warnings from everything =====\n%s\n", strings.TrimSpace(out))
	return b.String(), nil
}

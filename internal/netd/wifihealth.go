package netd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Wi-Fi health. A card that broadcasts can hang while its interface still says "access point":
// the service is running, the driver is wedged, and nobody can join. This looks at the card the
// way a device would (is the access point enabled, does the driver still answer), and when it does
// not for a minute it restarts the service, then reloads the card's driver. It never gives up
// quietly: every step is reported to the caller, which logs it.

// WifiState is what one broadcasting card looks like right now.
type WifiState struct {
	Iface      string `json:"iface"`
	OK         bool   `json:"ok"`
	Reason     string `json:"reason,omitempty"` // when not ok, in plain words
	Stations   int    `json:"stations"`         // devices joined
	Action     string `json:"action,omitempty"` // what was just done about it
	Recoveries int    `json:"recoveries"`       // steps taken in the last hour
	GaveUp     bool   `json:"gave_up,omitempty"`
}

type whState struct {
	bad     int         // consecutive checks that failed
	grace   int         // checks to skip after a recovery step, while the card comes back
	steps   []time.Time // recovery steps in the last hour
	awakeAt time.Time
}

var (
	whMu    sync.Mutex
	whStore = map[string]*whState{}
)

// wifiRun runs a command with a time limit and returns what it printed. It is a variable so tests
// can stand in for the real tools.
var wifiRun = func(timeout time.Duration, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if ctx.Err() != nil {
		return string(out), errors.New("timed out")
	}
	return string(out), err
}

var wifiSleep = time.Sleep

const (
	whBadLimit    = 3 // checks in a row (about a minute) before anything is done
	whGraceChecks = 4
	whMaxPerHour  = 4
)

var reWifiUnit = regexp.MustCompile(`^(wl|wlan)[A-Za-z0-9]{0,12}$`)

func wifiUnit(iface string) string { return "gatehouse-wifi-ap@" + iface + ".service" }

// driverModule finds the kernel module that drives a card.
func (s *Service) driverModule(iface string) string {
	l, err := os.Readlink(filepath.Join(s.sys(), "class/net", iface, "device/driver/module"))
	if err != nil {
		return ""
	}
	return filepath.Base(l)
}

func (s *Service) sys() string {
	if s.Root != "" {
		return filepath.Join(s.Root, "sys")
	}
	return "/sys"
}

// probe says whether a broadcasting card is really serving, and how many devices are on it.
func probeWifi(iface string) (ok bool, reason string, stations int) {
	if out, _ := wifiRun(5*time.Second, "systemctl", "is-active", wifiUnit(iface)); strings.TrimSpace(out) != "active" {
		return false, "the Wi-Fi service is not running", 0
	}
	if out, err := wifiRun(5*time.Second, "iw", "dev", iface, "info"); err != nil || !strings.Contains(out, "type AP") {
		return false, "the card is not in access point mode", 0
	}
	out, err := wifiRun(6*time.Second, "hostapd_cli", "-i", iface, "status")
	if err != nil {
		return false, "the access point service does not answer", 0
	}
	if st := lineValue(out, "state"); st != "ENABLED" {
		if st == "" {
			st = "unknown"
		}
		return false, "the access point is " + strings.ToLower(st) + ", not enabled", 0
	}
	// A hung driver stops answering questions about the radio.
	if _, err := wifiRun(6*time.Second, "iw", "dev", iface, "survey", "dump"); err != nil {
		return false, "the card's driver does not answer", 0
	}
	if out, err := wifiRun(5*time.Second, "iw", "dev", iface, "station", "dump"); err == nil {
		stations = strings.Count(out, "Station ")
	}
	return true, "", stations
}

func lineValue(out, key string) string {
	for _, l := range strings.Split(out, "\n") {
		if k, v, ok := strings.Cut(strings.TrimSpace(l), "="); ok && k == key {
			return v
		}
	}
	return ""
}

// WifiHealth checks each broadcasting card, takes the next recovery step for one that has stopped
// serving, and (when asked) keeps its power saving off. It is called about every twenty seconds.
func (s *Service) WifiHealth(ifaces []string, keepAwake bool) ([]WifiState, error) {
	whMu.Lock()
	defer whMu.Unlock()
	var out []WifiState
	now := time.Now()
	for _, iface := range ifaces {
		if !reWifiUnit.MatchString(iface) {
			continue
		}
		st := whStore[iface]
		if st == nil {
			st = &whState{}
			whStore[iface] = st
		}
		// Forget steps older than an hour.
		keep := st.steps[:0]
		for _, t := range st.steps {
			if now.Sub(t) < time.Hour {
				keep = append(keep, t)
			}
		}
		st.steps = keep
		res := WifiState{Iface: iface, Recoveries: len(st.steps)}
		if st.grace > 0 {
			st.grace--
			res.OK, res.Reason = false, "coming back after a restart"
			out = append(out, res)
			continue
		}
		ok, reason, n := probeWifi(iface)
		res.OK, res.Reason, res.Stations = ok, reason, n
		if ok {
			st.bad = 0
			if keepAwake && now.Sub(st.awakeAt) > 10*time.Minute {
				if o, _ := wifiRun(5*time.Second, "iw", "dev", iface, "get", "power_save"); strings.Contains(o, "on") {
					wifiRun(5*time.Second, "iw", "dev", iface, "set", "power_save", "off")
				}
				st.awakeAt = now
			}
			out = append(out, res)
			continue
		}
		st.bad++
		if st.bad < whBadLimit {
			out = append(out, res)
			continue
		}
		if len(st.steps) >= whMaxPerHour {
			res.GaveUp = true
			res.Action = "stopped trying: the card keeps failing, and a restart of the machine may be needed"
			out = append(out, res)
			continue
		}
		// Step 1 restarts the service; later steps reload the driver as well.
		reload := len(st.steps) >= 1
		res.Action = s.recoverWifi(iface, reload)
		st.steps = append(st.steps, now)
		res.Recoveries = len(st.steps)
		st.bad, st.grace = 0, whGraceChecks
		out = append(out, res)
	}
	return out, nil
}

// recoverWifi restarts the access point, and when asked reloads the card's driver first.
func (s *Service) recoverWifi(iface string, reloadDriver bool) string {
	unit := wifiUnit(iface)
	if !reloadDriver {
		if _, err := wifiRun(30*time.Second, "systemctl", "restart", unit); err != nil {
			return "restarted the Wi-Fi service, but it did not start cleanly"
		}
		return "restarted the Wi-Fi service"
	}
	mod := s.driverModule(iface)
	if mod == "" {
		wifiRun(30*time.Second, "systemctl", "restart", unit)
		return "restarted the Wi-Fi service (the card's driver could not be identified, so it was not reloaded)"
	}
	wifiRun(30*time.Second, "systemctl", "stop", unit)
	if _, err := wifiRun(30*time.Second, "modprobe", "-r", mod); err != nil {
		wifiRun(30*time.Second, "systemctl", "start", unit)
		return "tried to reload the driver " + mod + ", but it is busy; restarted the service instead"
	}
	wifiSleep(2 * time.Second)
	wifiRun(30*time.Second, "modprobe", mod)
	// Wait for the card to come back before starting the service.
	for i := 0; i < 15; i++ {
		if _, err := os.Stat(filepath.Join(s.sys(), "class/net", iface)); err == nil {
			break
		}
		wifiSleep(time.Second)
	}
	wifiRun(10*time.Second, "rfkill", "unblock", "wifi")
	wifiRun(30*time.Second, "systemctl", "start", unit)
	return "reloaded the card's driver (" + mod + ") and restarted the Wi-Fi service"
}

// WifiPowerSave sets whether the card is kept awake. For the Realtek rtw88 family it also writes the
// driver options that keep it from sleeping deeply; they apply the next time the driver loads.
func (s *Service) WifiPowerSave(iface string, keepAwake bool) error {
	if !reWifiUnit.MatchString(iface) {
		return errors.New("that is not a wireless card")
	}
	root := s.Root
	if root == "" {
		root = "/"
	}
	file := filepath.Join(root, "etc/modprobe.d/gatehouse-wifi.conf")
	if !keepAwake {
		os.Remove(file)
		return nil
	}
	wifiRun(5*time.Second, "iw", "dev", iface, "set", "power_save", "off")
	var lines []string
	for _, o := range []struct{ mod, param, val string }{
		{"rtw88_pci", "disable_aspm", "y"},
		{"rtw88_core", "disable_lps_deep", "y"},
	} {
		// Only an option the running driver really has: an unknown option stops a driver loading.
		if _, err := os.Stat(filepath.Join(s.sys(), "module", o.mod, "parameters", o.param)); err == nil {
			lines = append(lines, fmt.Sprintf("options %s %s=%s", o.mod, o.param, o.val))
		}
	}
	if len(lines) == 0 {
		os.Remove(file)
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	body := "# Written by Gatehouse: keeps the Realtek Wi-Fi card from sleeping deeply.\n" + strings.Join(lines, "\n") + "\n"
	return os.WriteFile(file, []byte(body), 0o644)
}

// WifiReport gathers what is known about one card, for reading in the console.
func (s *Service) WifiReport(iface string) (string, error) {
	if !reWifiUnit.MatchString(iface) {
		return "", errors.New("that is not a wireless card")
	}
	var b strings.Builder
	section := func(title string, timeout time.Duration, name string, args ...string) {
		out, err := wifiRun(timeout, name, args...)
		fmt.Fprintf(&b, "== %s ==\n", title)
		if err != nil && strings.TrimSpace(out) == "" {
			fmt.Fprintf(&b, "(%v)\n\n", err)
			return
		}
		b.WriteString(strings.TrimSpace(out) + "\n\n")
	}
	section("Time", 3*time.Second, "date")
	section("Card", 5*time.Second, "iw", "dev", iface, "info")
	section("Power saving", 5*time.Second, "iw", "dev", iface, "get", "power_save")
	section("Access point", 6*time.Second, "hostapd_cli", "-i", iface, "status")
	section("Devices joined", 5*time.Second, "iw", "dev", iface, "station", "dump")
	section("Wi-Fi service", 5*time.Second, "systemctl", "status", wifiUnit(iface), "--no-pager", "-n", "0")
	section("Radio switches", 5*time.Second, "rfkill", "list")
	logs, _ := wifiRun(10*time.Second, "journalctl", "-b", "--no-pager", "-u", wifiUnit(iface), "-n", "60", "-o", "short-iso")
	fmt.Fprintf(&b, "== Wi-Fi service log (last 60 lines) ==\n%s\n\n", strings.TrimSpace(logs))
	kern, _ := wifiRun(10*time.Second, "journalctl", "-k", "-b", "--no-pager", "-o", "short-iso")
	var keep []string
	re := regexp.MustCompile(`(?i)` + regexp.QuoteMeta(iface) + `|rtw|rtl|iwlwifi|ath[0-9]|brcm|mt76|cfg80211|firmware|wlan|wifi|pcie.*error|aspm`)
	for _, l := range strings.Split(kern, "\n") {
		if re.MatchString(l) {
			keep = append(keep, l)
		}
	}
	if len(keep) > 80 {
		keep = keep[len(keep)-80:]
	}
	fmt.Fprintf(&b, "== Kernel messages about the card (last %d) ==\n%s\n", len(keep), strings.Join(keep, "\n"))
	return b.String(), nil
}

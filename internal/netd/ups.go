package netd

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// UPS monitoring through NUT (Network UPS Tools), the standard way to read a UPS on Linux. A UPS on
// a USB cable is found and read by NUT's own driver; a UPS on the network (another machine running
// NUT, or a card in the UPS) is read with upsc. NopeWarden only reads the UPS and decides for itself
// what to do, so NUT's own shutdown logic is never left running beside it.

// UPSSetup says where the UPS is.
type UPSSetup struct {
	Source string `json:"source"` // usb or network
	Host   string `json:"host"`   // network only
	Name   string `json:"name"`   // network only: the UPS's name on that host, "ups" if empty
}

// UPSStatus is one reading. Numbers the UPS does not report are -1.
type UPSStatus struct {
	Tool       bool    `json:"tool"`        // NUT's reading program is installed
	CanInstall bool    `json:"can_install"` // apt-get is there to install it
	Answering  bool    `json:"answering"`   // the UPS answered this reading
	Error      string  `json:"error,omitempty"`
	Model      string  `json:"model"`
	Maker      string  `json:"maker"`
	Raw        string  `json:"raw"` // ups.status as the UPS gives it, e.g. "OL CHRG"
	OnBattery  bool    `json:"on_battery"`
	LowBattery bool    `json:"low_battery"` // the UPS itself says the battery is low
	Replace    bool    `json:"replace"`     // the UPS says the battery needs replacing
	Charging   bool    `json:"charging"`
	Charge     int     `json:"charge"`  // percent
	Runtime    int     `json:"runtime"` // seconds left on battery
	Load       int     `json:"load"`    // percent
	InputV     float64 `json:"input_v"`
	BatteryV   float64 `json:"battery_v"`
	At         int64   `json:"at"`
}

const (
	nutDir       = "/etc/nut"
	upsKillPower = "/etc/killpower"
)

func (s *Service) nutPath(p string) string {
	if s.Root != "" {
		return filepath.Join(s.Root, p)
	}
	return p
}

// upsTarget is the "name@host" upsc is asked about.
func upsTarget(u UPSSetup) string {
	if u.Source == "network" {
		n := u.Name
		if n == "" {
			n = "ups"
		}
		return n + "@" + u.Host
	}
	return "ups@localhost"
}

// parseUPS turns upsc's "key: value" lines into a reading.
func ParseUPS(out string) UPSStatus {
	v := map[string]string{}
	for _, ln := range strings.Split(out, "\n") {
		if i := strings.Index(ln, ": "); i > 0 {
			v[strings.TrimSpace(ln[:i])] = strings.TrimSpace(ln[i+2:])
		}
	}
	num := func(k string) int {
		f, err := strconv.ParseFloat(v[k], 64)
		if err != nil {
			return -1
		}
		return int(f + 0.5)
	}
	flt := func(k string) float64 {
		f, _ := strconv.ParseFloat(v[k], 64)
		return f
	}
	st := UPSStatus{Raw: v["ups.status"], Charge: num("battery.charge"), Runtime: num("battery.runtime"), Load: num("ups.load"),
		InputV: flt("input.voltage"), BatteryV: flt("battery.voltage"), Maker: v["device.mfr"], Model: v["device.model"], Answering: len(v) > 0 && v["ups.status"] != ""}
	if st.Model == "" {
		st.Model = v["ups.model"]
	}
	if st.Maker == "" {
		st.Maker = v["ups.mfr"]
	}
	for _, f := range strings.Fields(st.Raw) {
		switch f {
		case "OB":
			st.OnBattery = true
		case "LB":
			st.LowBattery = true
		case "RB":
			st.Replace = true
		case "CHRG":
			st.Charging = true
		}
	}
	return st
}

// UPSStatus reads the UPS. A machine with no UPS set up answers "not answering" quickly.
func (s *Service) UPSStatus(u UPSSetup) UPSStatus {
	st := UPSStatus{Charge: -1, Runtime: -1, Load: -1, At: time.Now().Unix()}
	_, err := lookPath("upsc")
	st.Tool = err == nil
	_, aerr := lookPath("apt-get")
	st.CanInstall = aerr == nil
	if !st.Tool {
		return st
	}
	if u.Source == "network" && !validUPSHost(u.Host) {
		st.Error = "Enter the address of the machine the UPS is attached to."
		return st
	}
	out, err := sysRun(8*time.Second, "upsc", upsTarget(u))
	if err != nil {
		st.Error = smartLastLine(out)
		if st.Error == "" {
			st.Error = err.Error()
		}
		return st
	}
	r := ParseUPS(out)
	r.Tool, r.CanInstall, r.At = true, st.CanInstall, st.At
	if !r.Answering {
		r.Error = "The UPS did not report its status."
	}
	return r
}

func validUPSHost(h string) bool {
	if h == "" || len(h) > 253 {
		return false
	}
	for _, c := range h {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '-' || c == ':' || c == '_') {
			return false
		}
	}
	return true
}

// UPSSetupApply installs NUT when it is missing and points it at the UPS. For a USB UPS it runs NUT's
// own driver and a server that only answers this machine; for a network UPS nothing runs here, upsc
// just asks the other machine.
func (s *Service) UPSSetupApply(u UPSSetup) error {
	if u.Source != "usb" && u.Source != "network" {
		return fmt.Errorf("choose USB or network")
	}
	if u.Source == "network" && !validUPSHost(u.Host) {
		return fmt.Errorf("enter the address of the machine the UPS is attached to")
	}
	if u.Source == "network" && u.Name != "" && !validUPSHost(u.Name) {
		return fmt.Errorf("the UPS name can only use letters, numbers, dots and dashes")
	}
	if _, err := lookPath("upsc"); err != nil {
		if _, err := lookPath("apt-get"); err != nil {
			return fmt.Errorf("this machine cannot install programs by itself")
		}
		pk := []string{"install", "--no-install-recommends", "nut-client"}
		if u.Source == "usb" {
			pk = append(pk, "nut-server")
		}
		if b, err := aptRun(10*time.Minute, "apt-get", aptArgs(pk...)...); err != nil {
			return fmt.Errorf("the UPS software did not install: %s", smartLastLine(string(b)))
		}
	}
	if u.Source == "network" {
		sysRun(30*time.Second, "systemctl", "disable", "--now", "nut-server.service", "nut-driver.target", "nut-monitor.service")
		return nil
	}
	pass := make([]byte, 12)
	rand.Read(pass)
	files := map[string]string{
		"nut.conf":    "MODE=standalone\n",
		"ups.conf":    "maxretry = 3\n\n[ups]\n\tdriver = usbhid-ups\n\tport = auto\n\tdesc = \"UPS watched by NopeWarden\"\n",
		"upsd.conf":   "LISTEN 127.0.0.1 3493\n",
		"upsd.users":  "[nopewarden]\n\tpassword = " + hex.EncodeToString(pass) + "\n\tupsmon master\n",
		"upsmon.conf": "# NopeWarden decides what to do when the power fails, so upsmon is not run.\n# This file only tells Debian's shutdown step where the \"cut the UPS output\" flag is.\nMONITOR ups@localhost 1 nopewarden " + hex.EncodeToString(pass) + " master\nSHUTDOWNCMD \"/bin/true\"\nPOWERDOWNFLAG " + upsKillPower + "\n",
	}
	if err := os.MkdirAll(s.nutPath(nutDir), 0o755); err != nil {
		return err
	}
	for name, body := range files {
		mode := os.FileMode(0o640)
		if err := os.WriteFile(s.nutPath(filepath.Join(nutDir, name)), []byte(body), mode); err != nil {
			return err
		}
		exec.Command("chgrp", "nut", s.nutPath(filepath.Join(nutDir, name))).Run()
	}
	sysRun(30*time.Second, "systemctl", "enable", "nut-server.service")
	sysRun(30*time.Second, "systemctl", "disable", "--now", "nut-monitor.service")
	if out, err := sysRun(60*time.Second, "systemctl", "restart", "nut-driver-enumerator.service", "nut-server.service"); err != nil {
		return fmt.Errorf("the UPS service did not start: %s", smartLastLine(out))
	}
	return nil
}

// UPSShutdown shuts the machine down cleanly. With cut set, the UPS is also told to switch its own
// output off a little later and to switch back on when the power returns, so the machine starts
// again by itself. That needs the flag file Debian's shutdown step looks for.
func (s *Service) UPSShutdown(cut bool) error {
	if cut {
		if err := os.WriteFile(s.nutPath(upsKillPower), []byte("NUT will cut the power to the UPS output\n"), 0o644); err != nil {
			return err
		}
	} else {
		os.Remove(s.nutPath(upsKillPower))
	}
	return s.Power("poweroff")
}

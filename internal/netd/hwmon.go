package netd

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Temperature and fan chips. The processor's own sensor and the board's monitoring chip each need a
// kernel driver, and a driver only loads when its chip is really there, so trying the usual ones is
// safe: a driver whose chip is absent simply refuses. Those that load are kept for the next start.

var hwDrivers = []string{"coretemp", "k10temp", "nct6775", "w83627ehf", "it87", "f71882fg", "lm75"}

const hwLoadFile = "/etc/modules-load.d/nopewarden-hwmon.conf"

// hwLoadDrivers is a variable so a test can run without touching the machine.
var hwLoadDrivers = func() []string {
	var loaded []string
	for _, d := range hwDrivers {
		if _, err := sysRun(20*time.Second, "modprobe", d); err != nil {
			continue
		}
		if b, err := os.ReadFile("/proc/modules"); err == nil && strings.Contains("\n"+string(b), "\n"+strings.ReplaceAll(d, "-", "_")+" ") {
			loaded = append(loaded, d)
		}
	}
	return loaded
}

// HwLoad loads the sensor drivers this machine has chips for and returns their names.
func (s *Service) HwLoad() []string {
	loaded := hwLoadDrivers()
	if len(loaded) > 0 {
		if err := os.MkdirAll(filepath.Dir(hwLoadFile), 0o755); err == nil {
			os.WriteFile(hwLoadFile, []byte("# Sensor drivers NopeWarden found chips for.\n"+strings.Join(loaded, "\n")+"\n"), 0o644)
		}
	}
	return loaded
}

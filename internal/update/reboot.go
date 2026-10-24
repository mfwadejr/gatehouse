package update

import (
	"os"
	"strings"
)

// Where the running kernel and the installed ones are found; tests point these elsewhere.
var (
	RebootRequiredFile = "/var/run/reboot-required"
	RunningKernelFile  = "/proc/sys/kernel/osrelease"
	ModulesDir         = "/lib/modules"
)

// RebootNeeded is true when the operating system has a newer kernel installed than the one
// running, or something marked a restart as required. Debian does not write the marker file
// itself, so the kernel comparison is the check that matters.
func RebootNeeded() bool {
	if _, err := os.Stat(RebootRequiredFile); err == nil {
		return true
	}
	b, err := os.ReadFile(RunningKernelFile)
	if err != nil {
		return false
	}
	running := strings.TrimSpace(string(b))
	ents, err := os.ReadDir(ModulesDir)
	if err != nil {
		return false
	}
	for _, e := range ents {
		if e.IsDir() && Compare(e.Name(), running) > 0 {
			return true
		}
	}
	return false
}
